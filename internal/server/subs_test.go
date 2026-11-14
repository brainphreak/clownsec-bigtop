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
	want := []Sidecar{{N: 0, File: "Movie.srt", Ext: "srt"}, {N: 1, File: "Movie.en.forced.srt", Label: "en.forced", Ext: "srt", Forced: true},
		{N: 2, File: "Movie.ja.ass", Label: "ja", Ext: "ass"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
}
