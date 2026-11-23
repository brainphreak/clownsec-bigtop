// Package tmdb fills in movie and TV info (title, year, summary, genres, runtime, poster) for
// videos Big Top can't identify otherwise, from The Movie Database. By default it asks through
// clownsec.com (which holds the TMDB key, so Big Top needs none); with a key of the user's own it
// asks TMDB directly.
//
// This product uses the TMDB API but is not endorsed or certified by TMDB.
package tmdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Proxy is clownsec.com's lookup (see its tmdb.php for what it answers).
const Proxy = "https://clownsec.com/bigtop/tmdb.php"

// ImageBase: the posters, straight from TMDB (no key needed).
const ImageBase = "https://image.tmdb.org/t/p/w342"

// Info is what a lookup found.
type Info struct {
	ID       int
	TV       bool
	Title    string
	Year     int
	Overview string
	Genres   []string
	Runtime  int // minutes (movies)
	Poster   string
}

type Client struct {
	key  string // the user's own (v3 API key or v4 read-access token); "" = through clownsec.com
	http *http.Client

	mu   sync.Mutex
	last time.Time // lookups are paced (clownsec.com allows 240 a minute per address)
}

func New(key string) *Client {
	return &Client{key: strings.TrimSpace(key), http: &http.Client{Timeout: 20 * time.Second}}
}

// ErrNotFound: TMDB has nothing for it.
var ErrNotFound = errors.New("not found on TMDB")

const gap = 300 * time.Millisecond

func (c *Client) pace() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w := gap - time.Since(c.last); w > 0 {
		time.Sleep(w)
	}
	c.last = time.Now()
}

