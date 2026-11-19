package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
)

// The browser player: any file becomes HLS (H.264 + AAC in 2-second MPEG-TS segments), which every
// browser plays -- Safari itself, the others through hls.js. A 3D file keeps both eyes in one frame,
// side by side; the page draws them as anaglyph, side-by-side or 2D.

// WebJob is one browser stream.
type WebJob struct {
	Path    string
	StartMs int64 // where to start (exact: a moflex decodes from its keyframe and drops the gap)

	// moflex: the file from byte In (a keyframe start, StartMs-GapMs), the frame rate per picture
	Moflex   bool
	In       io.ReadSeeker
	GapMs    int64
	Stereo   bool // pairs of eye frames, interleaved: one side-by-side picture per pair
	Num, Den int  // pictures per second = Num/Den

	// anything else: what ffprobe found, the audio track (stream index, -1 = default) and the frame
	// layout ("2d", "hsbs", "fsbs": side-by-side frames get more width)
	Media  *library.Media
	Audio  int
	Layout string

	noReadrate bool // an FFmpeg older than 6.1 doesn't know -readrate_initial_burst
}

// WebArgs is the FFmpeg command line writing a job's HLS into dir.
func WebArgs(j WebJob, dir string) []string {
	a := []string{"-hide_banner", "-nostdin", "-v", "error"}
	if !j.noReadrate { // about real time once the first seconds are there: no converting a whole film ahead
		a = append(a, "-readrate", "1.5", "-readrate_initial_burst", "20")
	}
	var vf string
	if j.Moflex {
		num, den := j.Num, j.Den
		if num <= 0 || den <= 0 {
			num, den = 24000, 1001
		}
		r := fmt.Sprintf("%d/%d", num, den)
		// FFmpeg times moflex video by counting frames at the declared rate, so the pictures are
		// re-timed here; 3D: the even frames are the left eye, the odd ones the right
		a = append(a, "-f", "moflex", "-i", "pipe:0")
		if j.Stereo {
			vf = fmt.Sprintf("[0:v:0]split[a][b];[a]select='not(mod(n\\,2))',setpts=N/(%s)/TB[l];"+
				"[b]select='mod(n\\,2)',setpts=N/(%s)/TB[r];[l][r]hstack=2,format=yuv420p[v]", r, r)
		} else {
			vf = fmt.Sprintf("[0:v:0]setpts=N/(%s)/TB,format=yuv420p[v]", r)
		}
		a = append(a, "-filter_complex", vf, "-map", "[v]", "-map", "0:a:0?", "-r", r)
		if j.GapMs > 0 {
			a = append(a, "-ss", fmt.Sprintf("%.3f", float64(j.GapMs)/1000))
		}
	} else {
		m := j.Media
		if j.StartMs > 0 {
			a = append(a, "-ss", fmt.Sprintf("%.3f", float64(j.StartMs)/1000))
		}
		a = append(a, "-i", j.Path)
		maxW := 1920
		if j.Layout == "fsbs" {
			maxW = 2560 // two eyes of 1280
		}
		var chain []string
		if m.Interl {
			chain = append(chain, "yadif")
		}
		// square pixels (a DVD's are not) -- except side-by-side, where the layout says how the
		// eyes are packed whatever the file claims -- then at most maxW x 1080
		if j.Layout != "hsbs" && j.Layout != "fsbs" {
			chain = append(chain, "scale=w=trunc(iw*sar/2)*2:h=ih")
		}
		chain = append(chain, "setsar=1",
			fmt.Sprintf("scale=w='min(%d,iw)':h='min(1080,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2", maxW),
			"format=yuv420p")
		a = append(a, "-map", fmt.Sprintf("0:%d", m.Video), "-vf", strings.Join(chain, ","))
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
	}
	return append(a, "-sn", "-dn",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "21", "-maxrate", "6M", "-bufsize", "12M",
		"-profile:v", "high", "-pix_fmt", "yuv420p", "-force_key_frames", "expr:gte(t,n_forced*2)",
		"-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "160k",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "0", "-hls_playlist_type", "event",
		"-hls_flags", "independent_segments+temp_file", "-muxdelay", "0", "-muxpreload", "0",
		"-hls_segment_filename", filepath.Join(dir, "s%05d.ts"), filepath.Join(dir, "index.m3u8"))
}

// WebTranscode runs a browser stream into dir until it ends or ctx is cancelled.
func (t *Tool) WebTranscode(ctx context.Context, j WebJob, dir string) error {
	ff, _ := t.Paths()
	if ff == "" {
		return errors.New("ffmpeg not found")
	}
	cmd := exec.CommandContext(ctx, ff, WebArgs(j, dir)...)
	hideWindow(cmd)
	if j.Moflex {
		cmd.Stdin = j.In
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limited{&stderr, 4096}
	cmd.WaitDelay = 3 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil
	}
	if err != nil && !j.noReadrate && strings.Contains(stderr.String(), "readrate") {
		j.noReadrate = true // an older FFmpeg: again, without the pacing
		if j.Moflex {
			j.In.Seek(0, io.SeekStart)
		}
		return t.WebTranscode(ctx, j, dir)
	}
	if err != nil && stderr.Len() > 0 {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return err
}
