package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/media"
)

// MP4, MKV, AVI and the rest: the 3DS can't play them as they are, so Big Top has FFmpeg turn
// them into the stream its Plex path already plays -- MPEG-TS of H.264 + AAC at 3DS size.
// A seek is a new request from that time (the transcode restarts there).

// mediaOf returns an item's streams, asking ffprobe when they aren't known (or the file changed).
func (s *Server) mediaOf(id string) (*library.Item, *library.Media, string) {
	s.Store.RLock()
	it := s.Store.DB.Items[id]
	var m *library.Media
	path, mtime := "", int64(0)
	if it != nil && it.Missing == 0 && !library.IsMoflex(it.Path) {
		m, path, mtime = it.Media, it.Path, it.MTime
	}
	s.Store.RUnlock()
	if path == "" {
		return nil, nil, ""
	}
	if m == nil || m.Probed != mtime || m.Ver < library.ProbeVer {
		pm, err := s.Media.Probe(path)
		if err != nil {
			log.Printf("ffprobe %s: %v", filepath.Base(path), err)
			return it, nil, path
		}
		pm.Probed = mtime
		s.Store.Lock()
		it.Media = pm
		s.Store.Unlock()
		s.Store.Dirty()
		m = pm
	}
	return it, m, path
}

// one transcode per console: asking for another place stops the one before
type streamTok struct{ cancel context.CancelFunc }

var running = struct {
	sync.Mutex
	m map[string]*streamTok
}{m: map[string]*streamTok{}}

type flushWriter struct {
	w http.ResponseWriter
	c *http.ResponseController
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err == nil {
		f.c.Flush() // the 3DS should see the start at once, not when a buffer fills
	}
	return n, err
}

// GET /api/items/{id}/stream.ts?t=ms&fmt=0|1|2&audio=n&burn=n
//
//	fmt    the player's frame layout (0 2D, 1 side-by-side, 2 half side-by-side): non-zero gets
//	       a 3D-sized frame
//	audio  index into the item's audioTracks (default: the file's default track)
//	burn   index into the item's burnSubs, drawn into the picture (default: none)
func (s *Server) apiStream(w http.ResponseWriter, r *http.Request) {
	j, ctx, done := s.startStream(w, r)
	if done == nil {
		return
	}
	defer done()
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	conv := s.wants3D(r)
	w.WriteHeader(http.StatusOK)
	fw := flushWriter{w, http.NewResponseController(w)}
	if conv != nil {
		if err := s.Media.Transcode3D(ctx, j, false, 0, conv, fw); err != nil {
			log.Printf("2D-to-3D stream %s: %v", filepath.Base(j.Path), err)
		}
		return
	}
	if err := s.Media.Transcode(ctx, j, fw); err != nil {
		log.Printf("transcode %s: %v", filepath.Base(j.Path), err)
	}
}

// GET /api/items/{id}/stream.moflex?t=ms&fmt=0|1|2&audio=n&burn=n&q=n
//
// The same video as stream.ts, encoded live to moflex for an Old 3DS (which can't decode H.264).
// fmt picks the frame's shape (0 2D, 1 full side-by-side, 2 half side-by-side -> a 3D moflex);
// q the MobiClip quantizer (lower = better and bigger). "at" is taken for t too.
func (s *Server) apiStreamMoflex(w http.ResponseWriter, r *http.Request) {
	if s.Media.MoflexEncoder() == "" {
		http.Error(w, "The moflex encoder isn't installed on the Big Top computer", http.StatusServiceUnavailable)
		return
	}
	j, ctx, done := s.startStream(w, r)
	if done == nil {
		return
	}
	defer done()
	layout := ""
	switch r.URL.Query().Get("fmt") {
	case "1":
		layout = "fsbs"
	case "2":
		layout = "hsbs"
	}
	q, _ := strconv.Atoi(r.URL.Query().Get("q"))
	w.Header().Set("Content-Type", "video/x-moflex")
	w.Header().Set("Cache-Control", "no-store")
	conv := s.wants3D(r)
	w.WriteHeader(http.StatusOK)
	fw := flushWriter{w, http.NewResponseController(w)}
	if conv != nil {
		if err := s.Media.Transcode3D(ctx, j, true, q, conv, fw); err != nil {
			log.Printf("2D-to-3D moflex stream %s: %v", filepath.Base(j.Path), err)
		}
		return
	}
	if err := s.Media.TranscodeMoflex(ctx, j, layout, q, fw); err != nil {
		log.Printf("moflex stream %s: %v", filepath.Base(j.Path), err)
	}
}

// wants3D: conv3d=1 on a stream asks for the 2D video converted to 3D (full side-by-side 800x240);
// nil when not asked, or 2D to 3D isn't set up here (the stream is then plain 2D).
func (s *Server) wants3D(r *http.Request) media.NewConverter3D {
	if r.URL.Query().Get("conv3d") != "1" || s.Depth3D == nil || !s.Depth3D.Ready() {
		return nil
	}
	return func(w, h, ow, oh int) (media.Converter3D, error) { return s.Depth3D.Converter(w, h, ow, oh) }
}

