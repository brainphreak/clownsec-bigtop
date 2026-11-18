package media

import (
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
)

// Live moflex: for an Old 3DS, which can't decode the H.264 of the MPEG-TS path, the video is
// encoded to MobiClip as it plays -- the format the console's own decoder runs in software. The
// encoder is an FFmpeg build with a MobiClip encoder and the moflex muxer's live mode (Mobipeg-
// MOFLEX), kept apart from the FFmpeg Big Top uses for everything else.

// MoflexEncoderName is the encoder's file name (without .exe).
const MoflexEncoderName = "moflex-ffmpeg"

// MoflexEncoder returns the live moflex encoder, "" when there isn't one: $BIGTOP_MOFLEX_ENCODER,
// Big Top's own copy, or one next to Big Top.
func (t *Tool) MoflexEncoder() string {
	var c []string
	if e := os.Getenv("BIGTOP_MOFLEX_ENCODER"); e != "" {
		c = append(c, e)
	}
	c = append(c, filepath.Join(t.dataDir, "moflex", exe(MoflexEncoderName)))
	if self, err := os.Executable(); err == nil {
		c = append(c, filepath.Join(filepath.Dir(self), exe(MoflexEncoderName)))
	}
	for _, p := range c {
		if isFile(p) {
			return p
		}
	}
	return ""
}

// MoflexQ is the base quantizer. By default the encoder picks a quantizer per frame from the
// frame's brightness and detail (MOBI_AQ, fitted to the official encoder's own choices: the finest
// step on dark frames, coarser on bright busy ones) -- about 1.5 Mbit/s of 3D video, which an Old
// 3DS streams and decodes with room to spare. A q in the request means one fixed quantizer instead.
const MoflexQ = 16

// MoflexArgs: the encoder command line for a job. layout is the item's frame shape ("", "2d",
// "hsbs", "fsbs"); q the MobiClip quantizer (12..59, 0 = MoflexQ).
func MoflexArgs(j Job, layout string, q int) []string {
	m := j.Media
	if q < 12 || q > 59 {
		q = MoflexQ // with the per-frame quantizer (moflexEnv) this is only its starting point
	}
	num, den := m.FpsNum, m.FpsDen
	if num <= 0 || den <= 0 {
		num, den = 24000, 1001
	}
	var pre []string
	if m.Interl {
		pre = append(pre, "yadif")
	}
	if float64(num)/float64(den) > 30.5 { // 50/60 fps: every other frame (the 3DS shows 30 at most)
		den *= 2
	}
	pre = append(pre, fmt.Sprintf("fps=%d/%d", num, den))
	// one eye: the source's shape (its pixel aspect applied, a half-width eye stretched back),
	// fitted into 400x240, black bars, 4:2:0
	// The picture's size and position are multiples of 8, so a letterbox or pillarbox edge falls on
	// a transform block boundary: an edge inside a block rings into the black bar (the official
	// encoder blanks to an 8-pixel boundary for the same reason).
	eye := func(k int) string {
		return fmt.Sprintf("scale=w=iw*sar*%d:h=ih:flags=lanczos,setsar=1,"+
			"scale=400:240:force_original_aspect_ratio=decrease:force_divisible_by=8:flags=lanczos,"+
			"pad=400:240:floor((ow-iw)/16)*8:floor((oh-ih)/16)*8:black,setsar=1,format=yuv420p", k)
	}
	stereo := layout == "hsbs" || layout == "fsbs"

	a := []string{"-hide_banner", "-nostdin", "-v", "error"}
	if j.StartMs > 0 {
		a = append(a, "-ss", fmt.Sprintf("%.3f", float64(j.StartMs)/1000))
	}
	a = append(a, "-i", j.Path)
	src := fmt.Sprintf("[0:%d]", m.Video)
	var fc string
	if j.Burn >= 0 && !stereo { // a picture subtitle goes on at full size, before the shrink
		fc = fmt.Sprintf("[0:%d][0:%d]overlay=(W-w)/2:H-h,", m.Video, j.Burn)
		src = ""
	}
	if !stereo {
		fc += src + strings.Join(pre, ",") + "," + eye(1) + "[v]"
		a = append(a, "-filter_complex", fc, "-map", "[v]", "-r", fmt.Sprintf("%d/%d", num, den),
			"-mo_layout", "6")
	} else {
		k := 1
		if layout == "hsbs" {
			k = 2
		}
		// the eyes interleaved L,R,L,R at twice the rate: the moflex 3D layout
		tb := fmt.Sprintf("settb=expr=1/(2*(%d/%d))", num, den)
		fc += fmt.Sprintf("%s%s,split=2[l0][r0];[l0]crop=iw/2:ih:0:0,%s,%s,setpts=2*N[l];"+
			"[r0]crop=iw/2:ih:iw/2:0,%s,%s,setpts=2*N+1[r];[l][r]interleave=nb_inputs=2[v]",
			src, strings.Join(pre, ","), eye(k), tb, eye(k), tb)
		a = append(a, "-filter_complex", fc, "-map", "[v]", "-r", fmt.Sprintf("%d/%d", 2*num, den),
			"-mo_layout", "0", "-mo_interleave_input_double_rate", "1")
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
		"-c:v", "mobiclip", "-mobiclip", "1",
		"-qp", fmt.Sprint(12+(q-12)%6), "-mobi_qyx", fmt.Sprint((q-12)/6),
		"-preset", "medium", "-me_method", "hex", "-g", "240", "-keyint_min", "48")
	if audio >= 0 {
		a = append(a, "-c:a", "pcm_s16le", "-ar", "44100", "-ac", "2", "-mo_audio", "adpcm")
	}
	return append(a, "-mo_live", "1", "-f", "moflex", "pipe:1")
}

// moflexEnv: the encoder's tuning, set through its environment -- the 8x8 luma transform, 4x4 chroma
// split in P frames, inter 4x4 deadzone, intra-in-P bias, motion search, and the per-frame
// quantizer (brightness/detail model, dark frames at the finest step). Measured against the
// official encoder on dark film: see the moflex-encoder notes.
var moflexEnv = []string{"MOBI_8X8=1", "MOBI_8X8_MS=1", "MOBI_SUBME=9",
	"MOBI_V11_MV_REFINE=1", "MOBI_V11_MV_PENALTY=0", "MOBI_SKIP=0",
	"MOBI_CSPLIT=1", "MOBI_DZ=3", "MOBI_INTER4_DZ=1", "MOBI_P_INTRA_BIAS=200"}

// moflexAQ: the per-frame quantizer, unless the request fixed one (offset 4 = ~1.5 Mbit/s 3D)
var moflexAQ = []string{"MOBI_AQ=1", "MOBI_AQ_OFFSET=4.0"}

// TranscodeMoflex runs a live moflex job, writing to w until it ends or ctx is cancelled.
func (t *Tool) TranscodeMoflex(ctx context.Context, j Job, layout string, q int, w io.Writer) error {
	enc := t.MoflexEncoder()
	if enc == "" {
		return errors.New("the moflex encoder isn't installed")
	}
	cmd := exec.CommandContext(ctx, enc, MoflexArgs(j, layout, q)...)
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), moflexEnv...)
	if q < 12 || q > 59 {
		cmd.Env = append(cmd.Env, moflexAQ...)
	}
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
