package moflex

import (
	"encoding/binary"
	"errors"
	"io"
)

// The contents of a super moflex trailer's extra tracks, for the browser player: subtitle text
// (SUB0/SUB1), the extra audio tracks (AUD1: IMA ADPCM in fixed-size packets, each starting from
// its own decoder state, so any time is a packet number away) and the alternate video versions
// (VID1: a whole block-only moflex).

// Section is one trailer section: its tag and where its data is.
type Section struct {
	Tag string
	Off int64 // the data, after the 8-byte header
	Len int64
}

// Sections lists a super moflex's trailer sections in file order.
func Sections(r io.ReaderAt, size int64) ([]Section, error) {
	if size < 16 {
		return nil, ErrNotSuper
	}
	tail := make([]byte, 16)
	if _, err := r.ReadAt(tail, size-16); err != nil {
		return nil, err
	}
	if string(tail[8:]) != trailerMagic {
		return nil, ErrNotSuper
	}
	off, end := int64(binary.LittleEndian.Uint64(tail[:8])), size-16
	if off < 0 || off >= end {
		return nil, errors.New("super moflex: bad trailer offset")
	}
	var out []Section
	hdr := make([]byte, 8)
	for off+8 <= end {
		if _, err := r.ReadAt(hdr, off); err != nil {
			return nil, err
		}
		n := int64(binary.LittleEndian.Uint32(hdr[4:]))
		if n < 0 || off+8+n > end {
			return nil, errors.New("super moflex: a section runs past the trailer")
		}
		out = append(out, Section{string(hdr[:4]), off + 8, n})
		off += 8 + n
	}
	return out, nil
}

// pick returns the k-th section with one of the tags.
func pick(secs []Section, k int, tags ...string) (Section, bool) {
	for _, s := range secs {
		for _, t := range tags {
			if s.Tag == t {
				if k == 0 {
					return s, true
				}
				k--
			}
		}
	}
	return Section{}, false
}

// SubText returns the k-th subtitle track's file (.srt or .ass bytes).
func SubText(r io.ReaderAt, size int64, k int) ([]byte, error) {
	secs, err := Sections(r, size)
	if err != nil {
		return nil, err
	}
	s, ok := pick(secs, k, "SUB0", "SUB1")
	if !ok || s.Len > 16<<20 {
		return nil, errors.New("no such subtitle track")
	}
	b := make([]byte, s.Len)
	if _, err := r.ReadAt(b, s.Off); err != nil {
		return nil, err
	}
	if s.Tag == "SUB1" {
		if len(b) < 4 {
			return nil, errors.New("bad subtitle track")
		}
		b = b[4:]
	}
	return b, nil
}

// Version returns the k-th alternate video version (VID1, from 0) as a moflex of its own.
func Version(r io.ReaderAt, size int64, k int) (*io.SectionReader, error) {
	secs, err := Sections(r, size)
	if err != nil {
		return nil, err
	}
	s, ok := pick(secs, k, "VID1")
	if !ok || s.Len <= 8 {
		return nil, errors.New("no such video version")
	}
	return io.NewSectionReader(r, s.Off+8, s.Len-8), nil
}

// AltAudio is an extra audio track (AUD1).
type AltAudio struct {
	Rate, Channels, PktSamples int
	r                          io.ReaderAt
	off, n                     int64 // the packets
}

// Audio returns the k-th extra audio track (AUD1, from 0).
func Audio(r io.ReaderAt, size int64, k int) (*AltAudio, error) {
	secs, err := Sections(r, size)
	if err != nil {
		return nil, err
	}
	s, ok := pick(secs, k, "AUD1")
	if !ok || s.Len < 12 {
		return nil, errors.New("no such audio track")
	}
	h := make([]byte, 12)
	if _, err := r.ReadAt(h, s.Off); err != nil {
		return nil, err
	}
	a := &AltAudio{Rate: int(binary.LittleEndian.Uint32(h)), Channels: int(binary.LittleEndian.Uint16(h[4:])),
		PktSamples: int(binary.LittleEndian.Uint16(h[6:])), r: r, off: s.Off + 12, n: s.Len - 12}
	if a.Rate <= 0 || a.Channels <= 0 || a.Channels > 8 || a.PktSamples <= 0 || a.PktSamples%256 != 0 {
		return nil, errors.New("bad audio track header")
	}
	return a, nil
}

// pktBytes: a full packet's size -- a state header per channel, then 4-bit samples.
func (a *AltAudio) pktBytes() int64 { return int64(4*a.Channels + a.PktSamples/2*a.Channels) }

var imaStep = [89]int{7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34, 37, 41, 45,
	50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143, 157, 173, 190, 209, 230, 253, 279, 307, 337,
	371, 408, 449, 494, 544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411, 1552, 1707, 1878,
	2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026, 4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630,
	9493, 10442, 11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767}
var imaIdx = [8]int{-1, -1, -1, -1, 2, 4, 6, 8}

// PCM streams the track as 16-bit little-endian interleaved samples from startMs.
func (a *AltAudio) PCM(startMs int64) io.Reader {
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(a.decode(startMs, pw)) }()
	return pr
}

func (a *AltAudio) decode(startMs int64, w io.Writer) error {
	first := startMs * int64(a.Rate) / 1000 // the first sample wanted
	pkt := first / int64(a.PktSamples)
	skip := int(first - pkt*int64(a.PktSamples))
	pb := a.pktBytes()
	buf := make([]byte, pb)
	ch := a.Channels
	out := make([]byte, a.PktSamples*ch*2)
	for off := pkt * pb; off < a.n; off += pb {
		n := pb
		if off+n > a.n {
			n = a.n - off // the last packet can be shorter
		}
		if _, err := a.r.ReadAt(buf[:n], a.off+off); err != nil && err != io.EOF {
			return err
		}
		if n < int64(4*ch) {
			return nil
		}
		pred, step := make([]int, ch), make([]int, ch)
		for c := 0; c < ch; c++ {
			step[c] = int(int16(binary.LittleEndian.Uint16(buf[4*c:])))
			pred[c] = int(int16(binary.LittleEndian.Uint16(buf[4*c+2:])))
			if step[c] < 0 || step[c] > 88 {
				step[c] = 0
			}
		}
		data := buf[4*ch : n]
		samples := len(data) / ch * 2 / 256 * 256 // per channel, whole 256-sample subframes
		p := 0
		for sf := 0; sf < samples/256; sf++ {
			for c := 0; c < ch; c++ {
				for k := 0; k < 128; k++ {
					b := data[p]
					p++
					for h, nib := range [2]int{int(b & 15), int(b >> 4)} {
						st := imaStep[step[c]]
						d := ((2*(nib&7) + 1) * st) >> 3
						if nib&8 != 0 {
							pred[c] -= d
						} else {
							pred[c] += d
						}
						if pred[c] > 32767 {
							pred[c] = 32767
						} else if pred[c] < -32768 {
							pred[c] = -32768
						}
						step[c] += imaIdx[nib&7]
						if step[c] < 0 {
							step[c] = 0
						} else if step[c] > 88 {
							step[c] = 88
						}
						i := (sf*256 + 2*k + h) * ch
						binary.LittleEndian.PutUint16(out[2*(i+c):], uint16(int16(pred[c])))
					}
				}
			}
		}
		o := out[:samples*ch*2]
		if skip > 0 {
			if skip*ch*2 >= len(o) {
				skip -= samples
				continue
			}
			o = o[skip*ch*2:]
			skip = 0
		}
		if _, err := w.Write(o); err != nil {
			return err
		}
	}
	return nil
}
