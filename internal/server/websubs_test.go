package server

import (
	"strings"
	"testing"
)

func TestParseSubs(t *testing.T) {
	srt := "\ufeff1\r\n00:00:01,000 --> 00:00:04,500\r\n<i>Hello</i>\r\nthere\r\n\r\n2\r\n01:02:03.4 --> 01:02:05,000\r\nLate\r\n"
	c := parseSubs(srt)
	if len(c) != 2 || c[0].Start != 1000 || c[0].End != 4500 || c[0].Text != "Hello\nthere" || c[1].Start != 3723400 {
		t.Fatalf("srt: %+v", c)
	}
	ass := "[Script Info]\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:08.00,0:00:14.00,Default,,0,0,0,,Later, with a comma\n" +
		"Dialogue: 0,0:00:02.00,0:00:06.50,Default,,0,0,0,,{\\i1}One{\\i0}\\Ntwo\n"
	c = parseSubs(ass)
	if len(c) != 2 || c[0].Start != 2000 || c[0].End != 6500 || c[0].Text != "One\ntwo" || c[1].Text != "Later, with a comma" {
		t.Fatalf("ass: %+v", c)
	}
	v := vtt(c, 2)
	if strings.Count(v, "position:25%") != 2 || strings.Count(v, "position:75%") != 2 || !strings.HasPrefix(v, "WEBVTT") {
		t.Fatalf("vtt: %s", v)
	}
}
