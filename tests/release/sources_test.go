package release_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"marquee/internal/httpx"
	"marquee/internal/release"
)

func TestMain(m *testing.M) {
	httpx.RetryBaseDelay = time.Millisecond
	os.Exit(m.Run())
}

func archiveServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/advancedsearch.php":
			if !strings.Contains(r.URL.Query().Get("q"), "Example Film") {
				t.Errorf("query = %q", r.URL.Query().Get("q"))
			}
			_, _ = w.Write([]byte(`{"response":{"docs":[
				{"identifier":"example-film-2014","title":"Example Film","year":"2014","licenseurl":"http://creativecommons.org/publicdomain/mark/1.0/"},
				{"identifier":"no-license","title":"Example Film","year":"2014"},
				{"identifier":"other","title":"Other Film","year":"2014","licenseurl":"http://creativecommons.org/publicdomain/mark/1.0/"}]}}`))
		case "/metadata/example-film-2014":
			_, _ = w.Write([]byte(`{"files":[
				{"name":"example-film-2014_archive.torrent","format":"Archive BitTorrent"},
				{"name":"Example Film.mp4","format":"h.264","size":"734003200","height":"720","width":"1280"},
				{"name":"Example Film.ogv","format":"Ogg Video","size":"300000000","height":"480"},
				{"name":"Example Film.en.srt","format":"SubRip","size":"80000"},
				{"name":"Example Film.es.srt","format":"SubRip","size":"81000"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInternetArchiveSearch(t *testing.T) {
	ia := release.NewInternetArchive()
	ia.BaseURL = archiveServer(t).URL

	rs, err := ia.Search(context.Background(), film())
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 {
		t.Fatalf("got %d releases, want 1 (unlicensed and other titles skipped): %+v", len(rs), rs)
	}
	r := rs[0]
	if r.Parsed.Resolution != "720p" || r.Parsed.Codec != "avc" || r.Seeders != -1 || r.SizeBytes != 734003200 {
		t.Fatalf("release = %+v", r)
	}
	if !strings.HasSuffix(r.TorrentURL, "/download/example-film-2014/example-film-2014_archive.torrent") {
		t.Fatalf("torrent URL = %s", r.TorrentURL)
	}
	if subs := r.SubtitleLanguages(); len(subs) != 2 {
		t.Fatalf("subtitle languages = %v, want en and es", subs)
	}
	if ia.Supports(show(release.ScopeEpisode, 1, 1)) {
		t.Fatal("the archive source should not claim episodes")
	}
}

func TestProwlarrSearch(t *testing.T) {
	var gotKey, gotQuery, gotCat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotQuery, gotCat = r.Header.Get("X-Api-Key"), r.URL.Query().Get("query"), r.URL.Query().Get("categories")
		_, _ = w.Write([]byte(`[
			{"title":"Example.Show.S01E02.1080p.WEB.h264-GRP","size":1500000000,"seeders":120,"leechers":8,
			 "infoHash":"ABCDEF0123456789ABCDEF0123456789ABCDEF01","magnetUrl":"magnet:?xt=urn:btih:abc","indexer":"Example Indexer","protocol":"torrent"},
			{"title":"Example.Show.S01E02.1080p.NZB","size":1,"protocol":"usenet"}]`))
	}))
	defer srv.Close()

	p := release.NewProwlarr(srv.URL, "secret-key")
	rs, err := p.Search(context.Background(), show(release.ScopeEpisode, 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "secret-key" || gotQuery != "Example Show S01E02" || gotCat != "5000" {
		t.Fatalf("key=%q query=%q cat=%q", gotKey, gotQuery, gotCat)
	}
	if len(rs) != 1 || rs[0].Seeders != 120 || rs[0].InfoHash != "abcdef0123456789abcdef0123456789abcdef01" || rs[0].Indexer != "Example Indexer" {
		t.Fatalf("releases = %+v", rs)
	}
}
