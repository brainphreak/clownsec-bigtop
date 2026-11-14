package update

import "testing"

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"261115.0900", "261114.0306", true},
		{"v261114.0400", "261114.0306", true},
		{"261114.0306", "261114.0306", false},
		{"261114.0305", "261114.0306", false},
		{"270101.0000", "261231.2359", true},
		{"261115.0900", "dev", false}, // a dev build never updates
		{"garbage", "261114.0306", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestPickAndOwnName(t *testing.T) {
	if !ownName.MatchString("ClownsecBigTop-261114.0306.exe") || !ownName.MatchString("clownsec-bigtop-261114.0306-macos-arm64") {
		t.Fatal("own names not recognised")
	}
	if ownName.MatchString("notes.txt") || ownName.MatchString("ClownsecBigTop.exe") {
		t.Fatal("a foreign file matched")
	}
	assets := []asset{{Name: "SHA256SUMS.txt"}, {Name: "ClownsecBigTop-261115.0900-windows.zip"},
		{Name: "ClownsecBigTop-261115.0900.exe"}, {Name: "clownsec-bigtop-261115.0900-macos-arm64"},
		{Name: "clownsec-bigtop-261115.0900-macos-intel"}, {Name: "clownsec-bigtop-261115.0900-linux-amd64"},
		{Name: "clownsec-bigtop-261115.0900-macos-arm64.command"}, {Name: "clownsec-bigtop-261115.0900-macos-intel.command"},
		{Name: "clownsec-bigtop-261115.0900-x86_64.AppImage"}}
	if a := pick(assets); a == nil {
		t.Fatal("no asset picked for this system")
	} else {
		t.Logf("this system takes %s", a.Name)
	}
}
