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
// browser plays -- through hls.js, or the browser's own HLS. A stream is the whole film, segment k
// holding [2k, 2k+2) s; Big Top runs FFmpeg from whichever segment is wanted (WebJob.Seg), so a
// seek anywhere -- the page's or a phone's own full-screen controls -- finds its piece.
//
// A 3D file keeps both eyes in one frame, side by side, for the page to draw (anaglyph, side by
// side, 2D) -- or, for a phone that can only put a plain video full screen, FFmpeg draws the chosen
// view into the picture itself (WebJob.View).

// SegSec is a segment's length.
const SegSec = 2

// WebJob is one run of FFmpeg for a browser stream.
type WebJob struct {
	Path string
	Seg  int // the first segment to write (it starts at Seg*SegSec in the film)

	// moflex: the file from byte In (a keyframe start, GapMs before the segment's time), the
	// frame rate per picture
	Moflex   bool
	In       io.ReadSeeker
	GapMs    int64
	Stereo   bool // pairs of eye frames, interleaved: one side-by-side picture per pair
	Num, Den int  // pictures per second = Num/Den
	InAudio  int  // which audio stream inside the blocks (0 = the first)

	// anything else: what ffprobe found and the audio track (stream index, -1 = default)
	Media *library.Media
	Audio int

	// Frame is the picture's layout ("2d", or both eyes side by side: "hsbs", "fsbs"); View, when
	// set, is drawn into the picture: "anaglyph", "fsbs", "hsbs" or "2d" (Swap: right eye first)
	Frame string
	View  string
	Swap  bool

	// AltAudio: a moflex's extra audio track as raw PCM (s16le) from this URL -- Big Top's own,
	// starting where the moflex input does -- instead of the in-band track
	AltAudio          string
	AltRate, AltChans int

	noReadrate bool // an FFmpeg older than 6.1 doesn't know -readrate_initial_burst
}

// StartMs is where in the film the job starts.
func (j WebJob) StartMs() int64 { return int64(j.Seg) * SegSec * 1000 }

// bake: the filters that draw a side-by-side picture as the chosen view (nil = leave it as it is).
func bake(frame, view string, swap bool) []string {
	if frame != "hsbs" && frame != "fsbs" || view == "" {
		return nil
	}
	var f []string
	if frame == "hsbs" {
		f = append(f, "scale=w=iw*2:h=ih") // each eye back to its full width
	}
	in := "sbsl"
	if swap {
		in = "sbsr"
	}
	switch view {
	case "anaglyph": // red/cyan, Dubois (as the page draws it)
		f = append(f, "stereo3d=in="+in+":out=arcd")
	case "2d":
		f = append(f, "crop=w=iw/2:h=ih:x=0:y=0")
	case "hsbs":
		if swap {
			f = append(f, "stereo3d=in=sbsr:out=sbsl")
		}
		f = append(f, "scale=w=iw/2:h=ih")
	default: // fsbs
		if swap {
			f = append(f, "stereo3d=in=sbsr:out=sbsl")
		}
	}
	return append(f, "scale=w='min(1920,iw)':h=-2", "setsar=1")
}

// WebArgs is the FFmpeg command line writing a job's segments into dir.
func WebArgs(j WebJob, dir string) []string {
	a := []string{"-hide_banner", "-nostdin", "-v", "error"}
	if !j.noReadrate { // about real time once the first seconds are there: no converting a whole film ahead
		a = append(a, "-readrate", "1.5", "-readrate_initial_burst", "60")
	}
	start := float64(j.StartMs()) / 1000
	if j.Moflex {
		num, den := j.Num, j.Den
		if num <= 0 || den <= 0 {
			num, den = 24000, 1001
		}
		r := fmt.Sprintf("%d/%d", num, den)
		// FFmpeg times moflex video by counting frames at the declared rate, so the pictures are
		// re-timed here; 3D: the even frames are the left eye, the odd ones the right
		a = append(a, "-f", "moflex", "-i", "pipe:0")
		amap := fmt.Sprintf("0:a:%d?", j.InAudio)
		if j.AltAudio != "" {
			a = append(a, "-f", "s16le", "-ar", fmt.Sprint(j.AltRate), "-ac", fmt.Sprint(j.AltChans), "-i", j.AltAudio)
			amap = "1:a:0"
		}
		var vf string
		if j.Stereo {
			vf = fmt.Sprintf("[0:v:0]split[a][b];[a]select='not(mod(n\\,2))',setpts=N/(%s)/TB[l];"+
				"[b]select='mod(n\\,2)',setpts=N/(%s)/TB[r];[l][r]hstack=2", r, r)
			if b := bake("fsbs", j.View, j.Swap); b != nil {
				vf += "," + strings.Join(b, ",")
			}
		} else {
			vf = fmt.Sprintf("[0:v:0]setpts=N/(%s)/TB", r)
		}
		a = append(a, "-filter_complex", vf+",format=yuv420p[v]", "-map", "[v]", "-map", amap, "-r", r)
		if j.GapMs > 0 {
			a = append(a, "-ss", fmt.Sprintf("%.3f", float64(j.GapMs)/1000))
		}
	} else {
		m := j.Media
		if start > 0 {
			a = append(a, "-ss", fmt.Sprintf("%.3f", start))
		}
		a = append(a, "-i", j.Path)
		maxW := 1920
		if j.Frame == "fsbs" {
			maxW = 2560 // two eyes of 1280
		}
		var chain []string
		if m.Interl {
			chain = append(chain, "yadif")
		}
		// square pixels (a DVD's are not) -- except side-by-side, where the layout says how the
		// eyes are packed whatever the file claims -- then at most maxW x 1080
		if j.Frame != "hsbs" && j.Frame != "fsbs" {
			chain = append(chain, "scale=w=trunc(iw*sar/2)*2:h=ih")
		}
		chain = append(chain, "setsar=1",
			fmt.Sprintf("scale=w='min(%d,iw)':h='min(1080,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2", maxW))
		chain = append(chain, bake(j.Frame, j.View, j.Swap)...)
		chain = append(chain, "format=yuv420p")
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
	// keyframes every SegSec from the job's start (a multiple of SegSec), so segment k always
	// begins at k*SegSec; the timestamps carry on from where in the film the job starts
	return append(a, "-sn", "-dn",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "21", "-maxrate", "6M", "-bufsize", "12M",
		"-profile:v", "high", "-pix_fmt", "yuv420p", "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", SegSec),
		"-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "160k",
		"-output_ts_offset", fmt.Sprintf("%.3f", start),
		"-f", "hls", "-hls_time", fmt.Sprint(SegSec), "-hls_list_size", "0", "-start_number", fmt.Sprint(j.Seg),
		"-hls_flags", "independent_segments+temp_file", "-muxdelay", "0", "-muxpreload", "0",
		"-hls_segment_filename", filepath.Join(dir, "s%05d.ts"), filepath.Join(dir, "ffmpeg.m3u8"))
}

// WebTranscode runs one job into dir until it ends or ctx is cancelled.
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
