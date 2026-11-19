package moflex

import (
	"io"
	"os"
	"testing"
)

// MOFLEX_TEST_FILE=<a super moflex> go test -run Tracks -v ./internal/moflex/
func TestTracks(t *testing.T) {
	p := os.Getenv("MOFLEX_TEST_FILE")
	if p == "" {
		t.Skip("set MOFLEX_TEST_FILE")
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	secs, err := Sections(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secs {
		t.Logf("%s %d bytes", s.Tag, s.Len)
	}
	if b, err := SubText(f, st.Size(), 0); err == nil {
		t.Logf("subtitle 0: %q", string(b[:min(120, len(b))]))
	}
	if a, err := Audio(f, st.Size(), 0); err == nil {
		t.Logf("audio 0: %d Hz x%d, %d/packet", a.Rate, a.Channels, a.PktSamples)
		n, _ := io.Copy(io.Discard, io.LimitReader(a.PCM(60000), 1<<20))
		t.Logf("decoded %d bytes from 1:00", n)
		if out := os.Getenv("MOFLEX_PCM_OUT"); out != "" {
			o, _ := os.Create(out)
			io.Copy(o, io.LimitReader(a.PCM(60000), int64(a.Rate*a.Channels*2*10)))
			o.Close()
		}
	}
	if v, err := Version(f, st.Size(), 0); err == nil {
		in := Probe(v, v.Size())
		t.Logf("version 0: %d bytes, %+v", v.Size(), in)
	}
}
