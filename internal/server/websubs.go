package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/moflex"
)

// Subtitles for the browser player: every text track an item has -- a super moflex's own (its
// trailer), files beside the video, and an MKV/MP4's text tracks -- as plain timed lines the page
// draws over the picture (once per eye in 3D), or that FFmpeg burns into each eye for a phone.

type webSub struct {
	N    int    `json:"n"`
	Name string `json:"name"`
}

// cue: [start ms, end ms, text] (text: plain, lines split by \n)
type cue struct {
	Start, End int64
	Text       string
}

// webSubList: the trailer's tracks first, then sidecars and embedded tracks (subList).
func (s *Server) webSubList(id, path string) []webSub {
	var out []webSub
	if library.IsMoflex(path) {
		if f, err := os.Open(path); err == nil {
			st, _ := f.Stat()
			if sup, err := moflex.ReadSuper(f, st.Size()); err == nil {
				for _, l := range sup.SubLangs {
					name := langName(strings.ToLower(l))
					if name == "" || l == "SUB" {
						name = "Subtitles"
					}
					out = append(out, webSub{N: len(out), Name: name})
				}
			}
			f.Close()
		}
	}
	for _, sc := range s.subList(id, path) {
		name := sc.Label
		if sc.Embedded {
			// already a track name
		} else if name == "" {
			name = "Subtitles (" + sc.File + ")"
		} else if ln := langName(strings.ToLower(strings.SplitN(name, ".", 2)[0])); ln != "" {
			name = ln + strings.TrimPrefix(name, strings.SplitN(name, ".", 2)[0])
		}
		out = append(out, webSub{N: len(out), Name: name})
	}
	return out
}

// webSubCues reads track n of webSubList.
func (s *Server) webSubCues(id, path string, n int) ([]cue, error) {
	var trailer int
	if library.IsMoflex(path) {
		f, err := os.Open(path)
		if err == nil {
			st, _ := f.Stat()
			if sup, err := moflex.ReadSuper(f, st.Size()); err == nil {
				trailer = len(sup.SubLangs)
				if n < trailer {
					b, err := moflex.SubText(f, st.Size(), n)
					f.Close()
					if err != nil {
						return nil, err
					}
					return parseSubs(string(b)), nil
				}
			}
			f.Close()
		}
	}
	list := s.subList(id, path)
	k := n - trailer
	if k < 0 || k >= len(list) {
		return nil, fmt.Errorf("no subtitle track %d", n)
	}
	file := filepath.Join(filepath.Dir(path), list[k].File)
	if list[k].Embedded {
		it, m, p := s.mediaOf(id)
		ts := textSubs(m)
		if it == nil || list[k].track >= len(ts) {
			return nil, fmt.Errorf("no subtitle track %d", n)
		}
		f, err := s.embeddedSub(it, p, ts[list[k].track])
		if err != nil {
			return nil, err
		}
		file = f
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return parseSubs(string(b)), nil
}

// GET /api/items/{id}/websubs: the subtitle tracks.
func (s *Server) apiWebSubs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.itemPath(id)
	if p == "" {
		http.NotFound(w, r)
		return
	}
	list := s.webSubList(id, p)
	if list == nil {
		list = []webSub{}
	}
	writeJSON(w, list)
}

