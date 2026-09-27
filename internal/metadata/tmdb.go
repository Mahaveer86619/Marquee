package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"marquee/internal/catalog"
)

// TMDB is a client for The Movie Database API (v3 endpoints).
// Attribution: "This product uses the TMDB API but is not endorsed or certified by TMDB."
type TMDB struct {
	BaseURL   string // default https://api.themoviedb.org/3
	ImageBase string // default https://image.tmdb.org/t/p/w342
	Language  string // e.g. "en-US"
	token     string
	apiKey    bool // true for the short v3 API key, false for the Read Access Token
	client    *http.Client
}

var v3KeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// NewTMDB returns a TMDB client. It accepts either credential TMDB issues: the
// API Read Access Token (sent as a bearer token) or the 32-character API key
// (sent as the api_key parameter). Surrounding quotes and spaces are ignored.
func NewTMDB(credential, language string) *TMDB {
	cred := strings.Trim(strings.TrimSpace(credential), `"'`)
	return &TMDB{
		BaseURL:   "https://api.themoviedb.org/3",
		ImageBase: "https://image.tmdb.org/t/p/w342",
		Language:  language,
		token:     cred,
		apiKey:    v3KeyPattern.MatchString(cred),
		client:    newHTTPClient(),
	}
}

func (t *TMDB) Name() string { return "tmdb" }

// CredentialFormat describes the kind of credential in use without revealing it.
func (t *TMDB) CredentialFormat() string {
	switch {
	case t.apiKey:
		return "api_key"
	case strings.HasPrefix(t.token, "eyJ") && strings.Count(t.token, ".") == 2:
		return "read_access_token"
	default:
		return "unrecognized"
	}
}

func (t *TMDB) get(ctx context.Context, path string, query url.Values, out any) error {
	if query == nil {
		query = url.Values{}
	}
	if t.Language != "" {
		query.Set("language", t.Language)
	}
	header := http.Header{}
	if t.apiKey {
		query.Set("api_key", t.token)
	} else {
		header.Set("Authorization", "Bearer "+t.token)
	}
	u := t.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return getJSON(ctx, t.client, u, header, out)
}

// KeepWarm sends a small request every interval until ctx ends, so a working
// connection stays open and real requests rarely need a new TLS handshake
// (some networks drop many new connections to TMDB; see F-020). Errors are
// ignored: this is only a keep-alive.
func (t *TMDB) KeepWarm(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		var body struct {
			Images struct {
				SecureBaseURL string `json:"secure_base_url"`
			} `json:"images"`
		}
		warmCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_ = t.get(warmCtx, "/configuration", nil, &body)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Validate checks that TMDB accepts the token.
func (t *TMDB) Validate(ctx context.Context) error {
	var body struct {
		Success bool `json:"success"`
	}
	if err := t.get(ctx, "/authentication", nil, &body); err != nil {
		return err
	}
	if !body.Success {
		return ErrUnauthorized
	}
	return nil
}

func (t *TMDB) poster(path string) string {
	if path == "" {
		return ""
	}
	return t.ImageBase + path
}

// Search returns films and series matching the query (people are skipped).
func (t *TMDB) Search(ctx context.Context, query string) ([]catalog.SearchResult, error) {
	var body struct {
		Results []struct {
			ID               int     `json:"id"`
			MediaType        string  `json:"media_type"`
			Title            string  `json:"title"`
			Name             string  `json:"name"`
			ReleaseDate      string  `json:"release_date"`
			FirstAirDate     string  `json:"first_air_date"`
			Overview         string  `json:"overview"`
			PosterPath       string  `json:"poster_path"`
			VoteAverage      float64 `json:"vote_average"`
			OriginalLanguage string  `json:"original_language"`
		} `json:"results"`
	}
	q := url.Values{"query": {query}, "include_adult": {"false"}, "page": {"1"}}
	if err := t.get(ctx, "/search/multi", q, &body); err != nil {
		return nil, err
	}
	var out []catalog.SearchResult
	for _, r := range body.Results {
		res := catalog.SearchResult{
			Overview: r.Overview, PosterURL: t.poster(r.PosterPath), Source: t.Name(),
			Rating: r.VoteAverage, Language: r.OriginalLanguage,
		}
		switch r.MediaType {
		case "movie":
			res.Ref = catalog.Ref{Source: "tmdb", Type: "movie", ID: r.ID}.String()
			res.Kind, res.Name, res.ReleaseDate = catalog.Movie, r.Title, r.ReleaseDate
		case "tv":
			res.Ref = catalog.Ref{Source: "tmdb", Type: "tv", ID: r.ID}.String()
			res.Kind, res.Name, res.ReleaseDate = catalog.Series, r.Name, r.FirstAirDate
		default:
			continue
		}
		res.Year = catalog.YearOf(res.ReleaseDate)
		out = append(out, res)
	}
	return out, nil
}

type tmdbGenre struct {
	Name string `json:"name"`
}

type tmdbName struct {
	Name string `json:"name"`
}

type tmdbCredits struct {
	Cast []struct {
		Name string `json:"name"`
	} `json:"cast"`
	Crew []struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	} `json:"crew"`
}

