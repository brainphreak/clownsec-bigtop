// Package media is Big Top's FFmpeg side: finding (or fetching) ffmpeg and ffprobe, reading a
// file's streams, and turning MP4/MKV/AVI/... into the H.264 + AAC MPEG-TS the 3DS player plays.
package media

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Status is what the setup page shows about FFmpeg.
type Status struct {
	Found       bool   `json:"found"`
	FFmpeg      string `json:"ffmpeg,omitempty"`
	Version     string `json:"version,omitempty"`
	CanDownload bool   `json:"canDownload"` // Big Top can fetch it on this system
	Hint        string `json:"hint,omitempty"`
	Problem     string `json:"problem,omitempty"` // found something that can't be used, and why
	State       string `json:"state,omitempty"`   // "", "downloading", "unpacking"
	Done        int64  `json:"done,omitempty"`
	Total       int64  `json:"total,omitempty"`
	Error       string `json:"error,omitempty"`
}

type Tool struct {
	dataDir string

	mu      sync.Mutex
	ffmpeg  string
	ffprobe string
	version string
	problem string
	checked time.Time
	st      Status
}

func New(dataDir string) *Tool { return &Tool{dataDir: dataDir} }

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// the places looked in, first match wins: Big Top's own copy (downloaded), next to Big Top,
// the PATH, then where Homebrew and the usual packages put it (a Mac app started from Finder
// has a short PATH that misses Homebrew).
func (t *Tool) candidates(name string) []string {
	var out []string
	out = append(out, filepath.Join(t.dataDir, "ffmpeg", exe(name)))
	if self, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(self), exe(name)))
	}
	if p, err := exec.LookPath(exe(name)); err == nil {
		out = append(out, p)
	}
	if runtime.GOOS != "windows" {
		out = append(out, "/opt/homebrew/bin/"+name, "/usr/local/bin/"+name, "/usr/bin/"+name, "/snap/bin/"+name)
	}
	return out
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Paths returns ffmpeg and ffprobe ("" when missing). Looked up again at most every 30 s, so
// installing FFmpeg while Big Top runs is noticed.
func (t *Tool) Paths() (ffmpeg, ffprobe string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ffmpeg != "" && t.ffprobe != "" && isFile(t.ffmpeg) && isFile(t.ffprobe) {
		return t.ffmpeg, t.ffprobe
	}
	if time.Since(t.checked) < 30*time.Second {
		return t.ffmpeg, t.ffprobe
	}
	t.checked = time.Now()
	t.ffmpeg, t.ffprobe, t.version, t.problem = "", "", "", ""
	// ffprobe beside ffmpeg first (a matched pair); failing that, any ffprobe that can be found:
	// a static ffmpeg dropped in ~/bin often comes without one while the distro's is in /usr/bin,
	// and the snap names it "ffmpeg.ffprobe"
	for _, c := range t.candidates("ffmpeg") {
		if isFile(c) {
			if p := filepath.Join(filepath.Dir(c), exe("ffprobe")); isFile(p) {
				t.ffmpeg, t.ffprobe = c, p
				break
			}
			if t.ffmpeg == "" {
				t.ffmpeg = c
			}
		}
	}
	if t.ffmpeg != "" && t.ffprobe == "" {
		probes := t.candidates("ffprobe")
		if runtime.GOOS != "windows" {
			if p, err := exec.LookPath("ffmpeg.ffprobe"); err == nil {
				probes = append(probes, p)
			}
			probes = append(probes, "/snap/bin/ffmpeg.ffprobe")
		}
		for _, p := range probes {
			if isFile(p) {
				t.ffprobe = p
				break
			}
		}
		if t.ffprobe == "" {
			t.problem = "Found " + t.ffmpeg + " but not ffprobe, which comes with it. Install ffprobe (on most systems it is part of the ffmpeg package)"
			t.ffmpeg = ""
		}
	}
	if t.ffmpeg != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, t.ffmpeg, "-hide_banner", "-version")
		hideWindow(cmd)
		out, err := cmd.Output()
		if err == nil {
			line, _, _ := strings.Cut(string(out), "\n")
			t.version = strings.TrimSpace(strings.TrimPrefix(line, "ffmpeg version "))
			if i := strings.Index(t.version, " Copyright"); i > 0 {
				t.version = t.version[:i]
			}
		} else {
			msg := strings.TrimSpace(string(out))
			if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
				msg = strings.TrimSpace(string(ee.Stderr))
			}
			if ctx.Err() != nil {
				msg = "it didn't answer within 10 seconds"
			} else if msg == "" {
				msg = err.Error()
			}
			if len(msg) > 300 {
				msg = msg[:300]
			}
			t.problem = "Found " + t.ffmpeg + " but it won't run: " + msg
			t.ffmpeg, t.ffprobe = "", "" // there but won't run
		}
	}
	return t.ffmpeg, t.ffprobe
}

