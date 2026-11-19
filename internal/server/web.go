package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/media"
	"github.com/brainphreak/clownsec-bigtop/internal/moflex"
)

// The browser player's streams. A session is one FFmpeg run from one place in one file, writing
// HLS into a temporary folder; a seek outside what's there starts a new one. Each browser has at
// most one: starting another (or closing the player) stops the one before.

type webSess struct {
	id, client, dir string
	cancel          context.CancelFunc
	mu              sync.Mutex
	last            time.Time
	err             string
	done            chan struct{}
}

var webs = struct {
	sync.Mutex
	m    map[string]*webSess
	once sync.Once
}{m: map[string]*webSess{}}

func (ws *webSess) touch() { ws.mu.Lock(); ws.last = time.Now(); ws.mu.Unlock() }

func (ws *webSess) stop() {
	ws.cancel()
	go func() { <-ws.done; os.RemoveAll(ws.dir) }()
}

// webJanitor stops sessions nobody has fetched from for a while (a closed tab).
func webJanitor() {
	for range time.Tick(30 * time.Second) {
		webs.Lock()
		for id, ws := range webs.m {
			ws.mu.Lock()
			idle := time.Since(ws.last)
			ws.mu.Unlock()
			if idle > 5*time.Minute {
				ws.stop()
				delete(webs.m, id)
			}
		}
		webs.Unlock()
	}
}

type webOut struct {
	URL    string     `json:"url"`
	Start  int64      `json:"start"` // ms: what the stream's time 0 is in the film
	Dur    int64      `json:"dur"`   // ms, 0 = unknown
	Frame  string     `json:"frame"` // the picture: "2d", or both eyes side by side: "hsbs", "fsbs"
	View   string     `json:"view"`  // how the item is labelled: "2d", "3d", "hsbs", "fsbs"
	Audio  []TrackOut `json:"audio,omitempty"`
	AudioN int        `json:"audioN"` // the track playing (index into audio), -1 = default
	Title  string     `json:"title"`
	Pos    int64      `json:"pos,omitempty"` // this browser's resume point, ms
}