type tmdbEpisodeBrief struct {
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
}

func (e *tmdbEpisodeBrief) brief() *catalog.EpisodeBrief {
	if e == nil {
		return nil
	}
	return &catalog.EpisodeBrief{Season: e.SeasonNumber, Number: e.EpisodeNumber, Name: e.Name, AirDate: e.AirDate}
}

// maxCast is how many cast members are kept for display.
const maxCast = 8

func genreNames(gs []tmdbGenre) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.Name)
	}
	return out
}

func names(ns []tmdbName) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Name)
	}
	return out
}

func (c tmdbCredits) cast() []string {
	var out []string
	for _, p := range c.Cast {
		if len(out) == maxCast {
			break
		}
		out = append(out, p.Name)
	}
	return out
}

func (c tmdbCredits) directors() []string {
	var out []string
	for _, p := range c.Crew {
		if p.Job == "Director" {
			out = append(out, p.Name)
		}
	}
	return out
}

// Title fetches a film ("movie") or series ("tv") with credits.
func (t *TMDB) Title(ctx context.Context, ref catalog.Ref) (catalog.Title, error) {
	q := url.Values{"append_to_response": {"external_ids,credits"}}
	switch ref.Type {
	case "movie":
		var m struct {
			ID                  int         `json:"id"`
			Title               string      `json:"title"`
			OriginalTitle       string      `json:"original_title"`
			Tagline             string      `json:"tagline"`
			ReleaseDate         string      `json:"release_date"`
			Overview            string      `json:"overview"`
			Runtime             int         `json:"runtime"`
			Status              string      `json:"status"`
			Genres              []tmdbGenre `json:"genres"`
			PosterPath          string      `json:"poster_path"`
			IMDbID              string      `json:"imdb_id"`
			VoteAverage         float64     `json:"vote_average"`
			VoteCount           int         `json:"vote_count"`
			OriginalLanguage    string      `json:"original_language"`
			Homepage            string      `json:"homepage"`
			ProductionCountries []tmdbName  `json:"production_countries"`
			Credits             tmdbCredits `json:"credits"`
		}
		if err := t.get(ctx, fmt.Sprintf("/movie/%d", ref.ID), q, &m); err != nil {
			return catalog.Title{}, err
		}
		return catalog.Title{
			Ref: ref.String(), Kind: catalog.Movie, Source: t.Name(), Name: m.Title, OriginalName: m.OriginalTitle,
			Year: catalog.YearOf(m.ReleaseDate), Overview: m.Overview, Status: m.Status, RuntimeMin: m.Runtime,
			Genres: genreNames(m.Genres), PosterURL: t.poster(m.PosterPath), TMDBID: m.ID, IMDbID: m.IMDbID,
			Details: catalog.Details{
				Version: catalog.DetailsVersion, Tagline: m.Tagline, ReleaseDate: m.ReleaseDate,
				Rating: m.VoteAverage, VoteCount: m.VoteCount, Language: m.OriginalLanguage,
				Countries: names(m.ProductionCountries), Creators: m.Credits.directors(),
				Cast: m.Credits.cast(), Homepage: m.Homepage,
			},
			FetchedAt: time.Now(),
		}, nil
	case "tv":
		var s struct {
			ID               int               `json:"id"`
			Name             string            `json:"name"`
			OriginalName     string            `json:"original_name"`
			Tagline          string            `json:"tagline"`
			FirstAirDate     string            `json:"first_air_date"`
			LastAirDate      string            `json:"last_air_date"`
			InProduction     bool              `json:"in_production"`
			Overview         string            `json:"overview"`
			EpisodeRunTime   []int             `json:"episode_run_time"`
			Status           string            `json:"status"`
			Genres           []tmdbGenre       `json:"genres"`
			PosterPath       string            `json:"poster_path"`
			VoteAverage      float64           `json:"vote_average"`
			VoteCount        int               `json:"vote_count"`
			OriginalLanguage string            `json:"original_language"`
			OriginCountry    []string          `json:"origin_country"`
			Networks         []tmdbName        `json:"networks"`
			CreatedBy        []tmdbName        `json:"created_by"`
			NumberOfSeasons  int               `json:"number_of_seasons"`
			NumberOfEpisodes int               `json:"number_of_episodes"`
			LastEpisode      *tmdbEpisodeBrief `json:"last_episode_to_air"`
			NextEpisode      *tmdbEpisodeBrief `json:"next_episode_to_air"`
			Homepage         string            `json:"homepage"`
			Credits          tmdbCredits       `json:"credits"`
			Seasons          []struct {
				SeasonNumber int    `json:"season_number"`
				Name         string `json:"name"`
				AirDate      string `json:"air_date"`
				EpisodeCount int    `json:"episode_count"`
			} `json:"seasons"`
			ExternalIDs struct {
				IMDbID string `json:"imdb_id"`
				TVDBID int    `json:"tvdb_id"`
			} `json:"external_ids"`
		}
		if err := t.get(ctx, fmt.Sprintf("/tv/%d", ref.ID), q, &s); err != nil {
			return catalog.Title{}, err
		}
		title := catalog.Title{
			Ref: ref.String(), Kind: catalog.Series, Source: t.Name(), Name: s.Name, OriginalName: s.OriginalName,
			Year: catalog.YearOf(s.FirstAirDate), Overview: s.Overview, Status: s.Status,
			Genres: genreNames(s.Genres), PosterURL: t.poster(s.PosterPath), TMDBID: s.ID,
			IMDbID: s.ExternalIDs.IMDbID, TVDBID: s.ExternalIDs.TVDBID,
			Details: catalog.Details{
				Version: catalog.DetailsVersion, Tagline: s.Tagline, ReleaseDate: s.FirstAirDate,
				Rating: s.VoteAverage, VoteCount: s.VoteCount, Language: s.OriginalLanguage,
				Countries: s.OriginCountry, Networks: names(s.Networks), Creators: names(s.CreatedBy),
				Cast: s.Credits.cast(), SeasonCount: s.NumberOfSeasons, EpisodeCount: s.NumberOfEpisodes,
				LastEpisode: s.LastEpisode.brief(), NextEpisode: s.NextEpisode.brief(), Homepage: s.Homepage,
			},
			FetchedAt: time.Now(),
		}
		if !s.InProduction {
			title.Details.EndDate = s.LastAirDate
		}
		if len(s.EpisodeRunTime) > 0 {
			title.RuntimeMin = s.EpisodeRunTime[0]
		}
		for _, se := range s.Seasons {
			title.Seasons = append(title.Seasons, catalog.Season{
				Number: se.SeasonNumber, Name: se.Name, AirDate: se.AirDate, EpisodeCount: se.EpisodeCount,
			})
		}
		return title, nil
	}
	return catalog.Title{}, fmt.Errorf("tmdb: unsupported reference type %q", ref.Type)
}

