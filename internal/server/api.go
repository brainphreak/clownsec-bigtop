// Package server is Big Top's HTTP side: the API the 3DS uses (/api), the setup page and its
// API (/, /admin/api), and the video files themselves.
package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/catalog"
	"github.com/brainphreak/clownsec-bigtop/internal/config"
	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/media"
	"github.com/brainphreak/clownsec-bigtop/internal/poster"
	"github.com/brainphreak/clownsec-bigtop/internal/scan"
	"github.com/brainphreak/clownsec-bigtop/internal/update"
)

type Server struct {
	Cfg     *config.Config
	Store   *library.Store
	Scanner *scan.Scanner
	Posters *poster.Store
	Catalog *catalog.Catalog
	Version string
	Updater *update.Updater
	Media   *media.Tool // FFmpeg: MP4/MKV/AVI streaming
	DataDir string
	Quit    func()                     // stop Big Top (the setup page's Quit button)
	Restart func(newPath string) error // start this new build and exit (after an update)
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	// ---- the 3DS ----
	m.HandleFunc("GET /api/server", s.apiServer)
	m.HandleFunc("GET /api/libraries", s.apiLibraries)
	m.HandleFunc("GET /api/libraries/{id}", s.apiLibrary)
	m.HandleFunc("GET /api/shows/{id}", s.apiShow)
	m.HandleFunc("GET /api/shows/{id}/seasons/{n}", s.apiSeason)
	m.HandleFunc("GET /api/shows/{id}/poster", s.apiShowPoster)
	m.HandleFunc("GET /api/items/{id}", s.apiItem)
	m.HandleFunc("GET /api/items/{id}/poster", s.apiItemPoster)
	m.HandleFunc("GET /api/items/{id}/file", s.apiFile)
	m.HandleFunc("GET /api/items/{id}/stream.ts", s.apiStream)
	m.HandleFunc("GET /api/items/{id}/stream.moflex", s.apiStreamMoflex)
	m.HandleFunc("GET /api/items/{id}/subs", s.apiSubs)
	m.HandleFunc("GET /api/items/{id}/subs/{n}", s.apiSubFile)
	m.HandleFunc("POST /api/items/{id}/progress", s.apiProgress)
	m.HandleFunc("POST /api/items/{id}/watched", s.apiWatched)
	// ---- the setup page ----
	s.adminRoutes(m)
	return logRequests(m)
}

// ---- what the 3DS gets ----

// Entry is one row of a list: a movie, a show, or an episode. Kept flat and small -- a 3DS parses it.
type Entry struct {
	Type     string `json:"type"` // "movie", "show", "episode"
	ID       string `json:"id"`
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Genres   string `json:"genres,omitempty"`
	Runtime  int    `json:"runtime,omitempty"`
	Is3D     bool   `json:"is3d"`
	Layout   string `json:"layout,omitempty"` // non-moflex 3D: "hsbs" / "fsbs"
	Poster   bool   `json:"poster"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	Pos      int64  `json:"pos,omitempty"` // resume point, ms
	Watched  bool   `json:"watched,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Super    bool   `json:"super,omitempty"`
	Format   string `json:"fmt,omitempty"`      // the file's container: "MOFLEX", "MP4", "MKV", ...
	Kind     string `json:"kind,omitempty"`     // movies: "movie", "short", "musicvideo"
	Added    int64  `json:"added,omitempty"`    // unix time first found
	Episodes int    `json:"episodes,omitempty"` // shows/seasons: how many
	Seen     int    `json:"seen,omitempty"`     // ...and how many watched
	Audio    string `json:"audio,omitempty"`
	AltAudio string `json:"altAudio,omitempty"`
	Subs     string `json:"subs,omitempty"`
	File     string `json:"file,omitempty"`
	// not moflex: the 3DS plays /stream.ts (H.264 + AAC MPEG-TS, New 3DS) instead of /file
	Stream      bool       `json:"stream,omitempty"`
	DurMs       int64      `json:"dur,omitempty"`
	AudioTracks []TrackOut `json:"audioTracks,omitempty"` // stream.ts?audio=n
	BurnSubs    []TrackOut `json:"burnSubs,omitempty"`    // stream.ts?burn=n (picture subtitles)
	StreamErr   string     `json:"streamErr,omitempty"`   // why it can't be streamed now (shown on the 3DS)
}

