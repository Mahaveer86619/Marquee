package download

import (
	"path"
	"slices"
	"strings"

	"marquee/internal/catalog"
	"marquee/internal/release"
)

// TorrentFile is a file listed in a torrent's metadata.
type TorrentFile struct {
	Index int
	Path  string // slash-separated path inside the torrent
	Size  int64
}

// videoInfo is what a video's path says about the episode it holds.
type videoInfo struct {
	stem, dir string
	seasons   []int
	episodes  []int
}

// sampleLimit is the size below which a "sample" video is treated as one.
const sampleLimit = 300 << 20

// SelectFiles decides which files of a torrent to download for the target:
//   - movie: the release's primary file when known, else the largest video;
//   - episode: the video for that season and episode (or the only video);
//   - season: every episode video of that season;
//   - series: every episode video.
//
// Subtitle and external audio files are added when their language was chosen
// and they belong to a selected video. Samples and extras are skipped.
func SelectFiles(files []TorrentFile, t release.Target, titleRef, primary string, audio, subtitles []string) []File {
	out := make([]File, len(files))
	videos := map[int]videoInfo{}

	for i, f := range files {
		kind := release.FileKind(f.Path)
		out[i] = File{Index: f.Index, Path: f.Path, Size: f.Size, Kind: kind}
		if kind == "subtitle" || kind == "audio" {
			out[i].Language = release.FileLanguage(f.Path)
		}
		if kind == "video" && !isExtra(f) {
			p := release.ParseName(path.Base(f.Path))
			seasons, episodes := p.Seasons, p.Episodes
			if len(seasons) == 0 { // season from a folder such as "Season 2" or "S02"
				seasons = release.ParseName(path.Dir(f.Path)).Seasons
			}
			videos[i] = videoInfo{stem: stem(f.Path), dir: path.Dir(f.Path), seasons: seasons, episodes: episodes}
		}
	}

	// Choose the videos.
	choose := func(i int, ref string) {
		out[i].Selected = true
		out[i].ItemRef = ref
	}
	switch t.Scope {
	case release.ScopeMovie:
		best := -1
		for i := range videos {
			if primary != "" && (files[i].Path == primary || path.Base(files[i].Path) == path.Base(primary)) {
				best = i
				break
			}
			if best < 0 || files[i].Size > files[best].Size {
				best = i
			}
		}
		if best >= 0 {
			choose(best, titleRef)
		}
	case release.ScopeEpisode:
		for i, v := range videos {
			if slices.Contains(v.seasons, t.Season) && slices.Contains(v.episodes, t.Episode) {
				choose(i, catalog.EpisodeRef(titleRef, t.Season, t.Episode))
			}
		}
		if !anySelected(out) && len(videos) == 1 { // single-episode torrent without S/E in the file name
			for i := range videos {
				choose(i, catalog.EpisodeRef(titleRef, t.Season, t.Episode))
			}
		}
	case release.ScopeSeason, release.ScopeSeries:
		for i, v := range videos {
			season := t.Season
			if len(v.seasons) > 0 {
				season = v.seasons[0]
			}
			if t.Scope == release.ScopeSeason && len(v.seasons) > 0 && !slices.Contains(v.seasons, t.Season) {
				continue
			}
			if len(v.episodes) == 0 {
				continue // not an episode (extras are already excluded)
			}
			for _, e := range v.episodes[:1] {
				choose(i, catalog.EpisodeRef(titleRef, season, e))
			}
		}
	}

	// Attach subtitles and external audio to the chosen videos.
	for i := range out {
		f := &out[i]
		if f.Kind != "subtitle" && f.Kind != "audio" {
			continue
		}
		wanted := subtitles
		if f.Kind == "audio" {
			wanted = audio
		}
		if len(wanted) == 0 {
			continue
		}
		all := slices.Contains(wanted, "*")
		if !all && f.Language != "" && !slices.Contains(wanted, f.Language) {
			continue
		}
		if ref := owner(files[i].Path, out, videos); ref != "" && (all || f.Language != "" || sameStemAsSelected(files[i].Path, out)) {
			f.Selected, f.ItemRef = true, ref
		}
	}
	return out
}

// owner returns the item ref of the selected video a side file belongs to:
// matching episode numbers, the same file stem, or the only selected video.
func owner(p string, out []File, videos map[int]videoInfo) string {
	pp := release.ParseName(path.Base(p))
	var selected []int
	for i := range out {
		if out[i].Selected && out[i].Kind == "video" {
			selected = append(selected, i)
		}
	}
	for _, i := range selected {
		v := videos[i]
		if len(pp.Episodes) > 0 && slices.Equal(pp.Episodes[:1], v.episodes[:min(1, len(v.episodes))]) &&
			(len(pp.Seasons) == 0 || slices.Contains(v.seasons, pp.Seasons[0])) {
			return out[i].ItemRef
		}
		if strings.HasPrefix(stem(p), v.stem) {
			return out[i].ItemRef
		}
	}
	if len(selected) == 1 {
		return out[selected[0]].ItemRef
	}
	return ""
}

func sameStemAsSelected(p string, out []File) bool {
	for _, f := range out {
		if f.Selected && f.Kind == "video" && strings.HasPrefix(stem(p), stem(f.Path)) {
			return true
		}
	}
	return false
}

func anySelected(files []File) bool {
	return slices.ContainsFunc(files, func(f File) bool { return f.Selected })
}

// stem is the file name without its extension (and language suffix), lowercased.
func stem(p string) string {
	base := strings.ToLower(path.Base(p))
	return strings.TrimSuffix(base, path.Ext(base))
}

// isExtra reports samples, trailers and bonus material.
func isExtra(f TorrentFile) bool {
	p := strings.ToLower(f.Path)
	if strings.Contains(p, "sample") && f.Size < sampleLimit {
		return true
	}
	for _, word := range []string{"/extras/", "/featurettes/", "/behind the scenes/", "/deleted scenes/", "/trailers/", "trailer."} {
		if strings.Contains("/"+p, word) {
			return true
		}
	}
	return false
}
