package moflex

import (
	"encoding/binary"
	"errors"
	"io"
)

// Seeking in a moflex for a decoder that starts cold (FFmpeg, for the browser player): it can only
// start where a video keyframe begins at the top of a sync block, with no packet half-read. The
// file has no index, so Big Top finds such a place the way the 3DS player does -- sync blocks
// carry their time, a few probes narrow the byte range, and a short walk picks the start.

// Start is a place to begin decoding: the byte offset of a sync block and its time.
type Start struct {
	Off int64
	Us  int64
}

const maxTs = 100 * 3600 * 1000000 // a sync time beyond 100 hours is a false marker inside data

// syncAt finds the first real sync block at or after off (within 4 MB): its offset and time.
// It reads a little at a time -- sync blocks come about once a second, and on a network share
// every megabyte read is time the player waits.
func syncAt(r io.ReaderAt, size, off int64) (int64, int64, bool) {
	const chunk = 128 << 10
	buf := make([]byte, chunk+14)
	for done := int64(0); done < 4<<20; done += chunk {
		n, _ := r.ReadAt(buf, off+done)
		b := buf[:n]
		for i := 0; i+14 <= len(b) && i < chunk; i++ {
			if b[i] != 0x4C || b[i+1] != 0x32 {
				continue
			}
			ts := int64(binary.BigEndian.Uint64(b[i+4:]))
			if ts >= 0 && ts < maxTs {
				return off + done + int64(i), ts, true
			}
		}
		if n < len(buf) {
			break // the end of the file
		}
	}
	return 0, 0, false
}

// Duration is the time of the last sync block (0 when none is found near the end). It reads the
// end of the file once (a larger piece only if that holds no sync block).
func Duration(r io.ReaderAt, size int64) int64 {
	for back := int64(1 << 20); back <= 16<<20; back *= 4 {
		from := size - back
		if from < 0 {
			from = 0
		}
		buf := make([]byte, size-from)
		n, _ := r.ReadAt(buf, from)
		b := buf[:n]
		last := int64(-1)
		for i := 0; i+14 <= len(b); i++ {
			if b[i] == 0x4C && b[i+1] == 0x32 {
				if ts := int64(binary.BigEndian.Uint64(b[i+4:])); ts >= 0 && ts < maxTs && ts > last {
					last = ts
				}
			}
		}
		if last >= 0 {
			return last
		}
		if from == 0 {
			break
		}
	}
	return 0
}

// near: a sync block at or a little before t (interpolating on byte offsets, like the player).
func near(r io.ReaderAt, size, t int64) (int64, int64) {
	ao, at := int64(0), int64(0)
	bo, bt := size, int64(-1)
	for i := 0; i < 24 && bo-ao > 1<<20; i++ {
		g := ao + (bo-ao)/2
		if bt > at && t > at {
			g = ao + int64(float64(t-at)/float64(bt-at)*float64(bo-ao))
		}
		lo, hi := ao+(bo-ao)/16, bo-(bo-ao)/16
		if g < lo {
			g = lo
		}
		if g > hi {
			g = hi
		}
		o, ts, ok := syncAt(r, size, g)
		if !ok {
			bo = g
			continue
		}
		if ts <= t {
			ao, at = o, ts
		} else {
			bo, bt = g, ts
		}
	}
	return ao, at
}

// KeyStart finds where to start decoding to show time t (µs): the last clean keyframe start at
// or before t. A file that won't parse gives the beginning.
func KeyStart(r io.ReaderAt, size, t int64) (Start, error) {
	if t <= 0 {
		return Start{}, nil
	}
	// walk from well before t: keyframes can be several seconds apart
	for back := int64(12_000_000); back <= 96_000_000; back *= 2 {
		from := t - back
		if from < 0 {
			from = 0
		}
		off, _ := near(r, size, from)
		if s, ok := walkStarts(r, size, off, t); ok {
			return s, nil
		}
		if from == 0 {
			break
		}
	}
	return Start{}, errors.New("no keyframe found")
}

// walkStarts parses blocks from the sync block at off up to time t and returns the last sync block
// where nothing was half-read and the next video packet is a keyframe.
func walkStarts(r io.ReaderAt, size, off, t int64) (Start, bool) {
	// read only as far as t: up to the first sync block after it (plus a margin)
	n := int64(24 << 20)
	if end, _ := near(r, size, t+1_500_000); end > off {
		n = end - off + 1<<20
	}
	if off+n > size {
		n = size - off
	}
	buf := make([]byte, n)
	got, _ := r.ReadAt(buf, off)
	d := &demux{b: buf[:got], vstream: -1, acc: map[int]int{}}
	var best Start
	found := false
	// the sync block being checked: clean, and waiting for its first video packet
	var cand Start
	pending := false
	for !d.eof() {
		start := d.p
		if d.sync() {
			if d.ts > t {
				break
			}
			clean := true
			for _, v := range d.acc {
				if v != 0 {
					clean = false
				}
			}
			if clean {
				cand, pending = Start{off + int64(start), d.ts}, true
			} else {
				pending = false
			}
		} else if d.size == 0 {
			return Start{}, false // not at a sync block
		}
		flags := d.r8()
		if flags&2 != 0 {
			d.p += 2
		}
		for d.p < start+d.size && !d.eof() {
			if d.b[d.p] == 0 {
				d.p++
				break
			}
			d.brPos, d.brLast = 0, 0
			bits := d.popLength()
			if bits < 0 {
				return best, found
			}
			si := d.popInt(bits)
			end := d.pop()
			if si < 0 || si >= d.nstreams || end < 0 {
				return best, found
			}
			if end == 1 {
				d.popInt(d.popLength())
				d.pop()
				d.popInt(d.popLength()*2 + 26)
			}
			psize := d.popInt(13) + 1
			if psize <= 0 || psize > d.size || d.p+psize > len(d.b) {
				return best, found
			}
			if si == d.vstream && d.acc[si] == 0 && pending {
				// the first video packet after a clean sync block: a keyframe makes it a start
				if psize > 1 && d.b[d.p+1]&0x80 != 0 {
					best, found = cand, true
				}
				pending = false
			}
			d.p += psize
			d.acc[si] += psize
			if end == 1 {
				d.acc[si] = 0
			}
		}
		if flags%2 == 0 {
			d.p = start + d.size
		}
	}
	return best, found
}

// Info is what the browser player needs to know about a moflex.
type Info struct {
	DurUs  int64
	Stereo bool
	TbNum  int // the video time base (frame period) as declared
	TbDen  int
	TbEye  bool // a 3D file declaring the eye rate (Nintendo's): a pair lasts two periods
	Audios int  // audio streams inside the blocks (1 usually; an older way to add a language made 2)
}

// Probe reads a moflex's length, 3D-ness and frame rate.
func Probe(r io.ReaderAt, size int64) Info {
	in := Info{DurUs: Duration(r, size)}
	n := int64(3 << 19)
	if size < n {
		n = size
	}
	buf := make([]byte, n)
	got, _ := r.ReadAt(buf, 0)
	d := &demux{b: buf[:got], vstream: -1}
	st, ok := d.stereo()
	in.Stereo = ok && st
	in.TbNum, in.TbDen, in.Audios = d.tbNum, d.tbDen, d.naudio
	if in.Stereo && d.tbNum > 0 && float64(d.tbDen)/float64(d.tbNum) >= 47 {
		in.TbEye = true
	}
	return in
}
