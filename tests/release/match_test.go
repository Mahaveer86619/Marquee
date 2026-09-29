package release_test

import (
	"strings"
	"testing"

	"marquee/internal/catalog"
	"marquee/internal/release"
)

func film() release.Target {
	return release.Target{Ref: "tmdb:movie:1", Scope: release.ScopeMovie, Kind: catalog.Movie, Title: "Example Film", Year: 2014}
}

func show(scope release.Scope, season, episode int) release.Target {
	return release.Target{Ref: "tmdb:tv:2", Scope: scope, Kind: catalog.Series, Title: "Example Show", Season: season, Episode: episode}
}

func TestMatchesByScope(t *testing.T) {
	cases := []struct {
		name   string
		target release.Target
		want   bool
	}{
		{"Example.Film.2014.1080p.BluRay.x264-GRP", film(), true},
		{"Example.Film.2015.1080p.WEB-DL.x264-GRP", film(), true}, // within a year
		{"Example.Film.1998.1080p.BluRay.x264-GRP", film(), false},
		{"Another.Film.2014.1080p.BluRay.x264-GRP", film(), false},
		{"The Example Film 2014 720p WEBRip", film(), true}, // leading article ignored
		{"Example.Show.S01E02.1080p.WEB.h264-GRP", show(release.ScopeEpisode, 1, 2), true},
		{"Example.Show.S01E03.1080p.WEB.h264-GRP", show(release.ScopeEpisode, 1, 2), false},
		{"Example.Show.S01E01-E03.720p.WEB", show(release.ScopeEpisode, 1, 2), true}, // multi-episode file covers E02
		{"Example.Show.S02.COMPLETE.1080p.WEB-DL.H.264-GRP", show(release.ScopeSeason, 2, 0), true},
		{"Example.Show.S02E01.1080p.WEB-DL.H.264-GRP", show(release.ScopeSeason, 2, 0), false},
		{"Example Show Season 1-2 Complete 1080p x265", show(release.ScopeSeries, 0, 0), true},
		{"Example.Show.S02.COMPLETE.1080p.WEB-DL", show(release.ScopeSeries, 0, 0), false}, // one season is not the series
		{"Example.Show.S01E02.1080p", film(), false},
	}
	for _, c := range cases {
		if got := release.Matches(release.ParseName(c.name), c.target); got != c.want {
			t.Errorf("Matches(%q, %s) = %v, want %v", c.name, c.target.Scope, got, c.want)
		}
	}
}

func TestParseNameLanguages(t *testing.T) {
	p := release.ParseName("Example Film (2014) 2160p WEB-DL HEVC DDP5.1 [Hindi + English] ESub")
	if len(p.Languages) != 2 || p.Resolution != "2160p" || p.Codec != "hevc" || len(p.Subtitles) == 0 {
		t.Fatalf("parsed = %+v", p)
	}
	if !release.ParseName("Example Show S01 1080p MULTi x264").Multi {
		t.Fatal("MULTi not detected")
	}
}

func TestScoreFavoursBrowserFriendlyAndPreferredLanguage(t *testing.T) {
	prefs := release.Prefs{AudioLanguages: []string{"hin", "eng"}}
	mk := func(name string, seeders int) release.Release {
		r := release.Release{Name: name, Seeders: seeders, SizeBytes: 4 << 30, Parsed: release.ParseName(name)}
		release.Score(&r, film(), prefs)
		return r
	}
	h264 := mk("Example.Film.2014.1080p.WEB-DL.AAC.x264-GRP", 200)
	hevc := mk("Example.Film.2014.1080p.WEB-DL.AAC.x265-GRP", 200)
	hindi := mk("Example Film 2014 1080p WEB-DL AAC x264 [Hindi]", 200)
	cam := mk("Example.Film.2014.HDCAM.x264-GRP", 900)
	dead := mk("Example.Film.2014.1080p.WEB-DL.AAC.x264-GRP", 0)

	if h264.Score <= hevc.Score {
		t.Fatalf("H.264 (%d) should outrank HEVC (%d)", h264.Score, hevc.Score)
	}
	if hindi.Score <= h264.Score {
		t.Fatalf("preferred Hindi audio (%d) should outrank no language info (%d)", hindi.Score, h264.Score)
	}
	if cam.Score >= 0 {
		t.Fatalf("camera copy scored %d, want negative", cam.Score)
	}
	if dead.Score >= h264.Score {
		t.Fatalf("no seeders (%d) should rank below seeded (%d)", dead.Score, h264.Score)
	}
	if len(h264.Reasons) == 0 || !strings.Contains(strings.Join(h264.Reasons, "|"), "H.264") {
		t.Fatalf("reasons = %v", h264.Reasons)
	}
}

func TestFileLanguage(t *testing.T) {
	cases := map[string]string{
		"Example.Film.en.srt":        "en",
		"Example.Film.eng.srt":       "en",
		"Example.Film.Hindi.srt":     "hi",
		"Example Film_spa.srt":       "es",
		"Example.Film.en.forced.srt": "en",
		"Example.Film.srt":           "",
	}
	for name, want := range cases {
		if got := release.FileLanguage(name); got != want {
			t.Errorf("FileLanguage(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseMagnet(t *testing.T) {
	hex := "0123456789abcdef0123456789abcdef01234567"
	r, err := release.ParseMagnet("magnet:?xt=urn:btih:" + strings.ToUpper(hex) + "&dn=Example.Film.2014.1080p")
	if err != nil || r.InfoHash != hex || r.Name != "Example.Film.2014.1080p" {
		t.Fatalf("release=%+v err=%v", r, err)
	}
	if _, err := release.ParseMagnet("https://example.com/file.torrent"); err == nil {
		t.Fatal("expected an error for a non-magnet link")
	}
	if _, err := release.ParseMagnet("magnet:?dn=nothing"); err == nil {
		t.Fatal("expected an error for a magnet without an info hash")
	}
}

func TestTargetFor(t *testing.T) {
	series := catalog.Title{Ref: "tmdb:tv:2", Kind: catalog.Series, Name: "Example Show"}
	tgt, err := release.TargetFor(series, release.ScopeSeason, 2, 0)
	if err != nil || tgt.Ref != "tmdb:tv:2:s02" || tgt.Key() != "tmdb:tv:2:s02|season" {
		t.Fatalf("target=%+v err=%v", tgt, err)
	}
	tgt, err = release.TargetFor(series, release.ScopeEpisode, 1, 3)
	if err != nil || tgt.Ref != "tmdb:tv:2:s01e03" {
		t.Fatalf("target=%+v err=%v", tgt, err)
	}
	if _, err := release.TargetFor(series, release.ScopeMovie, 0, 0); err == nil {
		t.Fatal("a series is not a film")
	}
}
