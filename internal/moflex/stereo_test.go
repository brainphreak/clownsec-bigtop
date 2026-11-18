package moflex

import (
	"os"
	"testing"
)

// Real files from the encoder work (skipped where absent): 3D ours, 3D official, 2D ours.
func TestDetectStereoFiles(t *testing.T) {
	cases := []struct {
		path   string
		stereo bool
	}{
		{os.ExpandEnv("$HOME/moflex-encoder/work/cmp/Robot-dark-G1500.moflex"), true},
		{os.ExpandEnv("$HOME/moflex-encoder/work/cmp/dk_official.moflex"), true},
		{os.ExpandEnv("$HOME/moflex-encoder/work/cmp/mb2d_test.moflex"), false},
	}
	for _, c := range cases {
		f, err := os.Open(c.path)
		if err != nil {
			t.Logf("skip %s: %v", c.path, err)
			continue
		}
		st, _ := f.Stat()
		s, ok := DetectStereo(f, st.Size())
		f.Close()
		if !ok || s != c.stereo {
			t.Errorf("%s: stereo=%v ok=%v, want %v", c.path, s, ok, c.stereo)
		}
	}
}

func TestDetectStereoNotMoflex(t *testing.T) {
	if _, ok := DetectStereo(byteReader("not a moflex file at all"), 24); ok {
		t.Error("a non-moflex was judged")
	}
}

type byteReader string

func (b byteReader) ReadAt(p []byte, off int64) (int, error) { return copy(p, b[off:]), nil }
