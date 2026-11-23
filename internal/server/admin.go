package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/catalog"
	"github.com/brainphreak/clownsec-bigtop/internal/config"
	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/media"
	"github.com/brainphreak/clownsec-bigtop/internal/tmdb"
	"github.com/brainphreak/clownsec-bigtop/web"
)

func (s *Server) adminRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /{$}", s.page)
	m.HandleFunc("GET /hls.min.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Write(web.HLS)
	})
	a := func(pattern string, h http.HandlerFunc) { m.HandleFunc(pattern, s.adminOnly(h)) }
	// browsing and watching work from any device on the network, like the 3DS's own API; the rest
	// only on the Big Top computer (or anywhere, with "allow remote admin" on)
	m.HandleFunc("GET /admin/api/state", s.adState)
	a("PUT /admin/api/settings", s.adSettings)
	a("POST /admin/api/libraries", s.adAddLibrary)
	a("PUT /admin/api/libraries/{id}", s.adEditLibrary)
	a("DELETE /admin/api/libraries/{id}", s.adDeleteLibrary)
	a("GET /admin/api/browse", s.adBrowse)
	a("POST /admin/api/scan", s.adScan)
	m.HandleFunc("GET /admin/api/libraries/{id}/items", s.adItems)
	a("PUT /admin/api/items/{id}", s.adEditItem)
	a("POST /admin/api/items/{id}/poster", s.adItemPoster)
	a("POST /admin/api/items/{id}/match", s.adMatchItem)
	a("PUT /admin/api/shows/{id}", s.adEditShow)
	a("POST /admin/api/shows/{id}/poster", s.adShowPoster)
	a("GET /admin/api/catalog", s.adCatalogSearch)
	a("POST /admin/api/update/check", s.adUpdateCheck)
	a("POST /admin/api/update", s.adUpdate)
	a("POST /admin/api/quit", s.adQuit)
	a("POST /admin/api/ffmpeg/download", s.adFFmpegDownload)
	a("POST /admin/api/speedtest", s.adSpeedTest)
	a("GET /admin/api/depth3d", s.adDepth3D)
	a("POST /admin/api/depth3d/install", s.adDepth3DInstall)
	a("POST /admin/api/depth3d/test", s.adDepth3DTest)
	a("POST /admin/api/depth3d/off", s.adDepth3DOff)
	a("POST /admin/api/setup/done", s.adSetupDone)
	// the page shows posters through the same URLs the 3DS uses
}

// adminOnly: the setup page changes things, so by default it answers only on this computer.
// viewer: a device that may browse and watch but not change anything (another device, with
// "allow remote admin" off).
func (s *Server) viewer(r *http.Request) bool {
	return !s.Cfg.Snapshot().AllowRemoteAdmin && !isLocal(r)
}

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
	if s.viewer(r) { // names and counts only: no folders, addresses or settings
		s.Store.RLock()
		libs := []map[string]any{}
		for _, l := range s.Store.DB.Libraries {
			libs = append(libs, map[string]any{"id": l.ID, "name": l.Name, "folders": []string{}})
		}
		s.Store.RUnlock()
		ff := s.Media.Status()
		writeJSON(w, map[string]any{"viewer": true, "name": c.Name, "version": s.Version, "libraries": libs,
			"scan": map[string]any{"running": false}, "update": map[string]any{},
			"ffmpeg": map[string]any{"found": ff.Found}})
		return
	}
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
		"encodeMode": c.Mode(), "useFast": c.UseFast(), "auto3d": c.Auto3D, "version": s.Version, "addresses": lanAddresses(c.Port), "libraries": libs,
		"scan": s.Scanner.Status(), "catalog": map[string]int{"movies": cm, "shows": cs},
		"update": s.Updater.Status(), "ffmpeg": s.Media.Status(), "setupDone": c.SetupDone, "depth3d": c.Depth3D,
		"tmdbOn": !c.TMDBOff, "tmdbOwnKey": c.TMDBKey != "", // the key itself never leaves Big Top
	})
}

