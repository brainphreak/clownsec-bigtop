package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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

// The browser player's streams. A session is one film for one browser, with one set of choices
// (audio track; for phones, the 3D view drawn in). Its playlist lists every 2-second segment of
// the whole film from the start, so any player -- the page, or a phone's own full-screen controls
// -- can seek anywhere. A segment that isn't there yet is made by FFmpeg, started from that very
// segment unless the run already going is about to reach it. Each browser has one session:
// starting another (or closing the player) stops the one before.

type webRun struct {
	seg    int
	cancel context.CancelFunc
	done   chan struct{}
	err    string
}

type webSess struct {
	id, client, dir string
	job             media.WebJob // the choices; Seg and the moflex fields are set per run
	size            int64        // moflex: the file's size
	dur             int64        // ms
	nseg            int

	mu     sync.Mutex
	last   time.Time
	run    *webRun
	closed bool
}

var webs = struct {
	sync.Mutex
	m    map[string]*webSess
	once sync.Once
}{m: map[string]*webSess{}}

func (ws *webSess) touch() { ws.mu.Lock(); ws.last = time.Now(); ws.mu.Unlock() }

func (ws *webSess) seg(k int) string {
	return filepath.Join(ws.dir, fmt.Sprintf("s%05d.ts", k))
}

func (ws *webSess) has(k int) bool { _, err := os.Stat(ws.seg(k)); return err == nil }

// startAt (ws.mu held): stop the run going, start one from segment k.
func (ws *webSess) startAt(s *Server, k int) {
	if ws.run != nil {
		ws.run.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &webRun{seg: k, cancel: cancel, done: make(chan struct{})}
	ws.run = r
	j := ws.job
	j.Seg = k
	go func() {
		defer close(r.done)
		if j.Moflex {
			f, err := os.Open(j.Path)
			if err != nil {
				r.err = err.Error()
				return
			}
			defer f.Close()
			st, err := moflex.KeyStart(f, ws.size, j.StartMs()*1000)
			if err != nil {
				st = moflex.Start{}
			}
			j.In = io.NewSectionReader(f, st.Off, ws.size-st.Off)
			j.GapMs = j.StartMs() - st.Us/1000
			if j.GapMs < 0 {
				j.GapMs = 0
			}
		}
		if err := s.Media.WebTranscode(ctx, j, ws.dir); err != nil {
			log.Printf("browser stream %s: %v", filepath.Base(j.Path), err)
			r.err = err.Error()
		}
	}()
}

// ahead: how far past a run's furthest segment a request may be and still be waited for, rather
// than starting FFmpeg again from it (about 20 s: the run gets there sooner than a restart would)
const ahead = 10

// produced (ws.mu held): the last segment of r's unbroken output so far (r.seg-1 = none yet).
func (ws *webSess) produced(r *webRun) int {
	k := r.seg
	for ws.has(k) {
		k++
	}
	return k - 1
}

var errEnded = errors.New("this stream has ended")

// waitSeg waits until segment k exists, starting FFmpeg there if nothing is about to make it.
func (ws *webSess) waitSeg(ctx context.Context, s *Server, k int) error {
	deadline := time.Now().Add(60 * time.Second)
	tried := false // a run started for k itself
	for {
		if ws.has(k) {
			return nil
		}
		ws.mu.Lock()
		if ws.closed {
			ws.mu.Unlock()
			return errEnded
		}
		r := ws.run
		finished := false
		if r != nil {
			select {
			case <-r.done:
				finished = true
			default:
			}
		}
		if finished && r.seg == k && tried {
			// started for this very segment and ended without it
			msg := r.err
			ws.mu.Unlock()
			if msg == "" {
				return errEnded
			}
			return errors.New(msg)
		}
		if r == nil || k < r.seg || k > ws.produced(r)+ahead || finished {
			ws.startAt(s, k)
			tried = true
		}
		ws.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return errors.New("Big Top is taking too long to convert this part")
		}
	}
}

func (ws *webSess) stop() {
	ws.mu.Lock()
	ws.closed = true
	r := ws.run
	ws.mu.Unlock()
	if r != nil {
		r.cancel()
		go func() { <-r.done; os.RemoveAll(ws.dir) }()
	} else {
		os.RemoveAll(ws.dir)
	}
}

// webJanitor stops sessions nobody has fetched from for a while (a closed tab).
func webJanitor() {
	for range time.Tick(30 * time.Second) {
		webs.Lock()
		for id, ws := range webs.m {
			ws.mu.Lock()
			idle := time.Since(ws.last)
			ws.mu.Unlock()
			if idle > 10*time.Minute {
				ws.stop()
				delete(webs.m, id)
			}
		}
		webs.Unlock()
	}
}

type webOut struct {
	URL    string     `json:"url"`
	Start  int64      `json:"start"`           // ms: the stream's time 0 in the film (0: it is the whole film)
	Dur    int64      `json:"dur"`             // ms
	Frame  string     `json:"frame"`           // the picture: "2d", or both eyes side by side: "hsbs", "fsbs"
	Baked  string     `json:"baked,omitempty"` // the 3D view drawn into the picture (?view=), if any
	View   string     `json:"view"`            // how the item is labelled: "2d", "3d", "hsbs", "fsbs"
	Audio  []TrackOut `json:"audio,omitempty"`
	AudioN int        `json:"audioN"` // the track playing (index into audio), -1 = default
	Title  string     `json:"title"`
	Pos    int64      `json:"pos,omitempty"` // this browser's resume point, ms
}