// GET /api/items/{id}/web?at=ms&audio=n&client=x: start a browser stream at a time.
func (s *Server) apiWeb(w http.ResponseWriter, r *http.Request) {
	if !s.Media.Available() {
		http.Error(w, "FFmpeg isn't installed on the Big Top computer", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	q := r.URL.Query()
	at, _ := strconv.ParseInt(q.Get("at"), 10, 64)
	if at < 0 {
		at = 0
	}
	client := clientID(r)

	s.Store.RLock()
	it := s.Store.DB.Items[id]
	var path, title, view string
	var pos int64
	if it != nil && it.Missing == 0 {
		in := it.Info()
		path, title, view = it.Path, in.Title, in.Is3D
		if it.IsEpisode() {
			title = in.Show + " - " + title
		}
		if p := s.Store.DB.Progress[client][it.ID]; p != nil && !p.Watched {
			pos = p.PosMs
		}
	}
	s.Store.RUnlock()
	if path == "" {
		http.NotFound(w, r)
		return
	}
	out := webOut{Title: title, View: view, AudioN: -1, Pos: pos}
	j := media.WebJob{Path: path, Audio: -1}

	var f *os.File
	if library.IsMoflex(path) {
		var err error
		if f, err = os.Open(path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		st, _ := f.Stat()
		info := moflex.Probe(f, st.Size())
		out.Dur = info.DurUs / 1000
		if out.Dur > 0 && at >= out.Dur {
			at = 0
		}
		start, err := moflex.KeyStart(f, st.Size(), at*1000)
		if err != nil {
			start = moflex.Start{}
		}
		j.Moflex, j.Stereo = true, info.Stereo
		j.Num, j.Den = info.TbDen, info.TbNum
		if info.TbEye { // Nintendo's 3D files declare the eye rate
			j.Num, j.Den = info.TbDen, 2*info.TbNum
		}
		j.In = io.NewSectionReader(f, start.Off, st.Size()-start.Off)
		j.GapMs = at - start.Us/1000
		if j.GapMs < 0 {
			j.GapMs = 0
		}
		out.Frame = "2d"
		if info.Stereo {
			out.Frame = "fsbs"
		}
	} else {
		_, m, _ := s.mediaOf(id)
		if m == nil {
			http.Error(w, "Big Top can't read this file", http.StatusUnsupportedMediaType)
			return
		}
		out.Dur = m.DurMs
		if out.Dur > 0 && at >= out.Dur {
			at = 0
		}
		j.Media = m
		j.Layout = view
		out.Frame = "2d"
		if view == "hsbs" || view == "fsbs" {
			out.Frame = view
		}
		out.Audio = trackList(m.Audio, true)
		if n, err := strconv.Atoi(q.Get("audio")); err == nil && n >= 0 && n < len(m.Audio) {
			j.Audio, out.AudioN = m.Audio[n].Index, n
		}
	}
	j.StartMs = at
	out.Start = at

	// the streams' folders live in the data folder ("web"), emptied when Big Top starts streaming
	// (what a Big Top that quit mid-stream left behind)
	root := filepath.Join(s.DataDir, "web")
	webs.once.Do(func() {
		os.RemoveAll(root)
		go webJanitor()
	})
	os.MkdirAll(root, 0o755)
	dir, err := os.MkdirTemp(root, "s-")
	if err != nil {
		if f != nil {
			f.Close()
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b := make([]byte, 8)
	rand.Read(b)
	ctx, cancel := context.WithCancel(context.Background())
	ws := &webSess{id: hex.EncodeToString(b), client: client, dir: dir, cancel: cancel, last: time.Now(), done: make(chan struct{})}
	webs.Lock()
	for k, o := range webs.m {
		if o.client == client {
			o.stop()
			delete(webs.m, k)
		}
	}
	webs.m[ws.id] = ws
	webs.Unlock()
	go func() {
		defer close(ws.done)
		if f != nil {
			defer f.Close()
		}
		if err := s.Media.WebTranscode(ctx, j, dir); err != nil {
			log.Printf("browser stream %s: %v", filepath.Base(path), err)
			ws.mu.Lock()
			ws.err = err.Error()
			ws.mu.Unlock()
		}
	}()

	// answer once the first segment is there, so the player starts at once
	pl := filepath.Join(dir, "index.m3u8")
	deadline := time.Now().Add(45 * time.Second)
wait:
	for {
		if b, err := os.ReadFile(pl); err == nil && strings.Contains(string(b), "#EXTINF") {
			break
		}
		select {
		case <-ws.done:
			if b, err := os.ReadFile(pl); err == nil && strings.Contains(string(b), "#EXTINF") {
				break wait // a short file: done already
			}
			ws.mu.Lock()
			msg := ws.err
			ws.mu.Unlock()
			if msg == "" {
				msg = "the stream ended before it started"
			}
			http.Error(w, "Big Top couldn't convert this file: "+msg, http.StatusInternalServerError)
			return
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			http.Error(w, "Big Top is taking too long to start this stream", http.StatusGatewayTimeout)
			return
		}
	}
	out.URL = "/api/web/" + ws.id + "/index.m3u8"
	writeJSON(w, out)
}

var segName = regexp.MustCompile(`^s(\d{5})\.ts$`)

// GET /api/web/{sid}/{name}: the playlist and segments of a session.
func (s *Server) apiWebFile(w http.ResponseWriter, r *http.Request) {
	webs.Lock()
	ws := webs.m[r.PathValue("sid")]
	webs.Unlock()
	if ws == nil {
		http.Error(w, "this stream has ended", http.StatusGone)
		return
	}
	ws.touch()
	name := r.PathValue("name")
	w.Header().Set("Cache-Control", "no-store")
	if name == "index.m3u8" {
		b, err := os.ReadFile(filepath.Join(ws.dir, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		// start at the beginning of what's there, not at the "live" end
		pl := strings.Replace(string(b), "#EXTM3U\n", "#EXTM3U\n#EXT-X-START:TIME-OFFSET=0,PRECISE=YES\n", 1)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		io.WriteString(w, pl)
		return
	}
	m := segName.FindStringSubmatch(name)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, filepath.Join(ws.dir, name))
	// keep the folder small: segments well behind the one playing go (a seek back that far
	// starts a new stream anyway)
	if n, _ := strconv.Atoi(m[1]); n > 90 {
		os.Remove(filepath.Join(ws.dir, "s"+pad5(n-90)+".ts"))
	}
}

func pad5(n int) string { s := strconv.Itoa(n); return strings.Repeat("0", 5-len(s)) + s }

// POST /api/web/{sid}/stop: the player closed.
func (s *Server) apiWebStop(w http.ResponseWriter, r *http.Request) {
	webs.Lock()
	if ws := webs.m[r.PathValue("sid")]; ws != nil {
		ws.stop()
		delete(webs.m, ws.id)
	}
	webs.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
