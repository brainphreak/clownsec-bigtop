package media

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
)

var imageSubs = map[string]bool{"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true}

// TextSub: a subtitle codec ffmpeg can turn into a .srt / .ass file.
func TextSub(codec string) bool {
	switch codec {
	case "subrip", "srt", "ass", "ssa", "mov_text", "webvtt", "text", "microdvd", "subviewer", "sami":
		return true
	}
	return false
}

// Probe reads a file's streams with ffprobe (headers only: quick, even over a network share).
func (t *Tool) Probe(path string) (*library.Media, error) {
	_, probe := t.Paths()
	if probe == "" {
		return nil, errors.New("ffprobe not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, probe, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseProbe(out)
}

func parseProbe(out []byte) (*library.Media, error) {
	var pr struct {
		Streams []struct {
			Index       int               `json:"index"`
			Type        string            `json:"codec_type"`
			Codec       string            `json:"codec_name"`
			W           int               `json:"width"`
			H           int               `json:"height"`
			SAR         string            `json:"sample_aspect_ratio"`
			FieldOrder  string            `json:"field_order"`
			Channels    int               `json:"channels"`
			AvgRate     string            `json:"avg_frame_rate"`
			RRate       string            `json:"r_frame_rate"`
			Tags        map[string]string `json:"tags"`
			Disposition map[string]int    `json:"disposition"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, err
	}
	m := &library.Media{Video: -1, Ver: library.ProbeVer}
	if d, err := strconv.ParseFloat(pr.Format.Duration, 64); err == nil {
		m.DurMs = int64(d * 1000)
	}
	for _, s := range pr.Streams {
		tag := func(k string) string {
			for kk, v := range s.Tags {
				if strings.EqualFold(kk, k) {
					return v
				}
			}
			return ""
		}
		switch s.Type {
		case "video":
			if m.Video >= 0 || s.Disposition["attached_pic"] == 1 || s.W == 0 {
				continue // cover art, or a second picture
			}
			m.Video, m.W, m.H, m.SAR = s.Index, s.W, s.H, 1
			if a, b, ok := strings.Cut(s.SAR, ":"); ok {
				x, _ := strconv.ParseFloat(a, 64)
				y, _ := strconv.ParseFloat(b, 64)
				if x > 0 && y > 0 {
					m.SAR = x / y
				}
			}
			for _, r := range []string{s.AvgRate, s.RRate} { // avg first: r_frame_rate can be a field rate
				a, b, ok := strings.Cut(r, "/")
				n, _ := strconv.Atoi(a)
				d, _ := strconv.Atoi(b)
				if ok && n > 0 && d > 0 && float64(n)/float64(d) >= 5 && float64(n)/float64(d) <= 120 {
					m.FpsNum, m.FpsDen = n, d
					break
				}
			}
			switch s.FieldOrder {
			case "tt", "bb", "tb", "bt":
				m.Interl = true
			}
		case "audio":
			m.Audio = append(m.Audio, library.Track{Index: s.Index, Codec: s.Codec, Lang: tag("language"),
				Title: tag("title"), Channels: s.Channels, Default: s.Disposition["default"] == 1})
		case "subtitle":
			if !TextSub(s.Codec) && !imageSubs[s.Codec] {
				continue
			}
			m.Subs = append(m.Subs, library.Track{Index: s.Index, Codec: s.Codec, Lang: tag("language"),
				Title: tag("title"), Default: s.Disposition["default"] == 1,
				Forced: s.Disposition["forced"] == 1 || strings.Contains(strings.ToLower(tag("title")), "forced"),
				Image:  imageSubs[s.Codec]})
		}
	}
	if m.Video < 0 {
		return nil, errors.New("no video stream")
	}
	return m, nil
}