// startStream: the job a stream request asks for, and this console's turn to run it (a stream it
// already has going stops). done == nil: the request was answered (an error); else call done
// when the stream ends.
func (s *Server) startStream(w http.ResponseWriter, r *http.Request) (media.Job, context.Context, func()) {
	var j media.Job
	if !s.Media.Available() {
		http.Error(w, "FFmpeg isn't installed on the Big Top computer", http.StatusServiceUnavailable)
		return j, nil, nil
	}
	_, m, path := s.mediaOf(r.PathValue("id"))
	if path == "" {
		http.NotFound(w, r)
		return j, nil, nil
	}
	if m == nil {
		http.Error(w, "Big Top can't read this file", http.StatusUnsupportedMediaType)
		return j, nil, nil
	}
	q := r.URL.Query()
	ts := q.Get("t")
	if ts == "" {
		ts = q.Get("at")
	}
	t, _ := strconv.ParseInt(ts, 10, 64)
	f, _ := strconv.Atoi(q.Get("fmt"))
	j = media.Job{Path: path, Media: m, StartMs: t, Stereo: f != 0, Audio: -1, Burn: -1}
	if n, err := strconv.Atoi(q.Get("audio")); err == nil && n >= 0 && n < len(m.Audio) {
		j.Audio = m.Audio[n].Index
	}
	if n, err := strconv.Atoi(q.Get("burn")); err == nil {
		if b := burnable(m); n >= 0 && n < len(b) {
			j.Burn = b[n].Index
		}
	}
	if j.StartMs < 0 || (m.DurMs > 0 && j.StartMs >= m.DurMs) {
		j.StartMs = 0
	}

	client := clientID(r)
	ctx, cancel := context.WithCancel(r.Context())
	tok := &streamTok{cancel}
	running.Lock()
	if prev := running.m[client]; prev != nil {
		prev.cancel()
	}
	running.m[client] = tok
	running.Unlock()
	return j, ctx, func() {
		cancel()
		running.Lock()
		if running.m[client] == tok {
			delete(running.m, client)
		}
		running.Unlock()
	}
}

// burnable: the picture subtitles (PGS, DVD) -- the 3DS can't draw them, so they're burned in.
func burnable(m *library.Media) []library.Track {
	var out []library.Track
	for _, t := range m.Subs {
		if t.Image {
			out = append(out, t)
		}
	}
	return out
}

// textSubs: the subtitle tracks that become files the 3DS draws itself (in both eyes in 3D).
func textSubs(m *library.Media) []library.Track {
	var out []library.Track
	if m == nil {
		return nil
	}
	for _, t := range m.Subs {
		if !t.Image && media.TextSub(t.Codec) {
			out = append(out, t)
		}
	}
	return out
}

// TrackOut is a track as the 3DS lists it.
type TrackOut struct {
	N       int    `json:"n"`
	Lang    string `json:"lang,omitempty"`
	Name    string `json:"name"`
	Forced  bool   `json:"forced,omitempty"`
	Default bool   `json:"default,omitempty"`
}

var langNames = map[string]string{
	"eng": "English", "en": "English", "spa": "Spanish", "es": "Spanish", "fre": "French", "fra": "French",
	"fr": "French", "ger": "German", "deu": "German", "de": "German", "ita": "Italian", "it": "Italian",
	"jpn": "Japanese", "ja": "Japanese", "por": "Portuguese", "pt": "Portuguese", "rus": "Russian",
	"ru": "Russian", "chi": "Chinese", "zho": "Chinese", "zh": "Chinese", "kor": "Korean", "ko": "Korean",
	"dut": "Dutch", "nld": "Dutch", "nl": "Dutch", "swe": "Swedish", "sv": "Swedish", "nor": "Norwegian",
	"dan": "Danish", "fin": "Finnish", "pol": "Polish", "pl": "Polish", "tur": "Turkish", "ara": "Arabic",
	"hin": "Hindi", "heb": "Hebrew", "gre": "Greek", "ell": "Greek", "cze": "Czech", "ces": "Czech",
	"hun": "Hungarian", "tha": "Thai", "vie": "Vietnamese", "ind": "Indonesian", "ukr": "Ukrainian",
}

func langName(code string) string {
	if n := langNames[strings.ToLower(code)]; n != "" {
		return n
	}
	if code == "" || strings.EqualFold(code, "und") {
		return ""
	}
	return strings.ToUpper(code)
}

func codecName(c string) string {
	switch c {
	case "aac":
		return "AAC"
	case "ac3":
		return "AC3"
	case "eac3":
		return "E-AC3"
	case "dts", "dca":
		return "DTS"
	case "truehd":
		return "TrueHD"
	case "mp3":
		return "MP3"
	case "flac":
		return "FLAC"
	case "opus":
		return "Opus"
	case "hdmv_pgs_subtitle":
		return "PGS"
	case "dvd_subtitle":
		return "DVD"
	}
	return strings.ToUpper(c)
}