func clientID(r *http.Request) string {
	c := r.Header.Get("X-BigTop-Client")
	if c == "" {
		c = r.URL.Query().Get("client")
	}
	if c == "" || len(c) > 64 {
		c = "default"
	}
	return c
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 { // not mid UTF-8 character
		n--
	}
	return s[:n] + "..."
}

// kindOf: a title's kind -- its own (detected or edited), else its catalog entry's (titles matched
// before Big Top recorded kinds), else a movie.
func (s *Server) kindOf(it *library.Item) string {
	if k := it.Info().Kind; k != "" {
		return library.KindOf(k)
	}
	if it.Catalog != "" && s.Catalog != nil {
		if e := s.Catalog.ByID(it.Catalog); e != nil {
			return library.KindOf(e.Type)
		}
	}
	return "movie"
}

// fileFormat: a file's container as shown to people ("MOFLEX", "MP4", "MKV", ...).
func fileFormat(path string) string {
	return strings.ToUpper(strings.TrimPrefix(filepath.Ext(path), "."))
}

// itemEntry: caller holds a read lock.
func (s *Server) itemEntry(it *library.Item, client string, full bool) Entry {
	in := it.Info()
	e := Entry{Type: "movie", ID: it.ID, Title: in.Title, Year: in.Year, Genres: in.Genres,
		Runtime: in.Runtime, Is3D: library.Stereo3D(in.Is3D), Poster: it.Poster != "", Size: it.Size, Super: it.Super,
		Kind: s.kindOf(it), Added: it.Added}
	if in.Is3D == "hsbs" || in.Is3D == "fsbs" {
		e.Layout = in.Is3D
	}
	if it.IsEpisode() {
		e.Type, e.Season, e.Episode = "episode", in.Season, in.Episode
		if e.Title == "" {
			e.Title = "Episode " + strconv.Itoa(in.Episode)
		}
	}
	if e.Title == "" {
		e.Title = strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
	}
	e.Format = fileFormat(it.Path)
	e.Summary = in.Summary
	if !full {
		e.Summary = clip(e.Summary, 400)
	} else {
		e.Audio, e.AltAudio, e.Subs, e.File = it.Audio, it.AltAudio, strings.Join(it.Subs, " "), filepath.Base(it.Path)
	}
	if !library.IsMoflex(it.Path) {
		e.Stream = true
		if full && !s.Media.Available() {
			e.StreamErr = "This video needs FFmpeg on the Big Top computer. Open Big Top's setup page to get it."
		} else if full && it.Media == nil {
			e.StreamErr = "Big Top can't read this video file."
		}
		if m := it.Media; m != nil {
			e.DurMs = m.DurMs
			if full {
				e.AudioTracks = trackList(m.Audio, true)
				e.BurnSubs = trackList(burnable(m), false)
			}
		}
	}
	if p := s.Store.DB.Progress[client][it.ID]; p != nil {
		e.Pos, e.Watched = p.PosMs, p.Watched
	}
	return e
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) apiServer(w http.ResponseWriter, r *http.Request) {
	c := s.Cfg.Snapshot()
	writeJSON(w, map[string]any{"service": "clownsec-bigtop", "name": c.Name, "id": c.ID, "version": s.Version})
}

func (s *Server) apiLibraries(w http.ResponseWriter, r *http.Request) {
	s.Store.RLock()
	defer s.Store.RUnlock()
	type L struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Movies int    `json:"movies"`
		Shows  int    `json:"shows"`
	}
	out := []L{}
	hide := s.shadowed()
	for _, l := range s.Store.DB.Libraries {
		x := L{ID: l.ID, Name: l.Name}
		shows := map[string]bool{}
		for _, it := range s.Store.DB.Items {
			if it.Lib != l.ID || it.Missing != 0 || hide[it.ID] {
				continue
			}
			if it.IsEpisode() {
				shows[library.ShowID(l.ID, it.Info().Show)] = true
			} else {
				x.Movies++
			}
		}
		x.Shows = len(shows)
		out = append(out, x)
	}
	writeJSON(w, out)
}

// sortKey orders titles the way a shelf does: "The Matrix" under M.
func sortKey(t string) string { return library.Norm(t) }

