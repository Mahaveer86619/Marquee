package release

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"marquee/internal/httpx"
)

// InternetArchive finds public-domain and openly licensed films on
// archive.org. Every item has a web-seeded torrent, so downloads work without
// peers. Items without a clear licence are skipped.
type InternetArchive struct {
	BaseURL string // default https://archive.org
	client  *http.Client
}

// NewInternetArchive returns the Internet Archive source.
func NewInternetArchive() *InternetArchive {
	return &InternetArchive{BaseURL: "https://archive.org", client: httpx.NewClient()}
}

func (s *InternetArchive) Name() string { return "internet_archive" }

// Supports: the archive's film collections have almost no episodic television.
func (s *InternetArchive) Supports(t Target) bool { return t.Scope == ScopeMovie }

// maxItems is how many matching items get their file lists fetched.
const maxItems = 4

func (s *InternetArchive) Search(ctx context.Context, t Target) ([]Release, error) {
	q := fmt.Sprintf(`title:(%s) AND mediatype:(movies)`, luceneEscape(t.Title))
	if t.Year > 0 {
		q += fmt.Sprintf(" AND year:[%d TO %d]", t.Year-1, t.Year+1)
	}
	u := s.BaseURL + "/advancedsearch.php?q=" + url.QueryEscape(q) +
		"&fl[]=identifier&fl[]=title&fl[]=year&fl[]=licenseurl&fl[]=downloads&sort[]=downloads+desc&rows=25&output=json"
	var body struct {
		Response struct {
			Docs []struct {
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
				Year       any    `json:"year"`
				LicenseURL string `json:"licenseurl"`
			} `json:"docs"`
		} `json:"response"`
	}
	if err := httpx.GetJSON(ctx, s.client, u, nil, &body); err != nil {
		return nil, err
	}

	type candidate struct {
		id, title, license string
		year               int
	}
	var cands []candidate
	for _, d := range body.Response.Docs {
		if !openLicense(d.LicenseURL) || !titleMatches(d.Title, t.Title) {
			continue
		}
		cands = append(cands, candidate{d.Identifier, d.Title, d.LicenseURL, anyInt(d.Year)})
		if len(cands) == maxItems {
			break
		}
	}

	results := make([]Release, len(cands))
	var wg sync.WaitGroup
	for i, c := range cands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, ok := s.item(ctx, c.id, c.title, c.license, c.year); ok {
				results[i] = r
			}
		}()
	}
	wg.Wait()

	var out []Release
	for _, r := range results {
		if r.Name != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

// item fetches an item's file list and builds its release.
func (s *InternetArchive) item(ctx context.Context, id, title, license string, year int) (Release, bool) {
	var meta struct {
		Files []struct {
			Name   string `json:"name"`
			Format string `json:"format"`
			Size   string `json:"size"`
			Height string `json:"height"`
			Width  string `json:"width"`
		} `json:"files"`
	}
	if err := httpx.GetJSON(ctx, s.client, s.BaseURL+"/metadata/"+url.PathEscape(id), nil, &meta); err != nil {
		return Release{}, false
	}

	var files []File
	torrent := ""
	for _, f := range meta.Files {
		if strings.HasSuffix(f.Name, "_archive.torrent") {
			torrent = f.Name
			continue
		}
		kind := FileKind(f.Name)
		if kind == "other" {
			continue
		}
		size, _ := strconv.ParseInt(f.Size, 10, 64)
		w, _ := strconv.Atoi(f.Width)
		h, _ := strconv.Atoi(f.Height)
		file := File{Path: f.Name, SizeBytes: size, Kind: kind, Width: w, Height: h, Format: f.Format}
		if kind != "video" {
			file.Language = FileLanguage(f.Name)
		}
		files = append(files, file)
	}
	if torrent == "" {
		return Release{}, false // uploader disabled the torrent
	}
	best := bestVideo(files)
	if best == nil {
		return Release{}, false
	}

	p := Parsed{Title: title, Year: year, Resolution: resolutionFor(best.Height), Codec: codecFor(best.Format, best.Path)}
	if strings.EqualFold(best.Format, "h.264") || strings.EqualFold(best.Format, "MPEG4") || strings.HasSuffix(strings.ToLower(best.Path), ".mp4") {
		p.Audio = []string{"AAC"}
	}
	for _, f := range files {
		if f.Kind == "subtitle" && f.Language != "" {
			p.Subtitles = appendUnique(p.Subtitles, f.Language)
		}
	}

	name := title
	if year > 0 {
		name += fmt.Sprintf(" (%d)", year)
	}
	name += " [" + strings.TrimSpace(best.Format+" "+p.Resolution) + "]"
	return Release{
		Source:      s.Name(),
		Name:        name,
		TorrentURL:  s.BaseURL + "/download/" + url.PathEscape(id) + "/" + url.PathEscape(torrent),
		PageURL:     s.BaseURL + "/details/" + url.PathEscape(id),
		License:     license,
		SizeBytes:   best.SizeBytes,
		PrimaryFile: best.Path,
		Seeders:     -1, // web seeded
		Parsed:      p,
		Files:       files,
	}, true
}

// openLicense accepts public-domain and Creative Commons licences.
func openLicense(u string) bool {
	u = strings.ToLower(u)
	return strings.Contains(u, "publicdomain") || strings.Contains(u, "creativecommons.org")
}

// bestVideo prefers browser-friendly H.264/MP4 files, then larger ones.
func bestVideo(files []File) *File {
	var videos []*File
	for i := range files {
		if files[i].Kind == "video" {
			videos = append(videos, &files[i])
		}
	}
	if len(videos) == 0 {
		return nil
	}
	rank := func(f *File) int {
		switch {
		case strings.EqualFold(f.Format, "h.264"), strings.EqualFold(f.Format, "MPEG4"):
			return 2
		case strings.HasSuffix(strings.ToLower(f.Path), ".mp4"):
			return 1
		}
		return 0
	}
	sort.SliceStable(videos, func(i, j int) bool {
		if ri, rj := rank(videos[i]), rank(videos[j]); ri != rj {
			return ri > rj
		}
		return videos[i].SizeBytes > videos[j].SizeBytes
	})
	return videos[0]
}

func resolutionFor(height int) string {
	switch {
	case height >= 2000:
		return "2160p"
	case height >= 1000:
		return "1080p"
	case height >= 700:
		return "720p"
	case height >= 470:
		return "480p"
	case height > 0:
		return "360p"
	}
	return ""
}

func codecFor(format, name string) string {
	f := strings.ToLower(format)
	switch {
	case strings.Contains(f, "h.264"), strings.Contains(f, "mpeg4"), strings.HasSuffix(strings.ToLower(name), ".mp4"):
		return "avc"
	case strings.Contains(f, "ogg"):
		return "theora"
	case strings.Contains(f, "matroska"):
		return ""
	}
	return ""
}

func anyInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}

// luceneEscape escapes characters with meaning in the archive's query syntax.
func luceneEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`+-&|!(){}[]^"~*?:\/`, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
