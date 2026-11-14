package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Sidecar subtitles: files next to the video, named after it -- the convention Plex and the
// 3DS player use for local files:
//
//	Movie.moflex
//	Movie.srt            (or .ass / .ssa) the default track
//	Movie.en.srt         a language
//	Movie.en.forced.srt  a language, forced-only lines
//	Movie.English SDH.ass  (anything after the dot is the track's label)
//
// They are listed when asked for (not at scan time), so a file added later shows up at once.
type Sidecar struct {
	N      int    `json:"n"`
	File   string `json:"file"`
	Label  string `json:"label"` // what sits between the video's name and the extension ("en.forced"), "" = default
	Ext    string `json:"ext"`   // "srt", "ass", "ssa"
	Forced bool   `json:"forced,omitempty"`
}

var subExt = map[string]bool{".srt": true, ".ass": true, ".ssa": true}

func sidecars(video string) []Sidecar {
	dir, base := filepath.Split(video)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Sidecar
	low := strings.ToLower(stem)
	for _, e := range ents {
		n := e.Name()
		ext := strings.ToLower(filepath.Ext(n))
		if e.IsDir() || !subExt[ext] {
			continue
		}
		name := strings.TrimSuffix(n, filepath.Ext(n))
		if !strings.HasPrefix(strings.ToLower(name), low) {
			continue
		}
		rest := name[len(stem):]
		if rest != "" && rest[0] != '.' {
			continue // "Movie 2.srt" belongs to "Movie 2", not to "Movie"
		}
		label := strings.Trim(rest, ". _-")
		out = append(out, Sidecar{File: n, Label: label, Ext: ext[1:],
			Forced: strings.Contains(strings.ToLower(label), "forced")})
	}
	sort.Slice(out, func(i, j int) bool { // the default first, then by name
		if (out[i].Label == "") != (out[j].Label == "") {
			return out[i].Label == ""
		}
		return out[i].File < out[j].File
	})
	for i := range out {
		out[i].N = i
	}
	return out
}

func (s *Server) itemPath(id string) string {
	s.Store.RLock()
	defer s.Store.RUnlock()
	if it := s.Store.DB.Items[id]; it != nil && it.Missing == 0 {
		return it.Path
	}
	return ""
}

func (s *Server) apiSubs(w http.ResponseWriter, r *http.Request) {
	p := s.itemPath(r.PathValue("id"))
	if p == "" {
		http.NotFound(w, r)
		return
	}
	list := sidecars(p)
	if list == nil {
		list = []Sidecar{}
	}
	writeJSON(w, list)
}

func (s *Server) apiSubFile(w http.ResponseWriter, r *http.Request) {
	p := s.itemPath(r.PathValue("id"))
	n, err := strconv.Atoi(r.PathValue("n"))
	if p == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	list := sidecars(p)
	if n < 0 || n >= len(list) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, filepath.Join(filepath.Dir(p), list[n].File))
}
