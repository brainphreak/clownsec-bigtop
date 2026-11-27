// Package scan finds the video files in the libraries' folders and works out what each one is.
package scan

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/catalog"
	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/media"
	"github.com/brainphreak/clownsec-bigtop/internal/moflex"
	"github.com/brainphreak/clownsec-bigtop/internal/poster"
	"github.com/brainphreak/clownsec-bigtop/internal/tmdb"
)

// Extensions Big Top lists. Everything but moflex is transcoded by FFmpeg for the 3DS.
var Extensions = map[string]bool{".moflex": true, ".mp4": true, ".m4v": true, ".mkv": true, ".avi": true,
	".mov": true, ".webm": true, ".wmv": true, ".mpg": true, ".mpeg": true, ".ts": true, ".m2ts": true,
	".flv": true, ".ogv": true, ".3gp": true, ".vob": true}

type Status struct {
	Running bool   `json:"running"`
	Library string `json:"library,omitempty"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current,omitempty"`
	Added   int    `json:"added"`
	Last    int64  `json:"last,omitempty"` // unix time the last scan finished
	Error   string `json:"error,omitempty"`
}

type Scanner struct {
	Store   *library.Store
	Catalog *catalog.Catalog
	Posters *poster.Store
	Media   *media.Tool // ffprobe for non-moflex files (nil or missing: probed later, on demand)
	DataDir string

	run    sync.Mutex // one scan at a time
	again  bool       // asked to scan while one ran: once more when it ends (a library changed)
	mu     sync.Mutex
	status Status
	tmdb   *tmdb.Client // nil = TMDB off
}

// SetTMDB: fill in missing info from TMDB (key "" = through clownsec.com), or not.
func (sc *Scanner) SetTMDB(on bool, key string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if !on {
		sc.tmdb = nil
		return
	}
	sc.tmdb = tmdb.New(key)
}

// TMDB: the TMDB client (nil = TMDB lookups are off), for the setup page's searches.
func (sc *Scanner) TMDB() *tmdb.Client { return sc.tmdbClient() }

func (sc *Scanner) tmdbClient() *tmdb.Client {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.tmdb
}

func (sc *Scanner) Status() Status {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.status
}

func (sc *Scanner) set(f func(*Status)) {
	sc.mu.Lock()
	f(&sc.status)
	sc.mu.Unlock()
}

// Start runs a scan in the background unless one is running. refresh = work out every file's
// info again (after a catalog update, or to pick up edits to the files' trailers).
func (sc *Scanner) Start(refresh bool) bool {
	if !sc.run.TryLock() {
		sc.mu.Lock()
		sc.again = true
		sc.mu.Unlock()
		return false
	}
	sc.set(func(s *Status) { *s = Status{Running: true, Last: s.Last} })
	go func() {
		defer sc.run.Unlock()
		for {
			err := sc.scanAll(refresh)
			sc.mu.Lock()
			again := sc.again
			sc.again = false
			sc.mu.Unlock()
			if !again {
				sc.set(func(s *Status) {
					s.Running, s.Current, s.Library, s.Last = false, "", "", time.Now().Unix()
					if err != nil {
						s.Error = err.Error()
					}
				})
				return
			}
			refresh = false
			sc.set(func(s *Status) { *s = Status{Running: true, Last: s.Last} })
		}
	}()
	return true
}

func (sc *Scanner) scanAll(refresh bool) error {
	if err := sc.Catalog.Load(sc.DataDir); err != nil {
		log.Printf("catalog: %v (matching by file name only)", err)
	}
	sc.Store.RLock()
	libs := append([]*library.Library(nil), sc.Store.DB.Libraries...)
	sc.Store.RUnlock()
	for _, lib := range libs {
		sc.scanLib(lib, refresh)
	}
	sc.buildGroups(libs)
	sc.Store.Dirty()
	return nil
}

type found struct {
	path  string
	size  int64
	mtime int64
}

var reSample = regexp.MustCompile(`(?i)(^|[\s._-])sample([\s._-]|$)`)