// pageOf applies ?start=&size= (the 3DS asks in pages when a list is big).
func pageOf[T any](r *http.Request, all []T) (int, []T) {
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if start < 0 || start > len(all) {
		start = len(all)
	}
	end := len(all)
	if size > 0 && start+size < end {
		end = start + size
	}
	return len(all), all[start:end]
}

// A library: its movies and its shows, together, by title.
func (s *Server) apiLibrary(w http.ResponseWriter, r *http.Request) {
	id, client := r.PathValue("id"), clientID(r)
	s.Store.RLock()
	defer s.Store.RUnlock()
	if s.Store.Lib(id) == nil {
		http.NotFound(w, r)
		return
	}
	var list []Entry
	shows := map[string]*Entry{}
	hide := s.shadowed()
	for _, it := range s.Store.DB.Items {
		if it.Lib != id || it.Missing != 0 || hide[it.ID] {
			continue
		}
		if !it.IsEpisode() {
			list = append(list, s.itemEntry(it, client, false))
			continue
		}
		sid := library.ShowID(id, it.Info().Show)
		e := shows[sid]
		if e == nil {
			e = s.showEntry(sid, it.Info().Show)
			shows[sid] = e
		}
		e.Episodes++
		if p := s.Store.DB.Progress[client][it.ID]; p != nil && p.Watched {
			e.Seen++
		}
	}
	for _, e := range shows {
		list = append(list, *e)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := sortKey(list[i].Title), sortKey(list[j].Title)
		if a != b {
			return a < b
		}
		return list[i].Year < list[j].Year
	})
	total, page := pageOf(r, list)
	writeJSON(w, map[string]any{"total": total, "items": page})
}

// showEntry: caller holds a read lock.
func (s *Server) showEntry(sid, fallbackTitle string) *Entry {
	e := &Entry{Type: "show", ID: sid, Title: fallbackTitle}
	if sh := s.Store.DB.Shows[sid]; sh != nil {
		in := sh.Info()
		if in.Title != "" {
			e.Title = in.Title
		}
		e.Year, e.Summary, e.Genres, e.Poster = in.Year, clip(in.Summary, 400), in.Genres, sh.Poster != ""
	}
	return e
}

func (s *Server) episodes(sid, client string) []*library.Item {
	var eps []*library.Item
	hide := s.shadowed()
	for _, it := range s.Store.DB.Items {
		if it.Missing == 0 && !hide[it.ID] && it.IsEpisode() && library.ShowID(it.Lib, it.Info().Show) == sid {
			eps = append(eps, it)
		}
	}
	sort.Slice(eps, func(i, j int) bool {
		a, b := eps[i].Info(), eps[j].Info()
		if a.Season != b.Season {
			return a.Season < b.Season
		}
		return a.Episode < b.Episode
	})
	return eps
}

// A show: its seasons.
func (s *Server) apiShow(w http.ResponseWriter, r *http.Request) {
	sid, client := r.PathValue("id"), clientID(r)
	s.Store.RLock()
	defer s.Store.RUnlock()
	eps := s.episodes(sid, client)
	if len(eps) == 0 {
		http.NotFound(w, r)
		return
	}
	show := s.showEntry(sid, eps[0].Info().Show)
	if sh := s.Store.DB.Shows[sid]; sh != nil {
		show.Summary = sh.Info().Summary
	}
	var seasons []Entry
	for _, it := range eps {
		n := it.Info().Season
		if len(seasons) == 0 || seasons[len(seasons)-1].Season != n {
			seasons = append(seasons, Entry{Type: "season", ID: sid + "/" + strconv.Itoa(n),
				Title: "Season " + strconv.Itoa(n), Season: n, Poster: show.Poster, Summary: clip(show.Summary, 400)})
		}
		x := &seasons[len(seasons)-1]
		x.Episodes++
		if p := s.Store.DB.Progress[client][it.ID]; p != nil && p.Watched {
			x.Seen++
		}
	}
	writeJSON(w, map[string]any{"show": show, "seasons": seasons})
}

// A season: its episodes.
func (s *Server) apiSeason(w http.ResponseWriter, r *http.Request) {
	sid, client := r.PathValue("id"), clientID(r)
	n, _ := strconv.Atoi(r.PathValue("n"))
	s.Store.RLock()
	defer s.Store.RUnlock()
	out := []Entry{}
	for _, it := range s.episodes(sid, client) {
		if it.Info().Season == n {
			out = append(out, s.itemEntry(it, client, false))
		}
	}
	writeJSON(w, map[string]any{"total": len(out), "items": out})
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) *library.Item {
	it := s.Store.DB.Items[r.PathValue("id")]
	if it == nil {
		http.NotFound(w, r)
	}
	return it
}

