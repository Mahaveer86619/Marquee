package download_test

import (
	"testing"

	"marquee/internal/catalog"
	"marquee/internal/download"
)

func TestLibraryPathsForFilm(t *testing.T) {
	files := []download.File{
		{Index: 0, Path: "x/Night.1968.MP4", Kind: "video", Selected: true, ItemRef: "tmdb:movie:10331"},
		{Index: 1, Path: "x/Night.1968.en.srt", Kind: "subtitle", Language: "en", Selected: true, ItemRef: "tmdb:movie:10331"},
		{Index: 2, Path: "x/Night.1968.eng.srt", Kind: "subtitle", Language: "en", Selected: true, ItemRef: "tmdb:movie:10331"},
		{Index: 3, Path: "x/cover.jpg", Kind: "other"},
	}
	got := download.LibraryPaths(download.Naming{Kind: catalog.Movie, Title: "Night of the Living Dead", Year: 1968}, files)
	want := map[int]string{
		0: "Movies/Night of the Living Dead (1968)/Night of the Living Dead (1968).mp4",
		1: "Movies/Night of the Living Dead (1968)/Night of the Living Dead (1968).en.srt",
		2: "Movies/Night of the Living Dead (1968)/Night of the Living Dead (1968).en.2.srt",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, p := range want {
		if got[i] != p {
			t.Fatalf("file %d: got %q, want %q", i, got[i], p)
		}
	}
}

func TestLibraryPathsForEpisodes(t *testing.T) {
	ref := catalog.EpisodeRef("tmdb:tv:1", 1, 2)
	files := []download.File{
		{Index: 0, Path: "S01E02.mkv", Kind: "video", Selected: true, ItemRef: ref},
		{Index: 1, Path: "S01E02.srt", Kind: "subtitle", Selected: true, ItemRef: ref},
	}
	n := download.Naming{Kind: catalog.Series, Title: "What: If?", Year: 2021, Episodes: map[[2]int]string{{1, 2}: "The Trial / Part 1"}}
	got := download.LibraryPaths(n, files)
	if want := "TV/What If (2021)/Season 01/What If - S01E02 - The Trial Part 1.mkv"; got[0] != want {
		t.Fatalf("video: got %q, want %q", got[0], want)
	}
	if want := "TV/What If (2021)/Season 01/What If - S01E02 - The Trial Part 1.srt"; got[1] != want {
		t.Fatalf("subtitle: got %q, want %q", got[1], want)
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		`AC/DC: Live?`: "AC DC Live",
		"Trailing...":  "Trailing",
		"CON":          "_CON",
		"  ":           "Untitled",
		"Amélie":       "Amélie",
	}
	for in, want := range cases {
		if got := download.SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}