func trackName(t library.Track, n int, audio bool) string {
	name := langName(t.Lang)
	if t.Title != "" && !strings.EqualFold(t.Title, name) {
		if name != "" {
			name += " - " + t.Title
		} else {
			name = t.Title
		}
	}
	if name == "" {
		name = "Track " + strconv.Itoa(n+1)
	}
	if audio {
		ch := ""
		switch t.Channels {
		case 1:
			ch = " mono"
		case 2:
			ch = " stereo"
		case 6:
			ch = " 5.1"
		case 8:
			ch = " 7.1"
		}
		name += " (" + codecName(t.Codec) + ch + ")"
	} else if t.Forced && !strings.Contains(strings.ToLower(name), "forced") {
		name += " (forced)"
	}
	return clip(name, 44)
}

func trackList(ts []library.Track, audio bool) []TrackOut {
	out := make([]TrackOut, 0, len(ts))
	for i, t := range ts {
		out = append(out, TrackOut{N: i, Lang: t.Lang, Name: trackName(t, i, audio), Forced: t.Forced, Default: t.Default})
	}
	return out
}

// ---- subtitle tracks inside the file, as files ----

// A video's text tracks are extracted together, in one background pass (see media.ExtractSubs), one
// video at a time (a network share serves one long read far better than several at once). Whoever
// needs a track waits a little for it; a 3DS that can't wait plays without it this time and has it
// the next time.
type subJob struct {
	done chan struct{}
	err  error
	at   time.Time
}

var (
	subMu   sync.Mutex
	subJobs = map[string]*subJob{} // by item and file version
	subSlot = make(chan struct{}, 1)
)

// errSubsPending: the video's subtitles are still being read.
var errSubsPending = errors.New("the subtitles are still being read from the video")

func (s *Server) subPath(it *library.Item, t library.Track) string {
	return filepath.Join(s.DataDir, "subcache", fmt.Sprintf("%s-%d-%d.%s", it.ID, it.MTime, t.Index, media.SubExt(t.Codec)))
}

// startSubs: the background extraction of an item's text tracks not cached yet (nil = all there).
func (s *Server) startSubs(it *library.Item, m *library.Media, path string) *subJob {
	if it == nil || !s.Media.Available() {
		return nil
	}
	var want []media.SubOut
	for _, t := range textSubs(m) {
		if out := s.subPath(it, t); !isFile(out) {
			want = append(want, media.SubOut{Index: t.Index, Codec: t.Codec, Out: out})
		}
	}
	if len(want) == 0 {
		return nil
	}
	key := fmt.Sprintf("%s-%d", it.ID, it.MTime)
	subMu.Lock()
	defer subMu.Unlock()
	if j := subJobs[key]; j != nil {
		select {
		case <-j.done:
			if j.err == nil || time.Since(j.at) < 10*time.Minute {
				return j // finished (or failed recently: not again straight away)
			}
		default:
			return j // running: join it
		}
	}
	j := &subJob{done: make(chan struct{})}
	subJobs[key] = j
	os.MkdirAll(filepath.Join(s.DataDir, "subcache"), 0o755)
	go func() {
		subSlot <- struct{}{}
		t0 := time.Now()
		err := s.Media.ExtractSubs(path, want)
		<-subSlot
		if err != nil {
			log.Printf("subtitles %s: %v", filepath.Base(path), err)
		} else {
			log.Printf("subtitles %s: %d track(s) read in %s", filepath.Base(path), len(want), time.Since(t0).Round(time.Second))
		}
		subMu.Lock()
		j.err, j.at = err, time.Now()
		subMu.Unlock()
		close(j.done)
	}()
	return j
}

// embeddedSub: the cached file for an item's text track; if it isn't extracted yet, the extraction is
// started (or joined) and waited for up to wait -- then errSubsPending.
func (s *Server) embeddedSub(it *library.Item, path string, t library.Track, wait time.Duration) (string, error) {
	out := s.subPath(it, t)
	if isFile(out) {
		return out, nil
	}
	_, m, _ := s.mediaOf(it.ID)
	j := s.startSubs(it, m, path)
	if j == nil {
		if isFile(out) {
			return out, nil
		}
		return "", errors.New("the subtitle track couldn't be read")
	}
	select {
	case <-j.done:
		if isFile(out) {
			return out, nil
		}
		if j.err != nil {
			return "", j.err
		}
		return "", errors.New("the subtitle track couldn't be read")
	case <-time.After(wait):
		return "", errSubsPending
	}
}

// prefetchSubs starts reading an item's text tracks when the 3DS opens its page, so they may be
// ready by the time Play is pressed.
func (s *Server) prefetchSubs(id string) {
	it, m, path := s.mediaOf(id)
	s.startSubs(it, m, path)
}

func isFile(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