func walk(folders []string) []found {
	var out []found
	for _, root := range folders {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // an unreadable folder: skip it, keep going
			}
			name := d.Name()
			if d.IsDir() {
				if p != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") ||
					name == "System Volume Information" || name == "@eaDir") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(name, "._") || !Extensions[strings.ToLower(filepath.Ext(name))] {
				return nil
			}
			if reSample.MatchString(name) { // a release's "sample" clip (Plex skips them too)
				if fi, err := d.Info(); err == nil && fi.Size() < 300<<20 {
					return nil
				}
			}
			fi, err := d.Info()
			if err == nil && d.Type()&fs.ModeSymlink != 0 {
				// a link to a video: the file it points to (its own size is just the path's length,
				// and the 3DS reads exactly as many bytes as the size says)
				fi, err = os.Stat(p)
				if err == nil && !fi.Mode().IsRegular() {
					return nil
				}
			}
			if err == nil && fi.Size() > 0 {
				out = append(out, found{p, fi.Size(), fi.ModTime().Unix()})
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// fingerprint: size + the first and last 64 KB -- unique in practice, and cheap over a network.
func fingerprint(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	h.Write([]byte(strconv.FormatInt(size, 10)))
	const n = 64 << 10
	if _, err := io.CopyN(h, f, n); err != nil && err != io.EOF {
		return "", err
	}
	if size > 2*n {
		if _, err := f.Seek(-n, io.SeekEnd); err != nil {
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (sc *Scanner) scanLib(lib *library.Library, refresh bool) {
	sc.set(func(s *Status) { s.Library = lib.Name; s.Current = "Looking for videos..." })
	files := walk(lib.Folders)
	sc.set(func(s *Status) { s.Total += len(files) })

	// what we already know about, by path and by fingerprint
	sc.Store.RLock()
	byPath, byFP := map[string]*library.Item{}, map[string]*library.Item{}
	for _, it := range sc.Store.DB.Items {
		if it.Lib == lib.ID {
			byPath[it.Path] = it
		}
		byFP[it.Fingerprint] = it
	}
	sc.Store.RUnlock()

	seen := map[string]bool{}
	for _, f := range files {
		sc.set(func(s *Status) { s.Current = filepath.Base(f.path) })
		it := byPath[f.path]
		unchanged := it != nil && it.Size == f.size && it.MTime == f.mtime
		named := lib.NameKey()
		if !unchanged {
			fp, err := fingerprint(f.path, f.size)
			if err != nil {
				log.Printf("scan: %s: %v", f.path, err)
				sc.set(func(s *Status) { s.Done++ })
				continue
			}
			if old := byFP[fp]; old != nil && (old.Path == f.path || !exists(old.Path)) {
				it = old // the same file, moved or renamed: it keeps its info and watch history
			} else if old != nil {
				it = nil // a second copy of a file we have: its own entry
			}
			sc.Store.Lock()
			if it == nil {
				id := hashID(fp)
				if sc.Store.DB.Items[id] != nil {
					id = hashID(fp, f.path)
				}
				it = &library.Item{ID: id, Added: time.Now().Unix()}
				sc.Store.DB.Items[id] = it
				sc.set(func(s *Status) { s.Added++ })
			}
			it.Lib, it.Path, it.Size, it.MTime, it.Fingerprint = lib.ID, f.path, f.size, f.mtime, fp
			sc.Store.Unlock()
			sc.identify(it, lib)
		} else if it.Named != named {
			// the library's type changed (or the TV name rules did): read the file again. What a
			// movie lookup said about a file that is now an episode goes (and the other way round).
			sc.Store.Lock()
			it.TmdbVer, it.TMDB = 0, 0
			if it.PosterBy == "tmdb" || it.PosterBy == "catalog" {
				it.Poster, it.PosterBy = "", ""
			}
			sc.Store.Unlock()
			sc.identify(it, lib)
		} else if refresh || (library.IsMoflex(it.Path) && !it.Checked3D) ||
			(!library.IsMoflex(it.Path) && it.Rules3D < library.Rules3DVer) {
			// a moflex from before Big Top read 3D from the file itself, or another video from
			// before the current 3D rules: once, for its 3D label and layout
			sc.identify(it, lib)
		}
		if it.Named != named {
			sc.Store.Lock()
			it.Named = named
			sc.Store.Unlock()
		}
		if library.IsMoflex(it.Path) && !it.Checked3D {
			sc.Store.Lock()
			it.Checked3D = true
			sc.Store.Unlock()
		}
		sc.Store.Lock()
		it.Missing = 0
		sc.Store.Unlock()
		sc.tmdbFill(it, lib)
		seen[it.ID] = true
		sc.set(func(s *Status) { s.Done++ })
	}

	// gone from disk: keep (a drive may just be unplugged) but mark it
	now := time.Now().Unix()
	sc.Store.Lock()
	for _, it := range sc.Store.DB.Items {
		if it.Lib == lib.ID && !seen[it.ID] && it.Missing == 0 {
			it.Missing = now
		}
	}
	sc.Store.Unlock()
	sc.buildShows(lib)
	sc.Store.Dirty()
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func hashID(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// identify works out a file's info: file name, then the catalog, then the super moflex trailer
// (each later source wins where it has the field). The user's edits live apart and are untouched.
func (sc *Scanner) identify(it *library.Item, lib *library.Library) {
	lt := library.LibType(lib.Type)
	var p library.Parsed
	switch lt {
	case library.TypeTV: // the show is the folder; every file is one of its episodes
		p = library.ParseTV(it.Path, lib.Folders)
	case library.TypeMovies:
		p = library.ParseMovieName(it.Path)
	default:
		p = library.ParseName(it.Path)
	}
	det := library.Info{Title: p.Title, Year: p.Year}
	if p.Is3D == 1 {
		det.Is3D = "3d"
		if !library.IsMoflex(it.Path) { // other formats: say how the frame splits
			det.Is3D = map[string]string{"half-sbs": "hsbs", "full-sbs": "fsbs"}[p.Layout]
			if det.Is3D == "" {
				det.Is3D = "hsbs" // the common 3D rip layout
			}
		}
	}
	if p.IsEpisode() {
		det = library.Info{Show: p.Title, ShowYear: p.Year, Season: p.Season, Episode: p.Episode,
			EpTitle: p.EpTitle, Title: p.EpTitle, Is3D: det.Is3D, InShow: p.InShow}
	}
	source := "filename"
	var catID, catArt string
	exact := false

	// the Clownsec catalog: the exact file (same size), else by title
	if e := sc.Catalog.BySize(it.Size); e != nil && !p.IsEpisode() {
		exact = true
		det = fromCatalog(det, e, true)
		catID, catArt, source = e.ID, e.ArtURL(), "catalog"
	} else if p.IsEpisode() {
		if e := sc.Catalog.Show(det.Show); e != nil {
			if t := e.EpisodeTitle(det.Season, det.Episode); t != "" && det.EpTitle == "" {
				det.EpTitle, det.Title = t, t
			}
			if det.ShowYear == 0 {
				det.ShowYear = e.Year
			}
			catID, source = e.ID, "catalog"
		}
	} else if e := sc.Catalog.Movie(det.Title, det.Year); e != nil {
		det = fromCatalog(det, e, false)
		catID, catArt, source = e.ID, e.ArtURL(), "catalog"
	}

	// a super moflex says what it is itself
	var sup *moflex.Super
	showDesc := ""
	contentIs3D := "" // a plain moflex: 2D or 3D from its own frames (the player's rule)
	if library.IsMoflex(it.Path) {
		if f, err := os.Open(it.Path); err == nil {
			sup, _ = moflex.ReadSuper(f, it.Size)
			if st, ok := moflex.DetectStereo(f, it.Size); ok {
				contentIs3D = map[bool]string{true: "3d", false: "2d"}[st]
			}
			f.Close()
		}
	}
	if sup != nil {
		source = "super"
		// What the file is: its own kind= (Moflex Studio 1.7+) decides; a file from before that is
		// an episode when its shelf is TV Shows or its name says SxxEyy.
		kind := strings.ToLower(sup.Field("kind"))
		isEp := kind == "episode" || (kind == "" && (strings.EqualFold(sup.Field("category"), "TV Shows") || p.IsEpisode()))
		switch lt { // the library's type beats the file's own word
		case library.TypeTV:
			isEp = true
		case library.TypeMovies:
			isEp = false
		}
		if isEp {
			if !p.IsEpisode() { // the name says nothing (renamed): the file's own info is all there is
				det = library.Info{Is3D: det.Is3D}
			}
			if lt == library.TypeTV {
				det.InShow = true
			}
			if t := sup.Field("title"); t != "" {
				det.Show = t
			}
			if y := sup.Int("year"); y != 0 {
				det.ShowYear = y
			}
			// season / episode / episode title: the file's own fields beat its name
			// (season 0 = the show's specials, episode 0 = a season's special)
			_, hs := sup.Info["season"]
			_, he := sup.Info["episode"]
			if hs && he {
				det.Season, det.Episode = sup.Int("season"), sup.Int("episode")
			}
			if et := sup.Field("eptitle"); et != "" {
				det.EpTitle, det.Title = et, et
			}
			if d := sup.Field("desc"); d != "" {
				det.Summary = d
				// the summary starts "S1.E2 “Title” -": the episode, even when the file name lost it
				if se, ep, title, rest, ok := moflex.EpisodeFromDesc(d); ok {
					det.Summary = rest
					if det.Season == 0 && det.Episode == 0 {
						det.Season, det.Episode = se, ep
					}
					if det.EpTitle == "" && title != "" {
						det.EpTitle, det.Title = title, title
					}
				}
			}
			showDesc = sup.Field("showdesc")
		} else {
			if p.IsEpisode() { // SxxEyy in the name, but the file says it is a film / short / music video
				det = library.Info{Title: p.Title, Year: p.Year, Is3D: det.Is3D}
			}
			switch kind {
			case "short", "musicvideo", "movie":
				det.Kind = kind
			}
			if t := sup.Field("title"); t != "" {
				det.Title = t
			}
			if y := sup.Int("year"); y != 0 {
				det.Year = y
			}
			if d := sup.Field("desc"); d != "" {
				det.Summary = d
			}
		}
		if g := sup.Field("genres"); g != "" {
			det.Genres = g
		}
		if r := sup.Int("runtime"); r != 0 {
			det.Runtime = r
		}
		switch sup.Field("is3d") {
		case "1":
			det.Is3D = "3d"
		case "0":
			det.Is3D = "2d"
		}
	}

	// the file's own frames beat its name; only its super moflex info says more
	if contentIs3D != "" && (sup == nil || sup.Field("is3d") == "") {
		det.Is3D = contentIs3D
	}

	// not moflex: its streams (length, audio and subtitle tracks), for transcoding
	var med *library.Media
	if !library.IsMoflex(it.Path) && sc.Media != nil && sc.Media.Available() {
		if m, err := sc.Media.Probe(it.Path); err == nil {
			m.Probed, med = it.MTime, m
			if det.Runtime == 0 && m.DurMs > 0 {
				det.Runtime = int((m.DurMs + 30000) / 60000)
			}
			// a double-wide frame is full side-by-side by its shape alone, whatever the name says
			// (the player's rule: two 16:9 halves are 3.55:1, normal films top out near 2.4:1)
			if m.W > 0 && m.H > 0 && m.W*10 > m.H*27 {
				det.Is3D = "fsbs"
			}
		} else {
			log.Printf("ffprobe %s: %v", filepath.Base(it.Path), err)
		}
	}

	// the poster: the user's own stays; else the catalog's art (sharper than the trailer's
	// 132x188 thumbnail) when this IS the catalog's file or there is nothing better; else the trailer's
	sc.Store.RLock()
	userPoster := it.PosterBy == "user"
	havePoster := it.Poster != ""
	sc.Store.RUnlock()
	var pfile, pby string
	if !userPoster {
		if catArt != "" && (exact || sup == nil || sup.Poster == nil) {
			if b, err := catalog.Fetch(catArt); err == nil {
				if fn, err := sc.Posters.SaveBytes(it.ID, b); err == nil {
					pfile, pby = fn, "catalog"
				}
			} else {
				log.Printf("poster %s: %v", catArt, err)
			}
		}
		if pfile == "" && sup != nil && sup.Poster != nil {
			if fn, err := sc.Posters.SaveImage(it.ID, sup.Poster); err == nil {
				pfile, pby = fn, "super"
			}
		}
	}

	sc.Store.Lock()
	defer sc.Store.Unlock()
	it.Detected, it.Source, it.Catalog = det, source, catID
	if source == "filename" { // nothing better than the name: TMDB (again) on this scan
		it.TmdbVer, it.TMDB = 0, 0
		if it.PosterBy == "tmdb" {
			it.Poster, it.PosterBy = "", ""
		}
	}
	if !library.IsMoflex(it.Path) {
		it.Rules3D = library.Rules3DVer
	}
	if med != nil {
		it.Media = med
	}
	it.Super = sup != nil
	it.ShowDesc = showDesc
	if sup != nil {
		it.Audio, it.AltAudio, it.Subs = sup.AudioLang, sup.AltAudio, sup.SubLangs
		it.AltAudios, it.Versions = sup.AltAudios, nil
		if len(sup.Versions) > 0 {
			main := sup.MainLabel
			if main == "" {
				main = "MAIN"
			}
			it.Versions = append([]string{main}, sup.Versions...)
		}
	} else {
		it.Audio, it.AltAudio, it.Subs = "", "", nil
		it.AltAudios, it.Versions = nil, nil
	}
	if pfile != "" {
		it.Poster, it.PosterBy = pfile, pby
	} else if !havePoster {
		it.Poster, it.PosterBy = "", ""
	}
}

func fromCatalog(det library.Info, e *catalog.Entry, exact bool) library.Info {
	if exact || det.Title == "" {
		det.Title = e.Title
	}
	if exact || det.Year == 0 {
		det.Year = e.Year
	}
	det.Genres = strings.Join(e.Genres, ", ")
	det.Runtime = e.Runtime
	det.Summary = e.Description
	det.Kind = library.KindOf(e.Type)
	if exact && e.Is3D() {
		det.Is3D = "3d"
	}
	return det
}

// buildShows groups a library's episodes into shows and gives each show its info and poster.
func (sc *Scanner) buildShows(lib *library.Library) {
	type grp struct {
		title string
		year  int
		eps   []*library.Item
	}
	groups := map[string]*grp{}
	sc.Store.RLock()
	for _, it := range sc.Store.DB.Items {
		if it.Lib != lib.ID || it.Missing != 0 || !it.IsEpisode() {
			continue
		}
		in := it.Info()
		id := library.ShowID(lib.ID, in.Show)
		g := groups[id]
		if g == nil {
			g = &grp{title: in.Show}
			groups[id] = g
		}
		if g.year == 0 {
			g.year = in.ShowYear
		}
		g.eps = append(g.eps, it)
	}
	sc.Store.RUnlock()

	for id, g := range groups {
		sc.Store.RLock()
		sh := sc.Store.DB.Shows[id]
		sc.Store.RUnlock()
		if sh == nil {
			sh = &library.Show{ID: id, Lib: lib.ID}
		}
		det := library.Info{Title: g.title, Year: g.year}
		for _, ep := range g.eps { // a super moflex episode carries the show's description
			if ep.ShowDesc != "" {
				det.Summary = ep.ShowDesc
				break
			}
		}
		var art string
		e := sc.Catalog.Show(g.title)
		if e == nil && len(g.eps) > 0 { // not in the catalog: TMDB's (looked up once)
			sc.showTMDB(sh, g.title, g.year, g.eps[0].Path)
			if t := sh.TMDB; t != nil {
				det.Summary, det.Genres = t.Summary, t.Genres
				if det.Year == 0 {
					det.Year = t.Year
				}
			}
		}
		if e != nil {
			if e.Description != "" {
				det.Summary = e.Description
			}
			det.Genres = strings.Join(e.Genres, ", ")
			if det.Year == 0 {
				det.Year = e.Year
			}
			sh.Catalog, art = e.ID, e.ArtURL()
		}
		if sh.PosterBy != "user" && sh.Poster == "" {
			if art != "" {
				if b, err := catalog.Fetch(art); err == nil {
					if fn, err := sc.Posters.SaveBytes("show-"+id, b); err == nil {
						sh.Poster, sh.PosterBy = fn, "catalog"
					}
				}
			}
			if sh.Poster == "" { // an episode's trailer art, which is the show's poster
				for _, ep := range g.eps {
					if ep.PosterBy == "super" && ep.Poster != "" {
						if b, err := os.ReadFile(sc.Posters.Path(ep.Poster)); err == nil {
							if fn, err := sc.Posters.SaveBytes("show-"+id, bytes.Clone(b)); err == nil {
								sh.Poster, sh.PosterBy = fn, "super"
							}
						}
						break
					}
				}
			}
		}
		sc.Store.Lock()
		sh.Detected = det
		sc.Store.DB.Shows[id] = sh
		sc.Store.Unlock()
	}
}

// buildGroups applies Plex's extras rules: an extra takes its title from its own name (no catalog
// match -- "Trailer-trailer" is not a film called Trailer) and joins its film's group: the film
// whose folder (or loose file) has the same title and year, in any library -- a film moved to
// another share keeps its extras. The 3DS lists a group as one entry with the extras inside.
// Path-based and cheap, so it runs over every file on every scan (re-identifying a file puts its
// catalog info back; this takes it off again).
func (sc *Scanner) buildGroups(libs []*library.Library) {
	roots := map[string][]string{}
	tv := map[string]bool{}
	for _, l := range libs {
		roots[l.ID] = l.Folders
		tv[l.ID] = library.LibType(l.Type) == library.TypeTV
	}
	sc.Store.Lock()
	defer sc.Store.Unlock()
	type fx struct{ kind, title, key string }
	info := map[*library.Item]fx{}
	hasExtras := map[string]bool{}
	for _, it := range sc.Store.DB.Items {
		if it.Missing != 0 {
			continue
		}
		if tv[it.Lib] { // a TV library's "Extras" are its shows' specials, not a film's extras
			it.Extra, it.Group = "", ""
			continue
		}
		k, t, d := library.ExtraOf(it.Path, roots[it.Lib])
		var key string
		if d != "" {
			key = library.FolderKey(d)
		} else if k == "" {
			key = library.FileKey(it.Path)
		}
		info[it] = fx{k, t, key}
		if k != "" && key != "" {
			hasExtras[key] = true
		}
	}
	for it, f := range info {
		if f.kind != "" {
			if it.Extra != f.kind || it.Detected.Title != f.title || it.Catalog != "" {
				it.Detected = library.Info{Title: f.title, Runtime: it.Detected.Runtime, Is3D: it.Detected.Is3D}
				it.Catalog, it.Source = "", "filename"
				if it.PosterBy == "catalog" { // another film's poster, matched by a title like "Trailer"
					it.Poster, it.PosterBy = "", ""
				}
			}
			it.Extra, it.Group = f.kind, ""
			if f.key != "" {
				it.Group = library.GroupID(f.key)
			}
			continue
		}
		it.Extra, it.Group = "", ""
		if f.key != "" && hasExtras[f.key] && !it.IsEpisode() {
			it.Group = library.GroupID(f.key)
		}
	}
}

// tmdbFill: a movie identified by nothing but its file name gets TMDB's info -- found by the IMDb id
// in its path ("{imdb-tt0086896}") when there is one, else by its title and year -- once per file.
// A network failure leaves it for the next scan; a miss is remembered.
func (sc *Scanner) tmdbFill(it *library.Item, lib *library.Library) {
	c := sc.tmdbClient()
	if c == nil {
		return
	}
	sc.Store.RLock()
	need := it.Source == "filename" && it.TmdbVer < library.TmdbVer && !it.IsEpisode() && it.Extra == ""
	in := it.Info()
	path := it.Path
	sc.Store.RUnlock()
	if !need {
		return
	}
	if k, _, _ := library.ExtraOf(path, lib.Folders); k != "" {
		return // an extra: its own name, not a film
	}
	if library.LibType(lib.Type) == library.TypeTV {
		return // a TV library holds no films (its shows get TMDB's info in buildShows)
	}
	sc.set(func(s *Status) { s.Current = "TMDB: " + in.Title })
	id, tv, err := 0, false, error(nil)
	if imdb := tmdb.IMDbID(path); imdb != "" {
		id, tv, err = c.FindIMDb(imdb)
	}
	if id == 0 && in.Title != "" && (err == nil || errors.Is(err, tmdb.ErrNotFound)) {
		id, err = c.Search(in.Title, in.Year, false, library.Norm)
	}
	var info tmdb.Info
	if id > 0 {
		info, err = c.Details(id, tv)
	}
	if err != nil && !errors.Is(err, tmdb.ErrNotFound) {
		log.Printf("TMDB %s: %v", filepath.Base(path), err)
		return
	}
	var pfile string
	if id > 0 && info.Poster != "" {
		sc.Store.RLock()
		want := it.PosterBy != "user" && (it.Poster == "" || it.PosterBy == "tmdb")
		sc.Store.RUnlock()
		if want {
			if b, err := c.Poster(info.Poster); err == nil {
				pfile, _ = sc.Posters.SaveBytes(it.ID, b)
			}
		}
	}
	sc.Store.Lock()
	defer sc.Store.Unlock()
	it.TmdbVer = library.TmdbVer
	if id == 0 {
		return
	}
	d := &it.Detected
	if info.Title != "" {
		d.Title = info.Title
	}
	if info.Year > 0 {
		d.Year = info.Year
	}
	d.Summary, d.Genres = info.Overview, strings.Join(info.Genres, ", ")
	if info.Runtime > 0 && d.Runtime == 0 {
		d.Runtime = info.Runtime
	}
	it.Source, it.TMDB = "tmdb", id
	if pfile != "" {
		it.Poster, it.PosterBy = pfile, "tmdb"
	}
}

// showTMDB: a show the catalog doesn't know -- TMDB's summary, genres, year and poster, looked up
// once (kept in the show, as buildShows rebuilds its info every scan).
func (sc *Scanner) showTMDB(sh *library.Show, title string, year int, epPath string) {
	c := sc.tmdbClient()
	if c == nil || sh.TmdbVer >= library.TmdbVer {
		return
	}
	sc.set(func(s *Status) { s.Current = "TMDB: " + title })
	id, err := 0, error(nil)
	if imdb := tmdb.IMDbID(epPath); imdb != "" {
		var tv bool
		if id, tv, err = c.FindIMDb(imdb); !tv {
			id = 0
		}
	}
	if id == 0 {
		id, err = c.Search(title, year, true, library.Norm)
	}
	var info tmdb.Info
	if id > 0 {
		info, err = c.Details(id, true)
	}
	if err != nil && !errors.Is(err, tmdb.ErrNotFound) {
		log.Printf("TMDB show %s: %v", title, err)
		return
	}
	sh.TmdbVer = library.TmdbVer
	if id == 0 {
		return
	}
	sh.TMDB = &library.ShowTMDB{ID: id, Year: info.Year, Summary: info.Overview, Genres: strings.Join(info.Genres, ", ")}
	if sh.PosterBy != "user" && sh.Poster == "" && info.Poster != "" {
		if b, err := c.Poster(info.Poster); err == nil {
			if fn, err := sc.Posters.SaveBytes("show-"+sh.ID, b); err == nil {
				sh.Poster, sh.PosterBy = fn, "tmdb"
			}
		}
	}
}
