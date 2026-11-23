package depth3d

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The 2D-to-3D packages (ONNX Runtime + the depth models) are downloaded on first use, not built
// into Big Top: they are ~16 MB per platform plus 50 MB per model size. A release of this repo holds
// them, clownsec.com mirrors it; depth3d.json lists every file with its size and SHA-256.
var Sources = []string{
	"https://github.com/brainphreak/clownsec-bigtop/releases/download/depth3d-v1/",
	"https://clownsec.com/bigtop/depth3d-v1/",
}

type File struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type RuntimePkg struct {
	File
	Lib   string `json:"lib"`   // the library inside the zip
	Accel string `json:"accel"` // "DirectML", "CoreML", "CPU"
}

type Model struct {
	File
	Name   string `json:"name"` // "distill", "midas"
	Tier   int    `json:"tier"` // 322 / 392 / 448 (the input width); MiDaS: 256
	H      int    `json:"h"`
	W      int    `json:"w"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

type Manifest struct {
	Version  int                   `json:"version"`
	Runtime  map[string]RuntimePkg `json:"runtime"`
	Models   []Model               `json:"models"`
	Licenses []File                `json:"licenses"`
}

// Platform: this build's key in Manifest.Runtime ("windows-amd64", "darwin-arm64", "linux-amd64").
func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// Status of the packages, for the setup page.
type Status struct {
	State     string `json:"state,omitempty"` // "", "downloading", "checking"
	What      string `json:"what,omitempty"`  // the file being fetched
	Done      int64  `json:"done,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Error     string `json:"error,omitempty"`
	Supported bool   `json:"supported"`        // a runtime exists for this platform
	Installed bool   `json:"installed"`        // the runtime is here
	Accel     string `json:"accel,omitempty"`  // what will run the models
	Models    []int  `json:"models,omitempty"` // Distill tiers present (322, 392, 448)
	MiDaS     bool   `json:"midas,omitempty"`  // MiDaS present
}

// Packages manages the downloaded files in <data>/depth3d.
type Packages struct {
	Dir string

	mu  sync.Mutex
	st  Status
	man *Manifest // the installed manifest (what the files on disk came from)
}

func NewPackages(dataDir string) *Packages {
	p := &Packages{Dir: filepath.Join(dataDir, "depth3d")}
	if b, err := os.ReadFile(filepath.Join(p.Dir, "depth3d.json")); err == nil {
		var m Manifest
		if json.Unmarshal(b, &m) == nil {
			p.man = &m
		}
	}
	return p
}

// Status says what is installed (from what is on disk) and any download in progress.
func (p *Packages) Status() Status {
	p.mu.Lock()
	st, man := p.st, p.man
	p.mu.Unlock()
	st.Supported = runtime.GOOS == "windows" && runtime.GOARCH == "amd64" ||
		runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" || runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
	if man == nil {
		return st
	}
	if rp, ok := man.Runtime[Platform()]; ok && exists(filepath.Join(p.Dir, "runtime", rp.Lib)) {
		st.Installed, st.Accel = true, rp.Accel
	}
	for _, m := range man.Models {
		if !exists(filepath.Join(p.Dir, m.File.File)) {
			continue
		}
		if m.Name == "midas" {
			st.MiDaS = true
		} else if !hasInt(st.Models, m.Tier) {
			st.Models = append(st.Models, m.Tier)
		}
	}
	return st
}

// LibPath: the ONNX Runtime library to Load ("" = not installed).
func (p *Packages) LibPath() string {
	p.mu.Lock()
	man := p.man
	p.mu.Unlock()
	if man == nil {
		return ""
	}
	rp, ok := man.Runtime[Platform()]
	if !ok {
		return ""
	}
	l := filepath.Join(p.Dir, "runtime", rp.Lib)
	if !exists(l) {
		return ""
	}
	return l
}

// Model: the installed model for a picture of aspect w/h -- Distill at tier (the input width), the
// one whose shape is closest to the picture's; tier 0 = MiDaS. ok = false: not installed.
func (p *Packages) Model(tier int, w, h int) (Model, string, bool) {
	p.mu.Lock()
	man := p.man
	p.mu.Unlock()
	if man == nil {
		return Model{}, "", false
	}
	var best Model
	found := false
	for _, m := range man.Models {
		if (tier == 0) != (m.Name == "midas") || (tier != 0 && m.Tier != tier) {
			continue
		}
		if !exists(filepath.Join(p.Dir, m.File.File)) {
			continue
		}
		if !found || absf(float64(m.W)/float64(m.H)-float64(w)/float64(h)) <
			absf(float64(best.W)/float64(best.H)-float64(w)/float64(h)) {
			best, found = m, true
		}
	}
	return best, filepath.Join(p.Dir, best.File.File), found
}

