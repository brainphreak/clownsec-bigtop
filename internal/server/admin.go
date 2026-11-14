package server

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/catalog"
	"github.com/brainphreak/clownsec-bigtop/internal/config"
	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/web"
)

func (s *Server) adminRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /{$}", s.page)
	a := func(pattern string, h http.HandlerFunc) { m.HandleFunc(pattern, s.adminOnly(h)) }
	a("GET /admin/api/state", s.adState)
	a("PUT /admin/api/settings", s.adSettings)
	a("POST /admin/api/libraries", s.adAddLibrary)
	a("PUT /admin/api/libraries/{id}", s.adEditLibrary)
	a("DELETE /admin/api/libraries/{id}", s.adDeleteLibrary)
	a("GET /admin/api/browse", s.adBrowse)
	a("POST /admin/api/scan", s.adScan)
	a("GET /admin/api/libraries/{id}/items", s.adItems)
	a("PUT /admin/api/items/{id}", s.adEditItem)
	a("POST /admin/api/items/{id}/poster", s.adItemPoster)
	a("POST /admin/api/items/{id}/match", s.adMatchItem)
	a("PUT /admin/api/shows/{id}", s.adEditShow)
	a("POST /admin/api/shows/{id}/poster", s.adShowPoster)
	a("GET /admin/api/catalog", s.adCatalogSearch)
	// the page shows posters through the same URLs the 3DS uses
}

// adminOnly: the setup page changes things, so by default it answers only on this computer.
func (s *Server) adminOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.Cfg.Snapshot().AllowRemoteAdmin && !isLocal(r) {
			http.Error(w, "Big Top's setup page only works on the computer it runs on.", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func isLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.Snapshot().AllowRemoteAdmin && !isLocal(r) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "Clownsec Big Top is running. Its setup page opens on the computer it runs on.\n")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(web.Index)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func bad(w http.ResponseWriter, msg string) { http.Error(w, msg, http.StatusBadRequest) }

func (s *Server) adState(w http.ResponseWriter, r *http.Request) {
	c := s.Cfg.Snapshot()
	cm, cs := s.Catalog.Count()
	s.Store.RLock()
	libs := []map[string]any{}
	for _, l := range s.Store.DB.Libraries {
		n, miss := 0, 0
		for _, it := range s.Store.DB.Items {
			if it.Lib == l.ID {
				if it.Missing != 0 {
					miss++
				} else {
					n++
				}
			}
		}
		libs = append(libs, map[string]any{"id": l.ID, "name": l.Name, "folders": l.Folders, "videos": n, "missing": miss})
	}
	s.Store.RUnlock()
	writeJSON(w, map[string]any{
		"name": c.Name, "id": c.ID, "port": c.Port, "allowRemoteAdmin": c.AllowRemoteAdmin,
		"version": s.Version, "addresses": lanAddresses(c.Port), "libraries": libs,
		"scan": s.Scanner.Status(), "catalog": map[string]int{"movies": cm, "shows": cs},
	})
}

// the addresses a 3DS can type in (ENTER ADDRESS), if discovery doesn't find this server
func lanAddresses(port int) []string {
	var out []string
	ifs, _ := net.InterfaceAddrs()
	for _, a := range ifs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
			out = append(out, n.IP.String()+":"+strconv.Itoa(port))
		}
	}
	return out
}

func (s *Server) adSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name             *string `json:"name"`
		AllowRemoteAdmin *bool   `json:"allowRemoteAdmin"`
	}
	if err := readJSON(r, &in); err != nil {
		bad(w, err.Error())
		return
	}
	err := s.Cfg.Update(func(c *config.Config) {
		if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
			c.Name = strings.TrimSpace(*in.Name)
		}
		if in.AllowRemoteAdmin != nil {
			c.AllowRemoteAdmin = *in.AllowRemoteAdmin
		}
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type libIn struct {
	Name    string   `json:"name"`
	Folders []string `json:"folders"`
}

func (in *libIn) check() string {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "Give the library a name."
	}
	var f []string
	for _, p := range in.Folders {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			return "Can't open the folder " + p
		}
		f = append(f, filepath.Clean(p))
	}
	if len(f) == 0 {
		return "Add at least one folder."
	}
	in.Folders = f
	return ""
}

