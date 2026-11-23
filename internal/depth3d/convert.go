package depth3d

import (
	"math"
)

// Settings: how strong and where. The defaults were tuned on an Old 3DS against iw3's settings for
// the 3DS (strength 1.7, convergence 0.65, edge fix, flicker reduction 0.99).
type Settings struct {
	Strength float32 // % of the picture's width between the eyes at the nearest point
	Conv     float32 // the depth (0 far .. 1 near) that sits on the screen; nearer pops out
	Edge     int     // edge fix: near depth grows this many pixels sideways, so an outline moves with its object
	Still    float32 // depth smoothing where the picture didn't change (share of the new depth kept); 0 = flat 0.6
	EMA      float32 // smoothing of the depth range (no pumping)
	Border   float32 // depth fades to the screen over this share of the width at the left/right edges
}

var Defaults = Settings{Strength: 1.7, Conv: 0.65, Edge: 3, Still: 0.2, EMA: 0.99, Border: 0.04}

// ImageNet normalisation, which both depth models (Distill-Any-Depth, MiDaS) were trained with
var mean = [3]float32{0.485, 0.456, 0.406}
var std = [3]float32{0.229, 0.224, 0.225}

// Converter makes the two eyes for one video's frames (it keeps state between frames).
type Converter struct {
	S      Settings
	sess   *Session
	W, H   int       // the picture (inside 400x240)
	small  []float32 // this frame at the model's size, RGB 0..1
	prevSm []float32
	depth  []float32 // model output
	prev   []float32 // smoothed depth of the last frame
	full   []float32 // depth at the picture's size
	tmp    []float32
	lo, hi float32
	have   bool
	hist   []int
}

func NewConverter(s *Session, w, h int, set Settings) *Converter {
	n := s.H * s.W
	return &Converter{S: set, sess: s, W: w, H: h, small: make([]float32, n*3), prevSm: make([]float32, n*3),
		depth: make([]float32, n), prev: make([]float32, n), full: make([]float32, w*h), tmp: make([]float32, w*h),
		hist: make([]int, 2048)}
}

// Frame turns one picture (RGB24, W x H) into the two eyes, written into sbs (RGB24, 800x240,
// left eye first, the picture centred in each 400x240 half; the bars are left as they are).
func (c *Converter) Frame(img []byte, sbs []byte) error {
	mw, mh := c.sess.W, c.sess.H
	resizeRGB(img, c.W, c.H, c.small, mw, mh)
	c.sess.mu.Lock()
	in := c.sess.Input()
	plane := mw * mh
	for i := 0; i < plane; i++ {
		for k := 0; k < 3; k++ {
			in[k*plane+i] = (c.small[i*3+k] - mean[k]) / std[k]
		}
	}
	err := c.sess.Run(c.depth)
	c.sess.mu.Unlock()
	if err != nil {
		return err
	}
	// a scene cut (the picture changed a lot): no smoothing across it
	cut := !c.have
	if !cut {
		var sum float64
		for i := range c.small {
			sum += math.Abs(float64(c.small[i] - c.prevSm[i]))
		}
		cut = sum/float64(len(c.small)) > 0.18
	}
	l, h := c.percentiles(c.depth, 0.02, 0.98)
	if cut {
		c.lo, c.hi = l, h
	} else {
		e := c.S.EMA
		c.lo, c.hi = c.lo*e+l*(1-e), c.hi*e+h*(1-e)
	}
	span := c.hi - c.lo
	if span < 1e-6 {
		span = 1e-6
	}
	for i, d := range c.depth {
		v := (d - c.lo) / span
		c.depth[i] = clamp01(v)
	}
	if !cut {
		for i := range c.depth {
			w := float32(0.6)
			if c.S.Still > 0 { // still areas keep mostly the old depth (no wobble); moving ones take the new
				m := (abs32(c.small[i*3]-c.prevSm[i*3]) + abs32(c.small[i*3+1]-c.prevSm[i*3+1]) +
					abs32(c.small[i*3+2]-c.prevSm[i*3+2])) / 3
				w = c.S.Still + m*8
				if w > 1 {
					w = 1
				}
			}
			c.depth[i] = c.depth[i]*w + c.prev[i]*(1-w)
		}
	}
	copy(c.prev, c.depth)
	c.small, c.prevSm = c.prevSm, c.small
	c.have = true

	resizeF(c.depth, mw, mh, c.full, c.W, c.H)
	if c.S.Edge > 0 {
		dilateH(c.full, c.tmp, c.W, c.H, c.S.Edge)
	}
	// disparity: total pixels between the eyes; + = in front of the screen. 0 at the side edges.
	k := c.S.Strength / 100 * float32(c.W)
	bw := float32(c.W) * c.S.Border
	if bw < 1 {
		bw = 1
	}
	for y := 0; y < c.H; y++ {
		row := c.full[y*c.W : (y+1)*c.W]
		for x := range row {
			ramp := float32(1)
			if c.S.Border > 0 {
				e := float32(min(x, c.W-1-x))
				if e < bw {
					ramp = e / bw
				}
			}
			row[x] = k * (row[x] - c.S.Conv) * ramp
		}
	}
	px, py := (400-c.W)/2, (240-c.H)/2
	warpBackward(img, c.full, c.W, c.H, sbs, 800, px, py, -1)     // left eye
	warpBackward(img, c.full, c.W, c.H, sbs, 800, 400+px, py, +1) // right eye
	return nil
}

