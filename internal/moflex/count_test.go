package moflex

import (
	"io"
	"os"
	"sync/atomic"
	"testing"
)

type countingReader struct {
	r io.ReaderAt
	n atomic.Int64
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.n.Add(int64(n))
	return n, err
}

// How much a seek reads (a network share pays for every byte).
func TestKeyStartReads(t *testing.T) {
	p := os.Getenv("MOFLEX_TEST_FILE")
	if p == "" {
		t.Skip("set MOFLEX_TEST_FILE")
	}
	f, _ := os.Open(p)
	defer f.Close()
	st, _ := f.Stat()
	for _, sec := range []int64{60, 1200, 3000} {
		c := &countingReader{r: f}
		s, err := KeyStart(c, st.Size(), sec*1e6)
		t.Logf("seek %d s -> %.1f s, read %.1f MB (%v)", sec, float64(s.Us)/1e6, float64(c.n.Load())/1e6, err)
	}
	c := &countingReader{r: f}
	Probe(c, st.Size())
	t.Logf("probe read %.1f MB", float64(c.n.Load())/1e6)
}
