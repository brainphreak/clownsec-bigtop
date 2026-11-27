package library

import (
	"regexp"
	"strconv"
	"strings"
)

// Library types: what a library holds, set by the user. "" = mixed (the file names decide, the
// way Big Top always worked); "tv" = every video is an episode of the show whose folder it is in;
// "movies" = every video is a film, whatever its name says.
// TVNameVer: bump when ParseTV reads names differently (TV libraries are identified again).
const TVNameVer = 1

const (
	TypeMixed  = ""
	TypeTV     = "tv"
	TypeMovies = "movies"
)

// LibType normalises a library type.
func LibType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "tv", "shows", "tv shows":
		return TypeTV
	case "movies", "movie", "films":
		return TypeMovies
	}
	return TypeMixed
}

var (
	// season folders: "Season 5", "Season.05", "Series 3", "S10", "S 2", "Staffel 1", "Saison 2"
	reTvSeasonDir = regexp.MustCompile(`(?i)^(?:season|series|staffel|saison|temporada|s)[\s._-]*(\d{1,3})\b`)
	// a season inside a release folder name: "21.Jump.Street.S02.DVDRip", "Doctor Who 2005 S11 1080p"
	reSeasonTok  = regexp.MustCompile(`(?i)(?:^|[\s._-])S(\d{1,2})(?:$|[\s._-])`)
	reSeasonWord = regexp.MustCompile(`(?i)\b(?:season|series|staffel|saison|temporada)[\s._-]*(\d{1,3})\b`) // "Looney Tunes Season 19"
	reSpecialDir = regexp.MustCompile(`(?i)^(specials?|extras?|bonus|featurettes?|season[\s._-]*0+)$`)

	reTvSE        = regexp.MustCompile(`(?i)\bS(\d{1,2})[\s._-]{0,3}E(\d{1,3})\b`) // S05E20, s03e01, S4-E30, S01 E02
	reTvNx        = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)              // 6x19, 2x5
	reTvOf        = regexp.MustCompile(`(?i)\b(\d{1,2})\s?of\s?\d{1,2}\b`)         // 5of9 (a documentary's parts)
	reTvBracketSE = regexp.MustCompile(`(?i)\[\s*(?:(\d{1,2})x(\d{1,3})|S(\d{1,2})\s?E(\d{1,3}))\s*\]`)
	reShowSeasons = regexp.MustCompile(`(?i)[\s(\[-]*\b(?:(?:seasons?|series)[\s._-]*\d.*|complete\b.*|all seasons\b.*)$`)
	reInitials    = regexp.MustCompile(`^(?:[A-Za-z]\.)+[A-Za-z]?\.?$`)
	reTvPart      = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:part|pt|chapter)[\s.#_-]*(\d{1,3})(?:$|[^0-9])`)
	reTvAnyNum    = regexp.MustCompile(`(?:^|[\s_.(\[-])(\d{1,3})(?:$|[\s_.)\]-])`) // last resort: "Care Bears Dic 07 - X"
	reTvSerEp     = regexp.MustCompile(`(?i)\b(?:season|series)[\s._-]*(\d{1,2})[\s._,-]*(?:episode|ep|e)[\s._-]*(\d{1,3})\b`)
	reTvEp        = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:episode|ep|e)[\s.#_-]*(\d{1,3})(?:$|[^0-9])`)
	reTvNum34     = regexp.MustCompile(`(?:^|[\s_.(\[-])(\d{3,4})(?:$|[\s_.)\]-])`)
	reTvNumLead   = regexp.MustCompile(`^(\d{1,3})(?:$|[\s_.)\]-])`)
	reTvNumDash   = regexp.MustCompile(`(?:^|\s)-\s*(\d{1,3})\s*(?:-|$)`)
	reTvParen     = regexp.MustCompile(`\((\d{1,3})\)\s*$`)
	reTvTitleLead = regexp.MustCompile(`^[\s._)\]-]+`)
)

// pathParts splits a path on both separators (a Windows library read anywhere).
func pathParts(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
}

// relParts: the folders between the library folder holding `path` and the file, and the file name.
func relParts(path string, roots []string) ([]string, string) {
	parts := pathParts(path)
	if len(parts) == 0 {
		return nil, ""
	}
	file := parts[len(parts)-1]
	best := -1
	for _, r := range roots {
		rp := pathParts(r)
		if len(rp) < len(parts) && len(rp) > best {
			ok := true
			for i := range rp {
				if !strings.EqualFold(rp[i], parts[i]) {
					ok = false
					break
				}
			}
			if ok {
				best = len(rp)
			}
		}
	}
	if best < 0 {
		return nil, file
	}
	return parts[best : len(parts)-1], file
}

