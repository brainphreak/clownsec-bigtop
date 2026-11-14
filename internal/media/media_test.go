package media

import (
	"strings"
	"testing"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
)

func TestSize(t *testing.T) {
	cases := []struct {
		w, h   int
		sar    float64
		stereo bool
		ww, wh int
	}{
		{1920, 1080, 1, false, 400, 224},   // 16:9 -> 400 wide
		{1920, 1080, 1, true, 800, 450},    // half SBS: the frame's shape is kept, the player splits it
		{3840, 1080, 1, true, 800, 224},    // full SBS
		{720, 480, 1.185, false, 400, 224}, // anamorphic 16:9 DVD -> pixel aspect applied
		{720, 480, 0.889, false, 320, 240}, // 4:3 DVD
		{320, 240, 1, false, 320, 240},     // never enlarged
	}
	for _, c := range cases {
		w, h := Size(&library.Media{W: c.w, H: c.h, SAR: c.sar}, c.stereo)
		if w != c.ww || h != c.wh {
			t.Errorf("%dx%d sar %.3f stereo=%v: got %dx%d want %dx%d", c.w, c.h, c.sar, c.stereo, w, h, c.ww, c.wh)
		}
	}
}

func TestArgs(t *testing.T) {
	m := &library.Media{Video: 0, W: 1920, H: 1080, SAR: 1,
		Audio: []library.Track{{Index: 1, Codec: "ac3", Channels: 6}, {Index: 2, Codec: "aac", Default: true}},
		Subs:  []library.Track{{Index: 3, Codec: "hdmv_pgs_subtitle", Image: true}}}
	a := strings.Join(Args(Job{Path: "x.mkv", Media: m, StartMs: 61500, Audio: -1, Burn: -1}), " ")
	for _, want := range []string{"-ss 61.500 -i x.mkv", "-map 0:0", "-map 0:2", "scale=400:224", "-ac 2", "-f mpegts"} {
		if !strings.Contains(a, want) {
			t.Errorf("missing %q in %s", want, a)
		}
	}
	a = strings.Join(Args(Job{Path: "x.mkv", Media: m, Audio: 1, Burn: 3}), " ")
	if !strings.Contains(a, "[0:0][0:3]overlay") || !strings.Contains(a, "-map [v]") || !strings.Contains(a, "-map 0:1") || strings.Contains(a, "-ss") {
		t.Errorf("burn/audio args wrong: %s", a)
	}
}
