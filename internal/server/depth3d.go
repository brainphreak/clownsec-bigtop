package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/config"
	"github.com/brainphreak/clownsec-bigtop/internal/depth3d"
	"github.com/brainphreak/clownsec-bigtop/internal/media"
)

// 2D to 3D on the setup page: download the packages, then measure which depth size this computer
// can stream with -- the whole stream (decode, convert, encode) on the built-in test clip, for an
// Old 3DS (moflex) and a New 3DS (H.264). The 3DS offers Play in 3D only where that came out at
// least config.Depth3DMin times real time.

// the depth sizes the setup downloads (Distill-Any-Depth input widths), finest first
var d3Tiers = []int{448, 392, 322}

type d3Result struct {
	Tier   int     `json:"tier"` // 0 = MiDaS
	Fps    float64 `json:"fps"`  // the converter alone
	Old    float64 `json:"old"`  // whole Old 3DS stream, x real time (0 = not run)
	New    float64 `json:"new"`  // whole New 3DS stream
	Device string  `json:"device,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type d3Web struct {
	Tier  int     `json:"tier"`
	Speed float64 `json:"speed"` // a browser stream, x real time
}

type d3Test struct {
	Running bool       `json:"running"`
	Step    string     `json:"step,omitempty"`
	Results []d3Result `json:"results,omitempty"`
	Web     []d3Web    `json:"web,omitempty"` // the browser's sizes tried
	Error   string     `json:"error,omitempty"`
	Done    bool       `json:"done,omitempty"`
}

var d3t = struct {
	sync.Mutex
	t d3Test
}{}

func tierName(t int) string {
	switch t {
	case 0:
		return "midas"
	case -1:
		return ""
	}
	return strconv.Itoa(t)
}

// GET /admin/api/depth3d
func (s *Server) adDepth3D(w http.ResponseWriter, r *http.Request) {
	c := s.Cfg.Snapshot()
	old, nw := c.Conv3D()
	d3t.Lock()
	t := d3t.t
	d3t.Unlock()
	writeJSON(w, map[string]any{"packages": s.Depth3D.Pk.Status(), "setting": c.Depth3D, "fps": c.Depth3DFps,
		"old": c.Depth3DOld, "new": c.Depth3DNew, "offerOld": old, "offerNew": nw, "device": s.Depth3D.Device(),
		"test": t, "setupDone": c.SetupDone, "need": config.Depth3DMin, "platform": depth3d.Platform(),
		"web": c.Depth3DWeb, "webSpd": c.Depth3DWebSpd})
}

// POST /admin/api/depth3d/install: the runtime and the setup's depth sizes, in the background
func (s *Server) adDepth3DInstall(w http.ResponseWriter, r *http.Request) {
	if !s.Depth3D.Pk.Status().Supported {
		bad(w, "2D to 3D isn't available for this computer ("+depth3d.Platform()+").")
		return
	}
	go func() {
		if err := s.Depth3D.Pk.Install(d3Tiers, false); err != nil {
			log.Printf("2D to 3D: download: %v", err)
		}
	}()
	writeJSON(w, map[string]bool{"started": true})
}

// POST /admin/api/depth3d/test: measure, pick and save, in the background
func (s *Server) adDepth3DTest(w http.ResponseWriter, r *http.Request) {
	d3t.Lock()
	if d3t.t.Running {
		d3t.Unlock()
		bad(w, "The test is already running.")
		return
	}
	d3t.t = d3Test{Running: true, Step: "Getting ready"}
	d3t.Unlock()
	go s.runDepth3DTest()
	writeJSON(w, map[string]bool{"started": true})
}

// POST /admin/api/depth3d/off
func (s *Server) adDepth3DOff(w http.ResponseWriter, r *http.Request) {
	s.Cfg.Update(func(c *config.Config) { c.Depth3D, c.Depth3DFps, c.Depth3DOld, c.Depth3DNew = "", 0, 0, 0 })
	s.Depth3D.SetTier(-1)
	writeJSON(w, map[string]bool{"ok": true})
}

// POST /admin/api/setup/done: the first-run setup was finished (or skipped)
func (s *Server) adSetupDone(w http.ResponseWriter, r *http.Request) {
	s.Cfg.Update(func(c *config.Config) { c.SetupDone = true })
	writeJSON(w, map[string]bool{"ok": true})
}

func d3Step(f string, a ...any) {
	d3t.Lock()
	d3t.t.Step = fmt.Sprintf(f, a...)
	d3t.Unlock()
}

func (s *Server) runDepth3DTest() {
	res, pick, err := s.depth3DTest()
	d3t.Lock()
	d3t.t.Results = res
	d3t.Unlock()
	finish := func(e error) {
		d3t.Lock()
		d3t.t.Running, d3t.t.Done, d3t.t.Step = false, true, ""
		if e != nil {
			d3t.t.Error = e.Error()
		}
		d3t.Unlock()
	}
	if err != nil {
		log.Printf("2D to 3D test: %v", err)
		finish(err)
		return
	}
	s.Cfg.Update(func(c *config.Config) {
		c.Depth3D, c.Depth3DFps, c.Depth3DOld, c.Depth3DNew = tierName(pick.Tier), pick.Fps, pick.Old, pick.New
		c.Depth3DWeb, c.Depth3DWebSpd = "", 0
		if pick.Tier < 0 {
			c.Depth3DFps, c.Depth3DOld, c.Depth3DNew = 0, 0, 0
		}
	})
	s.Depth3D.SetTier(pick.Tier)
	log.Printf("2D to 3D test: picked %q (converter %.0f fps, Old 3DS %.2fx, New 3DS %.2fx)",
		tierName(pick.Tier), pick.Fps, pick.Old, pick.New)
	if pick.Tier >= 0 {
		// the browser: bigger eyes and a heavier encode -- its own size, from the 3DS's down
		wt, wspd, tried := s.depth3DWebTest(pick.Tier)
		d3t.Lock()
		d3t.t.Web = tried
		d3t.Unlock()
		s.Cfg.Update(func(c *config.Config) { c.Depth3DWeb, c.Depth3DWebSpd = tierName(wt), wspd })
		log.Printf("2D to 3D test: the browser uses %q (%.2fx)", tierName(wt), wspd)
	}
	finish(nil)
}

// depth3DTest: each installed size, finest first, until one streams fast enough for both kinds of
// 3DS (else the best for either); MiDaS (downloaded then) when no Distill size can.
func (s *Server) depth3DTest() ([]d3Result, d3Result, error) {
	none := d3Result{Tier: -1}
	if !s.Media.Available() {
		return nil, none, fmt.Errorf("FFmpeg isn't installed on this computer")
	}
	path, err := s.Media.SpeedClip()
	if err != nil {
		return nil, none, err
	}
	m, err := s.Media.Probe(path)
	if err != nil {
		return nil, none, err
	}
	j := media.Job{Path: path, Media: m, Audio: -1, Burn: -1}
	ctx := context.Background()
	var results []d3Result
	var best d3Result
	best.Tier = -1
	score := func(r d3Result) int { // both kinds > Old 3DS > New 3DS
		n := 0
		if r.Old >= config.Depth3DMin {
			n += 2
		}
		if r.New >= config.Depth3DMin {
			n++
		}
		return n
	}
	try := func(tier int) d3Result {
		r := d3Result{Tier: tier}
		name := "MiDaS"
		if tier > 0 {
			name = fmt.Sprintf("depth size %d", tier)
		}
		d3Step("Loading the %s model (the first load can take a while)", name)
		prev := s.Depth3D.Tier()
		s.Depth3D.SetTier(tier)
		defer s.Depth3D.SetTier(prev)
		r.Fps, r.Device, err = s.Depth3D.Bench(tier, 120)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		if r.Fps < 24 { // can't even keep up with film on its own
			return r
		}
		conv := func(w, h, ow, oh int) (media.Converter3D, error) { return s.Depth3D.Converter(w, h, ow, oh) }
		dur := int64(media.SpeedClipMs)
		if s.Media.MoflexEncoder() != "" {
			d3Step("Old 3DS stream with %s", name)
			if r.Old, err = s.Media.Speed3D(ctx, j, true, conv, dur, 60*time.Second); err != nil {
				r.Error = err.Error()
			}
		}
		d3Step("New 3DS stream with %s", name)
		if r.New, err = s.Media.Speed3D(ctx, j, false, conv, dur, 60*time.Second); err != nil && r.Error == "" {
			r.Error = err.Error()
		}
		return r
	}
	st := s.Depth3D.Pk.Status()
	for _, t := range d3Tiers {
		if !hasTier(st.Models, t) {
			continue
		}
		r := try(t)
		results = append(results, r)
		if score(r) > score(best) {
			best = r
		}
		if score(r) == 3 {
			break
		}
	}
	if score(best) == 0 { // no Distill size can: MiDaS, the light one
		if !st.MiDaS {
			d3Step("Downloading MiDaS (the lighter model)")
			if err := s.Depth3D.Pk.Install(d3Tiers, true); err != nil {
				return results, none, err
			}
		}
		r := try(0)
		results = append(results, r)
		if score(r) > 0 {
			best = r
		}
	}
	if score(best) == 0 {
		// nothing fast enough: keep the size that came closest, so a 3DS can still turn 2D to 3D
		// on by hand (it warns that this computer tested too slow); none ran at all = off
		best = none
		for _, r := range results {
			if r.Error == "" && r.Fps > 0 && (best.Tier < 0 || max(r.Old, r.New) > max(best.Old, best.New)) {
				best = r
			}
		}
	}
	return results, best, nil
}

// webMin: what a browser stream must manage on the test clip. The clip is H.264; a real film is
// often HEVC, and the browser's bigger eyes take every core while FFmpeg decodes it -- measured on
// an M4, a 1080p HEVC film streamed at ~0.6x the clip's speed (448: 1.42x on the clip, 0.85x on the
// film; 392 streamed the film at full speed). The 3DS streams are far less affected (1.68x / 1.33x).
const webMin = 2.0

// depth3DWebTest: the finest size, from the 3DS's down, at which a browser stream keeps up (else the
// fastest tried).
func (s *Server) depth3DWebTest(from int) (int, float64, []d3Web) {
	path, err := s.Media.SpeedClip()
	if err != nil {
		return from, 0, nil
	}
	m, err := s.Media.Probe(path)
	if err != nil {
		return from, 0, nil
	}
	st := s.Depth3D.Pk.Status()
	var cand []int
	for _, t := range append(append([]int{}, d3Tiers...), 0) {
		if (t == 0 && st.MiDaS) || (t > 0 && t <= from && hasTier(st.Models, t)) || (from == 0 && t == 0) {
			cand = append(cand, t)
		}
	}
	var tried []d3Web
	best, bestSpd := from, 0.0
	for _, t := range cand {
		name := "MiDaS"
		if t > 0 {
			name = fmt.Sprintf("depth size %d", t)
		}
		d3Step("Loading the %s model", name)
		if _, _, err := s.Depth3D.Bench(t, 5); err != nil { // loaded before the timing (CoreML: ~10 s)
			log.Printf("2D to 3D browser test %s: %v", name, err)
			continue
		}
		d3Step("Browser stream with %s", name)
		tt := t
		conv := func(w, h, ow, oh int) (media.Converter3D, error) { return s.Depth3D.ConverterAt(tt, w, h, ow, oh) }
		spd, err := s.Media.SpeedWeb3D(context.Background(), m, path, conv, int64(media.SpeedClipMs), 60*time.Second, s.DataDir)
		if err != nil {
			log.Printf("2D to 3D browser test %s: %v", name, err)
			continue
		}
		tried = append(tried, d3Web{t, spd})
		if spd > bestSpd {
			best, bestSpd = t, spd
		}
		if spd >= webMin {
			return t, spd, tried
		}
	}
	return best, bestSpd, tried
}

func hasTier(s []int, t int) bool {
	for _, x := range s {
		if x == t {
			return true
		}
	}
	return false
}
