package media

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// With an encoder built in (-tags moflexenc): it is written into the data folder once, runs, and is
// not rewritten on the next call.
func TestExtractEncoder(t *testing.T) {
	if len(embeddedEncoder) == 0 {
		t.Skip("no encoder built in")
	}
	tool := New(t.TempDir())
	p := tool.MoflexEncoder()
	if p == "" {
		t.Fatal("no encoder")
	}
	out, err := exec.Command(p, "-hide_banner", "-h", "encoder=mobiclip").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "mobiclip") {
		t.Fatalf("extracted encoder doesn't run: %v %s", err, out)
	}
	st1, _ := os.Stat(p)
	encReady = "" // as after a restart: the .sha256 must spare the rewrite
	p2 := tool.MoflexEncoder()
	st2, _ := os.Stat(p2)
	if p2 != p || !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("the encoder was written again")
	}
}