// Available: both tools are here.
func (t *Tool) Available() bool { f, p := t.Paths(); return f != "" && p != "" }

func (t *Tool) Status() Status {
	f, _ := t.Paths()
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.st
	st.Found, st.FFmpeg, st.Version, st.Problem = f != "", f, t.version, t.problem
	st.CanDownload = runtime.GOOS == "windows" && runtime.GOARCH == "amd64"
	switch runtime.GOOS {
	case "darwin":
		st.Hint = "Install it with Homebrew: brew install ffmpeg"
	case "linux":
		st.Hint = "Install it with your package manager, e.g. sudo apt install ffmpeg"
	}
	return st
}

func (t *Tool) set(f func(*Status)) {
	t.mu.Lock()
	f(&t.st)
	t.mu.Unlock()
}

// Windows: the gyan.dev "essentials" build (it has libx264), checked against its published SHA-256.
const (
	winZip    = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
	winZipSum = winZip + ".sha256"
)

// Download fetches FFmpeg into Big Top's data folder (Windows only).
func (t *Tool) Download() error {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return errors.New("Big Top can only download FFmpeg on Windows; " + t.Status().Hint)
	}
	t.mu.Lock()
	if t.st.State != "" {
		t.mu.Unlock()
		return errors.New("already downloading")
	}
	t.st = Status{State: "downloading"}
	t.mu.Unlock()
	err := t.download()
	t.set(func(s *Status) {
		s.State = ""
		if err != nil {
			s.Error = err.Error()
		}
	})
	t.mu.Lock()
	t.checked = time.Time{} // look again now
	t.mu.Unlock()
	return err
}

type counter struct{ t *Tool }

func (c counter) Write(b []byte) (int, error) {
	c.t.set(func(s *Status) { s.Done += int64(len(b)) })
	return len(b), nil
}

func (t *Tool) download() error {
	cl := &http.Client{Timeout: 30 * time.Minute}
	get := func(u string) (*http.Response, error) {
		r, err := cl.Get(u)
		if err == nil && r.StatusCode != 200 {
			r.Body.Close()
			err = fmt.Errorf("%s: HTTP %d", u, r.StatusCode)
		}
		return r, err
	}
	r, err := get(winZipSum)
	if err != nil {
		return err
	}
	sb, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	r.Body.Close()
	want := strings.ToLower(strings.TrimSpace(strings.Fields(string(sb) + " ")[0]))
	if len(want) != 64 {
		return errors.New("FFmpeg's checksum file didn't make sense")
	}
	dir := filepath.Join(t.dataDir, "ffmpeg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "download.zip")
	defer os.Remove(tmp)
	r, err = get(winZip)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	t.set(func(s *Status) { s.Total = r.ContentLength })
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h, counter{t}), r.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return errors.New("the FFmpeg download doesn't match its checksum")
	}
	t.set(func(s *Status) { s.State = "unpacking" })
	zr, err := zip.OpenReader(tmp)
	if err != nil {
		return err
	}
	defer zr.Close()
	got := 0
	for _, zf := range zr.File { // ffmpeg-9.0.2-essentials_build/bin/ffmpeg.exe
		base := filepath.Base(zf.Name)
		if !strings.EqualFold(base, "ffmpeg.exe") && !strings.EqualFold(base, "ffprobe.exe") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		_, err = io.Copy(&buf, rc)
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, strings.ToLower(base)), buf.Bytes(), 0o755); err != nil {
			return err
		}
		got++
	}
	if got < 2 {
		return errors.New("ffmpeg.exe / ffprobe.exe weren't in the download")
	}
	return nil
}
