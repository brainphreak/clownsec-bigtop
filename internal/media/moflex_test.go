package media

import (
	"strings"
	"testing"

	"github.com/brainphreak/clownsec-bigtop/internal/library"
)

func TestMoflexArgs(t *testing.T) {
	m := &library.Media{Video: 0, W: 1920, H: 1080, SAR: 1, FpsNum: 24000, FpsDen: 1001,
		Audio: []library.Track{{Index: 1}, {Index: 2, Default: true}}}
	j := Job{Path: "in.mkv", Media: m, StartMs: 61500, Audio: -1, Burn: -1}

	a := strings.Join(MoflexArgs(j, "hsbs", 25), " ")
	for _, want := range []string{"-ss 61.500", "interleave=nb_inputs=2", "iw*sar*2", "-r 48000/1001",
		"-mo_layout 0", "-mo_interleave_input_double_rate 1", "-qp 13 -mobi_qyx 2", "-map 0:2", "-mo_live 1"} {
		if !strings.Contains(a, want) {
			t.Errorf("3D args miss %q:\n%s", want, a)
		}
	}
	a = strings.Join(MoflexArgs(j, "", 0), " ")
	for _, want := range []string{"fps=24000/1001", "-r 24000/1001", "-mo_layout 6", "-qp 16 -mobi_qyx 0"} {
		if !strings.Contains(a, want) {
			t.Errorf("2D args miss %q:\n%s", want, a)
		}
	}
	m.FpsNum, m.FpsDen = 60000, 1001 // 59.94 -> every other frame
	if a = strings.Join(MoflexArgs(j, "", 0), " "); !strings.Contains(a, "fps=60000/2002") {
		t.Errorf("60 fps not halved:\n%s", a)
	}
}
