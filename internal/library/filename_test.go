package library

import "testing"

func TestParseName(t *testing.T) {
	cases := []struct {
		in   string
		want Parsed
	}{
		{"Cabaret.1972.1080P.Bluray.X265-Lama Lrf Full Sbs_400x240_1400kbps_23.976fps_2pass.moflex",
			Parsed{Title: "Cabaret", Year: 1972, Is3D: 1, Layout: "full-sbs"}},
		{"Danny Phantom 1-01 Mystery Meat [Dummy] Lrf Full Sbs_400x240_1100kbps_47.750fps_2pass.moflex",
			Parsed{Title: "Danny Phantom", Season: 1, Episode: 1, EpTitle: "Mystery Meat", Is3D: 1, Layout: "full-sbs", Marked: true}},
		{"Danny Phantom 1-13 '13' [Honeyko] Lrf Full Sbs_400x240_1100kbps_47.750fps_2pass.moflex",
			Parsed{Title: "Danny Phantom", Season: 1, Episode: 13, EpTitle: "'13'", Is3D: 1, Layout: "full-sbs", Marked: true}},
		{"Drive 2011 Bd-Rip 1080P X265 Ac3-D3fil3r(1) Lrf Full Sbs_400x240_1400kbps_29.970fps_2pass.moflex",
			Parsed{Title: "Drive", Year: 2011, Is3D: 1, Layout: "full-sbs"}},
		{"Grown.Ups.2010.1080P.Bluray.X265 Lrf Full Sbs_400x240_1400kbps_23.976fps_2pass.moflex",
			Parsed{Title: "Grown Ups", Year: 2010, Is3D: 1, Layout: "full-sbs"}},
		{"Mighty.Morphin.Power.Rangers.The.Movie.1995.1080P.Bluray.X265 Lrf Full Sbs_400x240_1400kbps_23.976fps_2pass.moflex",
			Parsed{Title: "Mighty Morphin Power Rangers The Movie", Year: 1995, Is3D: 1, Layout: "full-sbs"}},
		{"Kill.Bill.The.Whole.Bloody.Affair.2006.1080P.Bluray.X264.Aac5.1-Lama(1) Lrf Full Sbs_400x240_1400kbps_23.976fps_2pass.moflex",
			Parsed{Title: "Kill Bill The Whole Bloody Affair", Year: 2006, Is3D: 1, Layout: "full-sbs"}},
		// the catalog's own naming
		{"101 Dalmations (1961) (3D).moflex", Parsed{Title: "101 Dalmations", Year: 1961, Is3D: 1}},
		{"A Charlie Brown Christmas (1965) (3D) (S).moflex", Parsed{Title: "A Charlie Brown Christmas", Year: 1965, Is3D: 1}},
		{"Aeon Flux (1991) (3D) - S02e01 - Gravity (3D).moflex",
			Parsed{Title: "Aeon Flux", Year: 1991, Season: 2, Episode: 1, EpTitle: "Gravity", Is3D: 1, Marked: true}},
		{"1917 (2019).moflex", Parsed{Title: "1917", Year: 2019}},
		{"Up.moflex", Parsed{Title: "Up"}},
		{"Resident Evil 2026 1080p WEBRip x265-DH_LRF_Full_SBS_400x240_1400kbps_23.976fps_2pass.moflex",
			Parsed{Title: "Resident Evil", Year: 2026, Is3D: 1, Layout: "full-sbs"}},
		{"Part 1 - Homeward Bound! (Tpot 23) Lrf Full Sbs_400x240_1400kbps_24.000fps_2pass.moflex",
			Parsed{Title: "Part 1 - Homeward Bound! (Tpot 23)", Is3D: 1, Layout: "full-sbs"}},
		{"Mr. Robot (2015) S02e01 (1080P Amzn Web-Dl H265 Sdr Ddp 5.1 English - Hone) Lr_400x240_1100kbps_23.976fps_2pass.moflex",
			Parsed{Title: "Mr. Robot", Year: 2015, Season: 2, Episode: 1, Marked: true}},
		{"Thundercats 1985 S01e01 1080P Web-Dl Ddp2.0 X264 Dual-Nogroup Lr_400x240_1100kbps_23.976fps_2pass.moflex",
			Parsed{Title: "Thundercats", Year: 1985, Season: 1, Episode: 1, Marked: true}},
		// 3D tags in other videos' names, the same words the player reads
		{"Avatar.2009.Half-SBS.1080p.mkv", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "half-sbs"}},
		{"Avatar 2009 HalfSBS.avi", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "half-sbs"}},
		{"Avatar (2009) H-SBS.mp4", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "half-sbs"}},
		{"Avatar_2009_Half_SBS.mkv", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "half-sbs"}},
		{"Avatar (2009) [HSBS].mkv", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "half-sbs"}},
		{"Avatar.2009.Full-SBS.mkv", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "full-sbs"}},
		{"Avatar 2009 FullSBS.avi", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "full-sbs"}},
		{"Avatar (2009) (FSBS).mkv", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "full-sbs"}},
		{"Avatar 2009 SBS.mp4", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "half-sbs"}},
		{"Avatar 2009 LR.mp4", Parsed{Title: "Avatar", Year: 2009, Is3D: 1, Layout: "half-sbs"}},
		{"Avatar.2009.3D.mkv", Parsed{Title: "Avatar", Year: 2009, Is3D: 1}},
		{"Ultraman (2004).mkv", Parsed{Title: "Ultraman", Year: 2004}},
		// specials: episode 0 of a season, or season 0
		{"To Love Ru OVAs (2009) (3D) (S) - S04e00 - Momo no Rakuen Keikaku Maruhi File.moflex",
			Parsed{Title: "To Love Ru OVAs", Year: 2009, Season: 4, Episode: 0, EpTitle: "Momo no Rakuen Keikaku Maruhi File", Is3D: 1, Marked: true}},
		{"Doctor Who (2005) - S00e01 - The Christmas Invasion.moflex",
			Parsed{Title: "Doctor Who", Year: 2005, Season: 0, Episode: 1, EpTitle: "The Christmas Invasion", Marked: true}},
	}
	for _, c := range cases {
		got := ParseName(c.in)
		if got != c.want {
			t.Errorf("%s\n got  %+v\n want %+v", c.in, got, c.want)
		}
	}
}

func TestIsEpisodeSpecials(t *testing.T) {
	for in, want := range map[string]bool{
		"Show (2009) - S04e00 - Special.moflex": true, // episode 0 = a special of season 4
		"Show (2009) - S00e03 - Special.moflex": true, // season 0 = the specials season
		"Show (2009) - S01e02 - Episode.moflex": true,
		"Show (2009) - S00e00 - Nothing.moflex": false, // says nothing
		"Movie (2009).moflex":                   false,
	} {
		if got := ParseName(in).IsEpisode(); got != want {
			t.Errorf("%s: IsEpisode = %v, want %v", in, got, want)
		}
	}
	for _, c := range []struct {
		in   Info
		want bool
	}{
		{Info{Show: "Show", Season: 4, Episode: 0}, true},
		{Info{Show: "Show", Season: 0, Episode: 3}, true},
		{Info{Show: "Show"}, false},
		{Info{Title: "Movie", Season: 1, Episode: 1}, false},
	} {
		it := &Item{Detected: c.in}
		if got := it.IsEpisode(); got != c.want {
			t.Errorf("%+v: IsEpisode = %v, want %v", c.in, got, c.want)
		}
	}
}
