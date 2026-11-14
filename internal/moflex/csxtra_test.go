package moflex

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildTrailer appends sections + the CSXTRA footer the way moflex_addstreams.py does.
func buildTrailer(body []byte, sections ...[]byte) []byte {
	out := append([]byte{}, body...)
	base := uint64(len(out))
	for _, s := range sections {
		out = append(out, s...)
	}
	var foot [16]byte
	binary.LittleEndian.PutUint64(foot[:8], base)
	copy(foot[8:], trailerMagic)
	return append(out, foot[:]...)
}

func section(tag string, data []byte) []byte {
	var h [8]byte
	copy(h[:4], tag)
	binary.LittleEndian.PutUint32(h[4:], uint32(len(data)))
	return append(h[:], data...)
}

func TestReadSuper(t *testing.T) {
	nfo := "title=A Charlie Brown Christmas\nyear=1965\ncategory=Movies\nis3d=1\nformat=converted\ndesc=Snow.\n"
	art := make([]byte, 4+2*2*3)
	binary.LittleEndian.PutUint16(art[0:], 2)
	binary.LittleEndian.PutUint16(art[2:], 3)
	binary.LittleEndian.PutUint16(art[4:], 0xF800) // first pixel pure red
	aud := append([]byte{0x44, 0xAC, 0, 0, 2, 0, 0, 4}, []byte("JPN\x00")...)
	aud = append(aud, make([]byte, 100)...)
	file := buildTrailer(make([]byte, 1000),
		section("NFO0", []byte(nfo)),
		section("ART5", art),
		section("LNG0", []byte("ENG\x00")),
		section("SUB1", append([]byte("SPA\x00"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHola\n")...)),
		section("AUD1", aud))
	s, err := ReadSuper(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	if s.Field("title") != "A Charlie Brown Christmas" || s.Int("year") != 1965 || s.Int("is3d") != 1 {
		t.Fatalf("info: %+v", s.Info)
	}
	if s.AudioLang != "ENG" || s.AltAudio != "JPN" || len(s.SubLangs) != 1 || s.SubLangs[0] != "SPA" {
		t.Fatalf("langs: %q %q %v", s.AudioLang, s.AltAudio, s.SubLangs)
	}
	if s.Poster == nil || s.Poster.Bounds().Dx() != 2 || s.Poster.Bounds().Dy() != 3 {
		t.Fatalf("poster: %v", s.Poster)
	}
	if r, g, b, _ := s.Poster.At(0, 0).RGBA(); r>>8 != 255 || g != 0 || b != 0 {
		t.Fatalf("pixel: %d %d %d", r>>8, g>>8, b>>8)
	}
}

func TestPlainMoflex(t *testing.T) {
	if _, err := ReadSuper(bytes.NewReader(make([]byte, 500)), 500); err != ErrNotSuper {
		t.Fatalf("want ErrNotSuper, got %v", err)
	}
}
