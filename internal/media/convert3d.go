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
	"strings"
	"time"
)

// Converted 3D: a 2D video streamed in 3D, its depth estimated as it plays (internal/depth3d).
//
//	ffmpeg (decode, from the start time, fitted into 400x240)  -> raw RGB frames
//	the converter (Go)                                        -> full side-by-side 800x240 frames
//	the encoder: libx264 MPEG-TS (New 3DS) or the moflex encoder (Old 3DS), the audio taken from
//	the file itself, from the same start time
//
// The decode keeps the source's frame rate (50/60 fps are halved, as for every stream).

// Converter3D makes the two eyes of a frame: img is RGB24 w x h (as asked of NewConverter3D),
// sbs RGB24 800x240, left eye first.
type Converter3D interface {
	Frame(img, sbs []byte) error
}

// NewConverter3D makes a converter for one stream (it keeps state between frames).
type NewConverter3D func(w, h int) (Converter3D, error)

const sbsW, sbsH = 800, 240

// Transcode3D streams j converted to 3D: MPEG-TS (H.264 800x240 full side-by-side + AAC) or, with
// moflex, a 3D moflex for an Old 3DS (q: its quantizer, 0 = the per-frame one).
func (t *Tool) Transcode3D(ctx context.Context, j Job, moflex bool, q int, newConv NewConverter3D, w io.Writer) error {
	ff, _ := t.Paths()
	if ff == "" {
		return errors.New("ffmpeg not found")
	}
	enc := ff
	if moflex {
		if enc = t.MoflexEncoder(); enc == "" {
			return errors.New("the moflex encoder isn't installed")
		}
	}
	m := j.Media
	fw, fh := Size(m, false) // the picture inside 400x240 (its pixel aspect applied, never enlarged)
	conv, err := newConv(fw, fh)
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// 1. decode
	da := []string{"-hide_banner", "-nostdin", "-v", "error"}
	if j.StartMs > 0 {
		da = append(da, "-ss", fmt.Sprintf("%.3f", float64(j.StartMs)/1000))
	}
	da = append(da, "-i", j.Path)
	var chain []string
	if m.Interl {
		chain = append(chain, "yadif")
	}
	chain = append(chain, fmt.Sprintf("fps=%d/%d", num, den), fmt.Sprintf("scale=%d:%d:flags=area", fw, fh), "setsar=1")
	if j.Burn >= 0 {
		da = append(da, "-filter_complex", fmt.Sprintf("[0:%d][0:%d]overlay=(W-w)/2:H-h,%s[v]",
			m.Video, j.Burn, strings.Join(chain, ",")), "-map", "[v]")
	} else {
		da = append(da, "-map", fmt.Sprintf("0:%d", m.Video), "-vf", strings.Join(chain, ","))
	}
	if j.DurMs > 0 {
		da = append(da, "-t", fmt.Sprintf("%.3f", float64(j.DurMs)/1000))
	}
	da = append(da, "-an", "-sn", "-dn", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	dec := exec.CommandContext(ctx, ff, da...)
	hideWindow(dec)
	playbackPriority(dec)
	var decErr bytes.Buffer
	dec.Stderr = &limited{&decErr, 4096}
	dout, err := dec.StdoutPipe()
	if err != nil {
		return err
	}

	// 3. encode
	ea := []string{"-hide_banner", "-nostdin", "-v", "error",
		"-f", "rawvideo", "-pix_fmt", "rgb24", "-s", fmt.Sprintf("%dx%d", sbsW, sbsH),
		"-framerate", fmt.Sprintf("%d/%d", num, den), "-i", "pipe:0"}
	if j.StartMs > 0 {
		ea = append(ea, "-ss", fmt.Sprintf("%.3f", float64(j.StartMs)/1000))
	}
	ea = append(ea, "-i", j.Path)
	audio := pickAudio(j)
	var cmdEnv []string
	if moflex {
		// the eyes interleaved L,R,L,R at twice the rate: the moflex 3D layout (as for a full SBS file)
		tb := fmt.Sprintf("settb=expr=1/(2*(%d/%d))", num, den)
		fc := fmt.Sprintf("[0:v]split=2[l0][r0];[l0]crop=400:240:0:0,format=yuv420p,%s,setpts=2*N[l];"+
			"[r0]crop=400:240:400:0,format=yuv420p,%s,setpts=2*N+1[r];[l][r]interleave=nb_inputs=2[v]", tb, tb)
		ea = append(ea, "-filter_complex", fc, "-map", "[v]", "-r", fmt.Sprintf("%d/%d", 2*num, den),
			"-mo_layout", "0", "-mo_interleave_input_double_rate", "1")
		if audio >= 0 {
			ea = append(ea, "-map", fmt.Sprintf("1:%d", audio))
		}
		if q < 12 || q > 59 {
			q = MoflexQ
		}
		ea = append(ea, "-sn", "-dn", "-c:v", "mobiclip", "-mobiclip", "1",
			"-qp", fmt.Sprint(12+(q-12)%6), "-mobi_qyx", fmt.Sprint((q-12)/6),
			"-preset", "medium", "-me_method", "hex", "-g", "240", "-keyint_min", "48")
		if audio >= 0 {
			ea = append(ea, "-c:a", "pcm_s16le", "-ar", "44100", "-ac", "2", "-mo_audio", "adpcm")
		}
		ea = append(ea, "-shortest", "-mo_live", "1", "-f", "moflex", "pipe:1")
		cmdEnv = append(os.Environ(), moflexEnv...)
		if t.FastMoflex() {
			cmdEnv = append(cmdEnv, moflexFastSubme)
		} else {
			cmdEnv = append(cmdEnv, moflexSubme)
		}
		cmdEnv = append(cmdEnv, moflexAQ...)
		cmdEnv = append(cmdEnv, "MOBI_DUAL_EYES=1")
	} else {
		kbps := j.Kbps
		if kbps <= 0 {
			kbps = 1600 // 800x240 of two eyes
		}
		ea = append(ea, "-map", "0:v")
		if audio >= 0 {
			ea = append(ea, "-map", fmt.Sprintf("1:%d", audio))
		}
		ea = append(ea, "-sn", "-dn", "-vf", "format=yuv420p",
			"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-level", "4.0",
			"-b:v", fmt.Sprintf("%dk", kbps), "-maxrate", fmt.Sprintf("%dk", kbps*3/2),
			"-bufsize", fmt.Sprintf("%dk", kbps*2), "-g", "48")
		if audio >= 0 {
			ea = append(ea, "-c:a", "aac", "-ac", "2", "-ar", "48000", "-b:a", "128k")
		}
		ea = append(ea, "-shortest", "-f", "mpegts", "-muxdelay", "0", "pipe:1")
	}
	ec := exec.CommandContext(ctx, enc, ea...)
	hideWindow(ec)
	playbackPriority(ec)
	ec.Env = cmdEnv
	ec.Stdout = w
	var encErr bytes.Buffer
	ec.Stderr = &limited{&encErr, 4096}
	ein, err := ec.StdinPipe()
	if err != nil {
		return err
	}
	dec.WaitDelay, ec.WaitDelay = 3*time.Second, 3*time.Second
	if err := dec.Start(); err != nil {
		return err
	}
	if err := ec.Start(); err != nil {
		cancel()
		dec.Wait()
		return err
	}

	// 2. convert, frame by frame, between the two
	convErr := make(chan error, 1)
	go func() {
		defer ein.Close()
		r := bufio.NewReaderSize(dout, fw*fh*3*2)
		img := make([]byte, fw*fh*3)
		sbs := make([]byte, sbsW*sbsH*3)
		for {
			if _, err := io.ReadFull(r, img); err != nil {
				convErr <- nil // the end of the video (or the decode stopped)
				return
			}
			if err := conv.Frame(img, sbs); err != nil {
				convErr <- err
				cancel()
				return
			}
			if _, err := ein.Write(sbs); err != nil {
				convErr <- nil // the encoder stopped (the 3DS went away)
				return
			}
		}
	}()
	eerr := ec.Wait()
	cancel()
	dec.Wait()
	cerr := <-convErr
	if ctx.Err() != nil && cerr == nil && eerr != nil && encErr.Len() == 0 {
		return nil // stopped on purpose
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

// pickAudio: the job's audio track, else the file's default (-1 = none)
func pickAudio(j Job) int {
	m := j.Media
	if j.Audio >= 0 {
		return j.Audio
	}
	if len(m.Audio) == 0 {
		return -1
	}
	for _, t := range m.Audio {
		if t.Default {
			return t.Index
		}
	}
	return m.Audio[0].Index
}