func (s *Server) adAddLibrary(w http.ResponseWriter, r *http.Request) {
	var in libIn
	if err := readJSON(r, &in); err != nil {
		bad(w, err.Error())
		return
	}
	if msg := in.check(); msg != "" {
		bad(w, msg)
		return
	}
	s.Store.Lock()
	id := library.ShowID("lib", in.Name+time.Now().String())[:8]
	s.Store.DB.Libraries = append(s.Store.DB.Libraries, &library.Library{ID: id, Name: in.Name, Folders: in.Folders})
	s.Store.Unlock()
	s.Store.Save()
	s.Scanner.Start(false)
	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) adEditLibrary(w http.ResponseWriter, r *http.Request) {
	var in libIn
	if err := readJSON(r, &in); err != nil {
		bad(w, err.Error())
		return
	}
	if msg := in.check(); msg != "" {
		bad(w, msg)
		return
	}
	s.Store.Lock()
	l := s.Store.Lib(r.PathValue("id"))
	if l != nil {
		l.Name, l.Folders = in.Name, in.Folders
	}
	s.Store.Unlock()
	if l == nil {
		http.NotFound(w, r)
		return
	}
	s.Store.Save()
	s.Scanner.Start(false)
	w.WriteHeader(http.StatusNoContent)
}

// Removing a library forgets its videos (the files themselves are never touched).
func (s *Server) adDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.Store.Lock()
	libs := s.Store.DB.Libraries[:0]
	for _, l := range s.Store.DB.Libraries {
		if l.ID != id {
			libs = append(libs, l)
		}
	}
	s.Store.DB.Libraries = libs
	for k, it := range s.Store.DB.Items {
		if it.Lib == id {
			delete(s.Store.DB.Items, k)
		}
	}
	for k, sh := range s.Store.DB.Shows {
		if sh.Lib == id {
			delete(s.Store.DB.Shows, k)
		}
	}
	s.Store.Unlock()
	s.Store.Save()
	w.WriteHeader(http.StatusNoContent)
}