func (s *Server) apiItem(w http.ResponseWriter, r *http.Request) {
	if s.Media.Available() {
		if _, m, _ := s.mediaOf(r.PathValue("id")); m != nil { // tracks for the page; subtitles readied
			go s.prefetchSubs(r.PathValue("id"))
		}
	}
	s.Store.RLock()
	defer s.Store.RUnlock()
	if it := s.item(w, r); it != nil {
		writeJSON(w, s.itemEntry(it, clientID(r), true))
	}
}

// posters: ?w=&h= gives exactly that size (stretched, never cropped)
func (s *Server) sendPoster(w http.ResponseWriter, r *http.Request, fn string) {
	if fn == "" {
		http.NotFound(w, r)
		return
	}
	pw, _ := strconv.Atoi(r.URL.Query().Get("w"))
	ph, _ := strconv.Atoi(r.URL.Query().Get("h"))
	p, err := s.Posters.Sized(fn, pw, ph)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=300")
	http.ServeFile(w, r, p)
}

func (s *Server) apiItemPoster(w http.ResponseWriter, r *http.Request) {
	s.Store.RLock()
	it := s.Store.DB.Items[r.PathValue("id")]
	fn := ""
	if it != nil {
		fn = it.Poster
	}
	s.Store.RUnlock()
	s.sendPoster(w, r, fn)
}

func (s *Server) apiShowPoster(w http.ResponseWriter, r *http.Request) {
	s.Store.RLock()
	sh := s.Store.DB.Shows[r.PathValue("id")]
	fn := ""
	if sh != nil {
		fn = sh.Poster
	}
	s.Store.RUnlock()
	s.sendPoster(w, r, fn)
}

// The video itself, with Range: a seek on the 3DS is a request from that byte on.
func (s *Server) apiFile(w http.ResponseWriter, r *http.Request) {
	s.Store.RLock()
	it := s.Store.DB.Items[r.PathValue("id")]
	path := ""
	if it != nil && it.Missing == 0 {
		path = it.Path
	}
	s.Store.RUnlock()
	if path == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "the file can't be opened", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// Where a console is in a video: ?pos=ms&dur=ms&state=playing|paused|stopped. Within the last
// tenth (or two minutes) of the video, stopping marks it watched and clears the resume point.
func (s *Server) apiProgress(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pos, _ := strconv.ParseInt(q.Get("pos"), 10, 64)
	dur, _ := strconv.ParseInt(q.Get("dur"), 10, 64)
	client := clientID(r)
	s.Store.Lock()
	it := s.Store.DB.Items[r.PathValue("id")]
	if it == nil {
		s.Store.Unlock()
		http.NotFound(w, r)
		return
	}
	if dur <= 0 {
		dur = int64(it.Info().Runtime) * 60000
	}
	m := s.Store.DB.Progress[client]
	if m == nil {
		m = map[string]*library.Progress{}
		s.Store.DB.Progress[client] = m
	}
	p := m[it.ID]
	if p == nil {
		p = &library.Progress{}
		m[it.ID] = p
	}
	p.PosMs, p.Updated = pos, time.Now().Unix()
	if dur > 0 && q.Get("state") == "stopped" && (pos >= dur*9/10 || dur-pos < 120000) {
		p.Watched, p.PosMs = true, 0
	}
	s.Store.Unlock()
	s.Store.Dirty()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) apiWatched(w http.ResponseWriter, r *http.Request) {
	on := r.URL.Query().Get("on") != "0"
	client := clientID(r)
	s.Store.Lock()
	it := s.Store.DB.Items[r.PathValue("id")]
	if it != nil {
		m := s.Store.DB.Progress[client]
		if m == nil {
			m = map[string]*library.Progress{}
			s.Store.DB.Progress[client] = m
		}
		m[it.ID] = &library.Progress{Watched: on, Updated: time.Now().Unix()}
	}
	s.Store.Unlock()
	if it == nil {
		http.NotFound(w, r)
		return
	}
	s.Store.Dirty()
	w.WriteHeader(http.StatusNoContent)
}
