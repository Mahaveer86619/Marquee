package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"marquee/internal/api"
	"marquee/internal/catalog"
	"marquee/internal/download"
	"marquee/internal/release"
)

type fakeReleases struct{}

func (fakeReleases) Search(context.Context, release.Target, bool, string) (release.Result, error) {
	return release.Result{}, nil
}
func (fakeReleases) Sources() []release.SourceInfo { return nil }
func (fakeReleases) Get(_ context.Context, id string) (release.Release, bool, error) {
	if id != "r1" {
		return release.Release{}, false, nil
	}
	return release.Release{ID: "r1", Name: "Example.Show.S01E01.1080p"}, true, nil
}
func (fakeReleases) AddMagnet(context.Context, release.Target, string) (release.Release, error) {
	return release.Release{}, nil
}
func (fakeReleases) Prefs() release.Prefs { return release.Prefs{} }

type fakeDownloads struct {
	req     download.Request
	actions []string
}

func (f *fakeDownloads) Enqueue(_ context.Context, r download.Request) (download.Download, error) {
	f.req = r
	return download.Download{ID: "d1", Name: r.Release.Name, State: download.Queued}, nil
}

func (f *fakeDownloads) List(context.Context, int) ([]download.View, error) {
	return []download.View{{Download: download.Download{ID: "d1", State: download.Downloading}, Progress: 0.5}}, nil
}

func (f *fakeDownloads) Get(_ context.Context, id string) (download.View, error) {
	if id != "d1" {
		return download.View{}, download.ErrNotFound
	}
	return download.View{Download: download.Download{ID: "d1", State: download.Paused}}, nil
}

func (f *fakeDownloads) Pause(_ context.Context, id string) error {
	f.actions = append(f.actions, "pause "+id)
	return nil
}

func (f *fakeDownloads) Resume(_ context.Context, id string) error {
	f.actions = append(f.actions, "resume "+id)
	return nil
}

func (f *fakeDownloads) Cancel(_ context.Context, id string) error {
	if id == "done" {
		return download.ErrInvalidState
	}
	f.actions = append(f.actions, "cancel "+id)
	return nil
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func downloadsHandler(f *fakeDownloads) http.Handler {
	return api.New(api.Options{Catalog: &fakeCatalog{}, Releases: fakeReleases{}, Downloads: f})
}

func TestCreateDownload(t *testing.T) {
	f := &fakeDownloads{}
	h := downloadsHandler(f)
	rec := post(t, h, "/api/v1/downloads",
		`{"release_id":"r1","ref":"tvmaze:show:82","scope":"episode","season":1,"episode":1,"audio":["en"],"subtitles":["en","fr"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if f.req.Release.ID != "r1" || f.req.TitleRef != "tvmaze:show:82" ||
		f.req.Target.Ref != catalog.EpisodeRef("tvmaze:show:82", 1, 1) || f.req.Target.Scope != release.ScopeEpisode ||
		len(f.req.Subtitles) != 2 {
		t.Fatalf("request = %+v", f.req)
	}
	var d download.Download
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || d.ID != "d1" {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestCreateDownloadErrors(t *testing.T) {
	h := downloadsHandler(&fakeDownloads{})
	cases := []struct {
		body string
		code int
	}{
		{`not json`, http.StatusBadRequest},
		{`{"ref":"tvmaze:show:82","scope":"series"}`, http.StatusBadRequest},                   // no release
		{`{"release_id":"gone","ref":"tvmaze:show:82","scope":"series"}`, http.StatusNotFound}, // expired
		{`{"release_id":"r1","ref":"tvmaze:show:1","scope":"series"}`, http.StatusNotFound},    // unknown title
		{`{"release_id":"r1","ref":"tvmaze:show:82","scope":"movie"}`, http.StatusBadRequest},  // wrong scope
	}
	for _, c := range cases {
		if rec := post(t, h, "/api/v1/downloads", c.body); rec.Code != c.code {
			t.Errorf("%s: status = %d, want %d (%s)", c.body, rec.Code, c.code, rec.Body)
		}
	}
}

func TestListGetAndActions(t *testing.T) {
	f := &fakeDownloads{}
	h := downloadsHandler(f)
	rec := do(t, h, "/api/v1/downloads")
	var list struct {
		Downloads []download.View `json:"downloads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Downloads) != 1 || list.Downloads[0].Progress != 0.5 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "/api/v1/downloads/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing download: status = %d", rec.Code)
	}
	for _, a := range []string{"pause", "resume", "cancel"} {
		if rec := post(t, h, "/api/v1/downloads/d1/"+a, ""); rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", a, rec.Code, rec.Body)
		}
	}
	if strings.Join(f.actions, ",") != "pause d1,resume d1,cancel d1" {
		t.Fatalf("actions = %v", f.actions)
	}
	if rec := post(t, h, "/api/v1/downloads/done/cancel", ""); rec.Code != http.StatusConflict {
		t.Fatalf("invalid state: status = %d", rec.Code)
	}
}

func TestDownloadsUnavailable(t *testing.T) {
	if rec := do(t, api.New(api.Options{}), "/api/v1/downloads"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
