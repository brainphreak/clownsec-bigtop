// Package catalog matches files to the Clownsec catalog (clownsec.com/3ds) for their info and
// artwork. The catalog lists every published moflex with its exact file size, so a file that came
// from the site is recognised exactly; anything else is matched by title and year.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CatalogURL = "https://clownsec.com/3ds/data/catalog.json"
	ArtBase    = "https://clownsec.com/3ds/"
	maxAge     = 24 * time.Hour
)

type Episode struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Filename string `json:"filename"`
}
type Season struct {
	Number   int       `json:"number"`
	Episodes []Episode `json:"episodes"`
}

// Entry is one movie or show as the catalog lists it.
type Entry struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Year        int      `json:"year"`
	Filename    string   `json:"filename"`
	Artwork     string   `json:"artwork"`
	FileSize    int64    `json:"fileSize"`
	Genres      []string `json:"genres"`
	Format      string   `json:"format"` // "native" / "converted" (3D origin)
	Type        string   `json:"type"`   // "movie" / "tvshow"
	Runtime     int      `json:"runtime"`
	Description string   `json:"description"`
	Folder      string   `json:"folder"`
	Seasons     []Season `json:"seasons"`
}

func (e *Entry) ArtURL() string {
	if e.Artwork == "" {
		return ""
	}
	return ArtBase + strings.ReplaceAll(url.PathEscape(e.Artwork), "+", "%2B")
}

// Is3D: everything on the Clownsec site is 3D unless its file name says otherwise.
func (e *Entry) Is3D() bool { return e.Format != "" || strings.Contains(e.Filename, "(3D)") }

// Catalog is the downloaded catalog, indexed for matching.
type Catalog struct {
	mu      sync.RWMutex
	movies  []*Entry
	shows   []*Entry
	bySize  map[int64]*Entry
	byTitle map[string][]*Entry // Norm(title) -> movies
	showBy  map[string]*Entry   // Norm(title) -> show
	loaded  time.Time
	norm    func(string) string
}

func New(norm func(string) string) *Catalog { return &Catalog{norm: norm} }

// Load reads the cached copy in dir, downloading a fresh one when it is a day old (or missing).
// A failed download keeps whatever copy there is.
func (c *Catalog) Load(dir string) error {
	path := filepath.Join(dir, "catalog.json")
	st, err := os.Stat(path)
	if err != nil || time.Since(st.ModTime()) > maxAge {
		if derr := download(CatalogURL, path); derr != nil && err != nil {
			return fmt.Errorf("catalog download: %w", derr)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw struct {
		Movies  []*Entry `json:"movies"`
		TVShows []*Entry `json:"tvShows"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.movies, c.shows = raw.Movies, raw.TVShows
	c.bySize = map[int64]*Entry{}
	c.byTitle = map[string][]*Entry{}
	c.showBy = map[string]*Entry{}
	for _, m := range c.movies {
		if m.FileSize > 0 {
			c.bySize[m.FileSize] = m
		}
		k := c.norm(m.Title)
		c.byTitle[k] = append(c.byTitle[k], m)
	}
	for _, s := range c.shows {
		c.showBy[c.norm(s.Title)] = s
	}
	c.loaded = time.Now()
	return nil
}

func (c *Catalog) Count() (movies, shows int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.movies), len(c.shows)
}

// BySize finds the catalog movie that IS this file (same byte size).
func (c *Catalog) BySize(size int64) *Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bySize[size]
}

// Movie finds a movie by title and year (year 0 = any; a one-year slip is allowed, since
// release years disagree between sources).
func (c *Catalog) Movie(title string, year int) *Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cands := c.byTitle[c.norm(title)]
	var best *Entry
	for _, e := range cands {
		switch {
		case year == 0 && len(cands) == 1:
			return e
		case e.Year == year:
			return e
		case best == nil && year != 0 && (e.Year == year-1 || e.Year == year+1):
			best = e
		}
	}
	return best
}

// Show finds a TV show by title.
func (c *Catalog) Show(title string) *Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.showBy[c.norm(title)]
}

// ByID finds any entry by its catalog id.
func (c *Catalog) ByID(id string) *Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, l := range [][]*Entry{c.movies, c.shows} {
		for _, e := range l {
			if e.ID == id {
				return e
			}
		}
	}
	return nil
}

// Search lists entries whose title contains q (for the "find a match" box in the web page).
func (c *Catalog) Search(q string, max int) []*Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	q = c.norm(q)
	var out []*Entry
	for _, l := range [][]*Entry{c.movies, c.shows} {
		for _, e := range l {
			if q != "" && strings.Contains(c.norm(e.Title), q) {
				out = append(out, e)
				if len(out) >= max {
					return out
				}
			}
		}
	}
	return out
}

// EpisodeTitle is the catalog's title for a show's episode, "" if it doesn't list it.
func (e *Entry) EpisodeTitle(season, ep int) string {
	for _, s := range e.Seasons {
		if s.Number == season {
			for _, x := range s.Episodes {
				if x.Number == ep {
					return x.Title
				}
			}
		}
	}
	return ""
}

// ---- downloads (the site rate-limits bursts: one request at a time, honour Retry-After) ----

var (
	dlMu   sync.Mutex
	dlLast time.Time
	client = &http.Client{Timeout: 60 * time.Second}
)

// Fetch gets a URL politely: requests are spaced out and a 429 is retried after the wait the
// server asks for.
func Fetch(u string) ([]byte, error) {
	dlMu.Lock()
	defer dlMu.Unlock()
	for try := 0; try < 4; try++ {
		if d := 700*time.Millisecond - time.Since(dlLast); d > 0 {
			time.Sleep(d)
		}
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", "ClownsecBigTop")
		resp, err := client.Do(req)
		dlLast = time.Now()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			if wait <= 0 || wait > 60 {
				wait = 5
			}
			time.Sleep(time.Duration(wait) * time.Second)
			continue
		}
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
		}
		return b, nil
	}
	return nil, errors.New(u + ": still rate-limited, try again later")
}

func download(u, dest string) error {
	b, err := Fetch(u)
	if err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
