package metadata_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"marquee/internal/catalog"
	"marquee/internal/metadata"
)

// fixtureServer serves canned JSON per path and records the Authorization header.
func fixtureServer(t *testing.T, routes map[string]string) (*httptest.Server, *string) {
	t.Helper()
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &auth
}

func newTMDB(url string) *metadata.TMDB {
	c := metadata.NewTMDB("test-token", "en-US")
	c.BaseURL = url
	return c
}

func TestTMDBSearchSkipsPeopleAndSendsToken(t *testing.T) {
	srv, auth := fixtureServer(t, map[string]string{"/search/multi": `{"results":[
		{"id":1399,"media_type":"tv","name":"Example Series","first_air_date":"2011-04-17","poster_path":"/p.jpg"},
		{"id":7,"media_type":"person","name":"Someone"},
		{"id":603,"media_type":"movie","title":"Example Film","release_date":"1999-03-30"}]}`})

	results, err := newTMDB(srv.URL).Search(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if *auth != "Bearer test-token" {
		t.Fatalf("authorization header = %q", *auth)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (person skipped)", len(results))
	}
	if results[0].Ref != "tmdb:tv:1399" || results[0].Kind != catalog.Series || results[0].Year != 2011 {
		t.Fatalf("series result = %+v", results[0])
	}
	if results[0].PosterURL == "" {
		t.Fatal("poster URL not built")
	}
	if results[1].Ref != "tmdb:movie:603" || results[1].Kind != catalog.Movie || results[1].Year != 1999 {
		t.Fatalf("movie result = %+v", results[1])
	}
}

func TestTMDBSeriesDetailAndEpisodes(t *testing.T) {
	srv, _ := fixtureServer(t, map[string]string{
		"/tv/1399": `{"id":1399,"name":"Example Series","first_air_date":"2011-04-17","last_air_date":"2019-05-19",
			"in_production":false,"episode_run_time":[60],"vote_average":8.4,"vote_count":25000,
			"original_language":"en","origin_country":["US"],"networks":[{"name":"Example Network"}],
			"created_by":[{"name":"Creator One"}],"number_of_seasons":8,"number_of_episodes":73,
			"last_episode_to_air":{"season_number":8,"episode_number":6,"name":"Finale","air_date":"2019-05-19"},
			"credits":{"cast":[{"name":"Actor A"},{"name":"Actor B"}]},
			"genres":[{"name":"Drama"}],"status":"Ended",
			"seasons":[{"season_number":0,"name":"Specials","episode_count":3},{"season_number":1,"name":"Season 1","air_date":"2011-04-17","episode_count":10}],
			"external_ids":{"imdb_id":"tt0944947","tvdb_id":121361}}`,
		"/tv/1399/season/1": `{"episodes":[{"season_number":1,"episode_number":1,"name":"Pilot","air_date":"2011-04-17","runtime":62}]}`,
	})
	c := newTMDB(srv.URL)
	ref := catalog.Ref{Source: "tmdb", Type: "tv", ID: 1399}

	title, err := c.Title(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if title.RuntimeMin != 60 || title.TVDBID != 121361 || len(title.Seasons) != 2 || title.Genres[0] != "Drama" {
		t.Fatalf("title = %+v", title)
	}
	d := title.Details
	if d.Version != catalog.DetailsVersion || d.Rating != 8.4 || d.VoteCount != 25000 || d.EndDate != "2019-05-19" ||
		d.SeasonCount != 8 || d.EpisodeCount != 73 || len(d.Cast) != 2 || d.Creators[0] != "Creator One" ||
		d.Networks[0] != "Example Network" || d.LastEpisode == nil || d.LastEpisode.Name != "Finale" || d.NextEpisode != nil {
		t.Fatalf("details = %+v", d)
	}
	eps, err := c.Episodes(context.Background(), ref, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].Ref != "tmdb:tv:1399:s01e01" || eps[0].RuntimeMin != 62 {
		t.Fatalf("episodes = %+v", eps)
	}
}

func TestTMDBCredentialFormats(t *testing.T) {
	var gotAuth, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotKey = r.Header.Get("Authorization"), r.URL.Query().Get("api_key")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	// The short v3 API key goes in the api_key parameter, with quotes and spaces stripped.
	key := metadata.NewTMDB(`  "0123456789abcdef0123456789abcdef" `, "en-US")
	key.BaseURL = srv.URL
	if err := key.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotKey != "0123456789abcdef0123456789abcdef" || gotAuth != "" || key.CredentialFormat() != "api_key" {
		t.Fatalf("api key mode: key=%q auth=%q format=%s", gotKey, gotAuth, key.CredentialFormat())
	}

	// The Read Access Token goes in the Authorization header.
	tok := metadata.NewTMDB("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.sig", "en-US")
	tok.BaseURL = srv.URL
	if err := tok.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.sig" || gotKey != "" || tok.CredentialFormat() != "read_access_token" {
		t.Fatalf("token mode: auth=%q key=%q format=%s", gotAuth, gotKey, tok.CredentialFormat())
	}
}

func TestTMDBRejectedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if err := newTMDB(srv.URL).Validate(context.Background()); !errors.Is(err, metadata.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestTVmazeSearchStripsHTML(t *testing.T) {
	srv, _ := fixtureServer(t, map[string]string{"/search/shows": `[
		{"score":0.9,"show":{"id":82,"name":"Example Show","premiered":"2011-04-17",
		 "summary":"<p>A <b>great</b> show &amp; more.</p>","image":{"medium":"https://img/82.jpg"}}}]`})
	tv := metadata.NewTVmaze()
	tv.BaseURL = srv.URL

	results, err := tv.Search(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Ref != "tvmaze:show:82" || results[0].Kind != catalog.Series {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Overview != "A great show & more." {
		t.Fatalf("overview = %q", results[0].Overview)
	}
}

func TestTVmazeEpisodesFilterSeasonAndSpecials(t *testing.T) {
	srv, _ := fixtureServer(t, map[string]string{
		"/shows/82": `{"id":82,"name":"Example Show","externals":{"thetvdb":121361,"imdb":"tt0944947"},
			"_embedded":{"seasons":[{"number":1,"episodeOrder":2},{"number":2,"episodeOrder":1}]}}`,
		"/shows/82/episodes": `[
			{"season":1,"number":1,"name":"One","airdate":"2011-04-17"},
			{"season":1,"number":null,"name":"Special"},
			{"season":1,"number":2,"name":"Two"},
			{"season":2,"number":1,"name":"Next season"}]`,
	})
	tv := metadata.NewTVmaze()
	tv.BaseURL = srv.URL
	ref := catalog.Ref{Source: "tvmaze", Type: "show", ID: 82}

	title, err := tv.Title(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(title.Seasons) != 2 || title.TVDBID != 121361 {
		t.Fatalf("title = %+v", title)
	}
	eps, err := tv.Episodes(context.Background(), ref, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 || eps[1].Ref != "tvmaze:show:82:s01e02" {
		t.Fatalf("episodes = %+v", eps)
	}
}
