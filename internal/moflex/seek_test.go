package moflex

import (
	"os"
	"testing"
)

// MOFLEX_TEST_FILE=<a full-length moflex> go test -run KeyStart -v ./internal/moflex/
func TestKeyStart(t *testing.T) {
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
	in := Probe(f, st.Size())
	t.Logf("info: %+v", in)
	for _, sec := range []int64{0, 5, 60, 600, 2066, 3000, in.DurUs/1e6 - 20} {
		s, err := KeyStart(f, st.Size(), sec*1e6)
		if err != nil {
			t.Fatalf("%d s: %v", sec, err)
		}
		if s.Us > sec*1e6 {
			t.Fatalf("%d s: start %d is after the target", sec, s.Us)
		}
		t.Logf("START %d %d %.3f (target %d s, %.1f s before)", sec, s.Off, float64(s.Us)/1e6, sec, float64(sec*1e6-s.Us)/1e6)
	}
}