// showFromFolder: a show folder's name as a title and year ("Cow and Chicken (1997) (DVDRip)" ->
// "Cow and Chicken", 1997; "Doctor Who 2005 S11 1080p BluRay" -> "Doctor Who", 2005, season 11).
func showFromFolder(name string) (title string, year, season int) {
	season = -1
	if m := reTvSE.FindStringSubmatchIndex(name); m != nil && m[0] > 0 { // "Bad.Thoughts.S01E02.1080p-Grp"
		season, _ = strconv.Atoi(name[m[2]:m[3]])
		name = name[:m[0]]
	} else if m := reSeasonTok.FindStringSubmatchIndex(name); m != nil && m[0] > 0 {
		season, _ = strconv.Atoi(name[m[2]:m[3]])
		name = name[:m[0]]
	}
	name = reBracket.ReplaceAllString(name, "")
	if m := reShowSeasons.FindStringIndex(name); m != nil && m[0] > 0 { // "Loiter Squad Season 2 MP4", "(Seasons 1-3)"
		name = name[:m[0]]
	}
	// "Black.Mirror" (one dot) is a scene name too; "C.O.P.S." (letters) is not
	if strings.Contains(name, ".") && !strings.Contains(strings.TrimSpace(name), " ") && !reInitials.MatchString(name) {
		name = strings.ReplaceAll(name, ".", " ")
	}
	title, year = titleYear(name)
	return title, year, season
}

// stripShow drops the show's name from the start of a file name ("Darkwing Duck - 56 - X" ->
// "- 56 - X"), so numbers in a show's name ("21 Jump Street") are not read as the episode.
func stripShow(name, show string) string {
	st := tokens(show)
	if len(st) == 0 {
		return name
	}
	// walk the name's tokens alongside the show's, case-insensitively
	low := strings.ToLower(name)
	pos, i := 0, 0
	for i < len(st) {
		j := pos
		for j < len(low) && !isAlnum(low[j]) {
			j++
		}
		if !strings.HasPrefix(low[j:], st[i]) {
			break
		}
		end := j + len(st[i])
		if end < len(low) && isAlnum(low[end]) {
			break
		}
		pos, i = end, i+1
	}
	if i == len(st) {
		return name[pos:]
	}
	return name
}

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

func isAlnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }

