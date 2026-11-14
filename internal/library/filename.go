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
	Season  int // > 0 for an episode
	Episode int
	EpTitle string
	Is3D    int // 1 = the name says 3D, 0 = says nothing
	Layout  string
}

func (p Parsed) IsEpisode() bool { return p.Season > 0 && p.Episode > 0 }

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
)

// ParseName reads title / year / episode / 3D hints out of a video file name.
func ParseName(path string) Parsed {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var p Parsed
	low := strings.ToLower(strings.NewReplacer("_", " ", ".", " ").Replace(name))
	switch {
	case strings.Contains(low, "half sbs") || strings.Contains(low, "hsbs"):
		p.Is3D, p.Layout = 1, "half-sbs"
	case strings.Contains(low, "full sbs") || strings.Contains(low, "fsbs"):
		p.Is3D, p.Layout = 1, "full-sbs"
	case strings.Contains(low, "(3d)") || regexp.MustCompile(`(?i)\b3d\b`).MatchString(name):
		p.Is3D = 1
	}
	name = reEncoderTail.ReplaceAllString(name, "")
	name = reParenTags.ReplaceAllString(name, "")

	// an episode: the show is what comes before the marker, the episode title what comes after
	if m := firstMatch(name, reSE, reNxNN, reDashNum); m != nil {
		p.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		p.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
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
