package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"marquee/internal/catalog"
	"marquee/internal/release"
)

// Naming holds what library paths are built from.
type Naming struct {
	Kind     catalog.Kind
	Title    string
	Year     int
	Episodes map[[2]int]string // episode names by (season, number)
}

// LibraryPaths returns the library path (slash-separated, relative to the
// library folder) of every selected file:
//
//	Movies/Title (Year)/Title (Year).mkv
//	Movies/Title (Year)/Title (Year).en.srt
//	TV/Show (Year)/Season 01/Show - S01E02 - Name.mkv
//	TV/Show (Year)/Season 01/Show - S01E02 - Name.en.srt
//
// Side files take their video's name plus a language suffix; repeats get a
// number (".en.2.srt").
func LibraryPaths(n Naming, files []File) map[int]string {
	folder := SafeName(n.Title)
	if n.Year > 0 {
		folder += fmt.Sprintf(" (%d)", n.Year)
	}
	base := func(ref string) (dir, name string) {
		if s, e, ok := episodeOf(ref); ok && n.Kind == catalog.Series {
			name = fmt.Sprintf("%s - S%02dE%02d", SafeName(n.Title), s, e)
			if title := n.Episodes[[2]int{s, e}]; title != "" {
				name += " - " + SafeName(title)
			}
			return path.Join("TV", folder, fmt.Sprintf("Season %02d", s)), name
		}
		if n.Kind == catalog.Series {
			return path.Join("TV", folder), folder
		}
		return path.Join("Movies", folder), folder
	}

	out := map[int]string{}
	used := map[string]bool{}
	videos := map[string]int{} // item ref -> number of videos placed
	for _, f := range files {
		if !f.Selected || f.Kind != "video" {
			continue
		}
		dir, name := base(f.ItemRef)
		if videos[f.ItemRef]++; videos[f.ItemRef] > 1 {
			name += fmt.Sprintf(" - part%d", videos[f.ItemRef])
		}
		p := path.Join(dir, name+strings.ToLower(path.Ext(f.Path)))
		out[f.Index], used[p] = p, true
	}
	for _, f := range files {
		if !f.Selected || f.Kind == "video" {
			continue
		}
		dir, name := base(f.ItemRef)
		ext := strings.ToLower(path.Ext(f.Path))
		suffix := ""
		if f.Language != "" {
			suffix = "." + f.Language
		}
		p := path.Join(dir, name+suffix+ext)
		for i := 2; used[p]; i++ {
			p = path.Join(dir, name+suffix+"."+strconv.Itoa(i)+ext)
		}
		out[f.Index], used[p] = p, true
	}
	return out
}

var episodeRef = regexp.MustCompile(`:s(\d+)e(\d+)$`)

// episodeOf reads the season and episode from an episode reference.
func episodeOf(ref string) (season, episode int, ok bool) {
	m := episodeRef.FindStringSubmatch(ref)
	if m == nil {
		return 0, 0, false
	}
	s, _ := strconv.Atoi(m[1])
	e, _ := strconv.Atoi(m[2])
	return s, e, true
}

// SafeName makes a string safe as a file or folder name on Windows, macOS and
// Linux (the library is usually a Windows folder).
func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 32:
			return -1
		case strings.ContainsRune(`<>:"/\|?*`, r):
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return "Untitled"
	}
	switch strings.ToUpper(strings.SplitN(s, ".", 2)[0]) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "LPT1", "LPT2", "LPT3":
		s = "_" + s
	}
	return s
}

// naming looks up the title (and episode names) for library paths, falling
// back to the release name when the catalog is unavailable.
func (m *Manager) naming(ctx context.Context, d Download, files []File) Naming {
	n := Naming{Kind: catalog.Movie, Title: d.Release.Parsed.Title, Year: d.Release.Parsed.Year, Episodes: map[[2]int]string{}}
	if d.Scope != release.ScopeMovie {
		n.Kind = catalog.Series
	}
	if n.Title == "" {
		n.Title = d.Name
	}
	if m.opts.Catalog == nil {
		return n
	}
	t, err := m.opts.Catalog.Title(ctx, d.TitleRef)
	if err != nil {
		m.log.Warn("library naming falls back to the release name", "id", d.ID, "err", err)
		return n
	}
	n.Title, n.Year, n.Kind = t.Name, t.Year, t.Kind
	seasons := map[int]bool{}
	for _, f := range files {
		if s, _, ok := episodeOf(f.ItemRef); ok && f.Selected {
			seasons[s] = true
		}
	}
	for s := range seasons {
		eps, err := m.opts.Catalog.Episodes(ctx, d.TitleRef, s)
		if err != nil {
			continue
		}
		for _, e := range eps {
			n.Episodes[[2]int{e.Season, e.Number}] = e.Name
		}
	}
	return n
}

// finalize moves the selected files into the library and records their paths.
// Files already placed in an earlier, interrupted run are skipped.
func (m *Manager) finalize(ctx context.Context, d Download, h Handle, files []File) error {
	if m.opts.LibraryDir == "" {
		return errors.New("no library folder configured")
	}
	targets := LibraryPaths(m.naming(ctx, d, files), files)
	type move struct {
		index    int
		src, rel string
	}
	var moves []move
	for _, f := range files {
		if !f.Selected || f.LibraryPath != "" {
			continue
		}
		src, err := h.LocalPath(f.Index)
		if err != nil {
			return err
		}
		moves = append(moves, move{f.Index, src, targets[f.Index]})
	}
	h.Drop() // release open files before moving them
	for _, mv := range moves {
		if err := ctx.Err(); err != nil {
			return err
		}
		dst := filepath.Join(m.opts.LibraryDir, filepath.FromSlash(mv.rel))
		if err := moveFile(ctx, mv.src, dst); err != nil {
			return err
		}
		if err := m.opts.Store.SetDownloadFileLibraryPath(ctx, d.ID, mv.index, mv.rel); err != nil {
			return err
		}
		m.log.Info("file placed in library", "id", d.ID, "path", mv.rel)
	}
	return nil
}

// moveFile renames src to dst, copying when they are on different volumes
// (the download volume and the library bind mount usually are). An existing
// dst is replaced.
func moveFile(ctx context.Context, src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".partial"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, ctxReader{ctx, in})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	in.Close()
	return os.Remove(src)
}

// ctxReader stops a copy when the context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
