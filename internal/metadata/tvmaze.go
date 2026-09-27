package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"marquee/internal/catalog"
)

// TVmaze is a client for the TVmaze API. It needs no key and covers series
// only. Data is licensed CC BY-SA 4.0 and must be credited to TVmaze.
type TVmaze struct {
	BaseURL string // default https://api.tvmaze.com
	client  *http.Client
}

// NewTVmaze returns a TVmaze client.
func NewTVmaze() *TVmaze {
	return &TVmaze{BaseURL: "https://api.tvmaze.com", client: newHTTPClient()}
}

func (t *TVmaze) Name() string { return "tvmaze" }

type tvmazeEpisodeBrief struct {
	Season  int    `json:"season"`
	Number  *int   `json:"number"`
	Name    string `json:"name"`
	Airdate string `json:"airdate"`
}

func (e *tvmazeEpisodeBrief) brief() *catalog.EpisodeBrief {
	if e == nil || e.Number == nil {
		return nil
	}
	return &catalog.EpisodeBrief{Season: e.Season, Number: *e.Number, Name: e.Name, AirDate: e.Airdate}
}

type tvmazeShow struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Language     string   `json:"language"`
	Premiered    string   `json:"premiered"`
	Ended        string   `json:"ended"`
	Summary      string   `json:"summary"`
	Status       string   `json:"status"`
	Runtime      int      `json:"runtime"`
	Genres       []string `json:"genres"`
	OfficialSite string   `json:"officialSite"`
	Rating       struct {
		Average float64 `json:"average"`
	} `json:"rating"`
	Network *struct {
		Name    string `json:"name"`
		Country *struct {
			Name string `json:"name"`
		} `json:"country"`
	} `json:"network"`
	WebChannel *struct {
		Name string `json:"name"`
	} `json:"webChannel"`
	Image *struct {
		Medium string `json:"medium"`
	} `json:"image"`
	Externals struct {
		TheTVDB int    `json:"thetvdb"`
		IMDb    string `json:"imdb"`
	} `json:"externals"`
	Embedded struct {
		Seasons []struct {
			Number       int    `json:"number"`
			Name         string `json:"name"`
			EpisodeOrder int    `json:"episodeOrder"`
			PremiereDate string `json:"premiereDate"`
		} `json:"seasons"`
		Cast []struct {
			Person struct {
				Name string `json:"name"`
			} `json:"person"`
		} `json:"cast"`
		PreviousEpisode *tvmazeEpisodeBrief `json:"previousepisode"`
		NextEpisode     *tvmazeEpisodeBrief `json:"nextepisode"`
	} `json:"_embedded"`
}

func (s tvmazeShow) poster() string {
	if s.Image == nil {
		return ""
	}
	return s.Image.Medium
}

func (s tvmazeShow) networks() []string {
	var out []string
	if s.Network != nil && s.Network.Name != "" {
		out = append(out, s.Network.Name)
	}
	if s.WebChannel != nil && s.WebChannel.Name != "" {
		out = append(out, s.WebChannel.Name)
	}
	return out
}

func (s tvmazeShow) countries() []string {
	if s.Network != nil && s.Network.Country != nil && s.Network.Country.Name != "" {
		return []string{s.Network.Country.Name}
	}
	return nil
}

func (s tvmazeShow) cast() []string {
	var out []string
	for _, c := range s.Embedded.Cast {
		if len(out) == maxCast {
			break
		}
		out = append(out, c.Person.Name)
	}
	return out
}