// Install downloads the runtime for this platform and the models for the given Distill tiers
// (midas: MiDaS too), checking each file; files already here and intact are kept.
func (p *Packages) Install(tiers []int, midas bool) error {
	p.mu.Lock()
	if p.st.State != "" {
		p.mu.Unlock()
		return errors.New("already downloading")
	}
	p.st = Status{State: "downloading", What: "depth3d.json"}
	p.mu.Unlock()
	err := p.install(tiers, midas)
	p.mu.Lock()
	p.st.State, p.st.What, p.st.Done, p.st.Total = "", "", 0, 0
	p.st.Error = ""
	if err != nil {
		p.st.Error = err.Error()
	}
	p.mu.Unlock()
	return err
}

func (p *Packages) install(tiers []int, midas bool) error {
	if err := os.MkdirAll(filepath.Join(p.Dir, "runtime"), 0o755); err != nil {
		return err
	}
	cl := &http.Client{Timeout: 60 * time.Minute}
	// the manifest: from the first source that answers
	var man Manifest
	var raw []byte
	var errs []string
	src := ""
	for _, s := range Sources {
		b, err := fetchSmall(cl, s+"depth3d.json")
		if err == nil && json.Unmarshal(b, &man) == nil && man.Version == 1 {
			raw, src = b, s
			break
		}
		errs = append(errs, fmt.Sprintf("%s: %v", s, err))
	}
	if src == "" {
		return errors.New("couldn't reach the 2D-to-3D packages (" + strings.Join(errs, "; ") + ")")
	}
	rp, ok := man.Runtime[Platform()]
	if !ok {
		return errors.New("2D to 3D isn't available for this computer (" + Platform() + ")")
	}
	want := []File{rp.File}
	for _, m := range man.Models {
		if (m.Name == "midas" && midas) || (m.Name == "distill" && hasInt(tiers, m.Tier)) {
			want = append(want, m.File)
		}
	}
	want = append(want, man.Licenses...)
	var total int64
	for _, f := range want {
		if !p.intact(f) {
			total += f.Size
		}
	}
	p.mu.Lock()
	p.st.Total = total
	p.mu.Unlock()
	for _, f := range want {
		if p.intact(f) {
			continue
		}
		if err := p.fetch(cl, f); err != nil {
			return err
		}
	}
	// the runtime: unpacked into runtime/
	if err := unzip(filepath.Join(p.Dir, rp.File.File), filepath.Join(p.Dir, "runtime")); err != nil {
		return fmt.Errorf("unpacking ONNX Runtime: %w", err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir, "depth3d.json"), raw, 0o644); err != nil {
		return err
	}
	p.mu.Lock()
	p.man = &man
	p.mu.Unlock()
	return nil
}

// fetch downloads one file from the first source that has it intact
func (p *Packages) fetch(cl *http.Client, f File) error {
	var last error
	for _, s := range Sources {
		p.mu.Lock()
		p.st.What = f.File
		done := p.st.Done
		p.mu.Unlock()
		err := p.fetchFrom(cl, s+f.File, f)
		if err == nil {
			return nil
		}
		last = err
		p.mu.Lock()
		p.st.Done = done // the next source starts the file over
		p.mu.Unlock()
	}
	return fmt.Errorf("%s: %w", f.File, last)
}

func (p *Packages) fetchFrom(cl *http.Client, url string, f File) error {
	r, err := cl.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	tmp := filepath.Join(p.Dir, f.File+".part")
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h, progress{p}), io.LimitReader(r.Body, f.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		err = errors.New("the download doesn't match its checksum")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Join(p.Dir, f.File))
}

type progress struct{ p *Packages }

func (w progress) Write(b []byte) (int, error) {
	w.p.mu.Lock()
	w.p.st.Done += int64(len(b))
	w.p.mu.Unlock()
	return len(b), nil
}

// intact: the file is here with the right size and checksum
func (p *Packages) intact(f File) bool {
	path := filepath.Join(p.Dir, f.File)
	fi, err := os.Stat(path)
	if err != nil || fi.Size() != f.Size {
		return false
	}
	fh, err := os.Open(path)
	if err != nil {
		return false
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == f.SHA256
}

func fetchSmall(cl *http.Client, url string) ([]byte, error) {
	r, err := cl.Get(url)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

// unzip: the zip's files, flat, into dir (the packages hold no folders)
func unzip(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(zf.Name)
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func hasInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
