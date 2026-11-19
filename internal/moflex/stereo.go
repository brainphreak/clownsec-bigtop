package moflex

import "io"

// DetectStereo tells a 3D moflex from a 2D one by its first two seconds -- the player's own rule
// (moflex_demux.c mfx_detect_stereo): 3D interleaves the left and right eye frames, so video frames
// arrive at twice the declared display rate; Nintendo's 3D files instead declare the eye rate itself
// (47+ per second) and arrive at about that rate. ok is false when the file can't be read that far.
func DetectStereo(r io.ReaderAt, size int64) (stereo, ok bool) {
	n := int64(3 << 19) // 1.5 MB: two seconds even of a high-rate 3D file
	if size < n {
		n = size
	}
	buf := make([]byte, n)
	got, _ := r.ReadAt(buf, 0)
	d := &demux{b: buf[:got], vstream: -1}
	return d.stereo()
}

type demux struct {
	b        []byte
	p        int
	ts       int64
	size     int
	nstreams int
	vstream  int
	tbNum    int
	tbDen    int
	acc      map[int]int
	naudio   int // audio streams declared (counted once each, as they first appear)
	brLast   uint32
	brPos    int
}

func (d *demux) eof() bool { return d.p >= len(d.b) }
func (d *demux) r8() int {
	if d.eof() {
		return 0
	}
	v := int(d.b[d.p])
	d.p++
	return v
}
func (d *demux) rb16() int { return d.r8()<<8 | d.r8() }
func (d *demux) rb24() int { return d.rb16()<<8 | d.r8() }
func (d *demux) rb64() int64 {
	var v int64
	for i := 0; i < 8; i++ {
		v = v<<8 | int64(d.r8())
	}
	return v
}
func (d *demux) varByte() int {
	v := d.r8()
	if v&0x80 == 0 {
		return v
	}
	val := (v & 0x7f) << 7
	v = d.r8()
	if v&0x80 == 0 {
		return val | v
	}
	val = ((v & 0x7f) | val) << 7
	v = d.r8()
	if v&0x80 == 0 {
		return val | v
	}
	return (((v & 0x7f) | val) << 7) | d.r8()
}

// the packet-header bit reader (pop / pop_int / pop_length)
func (d *demux) pop() int {
	if d.eof() {
		return -1
	}
	if d.brPos&7 == 0 {
		d.brLast = uint32(d.r8()) << 24
	} else {
		d.brLast <<= 1
	}
	d.brPos++
	if d.brLast&0x80000000 != 0 {
		return 1
	}
	return 0
}
func (d *demux) popInt(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		b := d.pop()
		if b < 0 {
			return -1
		}
		v = 2*v + b
	}
	return v
}
func (d *demux) popLength() int {
	n := 1
	for {
		b := d.pop()
		if b < 0 {
			return -1
		}
		if b == 1 {
			return n
		}
		n++
	}
}

// sync: a block that starts with the 0x4C32 marker carries the time and the stream table
func (d *demux) sync() bool {
	if d.p+2 > len(d.b) || d.b[d.p] != 0x4C || d.b[d.p+1] != 0x32 {
		return false
	}
	d.p += 4
	d.ts = d.rb64()
	d.size = d.rb16() + 1
	for !d.eof() {
		typ, ssize := d.varByte(), d.varByte()
		idx := -1
		switch typ {
		case 0:
			d.p += ssize
			return true
		case 2: // audio
			idx = d.r8()
			if idx == d.nstreams {
				d.naudio++
			}
			d.p += 1 + 3 + 1
		case 1, 3: // video
			idx = d.r8()
			d.r8()
			den, num := d.rb16(), d.rb16()
			d.p += 4
			if typ == 3 {
				d.p += 3
			} else {
				d.p += 2
			}
			if d.vstream < 0 && idx == d.nstreams {
				d.vstream, d.tbNum, d.tbDen = idx, num, den
			}
		case 4:
			idx = d.r8()
			d.p++
		default:
			if ssize >= 1 {
				idx = d.r8()
				d.p += ssize - 1
			}
		}
		if idx == d.nstreams && idx < 16 {
			d.nstreams++
		}
	}
	return true
}

func (d *demux) stereo() (bool, bool) {
	d.acc = map[int]int{}
	vc, t0, haveSync := 0, int64(-1), false
	for !d.eof() {
		start := d.p
		if d.sync() {
			haveSync = true
		} else if !haveSync {
			return false, false // not a moflex
		}
		flags := d.r8()
		if flags&2 != 0 {
			d.p += 2
		}
		for d.p < start+d.size && !d.eof() {
			if d.b[d.p] == 0 { // the zero byte that ends the block's packets (consumed, as in the player)
				d.p++
				break
			}
			d.brPos, d.brLast = 0, 0
			bits := d.popLength()
			if bits < 0 {
				return false, false
			}
			si := d.popInt(bits)
			end := d.pop()
			if si < 0 || si >= d.nstreams || end < 0 {
				return false, false
			}
			if end == 1 {
				d.popInt(d.popLength())
				d.pop()
				d.popInt(d.popLength()*2 + 26)
			}
			psize := d.popInt(13) + 1
			if psize <= 0 || psize > d.size {
				return false, false
			}
			d.p += psize
			d.acc[si] += psize
			if end == 1 && d.acc[si] > 0 {
				d.acc[si] = 0
				if si == d.vstream {
					if t0 < 0 {
						t0 = d.ts
					}
					vc++
				}
			}
			if t0 >= 0 && d.ts-t0 >= 2000000 {
				return d.decide(vc, d.ts-t0)
			}
		}
		if flags%2 == 0 { // an even flags byte: the next block starts at this one's declared end
			d.p = start + d.size
		}
	}
	if t0 >= 0 && d.ts > t0 {
		return d.decide(vc, d.ts-t0)
	}
	return false, false
}

func (d *demux) decide(vc int, dt int64) (bool, bool) {
	if d.vstream < 0 || d.tbNum <= 0 || d.tbDen <= 0 || vc <= 4 {
		return false, false
	}
	tbfps := float64(d.tbDen) / float64(d.tbNum)
	dfps := float64(vc) / (float64(dt) / 1e6)
	switch {
	case dfps/tbfps > 1.5:
		return true, true
	case tbfps >= 47 && dfps/tbfps > 0.75:
		return true, true
	}
	return false, true
}
