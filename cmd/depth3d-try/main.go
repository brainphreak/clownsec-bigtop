// depth3d-try: a developer tool for the 2D-to-3D converter -- one video through the converter to a
// full side-by-side MP4 (800x240, left eye first), with the speed reported.
//
//	depth3d-try -ort libonnxruntime.dylib -model distill_small_224x392.onnx -mh 224 -mw 392 in.mkv out.mp4
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/depth3d"
)

func main() {
	ortPath := flag.String("ort", "", "ONNX Runtime library")
	model := flag.String("model", "", "depth model (.onnx)")
	inName := flag.String("in", "image", "model input name (MiDaS: 0)")
	outName := flag.String("out", "depth", "model output name (MiDaS: 797)")
	mh := flag.Int("mh", 224, "model input height")
	mw := flag.Int("mw", 392, "model input width")
	cpu := flag.Bool("cpu", false, "CPU only")
	secs := flag.Float64("secs", 0, "only this many seconds")
	bench := flag.Int("bench", 0, "no video: time the converter on this many generated 400x224 frames")
	flag.Parse()
	if *bench == 0 && flag.NArg() != 2 {
		log.Fatal("usage: depth3d-try [flags] in out.mp4")
	}

	rt, err := depth3d.Load(*ortPath)
	if err != nil {
		log.Fatal(err)
	}
	rt.CacheDir = os.Getenv("DEPTH3D_CACHE")
	rt.Units = os.Getenv("DEPTH3D_UNITS")
	mb, err := os.ReadFile(*model)
	if err != nil {
		log.Fatal(err)
	}
	t0 := time.Now()
	sess, err := rt.NewSession(mb, *inName, *outName, *mh, *mw, !*cpu)
	if err != nil {
		log.Fatal(err)
	}
	defer sess.Close()
	log.Printf("ONNX Runtime %s, model on %s (loaded in %v)", rt.Version, sess.Device, time.Since(t0).Round(time.Millisecond))

	if *bench > 0 {
		runBench(sess, *bench)
		return
	}
	src, dst := flag.Arg(0), flag.Arg(1)
	sw, sh, fps := probe(src)
	fw, fh := depth3d.Fit(sw, sh)
	conv := depth3d.NewConverter(sess, fw, fh, depth3d.Defaults)
	log.Printf("%dx%d -> %dx%d, %s fps", sw, sh, fw, fh, fps)

	lim := []string{}
	if *secs > 0 {
		lim = []string{"-t", fmt.Sprint(*secs)}
	}
	dec := exec.Command("ffmpeg", append(append([]string{"-v", "error"}, lim...), "-i", src, "-vf",
		fmt.Sprintf("scale=%d:%d:flags=area", fw, fh), "-f", "rawvideo", "-pix_fmt", "rgb24", "-")...)
	dout, _ := dec.StdoutPipe()
	enc := exec.Command("ffmpeg", append(append([]string{"-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", "rgb24",
		"-s", "800x240", "-r", fps, "-i", "-"}, lim...), "-i", src, "-map", "0:v", "-map", "1:a:0?", "-c:v", "libx264",
		"-crf", "18", "-preset", "fast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", dst)...)
	ein, _ := enc.StdinPipe()
	enc.Stderr = os.Stderr
	if err := dec.Start(); err != nil {
		log.Fatal(err)
	}
	if err := enc.Start(); err != nil {
		log.Fatal(err)
	}
	r := bufio.NewReaderSize(dout, fw*fh*3*2)
	img := make([]byte, fw*fh*3)
	sbs := make([]byte, 800*240*3)
	n := 0
	var tConv time.Duration
	start := time.Now()
	for {
		if _, err := io.ReadFull(r, img); err != nil {
			break
		}
		t := time.Now()
		if err := conv.Frame(img, sbs); err != nil {
			log.Fatal(err)
		}
		tConv += time.Since(t)
		if _, err := ein.Write(sbs); err != nil {
			log.Fatal(err)
		}
		n++
	}
	ein.Close()
	enc.Wait()
	dec.Wait()
	el := time.Since(start)
	log.Printf("%d frames in %.1fs = %.1f fps overall; converter %.1f ms a frame", n, el.Seconds(),
		float64(n)/el.Seconds(), float64(tConv.Microseconds())/1000/float64(max(n, 1)))
}

func probe(path string) (int, int, string) {
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries",
		"stream=width,height,r_frame_rate", "-of", "csv=p=0", path).Output()
	if err != nil {
		log.Fatal(err)
	}
	f := strings.Split(strings.TrimSpace(strings.Split(string(out), "\n")[0]), ",")
	w, _ := strconv.Atoi(f[0])
	h, _ := strconv.Atoi(f[1])
	return w, h, f[2]
}

// runBench: the converter alone, on moving generated frames (a gradient with a sliding block)
func runBench(sess *depth3d.Session, n int) {
	w, h := 400, 224
	conv := depth3d.NewConverter(sess, w, h, depth3d.Defaults)
	img := make([]byte, w*h*3)
	sbs := make([]byte, 800*240*3)
	var tot time.Duration
	for i := 0; i < n+5; i++ {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := byte((x + y + i*3) & 255)
				if x > 100+i%200 && x < 180+i%200 && y > 60 && y < 160 {
					v = 255 - v
				}
				p := (y*w + x) * 3
				img[p], img[p+1], img[p+2] = v, byte(y), byte(x)
			}
		}
		t := time.Now()
		if err := conv.Frame(img, sbs); err != nil {
			log.Fatal(err)
		}
		if i >= 5 { // the first few warm up the GPU
			tot += time.Since(t)
		}
	}
	ms := float64(tot.Microseconds()) / 1000 / float64(n)
	log.Printf("converter: %.1f ms a frame = %.0f fps (%d frames)", ms, 1000/ms, n)
}
