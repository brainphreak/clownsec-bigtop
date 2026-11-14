package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSidecars(t *testing.T) {
	d := t.TempDir()
	for _, f := range []string{"Movie.moflex", "Movie.srt", "Movie.en.forced.srt", "Movie.ja.ass", "Movie 2.srt",
		"Movie 2.moflex", "Other.srt", "Movie.nfo"} {
		os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644)
	}
	got := sidecars(filepath.Join(d, "Movie.moflex"))
	want := []Sidecar{{0, "Movie.srt", "", "srt", false}, {1, "Movie.en.forced.srt", "en.forced", "srt", true}, {2, "Movie.ja.ass", "ja", "ass", false}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
}
