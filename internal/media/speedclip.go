package media

import (
	_ "embed"
	"os"
	"path/filepath"
)

// The speed test's clip: 20 s of Big Buck Bunny in 3D (1920x1080 half side-by-side H.264, AAC),
// the same everywhere so every computer's result compares. (c) Blender Foundation, CC BY 3.0 --
// see speedclip/NOTICE.txt.
//
//go:embed speedclip/speedtest-hsbs.mp4
var speedClip []byte

// SpeedClipMs is the clip's length.
const SpeedClipMs = 20000

// SpeedClip: the clip as a file in the data folder (the encoder reads a path), written once.
func (t *Tool) SpeedClip() (string, error) {
	p := filepath.Join(t.dataDir, "speedtest-hsbs.mp4")
	if fi, err := os.Stat(p); err == nil && fi.Size() == int64(len(speedClip)) {
		return p, nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, speedClip, 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}
