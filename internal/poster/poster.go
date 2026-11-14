// Package poster stores artwork in Big Top's data folder and serves it at the size a client asks
// for. Posters are kept as JPEG at their original size; scaled copies are cached beside them.
package poster

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // decode PNG uploads and ART5 conversions
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

func Open(dir string) (*Store, error) {
	d := filepath.Join(dir, "posters")
	if err := os.MkdirAll(filepath.Join(d, "sized"), 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: d}, nil
}

// SaveImage stores img as name.jpg; returns the file name.
func (s *Store) SaveImage(name string, img image.Image) (string, error) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		return "", err
	}
	return s.save(name, b.Bytes())
}

// SaveBytes stores an encoded image (JPEG or PNG, e.g. a download or an upload) as name.jpg.
func (s *Store) SaveBytes(name string, data []byte) (string, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if format == "jpeg" {
		return s.save(name, data)
	}
	return s.SaveImage(name, img)
}

func (s *Store) save(name string, jpg []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn := name + ".jpg"
	tmp := filepath.Join(s.dir, fn+".tmp")
	if err := os.WriteFile(tmp, jpg, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, filepath.Join(s.dir, fn)); err != nil {
		return "", err
	}
	// a new poster under the same name: drop the scaled copies of the old one
	old, _ := filepath.Glob(filepath.Join(s.dir, "sized", name+"_*.jpg"))
	for _, o := range old {
		os.Remove(o)
	}
	return fn, nil
}

// Sized returns the poster file scaled to exactly w x h (stretched, never cropped -- the
// Clownsec artwork convention), cached on disk. w or h 0 = the original.
func (s *Store) Sized(fn string, w, h int) (string, error) {
	if fn == "" || filepath.Base(fn) != fn {
		return "", errors.New("no poster")
	}
	src := filepath.Join(s.dir, fn)
	if w <= 0 || h <= 0 || w > 1000 || h > 1500 {
		return src, nil
	}
	name := fn[:len(fn)-len(filepath.Ext(fn))]
	out := filepath.Join(s.dir, "sized", name+"_"+strconv.Itoa(w)+"x"+strconv.Itoa(h)+".jpg")
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, Scale(img, w, h), &jpeg.Options{Quality: 88}); err != nil {
		return "", err
	}
	if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
		return "", err
	}
	return out, nil
}

// Scale resizes img to w x h with bilinear filtering (box-averaged when shrinking a lot).
func Scale(img image.Image, w, h int) image.Image {
	src := image.NewRGBA(img.Bounds())
	draw.Draw(src, src.Bounds(), img, img.Bounds().Min, draw.Src)
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	// halve first while far too big: plain bilinear skips pixels and shimmers
	for sw >= w*2 && sh >= h*2 {
		src = half(src)
		sw, sh = src.Bounds().Dx(), src.Bounds().Dy()
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		fy := (float64(y)+0.5)*float64(sh)/float64(h) - 0.5
		y0 := clamp(int(fy), 0, sh-1)
		y1 := clamp(y0+1, 0, sh-1)
		wy := fy - float64(int(fy))
		if fy < 0 {
			wy = 0
		}
		for x := 0; x < w; x++ {
			fx := (float64(x)+0.5)*float64(sw)/float64(w) - 0.5
			x0 := clamp(int(fx), 0, sw-1)
			x1 := clamp(x0+1, 0, sw-1)
			wx := fx - float64(int(fx))
			if fx < 0 {
				wx = 0
			}
			a, b := src.RGBAAt(x0, y0), src.RGBAAt(x1, y0)
			c, d := src.RGBAAt(x0, y1), src.RGBAAt(x1, y1)
			mix := func(p, q, r, t uint8) uint8 {
				top := float64(p)*(1-wx) + float64(q)*wx
				bot := float64(r)*(1-wx) + float64(t)*wx
				return uint8(top*(1-wy) + bot*wy + 0.5)
			}
			dst.SetRGBA(x, y, color.RGBA{mix(a.R, b.R, c.R, d.R), mix(a.G, b.G, c.G, d.G), mix(a.B, b.B, c.B, d.B), 255})
		}
	}
	return dst
}

func half(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx()/2, src.Bounds().Dy()/2
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p, q := src.RGBAAt(2*x, 2*y), src.RGBAAt(2*x+1, 2*y)
			r, t := src.RGBAAt(2*x, 2*y+1), src.RGBAAt(2*x+1, 2*y+1)
			dst.SetRGBA(x, y, color.RGBA{
				uint8((int(p.R) + int(q.R) + int(r.R) + int(t.R) + 2) / 4),
				uint8((int(p.G) + int(q.G) + int(r.G) + int(t.G) + 2) / 4),
				uint8((int(p.B) + int(q.B) + int(r.B) + int(t.B) + 2) / 4), 255})
		}
	}
	return dst
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Path is the full path of a stored poster (for serving the original).
func (s *Store) Path(fn string) string { return filepath.Join(s.dir, filepath.Base(fn)) }