// GET /api/items/{id}/websubs/{n}: a subtitle track as [[startMs, endMs, "text"], ...];
// {n}.vtt as WebVTT (?eyes=2: each line twice, one per eye of a side-by-side picture).
func (s *Server) apiWebSub(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.itemPath(id)
	name := r.PathValue("n")
	asVTT := strings.HasSuffix(name, ".vtt")
	n, err := strconv.Atoi(strings.TrimSuffix(name, ".vtt"))
	if p == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	cues, err := s.webSubCues(id, p, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if asVTT {
		eyes, _ := strconv.Atoi(r.URL.Query().Get("eyes"))
		depth, _ := strconv.ParseFloat(r.URL.Query().Get("depth"), 64)
		depth = max(-10, min(10, depth))
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		io.WriteString(w, vtt(cues, eyes, depth))
		return
	}
	out := make([][3]any, len(cues))
	for i, c := range cues {
		out[i] = [3]any{c.Start, c.End, c.Text}
	}
	writeJSON(w, out)
}

// ---- parsing: SRT and ASS/SSA, told apart by content

var (
	reSrtTime = regexp.MustCompile(`(\d+):(\d{1,2}):(\d{1,2})[,.](\d{1,3})\s*-->\s*(\d+):(\d{1,2}):(\d{1,2})[,.](\d{1,3})`)
	reTags    = regexp.MustCompile(`<[^>]*>`)
	reAssTags = regexp.MustCompile(`\{[^}]*\}`)
)

func parseSubs(t string) []cue {
	t = strings.TrimPrefix(strings.ReplaceAll(t, "\r\n", "\n"), "\ufeff")
	var out []cue
	if strings.Contains(t, "[Events]") || strings.Contains(t, "\nDialogue:") {
		out = parseASS(t)
	} else {
		out = parseSRT(t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func ms(h, m, s, frac string) int64 {
	H, _ := strconv.ParseInt(h, 10, 64)
	M, _ := strconv.ParseInt(m, 10, 64)
	S, _ := strconv.ParseInt(s, 10, 64)
	for len(frac) < 3 {
		frac += "0"
	}
	F, _ := strconv.ParseInt(frac[:3], 10, 64)
	return ((H*60+M)*60+S)*1000 + F
}

func parseSRT(t string) []cue {
	var out []cue
	for _, block := range strings.Split(t, "\n\n") {
		lines := strings.Split(strings.Trim(block, "\n"), "\n")
		for i, ln := range lines {
			m := reSrtTime.FindStringSubmatch(ln)
			if m == nil {
				continue
			}
			text := strings.TrimSpace(reTags.ReplaceAllString(strings.Join(lines[i+1:], "\n"), ""))
			if text != "" {
				out = append(out, cue{ms(m[1], m[2], m[3], m[4]), ms(m[5], m[6], m[7], m[8]), text})
			}
			break
		}
	}
	return out
}

func parseASS(t string) []cue {
	var out []cue
	fields := []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}
	inEvents := false
	for _, ln := range strings.Split(t, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "[") {
			inEvents = strings.EqualFold(ln, "[Events]")
			continue
		}
		if !inEvents {
			continue
		}
		if v, ok := strings.CutPrefix(ln, "Format:"); ok {
			fields = nil
			for _, f := range strings.Split(v, ",") {
				fields = append(fields, strings.ToLower(strings.TrimSpace(f)))
			}
			continue
		}
		v, ok := strings.CutPrefix(ln, "Dialogue:")
		if !ok {
			continue
		}
		parts := strings.SplitN(v, ",", len(fields)) // the text, last, may hold commas
		if len(parts) < len(fields) {
			continue
		}
		var st, en, text string
		for i, f := range fields {
			switch f {
			case "start":
				st = strings.TrimSpace(parts[i])
			case "end":
				en = strings.TrimSpace(parts[i])
			case "text":
				text = parts[i]
			}
		}
		text = reAssTags.ReplaceAllString(text, "")
		text = strings.NewReplacer(`\N`, "\n", `\n`, "\n", `\h`, " ").Replace(text)
		text = strings.TrimSpace(text)
		a, b := assTime(st), assTime(en)
		if text != "" && a >= 0 && b > a {
			out = append(out, cue{a, b, text})
		}
	}
	return out
}

// assTime: H:MM:SS.cc -> ms (-1 if malformed)
func assTime(s string) int64 {
	p := strings.Split(s, ":")
	if len(p) != 3 {
		return -1
	}
	sec, frac, _ := strings.Cut(p[2], ".")
	return ms(p[0], p[1], sec, frac)
}

// vtt writes cues as WebVTT. eyes=2 (a side-by-side picture): every line twice, one in the middle
// of each half -- one per eye, in a phone's own full-screen player, which shows text tracks but
// nothing else drawn over the video.
func vtt(cues []cue, eyes int, depth float64) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	t := func(v int64) string {
		return fmt.Sprintf("%02d:%02d:%02d.%03d", v/3600000, v/60000%60, v/1000%60, v%1000)
	}
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	for _, c := range cues {
		text := esc.Replace(c.Text)
		if eyes == 2 {
			// depth (-10..10): the copies shifted apart, as the page does -- 0.15% of an eye per step
			d := depth * 0.15 * 0.5
			fmt.Fprintf(&b, "%s --> %s position:%.1f%% align:center size:46%%\n%s\n\n", t(c.Start), t(c.End), 25+d, text)
			fmt.Fprintf(&b, "%s --> %s position:%.1f%% align:center size:46%%\n%s\n\n", t(c.Start), t(c.End), 75-d, text)
		} else {
			fmt.Fprintf(&b, "%s --> %s\n%s\n\n", t(c.Start), t(c.End), text)
		}
	}
	return b.String()
}
