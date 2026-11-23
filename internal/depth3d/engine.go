package depth3d

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Engine runs 2D to 3D for Big Top's streams: the installed packages, the chosen depth size, and
// the loaded models (kept loaded: CoreML takes ~10 s to load one, so the first is loaded ahead,
// in the background, and every later stream starts at once).
type Engine struct {
	Pk *Packages

	mu   sync.Mutex
	tier int // Distill input width (322/392/448), 0 = MiDaS, -1 = off
	rt   *Runtime
	sess map[string]*Session // by model file
}

func NewEngine(pk *Packages) *Engine { return &Engine{Pk: pk, tier: -1, sess: map[string]*Session{}} }

// SetTier picks the depth size (-1 = 2D to 3D off).
func (e *Engine) SetTier(t int) {
	e.mu.Lock()
	e.tier = t
	e.mu.Unlock()
}

func (e *Engine) Tier() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tier
}

// Ready: 2D to 3D is on and its packages are here.
func (e *Engine) Ready() bool {
	t := e.Tier()
	if t < 0 || e.Pk.LibPath() == "" {
		return false
	}
	_, _, ok := e.Pk.Model(t, 16, 9)
	return ok
}

// session: the loaded model for tier and a w x h picture (loading it the first time)
func (e *Engine) session(tier, w, h int) (*Session, error) {
	m, path, ok := e.Pk.Model(tier, w, h)
	if !ok {
		return nil, errors.New("the 2D-to-3D model isn't installed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if s := e.sess[path]; s != nil {
		return s, nil
	}
	if e.rt == nil {
		lib := e.Pk.LibPath()
		if lib == "" {
			return nil, errors.New("ONNX Runtime isn't installed")
		}
		rt, err := Load(lib)
		if err != nil {
			return nil, err
		}
		rt.CacheDir = filepath.Join(e.Pk.Dir, "coreml-cache")
		rt.Units = "CPUAndGPU" // as fast as with the Neural Engine on an M4, and quicker to load
		e.rt = rt
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t0 := time.Now()
	s, err := e.rt.NewSession(b, m.Input, m.Output, m.H, m.W, true)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", m.File.File, err)
	}
	log.Printf("2D to 3D: %s on %s (loaded in %v)", m.File.File, s.Device, time.Since(t0).Round(100*time.Millisecond))
	e.sess[path] = s
	return s, nil
}

// Converter: a converter for one stream of a w x h picture into an outW x outH side-by-side frame.
func (e *Engine) Converter(w, h, outW, outH int) (*Converter, error) {
	return e.ConverterAt(e.Tier(), w, h, outW, outH)
}

// ConverterAt: the same at a given depth size (the browser has its own).
func (e *Engine) ConverterAt(t, w, h, outW, outH int) (*Converter, error) {
	if t < 0 {
		return nil, errors.New("2D to 3D is off")
	}
	s, err := e.session(t, w, h)
	if err != nil {
		return nil, err
	}
	return NewConverterSize(s, w, h, outW, outH, Defaults), nil
}

// Preload loads the widescreen models in the background (the shape most videos have): the 3DS's
// depth size and any others given (the browser's).
func (e *Engine) Preload(more ...int) {
	if !e.Ready() {
		return
	}
	go func() {
		for _, t := range append([]int{e.Tier()}, more...) {
			if t < 0 {
				continue
			}
			if _, err := e.session(t, 400, 224); err != nil {
				log.Printf("2D to 3D: %v", err)
			}
		}
	}()
}

// Device: what runs the models ("" = nothing loaded yet).
func (e *Engine) Device() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.sess {
		return s.Device
	}
	return ""
}

// Bench: how many frames a second the converter manages at tier on this computer (a widescreen
// 400x224 picture, generated and moving; the model's cost doesn't depend on what is in it). The
// model stays loaded afterwards.
func (e *Engine) Bench(tier, frames int) (fps float64, device string, err error) {
	const w, h = 400, 224
	s, err := e.session(tier, w, h)
	if err != nil {
		return 0, "", err
	}
	c := NewConverter(s, w, h, Defaults)
	img := make([]byte, w*h*3)
	sbs := make([]byte, 800*240*3)
	var tot time.Duration
	for i := 0; i < frames+5; i++ {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := byte((x + y + i*3) & 255)
				if x > 100+i%200 && x < 180+i%200 && y > 60 && y < 160 {
					v = 255 - v
				}
				p := (y*w + x) * 3
				img[p], img[p+1], img[p+2] = v, byte(y), byte(x)
			}
		}
		t := time.Now()
		if err := c.Frame(img, sbs); err != nil {
			return 0, s.Device, err
		}
		if i >= 5 { // the first few warm the GPU up
			tot += time.Since(t)
		}
	}
	return float64(frames) / tot.Seconds(), s.Device, nil
}

// NeedFps: what a depth size must manage to be picked -- a 30 fps video (the most a stream sends)
// with room for the encoder, which runs at the same time on the same computer.
const NeedFps = 40.0