func (s *Server) adUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.Updater.Check(); err != nil {
		http.Error(w, "Couldn't check for updates: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, s.Updater.Status())
}

// Update: download, verify, start the new build, exit. The page watches /admin/api/state until
// the new version answers.
func (s *Server) adUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.Updater.Status().Available {
		bad(w, "No update is available.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go s.StartUpdate()
}

// StartUpdate downloads and installs the waiting update (the setup page and the Windows tray menu
// both come here). It returns only if that fails; on success Big Top restarts as the new version.
func (s *Server) StartUpdate() error {
	p, err := s.Updater.Download()
	if err != nil {
		log.Printf("update: %v", err)
		return err
	}
	s.Updater.MarkRestarting()
	log.Printf("update: starting %s", filepath.Base(p))
	if err := s.Restart(p); err != nil {
		log.Printf("update: couldn't start the new version: %v", err)
		return err
	}
	return nil
}

func (s *Server) adQuit(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	go func() { time.Sleep(300 * time.Millisecond); s.Quit() }()
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
		FastMoflex       *bool   `json:"fastMoflex"` // before encodeMode: true = "fast"
		EncodeMode       *string `json:"encodeMode"`
		TMDBOn           *bool   `json:"tmdbOn"`
		TMDBKey          *string `json:"tmdbKey"` // "" = through clownsec.com again
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
		if in.FastMoflex != nil {
			c.EncodeMode = map[bool]string{true: "fast", false: "best"}[*in.FastMoflex]
		}
		if in.EncodeMode != nil && (*in.EncodeMode == "auto" || *in.EncodeMode == "best" || *in.EncodeMode == "fast") {
			c.EncodeMode = *in.EncodeMode
		}
		c.FastMoflex = false // EncodeMode says it now
		if in.TMDBOn != nil {
			c.TMDBOff = !*in.TMDBOn
		}
		if in.TMDBKey != nil {
			c.TMDBKey = strings.TrimSpace(*in.TMDBKey)
		}
	})
	if err == nil {
		c := s.Cfg.Snapshot()
		s.Media.SetFastMoflex(c.UseFast())
		s.Scanner.SetTMDB(!c.TMDBOff, c.TMDBKey)
	}
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

// Speed test: can this computer encode the Old 3DS's live moflex stream faster than it plays?
// The built-in clip (20 s of Big Buck Bunny, 1080p half side-by-side, like a 3D rip) encoded
// in 2D and in 3D (two eyes: about twice the work), timed. The same clip everywhere, so results
// compare between computers.
func (s *Server) adSpeedTest(w http.ResponseWriter, r *http.Request) {
	if s.Media.MoflexEncoder() == "" {
		bad(w, "The moflex encoder isn't available on this computer.")
		return
	}
	path, err := s.Media.SpeedClip()
	if err != nil {
		bad(w, "Couldn't write the test clip: "+err.Error())
		return
	}
	m, err := s.Media.Probe(path)
	if err != nil {
		bad(w, "Couldn't read the test clip: "+err.Error())
		return
	}
	j := media.Job{Path: path, Media: m, Audio: -1, Burn: -1}
	out := map[string]any{"seconds": media.SpeedClipMs / 1000}
	for _, run := range []struct {
		key, layout string
		fast        bool
	}{{"speed2d", "", false}, {"speed3d", "hsbs", false}, {"fast2d", "", true}, {"fast3d", "hsbs", true}} {
		v, err := s.Media.MoflexSpeed(r.Context(), j, run.layout, media.SpeedClipMs, 60*time.Second, run.fast)
		if err != nil {
			bad(w, "The test encode failed: "+err.Error())
			return
		}
		out[run.key] = v
	}
	s.setAuto(out["speed3d"].(float64)) // the same measurement Auto makes
	c := s.Cfg.Snapshot()
	out["mode"], out["useFast"] = c.Mode(), c.UseFast()
	log.Printf("speed test: best quality 2D %.2fx 3D %.2fx, fast 2D %.2fx 3D %.2fx real time",
		out["speed2d"], out["speed3d"], out["fast2d"], out["fast3d"])
	writeJSON(w, out)
}

// setAuto: remember Auto's measurement (best-quality 3D speed) and apply what it decides.
func (s *Server) setAuto(speed3d float64) {
	s.Cfg.Update(func(c *config.Config) { c.Auto3D, c.AutoVer = speed3d, s.Version })
	c := s.Cfg.Snapshot()
	s.Media.SetFastMoflex(c.UseFast())
}

// AutoMeasure: in Auto, measure this computer once per Big Top version (an update can change the
// encoder), after a quiet start: best-quality 3D on the built-in clip, ~20 s of encoding.
func (s *Server) AutoMeasure(wait time.Duration) {
	c := s.Cfg.Snapshot()
	if c.Mode() != "auto" || (c.Auto3D > 0 && c.AutoVer == s.Version) {
		return
	}
	time.Sleep(wait)
	if c = s.Cfg.Snapshot(); s.Media.MoflexEncoder() == "" || c.Mode() != "auto" {
		return
	}
	path, err := s.Media.SpeedClip()
	if err != nil {
		return
	}
	m, err := s.Media.Probe(path)
	if err != nil {
		log.Printf("auto encoding: can't read the test clip: %v", err)
		return
	}
	j := media.Job{Path: path, Media: m, Audio: -1, Burn: -1}
	v, err := s.Media.MoflexSpeed(context.Background(), j, "hsbs", media.SpeedClipMs, 60*time.Second, false)
	if err != nil {
		log.Printf("auto encoding: the test encode failed: %v", err)
		return
	}
	s.setAuto(v)
	log.Printf("auto encoding: 3D at best quality %.2fx real time -> %s", v,
		map[bool]string{true: "Fast", false: "Best quality"}[s.Media.FastMoflex()])
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
		Kind     string       `json:"kind"`  // movie / short / musicvideo (episodes: "episode")
		Added    int64        `json:"added"` // unix time it was first found
		Fmt      string       `json:"fmt"`   // container: MOFLEX, MP4, MKV...
		DurMs    int64        `json:"dur,omitempty"`
		Pos      int64        `json:"pos,omitempty"`     // ?client=: its resume point, ms
		Watched  bool         `json:"watched,omitempty"` // ...and whether it has watched it
		Extra    string       `json:"extra,omitempty"`   // an extra's kind ("interview"...)
		Label    string       `json:"label,omitempty"`   // an extra's name as listed ("Chasing an Angel (Interview)")
		Group    string       `json:"group,omitempty"`   // the film-with-extras group it is in
		Foreign  bool         `json:"foreign,omitempty"` // an extra from another library, for its film here
	}
	// a film with extras: its film's item id ("" = none found; title / year then come from the
	// extras' folder), and its extras in listing order. Here = the group's card belongs to this library.
	type G struct {
		Main   string   `json:"main,omitempty"`
		Title  string   `json:"title,omitempty"`
		Year   int      `json:"year,omitempty"`
		Extras []string `json:"extras"`
		Here   bool     `json:"here"`
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
	prog := s.Store.DB.Progress[clientID(r)] // the browser asking (each keeps its own, like each 3DS)
	members := s.groupMembers()
	groups := map[string]*G{}
	foreign := map[string]bool{} // extras in other libraries whose film is in this one
	for gid, m := range members {
		sortGroup(m)
		mine := false
		for _, it := range m {
			if it.Lib == id {
				mine = true
			}
		}
		if !mine {
			continue
		}
		g := &G{Extras: []string{}}
		if m[0].Extra == "" {
			g.Main, g.Here = m[0].ID, m[0].Lib == id
		} else if e, ok := s.groupEntry(gid, id, append([]*library.Item(nil), m...), ""); ok {
			g.Title, g.Year, g.Here = e.Title, e.Year, true
		}
		for _, it := range m {
			if it.Extra != "" {
				g.Extras = append(g.Extras, it.ID)
				if it.Lib != id && g.Main != "" && g.Here {
					foreign[it.ID] = true
				}
			}
		}
		groups[gid] = g
	}
	for _, it := range s.Store.DB.Items {
		if it.Lib != id && !foreign[it.ID] {
			continue
		}
		x := I{ID: it.ID, Path: it.Path, Missing: it.Missing != 0, Info: it.Info(), Detected: it.Detected,
			Edit: it.Edit, Source: it.Source, Poster: it.Poster != "", PosterBy: it.PosterBy, Super: it.Super,
			Moflex: library.IsMoflex(it.Path), Kind: s.kindOf(it), Added: it.Added, Fmt: fileFormat(it.Path)}
		if it.Media != nil {
			x.DurMs = it.Media.DurMs
		}
		if p := prog[it.ID]; p != nil {
			x.Pos, x.Watched = p.PosMs, p.Watched
		}
		if it.IsEpisode() {
			x.ShowID = library.ShowID(id, x.Info.Show)
			x.Kind = "episode"
		}
		x.Extra, x.Group, x.Foreign = it.Extra, it.Group, it.Lib != id
		if it.Extra != "" {
			x.Label = library.ExtraName(it.Extra, x.Info.Title)
		}
		items = append(items, x)
	}
	for _, sh := range s.Store.DB.Shows {
		if sh.Lib == id {
			shows = append(shows, S{sh.ID, sh.Info(), sh.Detected, sh.Edit, sh.Poster != "", sh.PosterBy})
		}
	}
	if s.viewer(r) { // a viewer sees titles, not where the files are
		for i := range items {
			items[i].Path = ""
		}
	}
	writeJSON(w, map[string]any{"items": items, "shows": shows, "groups": groups})
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
		if in.Kind != "" {
			in.Kind = library.KindOf(in.Kind)
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
	if m := reTmdbPick.FindStringSubmatch(r.URL.Query().Get("catalog")); m != nil {
		s.adMatchTMDB(w, r, m[1] == "tvshow", m[2])
		return
	}
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
		ed.Kind = library.KindOf(e.Type)
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

var reTmdbPick = regexp.MustCompile(`^tmdb:(movie|tvshow):(\d+)$`)

// adMatchTMDB: a file is this TMDB movie (or show): its info becomes the item's edit layer, the poster too.
func (s *Server) adMatchTMDB(w http.ResponseWriter, r *http.Request, tv bool, tid string) {
	c := s.Scanner.TMDB()
	if c == nil {
		bad(w, "TMDB lookups are turned off in Settings.")
		return
	}
	n, _ := strconv.Atoi(tid)
	info, err := c.Details(n, tv)
	if err != nil {
		bad(w, "TMDB: "+err.Error())
		return
	}
	id := r.PathValue("id")
	var fn string
	if info.Poster != "" {
		if b, err := c.Poster(info.Poster); err == nil {
			fn, _ = s.Posters.SaveBytes(id, b)
		}
	}
	s.Store.Lock()
	it := s.Store.DB.Items[id]
	if it != nil {
		ed := it.Edit
		ed.Title, ed.Year, ed.Summary = info.Title, info.Year, info.Overview
		ed.Genres, ed.Runtime = strings.Join(info.Genres, ", "), info.Runtime
		it.Edit, it.TMDB, it.TmdbVer = ed, n, library.TmdbVer
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
	q := r.URL.Query().Get("q")
	out := []C{}
	if tmdb.IMDbID(q) == "" {
		for _, e := range s.Catalog.Search(q, 30) {
			out = append(out, C{e.ID, e.Title, e.Year, e.Type, e.ArtURL()})
		}
	}
	// and TMDB's (the catalog first: its entries carry the 3D art)
	if c := s.Scanner.TMDB(); c != nil && strings.TrimSpace(q) != "" {
		hits, err := c.Lookup(q)
		if err != nil && len(hits) == 0 && !errors.Is(err, tmdb.ErrNotFound) {
			log.Printf("TMDB search: %v", err)
		}
		for _, h := range hits {
			t, art := "movie", ""
			if h.TV {
				t = "tvshow"
			}
			if h.Poster != "" {
				art = "https://image.tmdb.org/t/p/w154" + h.Poster
			}
			out = append(out, C{fmt.Sprintf("tmdb:%s:%d", t, h.ID), h.Title, h.Year, t, art})
		}
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

// FFmpeg for MP4/MKV/AVI: Windows can fetch it from here; the page watches state.ffmpeg.
func (s *Server) adFFmpegDownload(w http.ResponseWriter, r *http.Request) {
	if !s.Media.Status().CanDownload {
		bad(w, "Big Top can't download FFmpeg on this system. "+s.Media.Status().Hint)
		return
	}
	go func() {
		if err := s.Media.Download(); err != nil {
			log.Printf("FFmpeg download: %v", err)
		} else {
			log.Printf("FFmpeg downloaded")
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}
