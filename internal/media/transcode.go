package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
)

// Job is one stream for the 3DS.
type Job struct {
	Path    string
	Media   *library.Media
	StartMs int64
	Stereo  bool  // the player wants a 3D-sized frame (side-by-side source): up to 800x480, not 400x240
	Audio   int   // ffmpeg stream index of the audio track (-1 = the file's default)
	Burn    int   // ffmpeg stream index of a subtitle to burn into the picture (-1 = none)
	Kbps    int   // video bitrate (0 = the default for the size)
	DurMs   int64 // stop after this much video (0 = to the end); the speed test
}

// Size is the frame the 3DS gets: the source's shape (its pixel aspect applied), shrunk to fit
// 400x240 (2D) or 800x480 (side by side, like the Plex path), never enlarged, even numbers.
// The player splits and fits a side-by-side frame itself, so its layout is kept as it is.
func Size(m *library.Media, stereo bool) (int, int) {
	bw, bh := 400.0, 240.0
	if stereo {
		bw, bh = 800, 480
	}
	sar := m.SAR
	if sar <= 0 {
		sar = 1
	}
	dw, dh := float64(m.W)*sar, float64(m.H)
	if dw <= 0 || dh <= 0 {
		return int(bw), int(bh)
	}
	f := bw / dw
	if bh/dh < f {
		f = bh / dh
	}
	if f > 1 {
		f = 1
	}
	even := func(v float64) int {
		n := int(v+0.5) &^ 1
		if n < 2 {
			n = 2
		}
		return n
	}
	return even(dw * f), even(dh * f)
}

// Args is the ffmpeg command line for a job: one MPEG-TS of H.264 (8-bit 4:2:0 High, which the
// New 3DS decoder takes) + stereo AAC on stdout.
func Args(j Job) []string {
	m := j.Media
	w, h := Size(m, j.Stereo)
	kbps := j.Kbps
	if kbps <= 0 {
		kbps = 900
		if j.Stereo {
			kbps = 2000
		}
	}
	a := []string{"-hide_banner", "-nostdin", "-v", "error"}
	if j.StartMs > 0 {
		a = append(a, "-ss", fmt.Sprintf("%.3f", float64(j.StartMs)/1000))
	}
	a = append(a, "-i", j.Path)

	var chain []string
	if m.Interl {
		chain = append(chain, "yadif")
	}
	chain = append(chain, fmt.Sprintf("scale=%d:%d", w, h), "setsar=1", "format=yuv420p")
	if j.Burn >= 0 { // the picture subtitle goes on at full size, before the shrink
		a = append(a, "-filter_complex", fmt.Sprintf("[0:%d][0:%d]overlay=(W-w)/2:H-h,%s[v]",
			m.Video, j.Burn, strings.Join(chain, ",")), "-map", "[v]")
	} else {
		a = append(a, "-map", fmt.Sprintf("0:%d", m.Video), "-vf", strings.Join(chain, ","))
	}
	audio := j.Audio
	if audio < 0 && len(m.Audio) > 0 {
		audio = m.Audio[0].Index
		for _, t := range m.Audio {
			if t.Default {
				audio = t.Index
				break
			}
		}
	}
	if audio >= 0 {
		a = append(a, "-map", fmt.Sprintf("0:%d", audio))
	}
	a = append(a, "-sn", "-dn",
		"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-level", "4.0",
		"-b:v", fmt.Sprintf("%dk", kbps), "-maxrate", fmt.Sprintf("%dk", kbps*3/2),
		"-bufsize", fmt.Sprintf("%dk", kbps*2), "-g", "48")
	if audio >= 0 {
		// stereo: a 5.1 track otherwise comes out as 6-channel AAC, which the 3DS plays as silence
		a = append(a, "-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "128k")
	}
	return append(a, "-f", "mpegts", "-muxdelay", "0", "pipe:1")
}

// Transcode runs a job, writing the stream to w until it ends or ctx is cancelled (the 3DS
// went away, or asked for another place in the video).
func (t *Tool) Transcode(ctx context.Context, j Job, w io.Writer) error {
	ff, _ := t.Paths()
	if ff == "" {
		return errors.New("ffmpeg not found")
	}
	cmd := exec.CommandContext(ctx, ff, Args(j)...)
	hideWindow(cmd)
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &limited{&stderr, 4096}
	cmd.WaitDelay = 3 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil // stopped on purpose
	}
	if err != nil && stderr.Len() > 0 {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return err
}

type limited struct {
	b *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.b.Len(); room > 0 {
		if len(p) > room {
			l.b.Write(p[:room])
		} else {
			l.b.Write(p)
		}
	}
	return len(p), nil
}

// SubExt is the file a text subtitle track becomes: ASS stays ASS (its styling), the rest SRT.
func SubExt(codec string) string {
	if codec == "ass" || codec == "ssa" {
		return "ass"
	}
	return "srt"
}

// ExtractSub writes one text subtitle track to out. ffmpeg reads the whole file for it, so on a
// big file over a network this takes a while -- callers cache the result.
func (t *Tool) ExtractSub(path string, index int, codec, out string) error {
	ff, _ := t.Paths()
	if ff == "" {
		return errors.New("ffmpeg not found")
	}
	ext := SubExt(codec)
	tmp := out + ".part"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c := "srt"
	if ext == "ass" {
		c = "copy"
	}
	cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-nostdin", "-v", "error", "-y", "-i", path,
		"-map", fmt.Sprintf("0:%d", index), "-c:s", c, "-f", ext, tmp)
	hideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &limited{&stderr, 2048}
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return os.Rename(tmp, out)
}
