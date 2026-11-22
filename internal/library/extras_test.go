package library

import "testing"

func TestExtraOf(t *testing.T) {
	roots := []string{"/nas/FULL RIPS", "/nas/TV"}
	cases := []struct{ path, kind, title, dir string }{
		{"/nas/FULL RIPS/Avenging Angel (1985) {imdb-tt0088757}/Chasing and Angel-interview.mkv", "interview", "Chasing and Angel", "/nas/FULL RIPS/Avenging Angel (1985) {imdb-tt0088757}"},
		{"/nas/FULL RIPS/Angel (1983) {imdb-tt0086896}/Chance Meeting-behindthescenes.mkv", "behindthescenes", "Chance Meeting", "/nas/FULL RIPS/Angel (1983) {imdb-tt0086896}"},
		{"/nas/FULL RIPS/Angel (1983) {imdb-tt0086896}/Angel-trailer.mkv", "trailer", "Angel", "/nas/FULL RIPS/Angel (1983) {imdb-tt0086896}"},
		{"/nas/FULL RIPS/Movie (2000)/Extras/Making Of.mkv", "other", "Making Of", "/nas/FULL RIPS/Movie (2000)"},
		{"/nas/FULL RIPS/Movie (2000)/Interviews/Bob.mkv", "interview", "Bob", "/nas/FULL RIPS/Movie (2000)"},
		{"/nas/FULL RIPS/Movie (2000)/Movie (2000).mkv", "", "", "/nas/FULL RIPS/Movie (2000)"},
		{"/nas/FULL RIPS/Loose-trailer.mkv", "", "", ""},                             // a library folder's own file: never an extra
		{"/nas/FULL RIPS/Trailers/Something.mkv", "", "", "/nas/FULL RIPS/Trailers"}, // an extras-named folder at the top
		{"/nas/TV/Show (2001)/Season 1/Making It-featurette.mkv", "featurette", "Making It", "/nas/TV/Show (2001)"},
	}
	for _, c := range cases {
		k, ti, d := ExtraOf(c.path, roots)
		if k != c.kind || ti != c.title || d != c.dir {
			t.Errorf("%s: got (%q %q %q) want (%q %q %q)", c.path, k, ti, d, c.kind, c.title, c.dir)
		}
	}
}

func TestMovieKey(t *testing.T) {
	a := MovieKey("Angel (1983) {imdb-tt0086896}")
	if a != MovieKey("Angel (1983)") || a != MovieKey("Angel.1983.1080p.BluRay") || a == MovieKey("Angel (1984)") {
		t.Errorf("keys: %q %q %q", a, MovieKey("Angel (1983)"), MovieKey("Angel.1983.1080p.BluRay"))
	}
}

func TestExtraName(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"interview", "Chasing and Angel"}:       "Chasing and Angel (Interview)",
		{"trailer", "Trailer"}:                   "Trailer",
		{"trailer", "Angel III"}:                 "Angel III (Trailer)",
		{"behindthescenes", "Behind the scenes"}: "Behind the scenes",
	} {
		if got := ExtraName(in[0], in[1]); got != want {
			t.Errorf("%v: %q want %q", in, got, want)
		}
	}
}

func TestKeys(t *testing.T) {
	if FolderKey("/a/Extras Stuff") == FolderKey("/b/Extras Stuff") {
		t.Error("yearless folders joined across shares")
	}
	if FolderKey("/a/Angel (1983) {imdb-tt0086896}") != FileKey("/b/Angel.1983.720p.mkv") {
		t.Error("film on another share not matched")
	}
	if FileKey("/b/Angel.mkv") != "" {
		t.Error("yearless loose file keyed")
	}
}
