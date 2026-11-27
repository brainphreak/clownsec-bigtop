package library

import "testing"

func TestParseTV(t *testing.T) {
	roots := []string{`Z:\TV`}
	for _, c := range []struct {
		path, show   string
		year, se, ep int
		title        string
	}{
		{`Z:\TV\Alfred Hitchcock Presents\Season 5\S05E20 The Day Of The Bullet.avi`, "Alfred Hitchcock Presents", 0, 5, 20, "The Day Of The Bullet"},
		{`Z:\TV\Bewitched\Season 6\6x19 - Tabatha's Very Own Samantha.avi`, "Bewitched", 0, 6, 19, "Tabatha's Very Own Samantha"},
		{`Z:\TV\Coach\Season 7\725_Oh_A_Pro_Job_pt1.avi`, "Coach", 0, 7, 25, "Oh A Pro Job pt1"},
		{`Z:\TV\Comedy Central Presents\S10\Comedy Central Presents - 1017 - Pat Dixon.avi`, "Comedy Central Presents", 0, 10, 17, "Pat Dixon"},
		{`Z:\TV\21.Jump.Street\21.Jump.Street.S02.DVDRip.XviD-uDF\21.jump.street.0212.dvdrip.xvid-udf.avi`, "21 Jump Street", 0, 2, 12, ""},
		{`Z:\TV\Darkwing Duck\Darkwing Duck - 56 - The Haunting of Mr. Banana Brain.avi`, "Darkwing Duck", 0, 1, 56, "The Haunting of Mr. Banana Brain"},
		{`Z:\TV\Captain Scarlet and the Mysterons (1967)\Captain Scarlet and the Mysterons - Episode 28 - Inferno.avi`, "Captain Scarlet and the Mysterons", 1967, 1, 28, "Inferno"},
		{`Z:\TV\Cow and Chicken (1997) (DVDRip)\Season 1\cow and chicken - 121 - chicken's first kiss [dfkt].avi`, "Cow and Chicken", 1997, 1, 21, "chicken's first kiss"},
		{`Z:\TV\A Pup Named Scooby Doo (1988) (DVDRip)\Season 4\S4-E30_Mayhem of the Moving Mollusk.avi`, "A Pup Named Scooby Doo", 1988, 4, 30, "Mayhem of the Moving Mollusk"},
		{`Z:\TV\Crystal Maze\The Crystal Maze Series 3 Episode 9.mp4`, "Crystal Maze", 0, 3, 9, ""},
		{`Z:\TV\Disney's House of Mouse\House_Of_Mouse_-_(Ep._45)_-_Pete_s_House_Of_Villains.avi`, "Disney's House of Mouse", 0, 1, 45, "Pete s House Of Villains"},
		{`Z:\TV\ALF\Season 01\S01E06 - For Your Eyes Only.avi`, "ALF", 0, 1, 6, "For Your Eyes Only"},
		{`Z:\TV\Babylon 5\067 - Babylon 5 [3x22] Z'ha'dum.mkv`, "Babylon 5", 0, 3, 22, ""},
		{`Z:\TV\Tom and Jerry (1940) (DVDRip)\274   Catastrophe Cat [1991].mp4`, "Tom and Jerry", 1940, 1, 274, "Catastrophe Cat"},
		{`Z:\TV\Looney Tunes (1930) (DVDRip)\Looney Tunes Season 19\1904 Farm Frolics.mkv`, "Looney Tunes", 1930, 19, 4, "Farm Frolics"},
		{`Z:\TV\Bad.Thoughts.S01E02.1080p.HEVC.x265-MeGusta\Bad.Thoughts.S01E02.1080p.HEVC.x265-MeGusta.mkv`, "Bad Thoughts", 0, 1, 2, ""},
		{`Z:\TV\Doctor Who 2005 S11 1080p BluRay H264 AC3 Will1869\Doctor.Who.2005.S11E03.mkv`, "Doctor Who", 2005, 11, 3, ""},
		{`Z:\TV\Ken Burns The Civil War\Ken.Burns.The.Civil.War.5of9.The.Universe.Of.Battle.avi`, "Ken Burns The Civil War", 0, 1, 5, "The Universe Of Battle"},
		{`Z:\TV\Loiter Squad Season 2 MP4\Loiter Squad 2x03.mp4`, "Loiter Squad", 0, 2, 3, ""},
		{`Z:\TV\Black.Mirror\Black.Mirror.S03E01.mkv`, "Black Mirror", 0, 3, 1, ""},
		{`Z:\TV\Comedy Central Presents\Specials\100 Greatest Stand-Ups Of All Time\Comedy Central Presents - 100 Greatest Stand-Ups Of All Time(4).avi`, "Comedy Central Presents", 0, 0, 100, ""},
		{`Z:\TV\Threes Company\Season 03\Extras\Three's Company - s03 - Extras - Best of Jack.m4v`, "Threes Company", 0, 3, 0, ""},
	} {
		p := ParseTV(c.path, roots)
		if p.Title != c.show || p.Year != c.year || p.Season != c.se || p.Episode != c.ep || (c.title != "" && p.EpTitle != c.title) || !p.IsEpisode() {
			t.Errorf("%s\n  got  %q %d S%02dE%02d %q ep=%v\n  want %q %d S%02dE%02d %q", c.path, p.Title, p.Year, p.Season, p.Episode, p.EpTitle, p.IsEpisode(), c.show, c.year, c.se, c.ep, c.title)
		}
	}
	// a file loose in the library folder: only its name can say
	if p := ParseTV(`Z:\TV\Seinfeld S02E03.mkv`, roots); p.Title != "Seinfeld" || p.Season != 2 || p.Episode != 3 {
		t.Errorf("loose: %+v", p)
	}
	// a movies library: an episode marker is just part of the title
	if p := ParseMovieName(`Z:\MOVIES\Spongebob S01E01.mkv`); p.IsEpisode() {
		t.Errorf("movies: %+v", p)
	}
}
