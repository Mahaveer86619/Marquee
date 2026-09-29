package download_test

import (
	"testing"

	"marquee/internal/catalog"
	"marquee/internal/download"
	"marquee/internal/release"
)

const mb = 1 << 20

func tf(files ...any) []download.TorrentFile {
	var out []download.TorrentFile
	for i := 0; i < len(files); i += 2 {
		out = append(out, download.TorrentFile{Index: i / 2, Path: files[i].(string), Size: int64(files[i+1].(int))})
	}
	return out
}

func selected(files []download.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		if f.Selected {
			out[f.Path] = f.ItemRef
		}
	}
	return out
}

func TestSelectMovieSkipsSampleAndPicksSubtitles(t *testing.T) {
	files := tf(
		"Film (1968)/Film.1968.720p.mp4", 900*mb,
		"Film (1968)/Sample/sample.mp4", 20*mb,
		"Film (1968)/Film.1968.720p.en.srt", 1*mb,
		"Film (1968)/Film.1968.720p.fr.srt", 1*mb,
		"Film (1968)/cover.jpg", 1*mb,
	)
	got := selected(download.SelectFiles(files, release.Target{Scope: release.ScopeMovie}, "tmdb:movie:1", "", []string{"*"}, []string{"en"}))
	want := map[string]string{
		"Film (1968)/Film.1968.720p.mp4":    "tmdb:movie:1",
		"Film (1968)/Film.1968.720p.en.srt": "tmdb:movie:1",
	}
	if len(got) != len(want) {
		t.Fatalf("selected %v, want %v", got, want)
	}
	for p, ref := range want {
		if got[p] != ref {
			t.Fatalf("selected %v, want %v", got, want)
		}
	}
}

func TestSelectMoviePrefersPrimaryFile(t *testing.T) {
	files := tf("a/big.mkv", 2000*mb, "a/small.mp4", 500*mb)
	got := selected(download.SelectFiles(files, release.Target{Scope: release.ScopeMovie}, "tmdb:movie:1", "a/small.mp4", nil, nil))
	if len(got) != 1 || got["a/small.mp4"] == "" {
		t.Fatalf("selected %v, want only the primary file", got)
	}
}

func TestSelectEpisodeFromSeasonPack(t *testing.T) {
	files := tf(
		"Show.S01/Show.S01E01.1080p.mkv", 1000*mb,
		"Show.S01/Show.S01E02.1080p.mkv", 1000*mb,
		"Show.S01/Show.S01E02.1080p.en.srt", 1*mb,
	)
	target := release.Target{Scope: release.ScopeEpisode, Season: 1, Episode: 2}
	got := selected(download.SelectFiles(files, target, "tmdb:tv:9", "", nil, []string{"en"}))
	ref := catalog.EpisodeRef("tmdb:tv:9", 1, 2)
	if len(got) != 2 || got["Show.S01/Show.S01E02.1080p.mkv"] != ref || got["Show.S01/Show.S01E02.1080p.en.srt"] != ref {
		t.Fatalf("selected %v", got)
	}
}

func TestSelectSeasonTakesEpisodesOnly(t *testing.T) {
	files := tf(
		"Show/Season 2/Show - 2x01.mkv", 700*mb,
		"Show/Season 2/Show.S02E02.mkv", 700*mb,
		"Show/Season 2/Extras/Making of.mkv", 400*mb,
		"Show/Season 3/Show.S03E01.mkv", 700*mb,
	)
	target := release.Target{Scope: release.ScopeSeason, Season: 2}
	got := selected(download.SelectFiles(files, target, "tmdb:tv:9", "", nil, nil))
	if got["Show/Season 2/Show.S02E02.mkv"] != catalog.EpisodeRef("tmdb:tv:9", 2, 2) {
		t.Fatalf("S02E02 not selected: %v", got)
	}
	if _, ok := got["Show/Season 2/Extras/Making of.mkv"]; ok {
		t.Fatalf("extras selected: %v", got)
	}
	if _, ok := got["Show/Season 3/Show.S03E01.mkv"]; ok {
		t.Fatalf("other season selected: %v", got)
	}
}