// ParseTV reads a file in a TV library: the show is the first folder under the library folder
// (a file loose in the library folder: the name's show, as ParseName reads it), the season the
// "Season N" folder or the name, the episode the name. An episode with no number it can read is
// still the show's: season from its folder (else 0, the specials), episode 0, Marked so it groups.
func ParseTV(path string, roots []string) Parsed {
	dirs, file := relParts(path, roots)
	p := ParseName(file)
	if len(dirs) == 0 {
		return p // loose in the library folder: only the name can say
	}
	show, year, season := showFromFolder(dirs[0])
	if show == "" {
		return p
	}
	special := false
	for _, d := range dirs[1:] {
		if m := reTvSeasonDir.FindStringSubmatch(strings.TrimSpace(d)); m != nil {
			season, _ = strconv.Atoi(m[1])
		} else if reSpecialDir.MatchString(strings.TrimSpace(d)) {
			special = true
		} else if m := reSeasonWord.FindStringSubmatch(d); m != nil {
			season, _ = strconv.Atoi(m[1])
		} else if m := reSeasonTok.FindStringSubmatch(d); m != nil {
			season, _ = strconv.Atoi(m[1])
		}
	}
	if special && season < 0 {
		season = 0
	}
	stem := file
	if i := strings.LastIndex(stem, "."); i > 0 {
		stem = stem[:i]
	}
	stem = reEncoderTail.ReplaceAllString(stem, "")
	bse, bep := -1, -1 // "067 - Babylon 5 [3x22] Z'ha'dum": the bracket has the real code
	if m := reTvBracketSE.FindStringSubmatch(stem); m != nil {
		x, y := m[1]+m[3], m[2]+m[4]
		bse, _ = strconv.Atoi(x)
		bep, _ = strconv.Atoi(y)
	}
	stem = reBracket.ReplaceAllString(stem, " ")
	stem = reParenTags.ReplaceAllString(stem, "")
	stem = reSbsTag.ReplaceAllString(stem, " ")
	stem = reJunk.ReplaceAllString(stem, "")
	name := strings.ReplaceAll(stem, "_", " ")
	if strings.Count(name, ".") >= 2 && !strings.Contains(strings.TrimSpace(name), " ") {
		name = strings.ReplaceAll(name, ".", " ")
	}
	name = stripShow(name, show)

	se, ep, rest := -1, -1, ""
	take := func(m []int, sg, eg int) {
		if sg >= 0 {
			se, _ = strconv.Atoi(name[m[sg]:m[sg+1]])
		}
		ep, _ = strconv.Atoi(name[m[eg]:m[eg+1]])
		rest = name[m[1]:]
		if strings.TrimSpace(reTvTitleLead.ReplaceAllString(rest, "")) == "" { // "Title - S01E02": the title is in front
			rest = name[:m[0]]
		}
	}
	switch {
	case p.IsEpisode() && p.Title == "" || p.IsEpisode() && sameShow(p.Title, show):
		se, ep, rest = p.Season, p.Episode, p.EpTitle
	default:
		if m := reTvSE.FindStringSubmatchIndex(name); m != nil {
			take(m, 2, 4)
		} else if m := reTvNx.FindStringSubmatchIndex(name); m != nil {
			take(m, 2, 4)
		} else if m := reTvSerEp.FindStringSubmatchIndex(name); m != nil {
			take(m, 2, 4)
		} else if m := reDashNum.FindStringSubmatchIndex(name); m != nil {
			take(m, 2, 4)
		} else if m := reTvEp.FindStringSubmatchIndex(name); m != nil {
			take(m, -1, 2)
		} else if m := reTvOf.FindStringSubmatchIndex(name); m != nil {
			take(m, -1, 2)
		} else if m := reTvNum34.FindStringSubmatchIndex(name); m != nil && num34(name[m[2]:m[3]], season, &se, &ep) {
			rest = name[m[1]:]
			if strings.TrimSpace(reTvTitleLead.ReplaceAllString(rest, "")) == "" {
				rest = name[:m[0]]
			}
		} else if m := reTvNumLead.FindStringSubmatchIndex(strings.TrimLeft(name, " -._")); m != nil {
			name = strings.TrimLeft(name, " -._")
			take(m, -1, 2)
		} else if m := reTvNumDash.FindStringSubmatchIndex(name); m != nil {
			take(m, -1, 2)
		} else if m := reTvParen.FindStringSubmatchIndex(name); m != nil {
			take(m, -1, 2)
			rest = name[:m[0]]
		} else if m := reTvPart.FindStringSubmatchIndex(name); m != nil {
			take(m, -1, 2)
		} else if m := reTvAnyNum.FindStringSubmatchIndex(name); m != nil {
			take(m, -1, 2)
		}
	}
	if bse >= 0 {
		se, ep = bse, bep
	}
	if se < 0 {
		se = season
	}
	if se < 0 {
		if ep > 0 {
			se = 1
		} else {
			se = 0
		}
	}
	if ep < 0 {
		ep = 0
		rest = name
	}
	out := Parsed{Title: show, Year: year, Season: se, Episode: ep, Marked: true, InShow: true, Is3D: p.Is3D, Layout: p.Layout}
	out.EpTitle = clean(reTvTitleLead.ReplaceAllString(rest, ""))
	if out.EpTitle == "" && ep == 0 {
		out.EpTitle = clean(stem)
	}
	return out
}

// num34 reads a 3-4 digit episode code: "725" = season 7 episode 25, "1017" = 10/17, "0212" = 2/12.
// A known season folder must agree with it; "002" alone is episode 2; a year is not a code.
func num34(s string, season int, se, ep *int) bool {
	n, _ := strconv.Atoi(s)
	if len(s) == 4 && n >= 1900 && n <= 2099 && !(season > 0 && strings.HasPrefix(s, strconv.Itoa(season))) {
		return false
	}
	if len(s) == 3 && s[0] == '0' {
		*ep = n
		return true
	}
	cut := len(s) - 2
	sv, _ := strconv.Atoi(s[:cut])
	ev, _ := strconv.Atoi(s[cut:])
	if ev == 0 { // "100 Greatest Stand-Ups": a number in the title, not an episode code
		return false
	}
	if season < 0 && len(s) == 3 && ev > 40 { // "274 Catastrophe Cat": no season folder, no such episode: absolute
		*ep = n
		return true
	}
	if season > 0 && sv != season {
		if len(s) == 3 { // "Season 2\Show - 121": not season 2's code, so an absolute episode number
			*ep = n
			return true
		}
		return false
	}
	*se, *ep = sv, ev
	return true
}

func sameShow(a, b string) bool {
	na, nb := Norm(a), Norm(b)
	return na != "" && (na == nb || strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na))
}