// percentiles from a histogram (exact enough, and no sort)
func (c *Converter) percentiles(d []float32, a, b float32) (float32, float32) {
	lo, hi := d[0], d[0]
	for _, v := range d {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if hi <= lo {
		return lo, lo + 1e-6
	}
	for i := range c.hist {
		c.hist[i] = 0
	}
	n := len(c.hist)
	sc := float32(n-1) / (hi - lo)
	for _, v := range d {
		c.hist[int((v-lo)*sc)]++
	}
	at := func(q float32) float32 {
		want, acc := int(q*float32(len(d))), 0
		for i, h := range c.hist {
			acc += h
			if acc > want {
				return lo + (float32(i)+0.5)/sc
			}
		}
		return hi
	}
	return at(a), at(b)
}

// warpBackward writes one eye: each output pixel takes the picture at x + sign*disp/2 (linear).
func warpBackward(img []byte, disp []float32, w, h int, dst []byte, dw, ox, oy int, sign float32) {
	for y := 0; y < h; y++ {
		src := img[y*w*3 : (y+1)*w*3]
		out := dst[((oy+y)*dw+ox)*3:]
		drow := disp[y*w : (y+1)*w]
		for x := 0; x < w; x++ {
			sx := float32(x) + sign*drow[x]*0.5
			if sx < 0 {
				sx = 0
			}
			if sx > float32(w-1) {
				sx = float32(w - 1)
			}
			x0 := int(sx)
			x1 := x0 + 1
			if x1 > w-1 {
				x1 = w - 1
			}
			t := sx - float32(x0)
			for k := 0; k < 3; k++ {
				v := float32(src[x0*3+k])*(1-t) + float32(src[x1*3+k])*t
				out[x*3+k] = byte(v + 0.5)
			}
		}
	}
}

// dilateH: near (bigger) depth spreads r pixels left and right
func dilateH(d, tmp []float32, w, h, r int) {
	copy(tmp, d)
	for y := 0; y < h; y++ {
		row, src := d[y*w:(y+1)*w], tmp[y*w:(y+1)*w]
		for x := range row {
			m := src[x]
			for k := 1; k <= r; k++ {
				if x-k >= 0 && src[x-k] > m {
					m = src[x-k]
				}
				if x+k < w && src[x+k] > m {
					m = src[x+k]
				}
			}
			row[x] = m
		}
	}
}

// resizeRGB: bilinear (pixel centres), RGB24 -> float RGB 0..1
func resizeRGB(src []byte, sw, sh int, dst []float32, dw, dh int) {
	fx, fy := float32(sw)/float32(dw), float32(sh)/float32(dh)
	for y := 0; y < dh; y++ {
		sy := (float32(y)+0.5)*fy - 0.5
		y0, ty := split(sy, sh)
		y1 := min(y0+1, sh-1)
		for x := 0; x < dw; x++ {
			sx := (float32(x)+0.5)*fx - 0.5
			x0, tx := split(sx, sw)
			x1 := min(x0+1, sw-1)
			for k := 0; k < 3; k++ {
				a := float32(src[(y0*sw+x0)*3+k])*(1-tx) + float32(src[(y0*sw+x1)*3+k])*tx
				b := float32(src[(y1*sw+x0)*3+k])*(1-tx) + float32(src[(y1*sw+x1)*3+k])*tx
				dst[(y*dw+x)*3+k] = (a*(1-ty) + b*ty) / 255
			}
		}
	}
}

// resizeF: bilinear for one float plane
func resizeF(src []float32, sw, sh int, dst []float32, dw, dh int) {
	fx, fy := float32(sw)/float32(dw), float32(sh)/float32(dh)
	for y := 0; y < dh; y++ {
		sy := (float32(y)+0.5)*fy - 0.5
		y0, ty := split(sy, sh)
		y1 := min(y0+1, sh-1)
		for x := 0; x < dw; x++ {
			sx := (float32(x)+0.5)*fx - 0.5
			x0, tx := split(sx, sw)
			x1 := min(x0+1, sw-1)
			a := src[y0*sw+x0]*(1-tx) + src[y0*sw+x1]*tx
			b := src[y1*sw+x0]*(1-tx) + src[y1*sw+x1]*tx
			dst[y*dw+x] = a*(1-ty) + b*ty
		}
	}
}

func split(v float32, n int) (int, float32) {
	if v <= 0 {
		return 0, 0
	}
	i := int(v)
	if i >= n-1 {
		return n - 1, 0
	}
	return i, v - float32(i)
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Fit: the picture's size inside the 3DS top screen (400x240), even sides.
func Fit(sw, sh int) (int, int) {
	s := math.Min(400/float64(sw), 240/float64(sh))
	return int(float64(sw)*s) / 2 * 2, int(float64(sh)*s) / 2 * 2
}