// Search returns series matching the query.
func (t *TVmaze) Search(ctx context.Context, query string) ([]catalog.SearchResult, error) {
	var body []struct {
		Show tvmazeShow `json:"show"`
	}
	if err := getJSON(ctx, t.client, t.BaseURL+"/search/shows?q="+url.QueryEscape(query), nil, &body); err != nil {
		return nil, err
	}
	out := make([]catalog.SearchResult, 0, len(body))
	for _, r := range body {
		out = append(out, catalog.SearchResult{
			Ref:  catalog.Ref{Source: "tvmaze", Type: "show", ID: r.Show.ID}.String(),
			Kind: catalog.Series, Name: r.Show.Name, Year: catalog.YearOf(r.Show.Premiered),
			ReleaseDate: r.Show.Premiered, Rating: r.Show.Rating.Average, Language: r.Show.Language,
			Overview: plainText(r.Show.Summary), PosterURL: r.Show.poster(), Source: t.Name(),
		})
	}
	return out, nil
}

// Title fetches a series with its seasons, cast and last/next episodes.
func (t *TVmaze) Title(ctx context.Context, ref catalog.Ref) (catalog.Title, error) {
	if ref.Type != "show" {
		return catalog.Title{}, fmt.Errorf("tvmaze: unsupported reference type %q", ref.Type)
	}
	var s tvmazeShow
	u := fmt.Sprintf("%s/shows/%d?embed[]=seasons&embed[]=cast&embed[]=previousepisode&embed[]=nextepisode", t.BaseURL, ref.ID)
	if err := getJSON(ctx, t.client, u, nil, &s); err != nil {
		return catalog.Title{}, err
	}
	episodeCount := 0
	for _, se := range s.Embedded.Seasons {
		episodeCount += se.EpisodeOrder
	}
	title := catalog.Title{
		Ref: ref.String(), Kind: catalog.Series, Source: t.Name(), Name: s.Name, Year: catalog.YearOf(s.Premiered),
		Overview: plainText(s.Summary), Status: s.Status, RuntimeMin: s.Runtime, Genres: s.Genres,
		PosterURL: s.poster(), TVmazeID: s.ID, TVDBID: s.Externals.TheTVDB, IMDbID: s.Externals.IMDb,
		Details: catalog.Details{
			Version: catalog.DetailsVersion, ReleaseDate: s.Premiered, EndDate: s.Ended,
			Rating: s.Rating.Average, Language: s.Language, Countries: s.countries(), Networks: s.networks(),
			Cast: s.cast(), SeasonCount: len(s.Embedded.Seasons), EpisodeCount: episodeCount,
			LastEpisode: s.Embedded.PreviousEpisode.brief(), NextEpisode: s.Embedded.NextEpisode.brief(),
			Homepage: s.OfficialSite,
		},
		FetchedAt: time.Now(),
	}
	for _, se := range s.Embedded.Seasons {
		title.Seasons = append(title.Seasons, catalog.Season{
			Number: se.Number, Name: se.Name, AirDate: se.PremiereDate, EpisodeCount: se.EpisodeOrder,
		})
	}
	return title, nil
}

// Episodes fetches the regular episodes of one season (specials are skipped).
func (t *TVmaze) Episodes(ctx context.Context, ref catalog.Ref, season int) ([]catalog.Episode, error) {
	if ref.Type != "show" {
		return nil, fmt.Errorf("tvmaze: unsupported reference type %q", ref.Type)
	}
	var body []struct {
		Season  int    `json:"season"`
		Number  *int   `json:"number"`
		Name    string `json:"name"`
		Airdate string `json:"airdate"`
		Runtime int    `json:"runtime"`
		Summary string `json:"summary"`
		Rating  struct {
			Average float64 `json:"average"`
		} `json:"rating"`
	}
	if err := getJSON(ctx, t.client, fmt.Sprintf("%s/shows/%d/episodes", t.BaseURL, ref.ID), nil, &body); err != nil {
		return nil, err
	}
	var out []catalog.Episode
	for _, e := range body {
		if e.Season != season || e.Number == nil {
			continue
		}
		out = append(out, catalog.Episode{
			Ref: catalog.EpisodeRef(ref.String(), e.Season, *e.Number), Season: e.Season, Number: *e.Number,
			Name: e.Name, Overview: plainText(e.Summary), AirDate: e.Airdate, RuntimeMin: e.Runtime,
			Rating: e.Rating.Average,
		})
	}
	return out, nil
}
