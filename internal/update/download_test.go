package update

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Download reports its progress while the file arrives, and verifies the checksum.
func TestDownloadProgress(t *testing.T) {
	body := make([]byte, 400000)
	for i := range body {
		body[i] = byte(i)
	}
	sum := sha256.Sum256(body)
	name := "clownsec-bigtop-270101.0000" + suffixForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sums" {
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"))
			return
		}
		for i := 0; i < len(body); i += 50000 { // slowly, so the progress can be seen
			w.Write(body[i : i+50000])
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("APPIMAGE", filepath.Join(dir, "clownsec-bigtop-261114.0300-x86_64.AppImage")) // Self() -> dir
	u := New("261114.0300")
	u.assets = []asset{{Name: "SHA256SUMS.txt", URL: srv.URL + "/sums"}, {Name: name, URL: srv.URL + "/file", Size: int64(len(body))}}
	done := make(chan error)
	var path string
	go func() { var err error; path, err = u.Download(); done <- err }()
	sawPartial := false
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if !sawPartial {
				t.Fatal("never saw a partial download in the status")
			}
			if b, _ := os.ReadFile(path); len(b) != len(body) {
				t.Fatalf("saved %d bytes", len(b))
			}
			return
		default:
			st := u.Status()
			if st.State == "downloading" && st.Total == int64(len(body)) && st.Done > 0 && st.Done < st.Total {
				sawPartial = true
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func suffixForTest() string {
	if a := pick([]asset{{Name: "clownsec-bigtop-270101.0000-macos-arm64.command"}, {Name: "clownsec-bigtop-270101.0000-macos-intel.command"},
		{Name: "ClownsecBigTop-270101.0000.exe"}, {Name: "clownsec-bigtop-270101.0000-linux-amd64"},
		{Name: "clownsec-bigtop-270101.0000-x86_64.AppImage"}}); a != nil {
		return a.Name[len("clownsec-bigtop-270101.0000"):]
	}
	return ""
}

// Windows-style: the update takes the running program's own name; the running file is moved aside
// and, in the new instance, Cleanup removes it.
func TestDownloadSameName(t *testing.T) {
	body := []byte("the new version")
	sum := sha256.Sum256(body)
	name := "clownsec-bigtop-270101.0000" + suffixForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sums" {
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"))
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	self := filepath.Join(dir, "ClownsecBigTop.exe")
	os.WriteFile(self, []byte("the running version"), 0o755)
	t.Setenv("APPIMAGE", self) // Self() -> this file
	old := InstallName
	InstallName = func(string) string { return "ClownsecBigTop.exe" }
	defer func() { InstallName = old }()
	u := New("261114.0300")
	u.assets = []asset{{Name: "SHA256SUMS.txt", URL: srv.URL + "/sums"}, {Name: name, URL: srv.URL + "/f", Size: int64(len(body))}}
	p, err := u.Download()
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(p, self) {
		t.Fatalf("installed as %s, want %s", p, self)
	}
	if b, _ := os.ReadFile(self); string(b) != "the new version" {
		t.Fatalf("the file holds %q", b)
	}
	if b, _ := os.ReadFile(self + ".old"); string(b) != "the running version" {
		t.Fatalf(".old holds %q", b)
	}
	Cleanup(self) // the new instance: told it replaced "itself"
	if _, err := os.Stat(self + ".old"); !os.IsNotExist(err) {
		t.Fatal(".old is still there")
	}
	if _, err := os.Stat(self); err != nil {
		t.Fatal("Cleanup removed the new version")
	}
}
