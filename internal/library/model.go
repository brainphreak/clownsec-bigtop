package library

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Library is a named set of folders (like a Plex library). Movies and TV episodes can share one.
type Library struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Folders []string `json:"folders"`
}

// Info is one layer of metadata. The scanner fills Detected from the file (super moflex trailer,
// Clownsec catalog, file name); Edit holds what the user typed in Big Top's page, and wins
// field by field. Empty / zero means "not set".
type Info struct {
	Title    string `json:"title,omitempty"`
	Year     int    `json:"year,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Genres   string `json:"genres,omitempty"`  // "Animation, Family"
	Runtime  int    `json:"runtime,omitempty"` // minutes
	Is3D     string `json:"is3d,omitempty"`    // "", "2d", "3d" (moflex: stereo is in the file), "hsbs", "fsbs" (other formats: how the frame splits)
	Show     string `json:"show,omitempty"`    // episodes: the show's title
	ShowYear int    `json:"showYear,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episode  int    `json:"episode,omitempty"`
	EpTitle  string `json:"epTitle,omitempty"`
}

// over lays the set fields of top over base.
func over(base, top Info) Info {
	r := base
	if top.Title != "" {
		r.Title = top.Title
	}
	if top.Year != 0 {
		r.Year = top.Year
	}
	if top.Summary != "" {
		r.Summary = top.Summary
	}
	if top.Genres != "" {
		r.Genres = top.Genres
	}
	if top.Runtime != 0 {
		r.Runtime = top.Runtime
	}
	if top.Is3D != "" {
		r.Is3D = top.Is3D
	}
	if top.Show != "" {
		r.Show = top.Show
	}
	if top.ShowYear != 0 {
		r.ShowYear = top.ShowYear
	}
	if top.Season != 0 {
		r.Season = top.Season
	}
	if top.Episode != 0 {
		r.Episode = top.Episode
	}
	if top.EpTitle != "" {
		r.EpTitle = top.EpTitle
	}
	return r
}

// Item is one video file.
type Item struct {
	ID          string `json:"id"`
	Lib         string `json:"lib"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	MTime       int64  `json:"mtime"`
	Fingerprint string `json:"fp"`
	Missing     int64  `json:"missing,omitempty"` // unix time the file was last not found (0 = present)
	Added       int64  `json:"added"`

	Detected Info     `json:"detected"`
	Edit     Info     `json:"edit"`
	Source   string   `json:"source"`            // where Detected came from: "super", "catalog", "filename"
	Catalog  string   `json:"catalog,omitempty"` // matched Clownsec catalog entry id
	Super    bool     `json:"super,omitempty"`
	Audio    string   `json:"audio,omitempty"`    // in-band audio language ("ENG")
	AltAudio string   `json:"altAudio,omitempty"` // the trailer's second audio track
	Subs     []string `json:"subs,omitempty"`     // trailer subtitle languages
	ShowDesc string   `json:"showDesc,omitempty"` // a super moflex episode: its show's description
	Poster   string   `json:"poster,omitempty"`   // file in the posters folder
	PosterBy string   `json:"posterBy,omitempty"` // "super", "catalog", "user"
	Media    *Media   `json:"media,omitempty"`    // not moflex: what ffprobe found (nil = not probed yet)
}

// Media is a non-moflex file's streams, as ffprobe reports them. Big Top transcodes these to the
// H.264 + AAC MPEG-TS the 3DS plays.
type Media struct {
	DurMs  int64   `json:"dur"`
	Video  int     `json:"video"` // stream index of the picture (-1 = none)
	W      int     `json:"w,omitempty"`
	H      int     `json:"h,omitempty"`
	SAR    float64 `json:"sar,omitempty"`        // pixel aspect (DVD rips: != 1)
	Interl bool    `json:"interlaced,omitempty"` // needs deinterlacing
	Audio  []Track `json:"audio,omitempty"`
	Subs   []Track `json:"subs,omitempty"`
	Probed int64   `json:"probed"` // the file's mtime when probed
}

// Track is one audio or subtitle stream inside a file.
type Track struct {
	Index    int    `json:"i"` // ffmpeg stream index
	Codec    string `json:"codec"`
	Lang     string `json:"lang,omitempty"`
	Title    string `json:"title,omitempty"`
	Channels int    `json:"ch,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	Image    bool   `json:"image,omitempty"` // a picture subtitle (PGS, DVD): can only be burned in
}

// Stereo3D: any of the 3D values.
func Stereo3D(v string) bool { return v == "3d" || v == "hsbs" || v == "fsbs" }

// IsMoflex: a moflex carries its own 3D layout; other formats need half/full SBS said.
func IsMoflex(path string) bool { return strings.EqualFold(filepath.Ext(path), ".moflex") }

// Info is the item's metadata as shown: detected, with the user's edits on top.
func (it *Item) Info() Info { return over(it.Detected, it.Edit) }

// IsEpisode: an item is a TV episode once it has a show and an episode number.
func (it *Item) IsEpisode() bool {
	in := it.Info()
	return in.Show != "" && in.Episode > 0
}

// Show groups episodes. Its id comes from the library + show title, so it is stable.
type Show struct {
	ID       string `json:"id"`
	Lib      string `json:"lib"`
	Detected Info   `json:"detected"`
	Edit     Info   `json:"edit"`
	Catalog  string `json:"catalog,omitempty"`
	Poster   string `json:"poster,omitempty"`
	PosterBy string `json:"posterBy,omitempty"`
}

func (s *Show) Info() Info { return over(s.Detected, s.Edit) }

// Progress is one console's place in one item.
type Progress struct {
	PosMs   int64 `json:"pos"`
	Watched bool  `json:"watched,omitempty"`
	Updated int64 `json:"t"`
}

func hashID(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// ShowID is the id a show gets in a library.
func ShowID(lib, title string) string { return hashID("show", lib, Norm(title)) }

// Norm folds a title for matching: lower case, letters and digits only, "&" as "and",
// a leading "the " dropped.
func Norm(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "&", " and "))
	var b strings.Builder
	space := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return strings.TrimPrefix(b.String(), "the ")
}
