// Package moflex reads what Big Top needs from a .moflex file without decoding any video:
// the SUPER MOFLEX trailer (library info, poster, track languages) that make_super_moflex.py
// appends after the last moflex block.
//
// Trailer layout (moflex_addstreams.py):
//
//	[moflex blocks][sections...][u64 LE payload offset]["CSXTRA01"]
//	section: [4cc][u32 LE length][data]
//	  NFO0  key=value lines: title year category genres runtime date is3d format desc showdesc
//	  ART5  u16 width, u16 height, then width*height RGB565 LE pixels (132x188)
//	  LNG0  char[4] language of the in-band audio ("ENG")
//	  SUB0  .srt bytes (old)       SUB1  char[4] language, then .srt/.ass bytes
//	  AUD1  u32 rate, u16 channels, u16 samples/packet, char[4] language, ADPCM packets
//
// Only section headers are read for SUB/AUD, so a file on a network share costs a few small reads.
package moflex

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const trailerMagic = "CSXTRA01"

// Super is what a super moflex trailer carries.
type Super struct {
	Info      map[string]string // NFO0 fields
	Poster    image.Image       // ART5, nil if none
	AudioLang string            // LNG0: the in-band track
	AltAudio  string            // AUD1's language: the second audio track ("" = none)
	SubLangs  []string          // SUB1 languages, in file order ("SUB" for SUB0)
}

// Title etc. read NFO0 fields with the conversions every caller wants.
func (s *Super) Field(k string) string { return strings.TrimSpace(s.Info[k]) }
func (s *Super) Int(k string) int      { n, _ := strconv.Atoi(s.Field(k)); return n }

// ErrNotSuper means the file has no CSXTRA trailer (a plain moflex).
var ErrNotSuper = errors.New("not a super moflex")

// ReadSuper parses the trailer of a moflex of the given size.
func ReadSuper(r io.ReaderAt, size int64) (*Super, error) {
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
	off := int64(binary.LittleEndian.Uint64(tail[:8]))
	end := size - 16
	if off < 0 || off >= end {
		return nil, errors.New("super moflex: bad trailer offset")
	}
	s := &Super{Info: map[string]string{}}
	hdr := make([]byte, 8)
	for off+8 <= end {
		if _, err := r.ReadAt(hdr, off); err != nil {
			return nil, err
		}
		tag := string(hdr[:4])
		n := int64(binary.LittleEndian.Uint32(hdr[4:]))
		data := off + 8
		if n < 0 || data+n > end {
			return nil, errors.New("super moflex: section " + tag + " runs past the trailer")
		}
		switch tag {
		case "NFO0":
			if n > 1<<20 {
				break
			}
			b := make([]byte, n)
			if _, err := r.ReadAt(b, data); err != nil {
				return nil, err
			}
			for _, ln := range strings.Split(string(b), "\n") {
				if k, v, ok := strings.Cut(ln, "="); ok {
					s.Info[strings.TrimSpace(k)] = strings.TrimRight(v, "\r")
				}
			}
		case "ART5":
			if n < 4 || n > 4<<20 {
				break
			}
			b := make([]byte, n)
			if _, err := r.ReadAt(b, data); err != nil {
				return nil, err
			}
			s.Poster = rgb565(b)
		case "LNG0":
			b := make([]byte, 4)
			if n >= 4 {
				if _, err := r.ReadAt(b, data); err == nil {
					s.AudioLang = cstr(b)
				}
			}
		case "SUB0":
			s.SubLangs = append(s.SubLangs, "SUB")
		case "SUB1":
			b := make([]byte, 4)
			if n >= 4 {
				if _, err := r.ReadAt(b, data); err == nil {
					s.SubLangs = append(s.SubLangs, cstr(b))
				}
			}
		case "AUD1":
			b := make([]byte, 12)
			if n >= 12 {
				if _, err := r.ReadAt(b, data); err == nil {
					s.AltAudio = cstr(b[8:12])
				}
			}
		}
		off = data + n
	}
	return s, nil
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

// rgb565 turns an ART5 payload (u16 w, u16 h, pixels) into an image.
func rgb565(b []byte) image.Image {
	w := int(binary.LittleEndian.Uint16(b[0:2]))
	h := int(binary.LittleEndian.Uint16(b[2:4]))
	px := b[4:]
	if w <= 0 || h <= 0 || len(px) < w*h*2 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		v := binary.LittleEndian.Uint16(px[i*2:])
		r := uint8((v >> 11) & 0x1f)
		g := uint8((v >> 5) & 0x3f)
		bl := uint8(v & 0x1f)
		img.SetRGBA(i%w, i/w, color.RGBA{r<<3 | r>>2, g<<2 | g>>4, bl<<3 | bl>>2, 255})
	}
	return img
}

// make_super_moflex writes an episode's summary as `S1.E2 “Episode Title” - synopsis`.
var reEpDesc = regexp.MustCompile(`^S(\d{1,2})\.E(\d{1,3})\s*(?:[“"](.*?)[”"])?\s*(?:-\s*)?`)

// EpisodeFromDesc reads season, episode and episode title out of such a summary, and returns the
// synopsis without that prefix. ok = the prefix was there.
func EpisodeFromDesc(desc string) (season, episode int, title, rest string, ok bool) {
	m := reEpDesc.FindStringSubmatchIndex(desc)
	if m == nil {
		return 0, 0, "", desc, false
	}
	season, _ = strconv.Atoi(desc[m[2]:m[3]])
	episode, _ = strconv.Atoi(desc[m[4]:m[5]])
	if m[6] >= 0 {
		title = strings.TrimSpace(desc[m[6]:m[7]])
	}
	return season, episode, title, strings.TrimSpace(desc[m[1]:]), true
}