// Episodes fetches the episodes of one season of a series.
func (t *TMDB) Episodes(ctx context.Context, ref catalog.Ref, season int) ([]catalog.Episode, error) {
	if ref.Type != "tv" {
		return nil, fmt.Errorf("tmdb: %s is not a series", ref)
	}
	var body struct {
		Episodes []struct {
			SeasonNumber  int     `json:"season_number"`
			EpisodeNumber int     `json:"episode_number"`
			Name          string  `json:"name"`
			Overview      string  `json:"overview"`
			AirDate       string  `json:"air_date"`
			Runtime       int     `json:"runtime"`
			VoteAverage   float64 `json:"vote_average"`
		} `json:"episodes"`
	}
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", ref.ID, season), nil, &body); err != nil {
		return nil, err
	}
	out := make([]catalog.Episode, 0, len(body.Episodes))
	for _, e := range body.Episodes {
		out = append(out, catalog.Episode{
			Ref: catalog.EpisodeRef(ref.String(), e.SeasonNumber, e.EpisodeNumber), Season: e.SeasonNumber,
			Number: e.EpisodeNumber, Name: e.Name, Overview: e.Overview, AirDate: e.AirDate,
			RuntimeMin: e.Runtime, Rating: e.VoteAverage,
		})
	}
	return out, nil
}
