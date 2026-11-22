package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Extras, by Plex's rules: in a movie's own folder, a video whose name ends "-interview",
// "-trailer", "-featurette", ... or that sits in an "Extras", "Interviews", "Trailers", ... folder
// inside it, belongs to that movie instead of being a movie of its own. A file directly in a
// library folder is never an extra (the rules are about a movie's folder).

// extraKinds: the kinds, in the order a movie's extras are listed, with what they are called.
var extraKinds = []struct{ kind, label string }{
	{"trailer", "Trailer"},
	{"featurette", "Featurette"},
	{"behindthescenes", "Behind the Scenes"},
	{"interview", "Interview"},
	{"deleted", "Deleted Scene"},
	{"scene", "Scene"},
	{"short", "Short"},
	{"other", "Extra"},
}

// the folder names Plex reads as extras (lower case) -> kind
var extraFolders = map[string]string{
	"trailers": "trailer", "featurettes": "featurette", "behind the scenes": "behindthescenes",
	"interviews": "interview", "deleted scenes": "deleted", "scenes": "scene", "shorts": "short",
	"other": "other", "extras": "other",
}

var reSeasonDir = regexp.MustCompile(`(?i)^(season[ ._-]*\d+|specials|s\d{1,2})$`)

// up from a season folder: a show's extras belong to the show's folder
func unseason(dir string, roots []string) string {
	if reSeasonDir.MatchString(filepath.Base(dir)) && !isRoot(filepath.Dir(dir), roots) && !isRoot(dir, roots) {
		return filepath.Dir(dir)
	}
	return dir
}

var reExtraSuffix = regexp.MustCompile(`(?i)[\s._]*-(behindthescenes|deleted|featurette|interview|scene|short|trailer|other|extra)s?$`)

// ExtraLabel: what an extra kind is called ("interview" -> "Interview").
func ExtraLabel(kind string) string {
	for _, k := range extraKinds {
		if k.kind == kind {
			return k.label
		}
	}
	return "Extra"
}

// ExtraOrder: where a kind goes in a movie's list of extras.
func ExtraOrder(kind string) int {
	for i, k := range extraKinds {
		if k.kind == kind {
			return i
		}
	}
	return len(extraKinds)
}

func isRoot(dir string, roots []string) bool {
	d := filepath.Clean(dir)
	for _, r := range roots {
		if filepath.Clean(r) == d {
			return true
		}
	}
	return false
}

// ExtraOf says whether a file is an extra: its kind ("" = not one), its own title, and the movie
// folder it belongs to. movieDir is also returned for a file that is not an extra (its folder),
// "" when that folder is a library folder itself.
func ExtraOf(path string, roots []string) (kind, title, movieDir string) {
	dir := filepath.Dir(path)
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if isRoot(dir, roots) {
		return "", "", ""
	}
	if k, ok := extraFolders[strings.ToLower(filepath.Base(dir))]; ok && !isRoot(filepath.Dir(dir), roots) {
		t := stem
		if m := reExtraSuffix.FindStringSubmatchIndex(stem); m != nil { // "Extras/Foo-interview.mkv"
			t = stem[:m[0]]
			k = suffixKind(stem[m[2]:m[3]])
		}
		return k, extraTitle(t), unseason(filepath.Dir(dir), roots)
	}
	if m := reExtraSuffix.FindStringSubmatchIndex(stem); m != nil {
		return suffixKind(stem[m[2]:m[3]]), extraTitle(stem[:m[0]]), unseason(dir, roots)
	}
	return "", "", dir
}

func suffixKind(s string) string {
	s = strings.ToLower(s)
	if s == "extra" {
		return "other"
	}
	return s
}

func extraTitle(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
	return reSpaces.ReplaceAllString(s, " ")
}

// ExtraName is how an extra is listed: "Chasing an Angel (Interview)", or just "Trailer" when the
// title already says what it is.
func ExtraName(kind, title string) string {
	label := ExtraLabel(kind)
	if title == "" {
		return label
	}
	if strings.Contains(strings.ToLower(title), strings.ToLower(strings.Fields(label)[0])) {
		return title
	}
	return title + " (" + label + ")"
}

// MovieKey names the film a folder (or a loose file's name) is: its title and year, folded.
// Extras find their film by it -- in their own folder, or in another folder or library altogether
// (a film moved to another share keeps its extras).
func MovieKey(name string) string {
	p := ParseName(name + ".mkv")
	if p.Title == "" {
		return ""
	}
	return Norm(p.Title) + "|" + strconv.Itoa(p.Year)
}

// FolderKey: MovieKey for a film's folder -- with no year in the name, the folder itself (a bare
// title is too weak to join folders on different shares).
func FolderKey(dir string) string {
	k := MovieKey(filepath.Base(dir))
	if k == "" || strings.HasSuffix(k, "|0") {
		return "dir|" + filepath.Clean(dir)
	}
	return k
}

// FileKey: MovieKey for a loose film file; "" without a year (it joins no group).
func FileKey(path string) string {
	k := MovieKey(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if strings.HasSuffix(k, "|0") {
		return ""
	}
	return k
}

// GroupID is the id a film with extras gets.
func GroupID(key string) string { return hashID("group", key) }
