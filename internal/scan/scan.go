// Package scan finds the video files in the libraries' folders and works out what each one is.
package scan

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/catalog"
	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/moflex"
	"github.com/brainphreak/clownsec-bigtop/internal/poster"
)

// Extensions Big Top lists. Only moflex for now; MP4/MKV/AVI come with transcoding.
var Extensions = map[string]bool{".moflex": true}

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
	DataDir string

	run    sync.Mutex // one scan at a time
	mu     sync.Mutex
	status Status
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
		return false
	}
	sc.set(func(s *Status) { *s = Status{Running: true, Last: s.Last} })
	go func() {
		defer sc.run.Unlock()
		err := sc.scanAll(refresh)
		sc.set(func(s *Status) {
			s.Running, s.Current, s.Library, s.Last = false, "", "", time.Now().Unix()
			if err != nil {
				s.Error = err.Error()
			}
		})
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
	sc.Store.Dirty()
	return nil
}

type found struct {
	path  string
	size  int64
	mtime int64
}

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
			if fi, err := d.Info(); err == nil && fi.Size() > 0 {
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
			sc.identify(it)
		} else if refresh {
			sc.identify(it)
		}
		sc.Store.Lock()
		it.Missing = 0
		sc.Store.Unlock()
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
func (sc *Scanner) identify(it *library.Item) {
	p := library.ParseName(it.Path)
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
			EpTitle: p.EpTitle, Title: p.EpTitle, Is3D: det.Is3D}
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
	if f, err := os.Open(it.Path); err == nil {
		sup, _ = moflex.ReadSuper(f, it.Size)
		f.Close()
	}
	if sup != nil {
		source = "super"
		if strings.EqualFold(sup.Field("category"), "TV Shows") || p.IsEpisode() {
			if t := sup.Field("title"); t != "" {
				det.Show = t
			}
			if y := sup.Int("year"); y != 0 {
				det.ShowYear = y
			}
			if d := sup.Field("desc"); d != "" {
				det.Summary = d
			}
		} else {
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
	it.Super = sup != nil
	if sup != nil {
		it.Audio, it.AltAudio, it.Subs = sup.AudioLang, sup.AltAudio, sup.SubLangs
	} else {
		it.Audio, it.AltAudio, it.Subs = "", "", nil
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
		var art string
		if e := sc.Catalog.Show(g.title); e != nil {
			det.Summary, det.Genres = e.Description, strings.Join(e.Genres, ", ")
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