// GET /api/items/{id}/web?at=ms&audio=n&src=2d|hsbs|fsbs&view=anaglyph|fsbs|hsbs|2d&swap=1&client=x
//
// Start a browser stream; it answers once the segment holding "at" exists. src overrides the
// file's 3D layout; view (with swap) has FFmpeg draw that 3D view into the picture.
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
	var path, title, label string
	var pos int64
	if it != nil && it.Missing == 0 {
		in := it.Info()
		path, title, label = it.Path, in.Title, in.Is3D
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
	out := webOut{Title: title, View: label, AudioN: -1, Pos: pos}
	ws := &webSess{client: client, last: time.Now()}
	j := media.WebJob{Path: path, Audio: -1}

	if library.IsMoflex(path) {
		f, err := os.Open(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		st, _ := f.Stat()
		info := moflex.Probe(f, st.Size())
		f.Close()
		ws.size, out.Dur = st.Size(), info.DurUs/1000
		j.Moflex, j.Stereo = true, info.Stereo
		j.Num, j.Den = info.TbDen, info.TbNum
		if info.TbEye { // Nintendo's 3D files declare the eye rate
			j.Num, j.Den = info.TbDen, 2*info.TbNum
		}
		j.Frame = "2d"
		if info.Stereo {
			j.Frame = "fsbs"
		}
	} else {
		_, m, _ := s.mediaOf(id)
		if m == nil {
			http.Error(w, "Big Top can't read this file", http.StatusUnsupportedMediaType)
			return
		}
		out.Dur = m.DurMs
		j.Media = m
		j.Frame = "2d"
		if label == "hsbs" || label == "fsbs" {
			j.Frame = label
		}
		if src := q.Get("src"); src == "2d" || src == "hsbs" || src == "fsbs" {
			j.Frame = src
		}
		out.Audio = trackList(m.Audio, true)
		if n, err := strconv.Atoi(q.Get("audio")); err == nil && n >= 0 && n < len(m.Audio) {
			j.Audio, out.AudioN = m.Audio[n].Index, n
		}
	}
	if out.Dur <= 0 {
		http.Error(w, "Big Top can't tell how long this video is", http.StatusUnsupportedMediaType)
		return
	}
	switch v := q.Get("view"); v {
	case "anaglyph", "fsbs", "hsbs", "2d":
		if j.Frame != "2d" {
			j.View, j.Swap, out.Baked = v, q.Get("swap") == "1", v
		}
	}
	if at >= out.Dur {
		at = 0
	}
	out.Frame = j.Frame
	ws.job, ws.dur = j, out.Dur
	ws.nseg = int((out.Dur + media.SegSec*1000 - 1) / (media.SegSec * 1000))

	// the streams' folders live in the data folder ("web"), emptied when Big Top first streams
	// (what a Big Top that quit mid-stream left behind)
	root := filepath.Join(s.DataDir, "web")
	webs.once.Do(func() {
		os.RemoveAll(root)
		go webJanitor()
	})
	os.MkdirAll(root, 0o755)
	dir, err := os.MkdirTemp(root, "s-")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b := make([]byte, 8)
	rand.Read(b)
	ws.id, ws.dir = hex.EncodeToString(b), dir
	webs.Lock()
	for k, o := range webs.m {
		if o.client == client {
			o.stop()
			delete(webs.m, k)
		}
	}
	webs.m[ws.id] = ws
	webs.Unlock()

	// answer once the first segment is there, so the player starts at once (and an FFmpeg
	// problem shows up here, not as a stalled player)
	if err := ws.waitSeg(r.Context(), s, int(at/(media.SegSec*1000))); err != nil {
		if r.Context().Err() == nil {
			http.Error(w, "Big Top couldn't convert this file: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	out.URL = "/api/web/" + ws.id + "/index.m3u8"
	writeJSON(w, out)
}

// playlist: every segment of the film, as a finished (VOD) playlist.
func (ws *webSess) playlist() string {
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n"+
		"#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n", media.SegSec+1)
	for k := 0; k < ws.nseg; k++ {
		d := float64(media.SegSec)
		if k == ws.nseg-1 {
			if rest := float64(ws.dur-int64(k)*media.SegSec*1000) / 1000; rest > 0 && rest < d {
				d = rest
			}
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\ns%05d.ts\n", d, k)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

var segName = regexp.MustCompile(`^s(\d{5})\.ts$`)

// GET /api/web/{sid}/{name}: the playlist and segments of a session.
func (s *Server) apiWebFile(w http.ResponseWriter, r *http.Request) {
	webs.Lock()
	ws := webs.m[r.PathValue("sid")]
	webs.Unlock()
	if ws == nil {
		http.Error(w, errEnded.Error(), http.StatusGone)
		return
	}
	ws.touch()
	name := r.PathValue("name")
	w.Header().Set("Cache-Control", "no-store")
	if name == "index.m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		io.WriteString(w, ws.playlist())
		return
	}
	m := segName.FindStringSubmatch(name)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	k, _ := strconv.Atoi(m[1])
	if k >= ws.nseg {
		http.NotFound(w, r)
		return
	}
	if err := ws.waitSeg(r.Context(), s, k); err != nil {
		if err == errEnded {
			http.Error(w, err.Error(), http.StatusGone)
		} else if r.Context().Err() == nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, ws.seg(k))
	// keep the folder small: segments well behind this one go (asked for again, they're remade)
	if k > 90 {
		os.Remove(ws.seg(k - 90))
	}
}

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