// get runs one lookup: q is the proxy's lookup name, a its arguments.
func (c *Client) get(q string, a url.Values, out any) error {
	c.pace()
	var req *http.Request
	if c.key == "" {
		v := url.Values{"q": {q}}
		for k, x := range a {
			v[k] = x
		}
		req, _ = http.NewRequest("GET", Proxy+"?"+v.Encode(), nil)
	} else { // the user's own key: TMDB itself
		path := ""
		v := url.Values{"language": {"en-US"}}
		switch q {
		case "find":
			path, v["external_source"] = "/3/find/"+a.Get("imdb"), []string{"imdb_id"}
		case "search_movie", "search_tv":
			path = "/3/search/movie"
			yk := "year"
			if q == "search_tv" {
				path, yk = "/3/search/tv", "first_air_date_year"
			}
			v.Set("query", a.Get("query"))
			v.Set("include_adult", "false")
			if y := a.Get("year"); y != "" {
				v.Set(yk, y)
			}
		case "movie", "tv":
			path = "/3/" + q + "/" + a.Get("id")
		case "tv_season":
			path = "/3/tv/" + a.Get("id") + "/season/" + a.Get("season")
		default:
			return fmt.Errorf("unknown lookup %q", q)
		}
		if !strings.HasPrefix(c.key, "eyJ") { // a v3 API key goes in the query; a v4 token is a header
			v.Set("api_key", c.key)
		}
		req, _ = http.NewRequest("GET", "https://api.themoviedb.org"+path+"?"+v.Encode(), nil)
		if strings.HasPrefix(c.key, "eyJ") {
			req.Header.Set("Authorization", "Bearer "+c.key)
		}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clownsec-bigtop")
	r, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	if r.StatusCode == 404 {
		return ErrNotFound
	}
	if r.StatusCode != 200 {
		var e struct {
			Error         string `json:"error"`
			StatusMessage string `json:"status_message"`
		}
		json.Unmarshal(b, &e)
		return fmt.Errorf("TMDB lookup: HTTP %d %s%s", r.StatusCode, e.Error, e.StatusMessage)
	}
	var nf struct {
		NotFound bool `json:"not_found"`
	}
	if json.Unmarshal(b, &nf) == nil && nf.NotFound {
		return ErrNotFound
	}
	return json.Unmarshal(b, out)
}

var reIMDb = regexp.MustCompile(`\btt\d{7,9}\b`)

// IMDbID: an IMDb id in a path ("Angel (1983) {imdb-tt0086896}"), "" = none.
func IMDbID(path string) string { return reIMDb.FindString(path) }

type result struct {
	ID         int     `json:"id"`
	Title      string  `json:"title"`
	Name       string  `json:"name"`
	Release    string  `json:"release_date"`
	FirstAir   string  `json:"first_air_date"`
	Popularity float64 `json:"popularity"`
}

func yearOf(d string) int {
	if len(d) >= 4 {
		y, _ := strconv.Atoi(d[:4])
		return y
	}
	return 0
}

// FindIMDb: the movie (or show) with this IMDb id.
func (c *Client) FindIMDb(imdb string) (id int, tv bool, err error) {
	var r struct {
		Movies []result `json:"movie_results"`
		TV     []result `json:"tv_results"`
	}
	if err := c.get("find", url.Values{"imdb": {imdb}}, &r); err != nil {
		return 0, false, err
	}
	if len(r.Movies) > 0 {
		return r.Movies[0].ID, false, nil
	}
	if len(r.TV) > 0 {
		return r.TV[0].ID, true, nil
	}
	return 0, false, ErrNotFound
}

// Search: the movie (tv: show) best matching a title and year (0 = any). A result must be the same
// title (folded by norm) or, with a year, from that year or one either side -- not just "something".
func (c *Client) Search(title string, year int, tv bool, norm func(string) string) (int, error) {
	q := "search_movie"
	if tv {
		q = "search_tv"
	}
	a := url.Values{"query": {title}}
	if year > 0 {
		a.Set("year", strconv.Itoa(year))
	}
	var r struct {
		Results []result `json:"results"`
	}
	if err := c.get(q, a, &r); err != nil {
		return 0, err
	}
	if len(r.Results) == 0 && year > 0 { // the year a file says can be off: once more without it
		a.Del("year")
		if err := c.get(q, a, &r); err != nil {
			return 0, err
		}
	}
	want := norm(title)
	best, bestScore := 0, -1
	for i, x := range r.Results {
		name, d := x.Title, x.Release
		if tv {
			name, d = x.Name, x.FirstAir
		}
		score := 0
		if norm(name) == want {
			score += 4
		}
		if y := yearOf(d); year > 0 && y > 0 {
			switch dy := y - year; {
			case dy == 0:
				score += 3
			case dy >= -1 && dy <= 1:
				score += 1
			default:
				score -= 3
			}
		}
		if i == 0 {
			score++ // TMDB's own order (popularity) breaks ties
		}
		if score > bestScore {
			best, bestScore = x.ID, score
		}
	}
	if bestScore < 3 { // neither the same title nor the year: rather nothing than a wrong film
		return 0, ErrNotFound
	}
	return best, nil
}

// Details: a movie's (tv: a show's) info.
func (c *Client) Details(id int, tv bool) (Info, error) {
	q := "movie"
	if tv {
		q = "tv"
	}
	var r struct {
		Title    string `json:"title"`
		Name     string `json:"name"`
		Release  string `json:"release_date"`
		FirstAir string `json:"first_air_date"`
		Overview string `json:"overview"`
		Runtime  int    `json:"runtime"`
		Genres   []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Poster string `json:"poster_path"`
	}
	if err := c.get(q, url.Values{"id": {strconv.Itoa(id)}}, &r); err != nil {
		return Info{}, err
	}
	in := Info{ID: id, TV: tv, Title: r.Title, Year: yearOf(r.Release), Overview: r.Overview,
		Runtime: r.Runtime, Poster: r.Poster}
	if tv {
		in.Title, in.Year = r.Name, yearOf(r.FirstAir)
	}
	for _, g := range r.Genres {
		in.Genres = append(in.Genres, g.Name)
	}
	return in, nil
}

// Poster: the poster image (JPEG) for a poster path from Details.
func (c *Client) Poster(path string) ([]byte, error) {
	if path == "" {
		return nil, ErrNotFound
	}
	r, err := c.http.Get(ImageBase + path)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("TMDB poster: HTTP %d", r.StatusCode)
	}
	return io.ReadAll(io.LimitReader(r.Body, 8<<20))
}
