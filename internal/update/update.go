// Package update keeps Big Top current from its GitHub releases: it checks for a newer release,
// and on request downloads the build for this system next to the running one, verifies it against
// the release's SHA256SUMS.txt, starts it, and lets the old one exit. The new instance then
// removes the old file (see Cleanup).
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Repo = "brainphreak/clownsec-bigtop"

// Status is what the setup page shows.
type Status struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	Notes     string `json:"notes,omitempty"`
	Page      string `json:"page,omitempty"` // the release's web page
	State     string `json:"state"`          // "", "checking", "downloading", "restarting"
	Error     string `json:"error,omitempty"`
	Checked   int64  `json:"checked,omitempty"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type Updater struct {
	Current string
	mu      sync.Mutex
	st      Status
	assets  []asset
	client  *http.Client
}

func New(current string) *Updater {
	return &Updater{Current: current, st: Status{Current: current}, client: &http.Client{Timeout: 10 * time.Minute}}
}

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

func (u *Updater) set(f func(*Status)) {
	u.mu.Lock()
	f(&u.st)
	u.mu.Unlock()
}

// Newer reports whether version a (YYMMDD.HHMM, as build.sh stamps them) is later than b.
// A "dev" build never updates.
func Newer(a, b string) bool {
	pa, oka := parse(a)
	pb, okb := parse(b)
	return oka && okb && pa > pb
}

func parse(v string) (int64, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	d, t, ok := strings.Cut(v, ".")
	if !ok || len(d) != 6 {
		return 0, false
	}
	di, err1 := strconv.ParseInt(d, 10, 64)
	ti, err2 := strconv.ParseInt(t, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return di*10000 + ti, true
}

// Check asks GitHub for the latest release.
func (u *Updater) Check() error {
	u.set(func(s *Status) { s.State, s.Error = "checking", "" })
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("User-Agent", "ClownsecBigTop/"+u.Current)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.client.Do(req)
	if err == nil && resp.StatusCode != 200 {
		err = fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode)
		resp.Body.Close()
	}
	if err != nil {
		u.set(func(s *Status) { s.State, s.Error = "", err.Error() })
		return err
	}
	defer resp.Body.Close()
	var rel struct {
		Tag    string  `json:"tag_name"`
		Body   string  `json:"body"`
		Page   string  `json:"html_url"`
		Assets []asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		u.set(func(s *Status) { s.State, s.Error = "", err.Error() })
		return err
	}
	latest := strings.TrimPrefix(rel.Tag, "v")
	u.mu.Lock()
	u.assets = rel.Assets
	u.st.Latest, u.st.Notes, u.st.Page = latest, rel.Body, rel.Page
	u.st.Available = Newer(latest, u.Current) && pick(rel.Assets) != nil
	u.st.State, u.st.Checked = "", time.Now().Unix()
	u.mu.Unlock()
	return nil
}

// Loop checks now (after a short pause) and every 12 hours.
func (u *Updater) Loop(onNew func(Status)) {
	time.Sleep(15 * time.Second)
	for {
		if u.Check() == nil {
			if st := u.Status(); st.Available && onNew != nil {
				onNew(st)
			}
		}
		time.Sleep(12 * time.Hour)
	}
}

// the release file for this system: a running AppImage updates to the AppImage. A Mac build is
// named .command (Finder would take the version's dot for an extension and call it a document);
// the plain name is the fallback releases also carry.
func pick(assets []asset) *asset {
	var suffixes []string
	switch {
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		suffixes = []string{".exe"}
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		suffixes = []string{"-macos-arm64.command", "-macos-arm64"}
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		suffixes = []string{"-macos-intel.command", "-macos-intel"}
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64" && os.Getenv("APPIMAGE") != "":
		suffixes = []string{"-x86_64.AppImage"}
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		suffixes = []string{"-linux-amd64"}
	}
	for _, suffix := range suffixes {
		for i := range assets {
			n := strings.ToLower(assets[i].Name)
			if strings.HasSuffix(n, strings.ToLower(suffix)) && ownName.MatchString(assets[i].Name) {
				return &assets[i]
			}
		}
	}
	return nil
}

// ownName: the files build.sh makes. Only these are ever replaced or deleted.
var ownName = regexp.MustCompile(`(?i)^(ClownsecBigTop|clownsec-bigtop)-\d{6}\.\d{4}`)

// Self is the file this instance runs from (the AppImage itself, for an AppImage).
func Self() (string, error) {
	if p := os.Getenv("APPIMAGE"); p != "" {
		return p, nil
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p, nil
}

// Download fetches the new build next to the running one and verifies it. Returns its path.
func (u *Updater) Download() (string, error) {
	u.mu.Lock()
	a := pick(u.assets)
	assets := u.assets
	u.mu.Unlock()
	if a == nil {
		return "", errors.New("no build for this system in the latest release")
	}
	self, err := Self()
	if err != nil {
		return "", err
	}
	u.set(func(s *Status) { s.State, s.Error = "downloading", "" })
	fail := func(err error) (string, error) {
		u.set(func(s *Status) { s.State, s.Error = "", err.Error() })
		return "", err
	}
	want := ""
	for _, x := range assets { // the release's checksums
		if strings.EqualFold(x.Name, "SHA256SUMS.txt") {
			b, err := u.get(x.URL)
			if err != nil {
				return fail(fmt.Errorf("checksums: %w", err))
			}
			for _, ln := range strings.Split(string(b), "\n") {
				f := strings.Fields(ln)
				if len(f) == 2 && strings.TrimPrefix(f[1], "*") == a.Name {
					want = strings.ToLower(f[0])
				}
			}
		}
	}
	if want == "" {
		return fail(errors.New("the release has no checksum for " + a.Name))
	}
	dest := filepath.Join(filepath.Dir(self), a.Name)
	tmp := dest + ".download"
	resp, err := u.client.Get(a.URL)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fail(fmt.Errorf("download: HTTP %d", resp.StatusCode))
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fail(fmt.Errorf("can't write next to Big Top (%v)", err))
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != a.Size && a.Size > 0 {
		err = fmt.Errorf("download incomplete (%d of %d bytes)", n, a.Size)
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = errors.New("the download doesn't match the release's checksum")
	}
	if err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	return dest, nil
}

func (u *Updater) get(url string) ([]byte, error) {
	resp, err := u.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// MarkRestarting is shown while the new instance starts.
func (u *Updater) MarkRestarting() { u.set(func(s *Status) { s.State = "restarting" }) }

// Cleanup runs in the NEW instance: the file it replaced goes, once the old instance has let go
// of it (Windows keeps a running .exe locked). Only Big Top's own build files are ever deleted.
func Cleanup(old string) {
	self, _ := Self()
	if old == "" || old == self || !ownName.MatchString(filepath.Base(old)) {
		return
	}
	for i := 0; i < 40; i++ {
		if err := os.Remove(old); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