// A folder picker for the page: the folders inside path ("" = the drives / top level).
func (s *Server) adBrowse(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	type D struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	dirs := []D{}
	if p == "" {
		if runtime.GOOS == "windows" {
			for c := 'A'; c <= 'Z'; c++ {
				d := string(c) + `:\`
				if _, err := os.Stat(d); err == nil {
					dirs = append(dirs, D{d, d})
				}
			}
		} else {
			home, _ := os.UserHomeDir()
			dirs = append(dirs, D{"Home", home}, D{"/", "/"})
			if runtime.GOOS == "darwin" {
				dirs = append(dirs, D{"Volumes (drives and shares)", "/Volumes"})
			} else {
				dirs = append(dirs, D{"/media", "/media"}, D{"/mnt", "/mnt"})
			}
		}
		writeJSON(w, map[string]any{"path": "", "parent": "", "dirs": dirs})
		return
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		bad(w, "Can't open "+p)
		return
	}
	videos := 0
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "$") {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, D{n, filepath.Join(p, n)})
		} else if strings.EqualFold(filepath.Ext(n), ".moflex") {
			videos++
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	writeJSON(w, map[string]any{"path": p, "parent": parent, "dirs": dirs, "videos": videos})
}

func (s *Server) adScan(w http.ResponseWriter, r *http.Request) {
	started := s.Scanner.Start(r.URL.Query().Get("refresh") == "1")
	writeJSON(w, map[string]bool{"started": started})
}

// everything in a library, for the page's grid and edit form
func (s *Server) adItems(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.Store.RLock()
	defer s.Store.RUnlock()
	type I struct {
		ID       string       `json:"id"`
		Path     string       `json:"path"`
		Missing  bool         `json:"missing,omitempty"`
		Info     library.Info `json:"info"`
		Detected library.Info `json:"detected"`
		Edit     library.Info `json:"edit"`
		Source   string       `json:"source"`
		Poster   bool         `json:"poster"`
		PosterBy string       `json:"posterBy,omitempty"`
		Super    bool         `json:"super,omitempty"`
		Moflex   bool         `json:"moflex"`
		ShowID   string       `json:"showId,omitempty"`
	}
	type S struct {
		ID       string       `json:"id"`
		Info     library.Info `json:"info"`
		Detected library.Info `json:"detected"`
		Edit     library.Info `json:"edit"`
		Poster   bool         `json:"poster"`
		PosterBy string       `json:"posterBy,omitempty"`
	}
	items, shows := []I{}, []S{}
	for _, it := range s.Store.DB.Items {
		if it.Lib != id {
			continue
		}
		x := I{ID: it.ID, Path: it.Path, Missing: it.Missing != 0, Info: it.Info(), Detected: it.Detected,
			Edit: it.Edit, Source: it.Source, Poster: it.Poster != "", PosterBy: it.PosterBy, Super: it.Super,
			Moflex: library.IsMoflex(it.Path)}
		if it.IsEpisode() {
			x.ShowID = library.ShowID(id, x.Info.Show)
		}
		items = append(items, x)
	}
	for _, sh := range s.Store.DB.Shows {
		if sh.Lib == id {
			shows = append(shows, S{sh.ID, sh.Info(), sh.Detected, sh.Edit, sh.Poster != "", sh.PosterBy})
		}
	}
	writeJSON(w, map[string]any{"items": items, "shows": shows})
}

// The user's edits replace the whole Edit layer: an empty field goes back to the detected value.
func (s *Server) adEditItem(w http.ResponseWriter, r *http.Request) {
	var in library.Info
	if err := readJSON(r, &in); err != nil {
		bad(w, err.Error())
		return
	}
	s.Store.Lock()
	it := s.Store.DB.Items[r.PathValue("id")]
	if it != nil {
		// moflex: 2D or 3D (the file holds its own layout); other formats: 2D, half or full SBS
		switch in.Is3D {
		case "", "2d":
		case "3d":
			if !library.IsMoflex(it.Path) {
				in.Is3D = "hsbs"
			}
		case "hsbs", "fsbs":
			if library.IsMoflex(it.Path) {
				in.Is3D = "3d"
			}
		default:
			in.Is3D = ""
		}
		it.Edit = in
	}
	s.Store.Unlock()
	if it == nil {
		http.NotFound(w, r)
		return
	}
	s.Store.Dirty()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adEditShow(w http.ResponseWriter, r *http.Request) {
	var in library.Info
	if err := readJSON(r, &in); err != nil {
		bad(w, err.Error())
		return
	}
	s.Store.Lock()
	sh := s.Store.DB.Shows[r.PathValue("id")]
	if sh != nil {
		sh.Edit = in
	}
	s.Store.Unlock()
	if sh == nil {
		http.NotFound(w, r)
		return
	}
	s.Store.Dirty()
	w.WriteHeader(http.StatusNoContent)
}

// a poster upload: the image file as the request body
func (s *Server) adItemPoster(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		bad(w, err.Error())
		return
	}
	id := r.PathValue("id")
	s.Store.RLock()
	ok := s.Store.DB.Items[id] != nil
	s.Store.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	fn, err := s.Posters.SaveBytes(id, data)
	if err != nil {
		bad(w, "That isn't a JPEG or PNG image.")
		return
	}
	s.Store.Lock()
	if it := s.Store.DB.Items[id]; it != nil {
		it.Poster, it.PosterBy = fn, "user"
	}
	s.Store.Unlock()
	s.Store.Dirty()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adShowPoster(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		bad(w, err.Error())
		return
	}
	id := r.PathValue("id")
	s.Store.RLock()
	ok := s.Store.DB.Shows[id] != nil
	s.Store.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	fn, err := s.Posters.SaveBytes("show-"+id, data)
	if err != nil {
		bad(w, "That isn't a JPEG or PNG image.")
		return
	}
	s.Store.Lock()
	if sh := s.Store.DB.Shows[id]; sh != nil {
		sh.Poster, sh.PosterBy = fn, "user"
	}
	s.Store.Unlock()
	s.Store.Dirty()
	w.WriteHeader(http.StatusNoContent)
}

// Pick the catalog entry a file is: its info becomes the item's edit layer, its art the poster.
func (s *Server) adMatchItem(w http.ResponseWriter, r *http.Request) {
	e := s.Catalog.ByID(r.URL.Query().Get("catalog"))
	if e == nil {
		bad(w, "No such catalog entry.")
		return
	}
	id := r.PathValue("id")
	var fn string
	if art := e.ArtURL(); art != "" {
		if b, err := catalog.Fetch(art); err == nil {
			fn, _ = s.Posters.SaveBytes(id, b)
		} else {
			log.Printf("match poster: %v", err)
		}
	}
	s.Store.Lock()
	it := s.Store.DB.Items[id]
	if it != nil {
		ed := it.Edit
		ed.Title, ed.Year, ed.Summary = e.Title, e.Year, e.Description
		ed.Genres, ed.Runtime = strings.Join(e.Genres, ", "), e.Runtime
		it.Edit, it.Catalog = ed, e.ID
		if fn != "" {
			it.Poster, it.PosterBy = fn, "user"
		}
	}
	s.Store.Unlock()
	if it == nil {
		http.NotFound(w, r)
		return
	}
	s.Store.Dirty()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adCatalogSearch(w http.ResponseWriter, r *http.Request) {
	type C struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Year  int    `json:"year"`
		Type  string `json:"type"`
		Art   string `json:"art"`
	}
	out := []C{}
	for _, e := range s.Catalog.Search(r.URL.Query().Get("q"), 30) {
		out = append(out, C{e.ID, e.Title, e.Year, e.Type, e.ArtURL()})
	}
	writeJSON(w, out)
}

// a one-line log per request (the console window is the server's activity view)
func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		h.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasSuffix(r.URL.Path, "/poster") {
			log.Printf("%s %s %s (%v)", r.RemoteAddr, r.Method, r.URL.Path, time.Since(t).Round(time.Millisecond))
		}
	})
}
