package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

	// Conv: a 2D video converted to 3D as it plays (internal/depth3d): the picture goes through it
	// and comes out full side-by-side (Frame "fsbs"), each eye at most WebEyeW x WebEyeH
	Conv NewConverter3D
	// DurMs / NoPace: the setup's speed test -- this much of the video, as fast as it goes
	DurMs  int64
	NoPace bool
}

// the browser's 2D-to-3D eye size: sharper than the 3DS's 400x240, still quick to convert
const WebEyeW, WebEyeH = 640, 360

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
	if j.Conv != nil && !j.Moflex {
		return t.webTranscode3D(ctx, j, dir, ff)
	}
	cmd := exec.CommandContext(ctx, ff, WebArgs(j, dir)...)
	hideWindow(cmd)
	playbackPriority(cmd)
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

// webTranscode3D: a 2D video converted to 3D for the browser. FFmpeg decodes from the job's start
// (paced like every browser stream), the converter makes full side-by-side frames, and a second
// FFmpeg encodes them -- through the chosen view, as for a real 3D file -- into the HLS segments,
// the audio from the file.
func (t *Tool) webTranscode3D(ctx context.Context, j WebJob, dir, ff string) error {
	m := j.Media
	sar := m.SAR
	if sar <= 0 {
		sar = 1
	}
	dw, dh := float64(m.W)*sar, float64(m.H)
	f := min(float64(WebEyeW)/dw, float64(WebEyeH)/dh, 1)
	fw, fh := int(dw*f)&^1, int(dh*f)&^1
	if fw < 16 || fh < 16 {
		return errors.New("the picture is too small to convert")
	}
	conv, err := j.Conv(fw, fh, 2*fw, fh)
	if err != nil {
		return err
	}
	num, den := m.FpsNum, m.FpsDen
	if num <= 0 || den <= 0 {
		num, den = 24000, 1001
	}
	if float64(num)/float64(den) > 30.5 {
		den *= 2
	}
	start := float64(j.StartMs()) / 1000
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	da := []string{"-hide_banner", "-nostdin", "-v", "error"}
	if !j.noReadrate && !j.NoPace {
		da = append(da, "-readrate", "1.5", "-readrate_initial_burst", "60")
	}
	if start > 0 {
		da = append(da, "-ss", fmt.Sprintf("%.3f", start))
	}
	da = append(da, "-i", j.Path)
	if j.DurMs > 0 {
		da = append(da, "-t", fmt.Sprintf("%.3f", float64(j.DurMs)/1000))
	}
	var chain []string
	if m.Interl {
		chain = append(chain, "yadif")
	}
	chain = append(chain, fmt.Sprintf("fps=%d/%d", num, den), fmt.Sprintf("scale=%d:%d:flags=area", fw, fh), "setsar=1")
	da = append(da, "-map", fmt.Sprintf("0:%d", m.Video), "-vf", strings.Join(chain, ","),
		"-an", "-sn", "-dn", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	dec := exec.CommandContext(ctx, ff, da...)
	hideWindow(dec)
	playbackPriority(dec)
	var decErr bytes.Buffer
	dec.Stderr = &limited{&decErr, 4096}
	dout, err := dec.StdoutPipe()
	if err != nil {
		return err
	}

	vf := append([]string{"setsar=1"}, bake("fsbs", j.View, j.Swap)...)
	vf = append(vf, "format=yuv420p")
	ea := []string{"-hide_banner", "-nostdin", "-v", "error",
		"-f", "rawvideo", "-pix_fmt", "rgb24", "-s", fmt.Sprintf("%dx%d", 2*fw, fh),
		"-framerate", fmt.Sprintf("%d/%d", num, den), "-i", "pipe:0"}
	if start > 0 {
		ea = append(ea, "-ss", fmt.Sprintf("%.3f", start))
	}
	ea = append(ea, "-i", j.Path, "-map", "0:v", "-vf", strings.Join(vf, ","))
	if a := pickAudio(Job{Media: m, Audio: j.Audio}); a >= 0 {
		ea = append(ea, "-map", fmt.Sprintf("1:%d", a))
	}
	ea = append(ea, "-sn", "-dn",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "21", "-maxrate", "6M", "-bufsize", "12M",
		"-profile:v", "high", "-pix_fmt", "yuv420p", "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", SegSec),
		"-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "160k", "-shortest",
		"-output_ts_offset", fmt.Sprintf("%.3f", start),
		"-f", "hls", "-hls_time", fmt.Sprint(SegSec), "-hls_list_size", "0", "-start_number", fmt.Sprint(j.Seg),
		"-hls_flags", "independent_segments+temp_file", "-muxdelay", "0", "-muxpreload", "0",
		"-hls_segment_filename", filepath.Join(dir, "s%05d.ts"), filepath.Join(dir, "ffmpeg.m3u8"))
	enc := exec.CommandContext(ctx, ff, ea...)
	hideWindow(enc)
	playbackPriority(enc)
	var encErr bytes.Buffer
	enc.Stderr = &limited{&encErr, 4096}
	ein, err := enc.StdinPipe()
	if err != nil {
		return err
	}
	dec.WaitDelay, enc.WaitDelay = 3*time.Second, 3*time.Second
	if err := dec.Start(); err != nil {
		return err
	}
	if err := enc.Start(); err != nil {
		cancel()
		dec.Wait()
		return err
	}
	convErr := make(chan error, 1)
	go func() {
		defer ein.Close()
		r := bufio.NewReaderSize(dout, fw*fh*3*2)
		img := make([]byte, fw*fh*3)
		sbs := make([]byte, 2*fw*fh*3)
		for {
			if _, err := io.ReadFull(r, img); err != nil {
				convErr <- nil
				return
			}
			if err := conv.Frame(img, sbs); err != nil {
				convErr <- err
				cancel()
				return
			}
			if _, err := ein.Write(sbs); err != nil {
				convErr <- nil
				return
			}
		}
	}()
	eerr := enc.Wait()
	cancel()
	derr := dec.Wait()
	cerr := <-convErr
	if parent.Err() != nil {
		return nil // stopped on purpose (another place, or the browser went away)
	}
	if derr != nil && !j.noReadrate && strings.Contains(decErr.String(), "readrate") {
		j.noReadrate = true // an older FFmpeg: again, without the pacing
		return t.webTranscode3D(parent, j, dir, ff)
	}
	switch {
	case cerr != nil:
		return fmt.Errorf("2D to 3D: %w", cerr)
	case eerr != nil && encErr.Len() > 0:
		return fmt.Errorf("%v: %s", eerr, strings.TrimSpace(encErr.String()))
	case decErr.Len() > 0:
		return fmt.Errorf("decode: %s", strings.TrimSpace(decErr.String()))
	}
	return nil
}

// SpeedWeb3D: how fast this computer makes a browser 2D-to-3D stream (x real time), over durMs
// of the video, unpaced, into a scratch folder.
func (t *Tool) SpeedWeb3D(ctx context.Context, m *library.Media, path string, conv NewConverter3D, durMs int64, limit time.Duration, scratch string) (float64, error) {
	ff, _ := t.Paths()
	if ff == "" {
		return 0, errors.New("ffmpeg not found")
	}
	dir, err := os.MkdirTemp(scratch, "speed3d-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	t0 := time.Now()
	err = t.webTranscode3D(ctx, WebJob{Path: path, Media: m, Audio: -1, Frame: "fsbs", View: "fsbs", Conv: conv,
		DurMs: durMs, NoPace: true}, dir, ff)
	el := time.Since(t0)
	if err != nil {
		return 0, err
	}
	if ctx.Err() != nil {
		el = limit
	}
	return float64(durMs) / 1000 / el.Seconds(), nil
}
