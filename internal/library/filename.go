package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Parsed is what a file name alone says about a video.
type Parsed struct {
	Title   string // movie title, or the show's title for an episode
	Year    int
	Season  int
	Episode int  // 0 is a special ("S04E00"), as is season 0 ("S00E05")
	Marked  bool // the name had an episode marker (S01E02, 1x02, 1-02)
	EpTitle string
	Is3D    int // 1 = the name says 3D, 0 = says nothing
	Layout  string
}

// IsEpisode: the name marks an episode. Episode 0 (S04E00) and season 0 (S00E05) are specials,
// still episodes of the show; only "S00E00" says nothing.
func (p Parsed) IsEpisode() bool { return p.Marked && (p.Season > 0 || p.Episode > 0) }

var (
	// what the moflex encoder appends: " Lrf Full Sbs_400x240_1400kbps_23.976fps_2pass"
	reEncoderTail = regexp.MustCompile(`(?i)([\s_]+lrf?[\s_].*|_\d{3}x\d{3}_\d+kbps.*)$`)
	reBracket     = regexp.MustCompile(`\[[^\]]*\]`)
	reSE          = regexp.MustCompile(`(?i)\bS(\d{1,2})\s?E(\d{1,3})\b`)
	reNxNN        = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{2,3})\b`)
	reDashNum     = regexp.MustCompile(`(?:^|\s)(\d{1,2})-(\d{2,3})(?:\s|$)`) // "Danny Phantom 1-01 Mystery Meat"
	reYear        = regexp.MustCompile(`(?:^|[\s.(\[_-])((?:19|20)\d{2})(?:$|[\s.)\]_-])`)
	reParenTags   = regexp.MustCompile(`(?i)\((3d|s|super|hsbs|fsbs|sbs|half sbs|full sbs)\)`)
	reJunk        = regexp.MustCompile(`(?i)\b(1080p|720p|480p|2160p|4k|uhd|bluray|blu-ray|bdrip|bd-rip|brrip|web-?dl|webrip|hdtv|dvdrip|x264|x265|h\.?264|h\.?265|hevc|aac[0-9.]*|ac3|dts|repack|proper|remux|limited|extended|unrated|10bit)\b.*$`)
	reSpaces      = regexp.MustCompile(`\s+`)

	// 3D tags, on a name whose "_" "." "-" are spaces. The player (mp4_play.c fmt_from_name) uses
	// the same words, so a file is 3D or not the same way on the SD card and through Big Top:
	// "HSBS" "H-SBS" "Half SBS" "Half-SBS" "HalfSBS" "Half.SBS" "Half_SBS" -> half side-by-side,
	// the same with Full / F -> full, "SBS" or "LR" alone -> half (a 16:9 SBS frame is half),
	// "3D" alone -> half too (the common 3D rip). Whole words only: no "lr" inside "Ultra".
	reHalfSbs = regexp.MustCompile(`\b(half ?|h ?)sbs\b`)
	reFullSbs = regexp.MustCompile(`\b(full ?|f ?)sbs\b`)
	reSbs     = regexp.MustCompile(`\b(sbs|lr)\b`)
	re3D      = regexp.MustCompile(`\b3d\b`)
	reLrTok   = regexp.MustCompile(`(?i)\blrf?\b`)
	// the side-by-side tags out of the title ("Avatar Half-SBS 2009" -> "Avatar 2009")
	reSbsTag = regexp.MustCompile(`(?i)[(\[]?\b(?:(?:half|full|h|f)[ ._-]?)?sbs\b[)\]]?`)
)

// NameLayout: what a file name says about 3D -- (1, "half-sbs" / "full-sbs") for a side-by-side
// tag, (1, "") for "3D" alone, (0, "") for nothing.
func NameLayout(name string) (int, string) {
	low := strings.ToLower(strings.NewReplacer("_", " ", ".", " ", "-", " ").Replace(name))
	switch {
	case reHalfSbs.MatchString(low):
		return 1, "half-sbs"
	case reFullSbs.MatchString(low):
		return 1, "full-sbs"
	case reSbs.MatchString(low):
		return 1, "half-sbs"
	case re3D.MatchString(low):
		return 1, ""
	}
	return 0, ""
}

// ParseName reads title / year / episode / 3D hints out of a video file name.
func ParseName(path string) Parsed {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var p Parsed
	// the encoder's tail: its "Full Sbs" counts, its "Lr" / "Lrf" does not (2D files have it too)
	tail := strings.ReplaceAll(reEncoderTail.FindString(name), "_", " ")
	name = reEncoderTail.ReplaceAllString(name, "")
	p.Is3D, p.Layout = NameLayout(name + " " + reLrTok.ReplaceAllString(tail, " "))
	name = reParenTags.ReplaceAllString(name, "")
	name = reSbsTag.ReplaceAllString(name, " ")

	// an episode: the show is what comes before the marker, the episode title what comes after
	if m := firstMatch(name, reSE, reNxNN, reDashNum); m != nil {
		p.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		p.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		p.Marked = true
		show, rest := name[:m[0]], name[m[1]:]
		p.Title, p.Year = titleYear(show)
		p.EpTitle = clean(rest)
		return p
	}
	p.Title, p.Year = titleYear(name)
	return p
}

func firstMatch(s string, res ...*regexp.Regexp) []int {
	for _, re := range res {
		if m := re.FindStringSubmatchIndex(s); m != nil {
			return m
		}
	}
	return nil
}

// titleYear splits "Cabaret.1972.1080P.Bluray" into ("Cabaret", 1972). The LAST plausible year
// wins, so a title that is itself a year ("1917 (2019)") keeps it.
func titleYear(s string) (string, int) {
	s = dots(s)
	year := 0
	if ms := reYear.FindAllStringSubmatchIndex(s, -1); len(ms) > 0 {
		for i := len(ms) - 1; i >= 0; i-- {
			m := ms[i]
			if m[2] == 0 && len(ms) == 1 && strings.TrimSpace(s[m[3]:]) == "" {
				break // the whole name is a number: a title, not a year
			}
			if m[2] > 0 || i < len(ms)-1 {
				year, _ = strconv.Atoi(s[m[2]:m[3]])
				s = s[:m[2]]
				break
			}
		}
	}
	return clean(s), year
}

// dots turns scene-style separators into spaces ("Grown.Ups.2010" -> "Grown Ups 2010").
func dots(s string) string {
	if strings.Count(s, ".") >= 2 && !strings.Contains(s, " ") {
		s = strings.ReplaceAll(s, ".", " ")
	}
	return strings.ReplaceAll(s, "_", " ")
}

func clean(s string) string {
	s = dots(s)
	s = reBracket.ReplaceAllString(s, "")
	s = reJunk.ReplaceAllString(s, "")
	s = strings.Trim(s, " -.[]_")
	s = strings.ReplaceAll(s, "()", "")
	// brackets only go when they no longer pair up ("Title (" left by a cut-off tag)
	for strings.HasSuffix(s, "(") || (strings.HasSuffix(s, ")") && strings.Count(s, ")") > strings.Count(s, "(")) {
		s = strings.TrimSpace(s[:len(s)-1])
	}
	for strings.HasPrefix(s, ")") || (strings.HasPrefix(s, "(") && strings.Count(s, "(") > strings.Count(s, ")")) {
		s = strings.TrimSpace(s[1:])
	}
	return reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
}
