# Clownsec Big Top

A small media server that streams your **moflex** library from a computer to the
[Clownsec 3DS video player](https://github.com/brainphreak/clownsec-moflex-player) — like Plex or
Jellyfin, but for the 3DS's own video format, which they can't play.

Point Big Top at the folders where your videos are. On the 3DS, the player's **BIG TOP** button
browses them — movies, TV shows by season, extras, posters, descriptions, what you've watched — and
plays them over your home Wi-Fi with seeking, resume, audio tracks, subtitles and 3D, exactly as if
the files were on the SD card.

- **Moflex** files are streamed as they are: no conversion, almost no CPU.
- **MP4 / MKV / AVI** (and more) are converted as they play: H.264 for a New 3DS, moflex for an
  Old 3DS (see [The moflex encoder](#the-moflex-encoder)).
- **2D to 3D**: any 2D video can be watched in 3D, its depth estimated as it plays — on the 3DS
  and in the browser (see [2D to 3D](#2d-to-3d)).
- **Info and posters** come from the Clownsec catalog, super moflex files, and
  [TMDB](#where-the-info-and-posters-come-from) for everything else.
- Any browser on your network can browse and watch too, and hand a stream to **VLC**.

> **Status: testing.** Everything above works; expect rough edges — see [Roadmap](#roadmap).

---

## Contents

- [Requirements](#requirements)
- [Installing and running](#installing-and-running)
- [Connecting the 3DS](#connecting-the-3ds)
- [Browsing and watching in a browser](#browsing-and-watching-in-a-browser)
- [Libraries and scanning](#libraries-and-scanning)
- [Naming your files](#naming-your-files) (extras too)
- [Where the info and posters come from](#where-the-info-and-posters-come-from)
- [Editing info](#editing-info)
- [3D](#3d)
- [2D to 3D](#2d-to-3d)
- [Subtitles](#subtitles)
- [Audio tracks](#audio-tracks)
- [Watch progress](#watch-progress)
- [Network, ports and security](#network-ports-and-security)
- [Where Big Top keeps its data](#where-big-top-keeps-its-data)
- [Command-line options](#command-line-options)
- [Troubleshooting](#troubleshooting)
- [Building from source](#building-from-source)
- [Roadmap](#roadmap)
- [License](#license)

---

## Requirements

**The computer running Big Top**

| | |
|---|---|
| Windows | Windows 10 or 11, 64-bit |
| macOS | macOS 11 (Big Sur) or newer — Apple silicon or Intel |
| Linux | 64-bit x86 (amd64) |
| Network | on the same home network as the 3DS (wired or Wi-Fi) |
| Videos | `.moflex`, `.mp4`, `.mkv`, `.avi` and other video files on a local drive, a USB drive or a mounted network share |
| 2D to 3D | optional: on Windows any DirectX 12 graphics card (NVIDIA, AMD or Intel), on a Mac Apple silicon, on Linux any graphics chip with a Vulkan driver (else the processor) — see [2D to 3D](#2d-to-3d) |

Big Top is a single program file. MP4 / MKV / AVI need FFmpeg: on Windows Big Top downloads it for
you (a bar in the setup page offers it); on a Mac `brew install ffmpeg`, on Linux your package
manager's `ffmpeg`.

**The 3DS**

| | |
|---|---|
| Console | any 3DS / 2DS. A New 3DS plays MP4 / MKV as H.264; an Old 3DS gets them as moflex Big Top makes as they play (see [Is this computer fast enough?](#is-this-computer-fast-enough-for-an-old-3ds)) |
| Player | the [Clownsec player](https://github.com/brainphreak/clownsec-moflex-player) — the newest build (it updates itself); PLAY IN 3D needs 61123.10 or newer |
| Network | the same home Wi-Fi as the computer |

Big Top is for your home network only — see [security](#network-ports-and-security).

---

## Installing and running

Download the file for your system from the releases page. Each is the complete program — a native
executable, not a script or an installer. The version (date.time of the build) is part of every
file name, e.g. `ClownsecBigTop-261114.0306.exe`.

| File | For |
|---|---|
| `ClownsecBigTop-<version>-windows.zip` (or the `.exe`) | Windows 10 / 11, 64-bit |
| `clownsec-bigtop-<version>-macos-arm64.zip` | Macs with Apple silicon (M1 and later) |
| `clownsec-bigtop-<version>-macos-intel.zip` | Macs with an Intel processor |
| `clownsec-bigtop-<version>-x86_64.AppImage` | 64-bit Linux, as an AppImage |
| `clownsec-bigtop-<version>-linux-amd64` | 64-bit Linux, the plain program |
| `SHA256SUMS.txt` | checksums of all of the above |

### Windows

1. Unzip `ClownsecBigTop-<version>-windows.zip` and run `ClownsecBigTop-<version>.exe`.
2. **"Windows protected your PC"** (SmartScreen) appears because the program isn't code-signed yet:
   click **More info → Run anyway**.
3. When **Windows Firewall** asks, allow it on **Private networks** (your home network — not
   public). Without this the 3DS can't reach it. It asks once more after the first update (the
   program's file becomes `ClownsecBigTop.exe`, see [Updates](#updates)), then never again.
4. Big Top starts in the **notification area** (the icons by the clock — it may be under the ^
   arrow) and the setup page opens in your browser. There's no window to keep open.
   - **Left-click** the clownsec icon: open the setup page.
   - **Right-click** it: *Open setup page*, *Check for updates* (or *Update to …*), *Show log*,
     *Open data folder*, *Restart Big Top*, *Quit Big Top*.
   - A notification pops up when an update is available.
   - Starting Big Top again while it runs just opens the setup page.
   - Prefer a console window? Start it with `-console` (e.g. in a shortcut's *Target*).

**If Microsoft Defender deletes the file:** unsigned programs written in Go are sometimes flagged
as a generic "Trojan" by mistake. To allow it:
- **Windows Security → Virus & threat protection → Protection history** → click the Big Top entry
  → **Actions → Allow on device** (this also restores the file), or
- **Virus & threat protection → Manage settings → Exclusions → Add an exclusion → Folder**, choose
  a folder, and keep Big Top in it.

### macOS

1. Download the `macos-arm64.zip` (Apple silicon: M1 and later) or the `macos-intel.zip`, and
   double-click it to unzip.
2. **The first time:** right-click (or Control-click) `clownsec-bigtop-<version>-macos-….command`
   → **Open** → **Open**. macOS asks because the program isn't notarized by Apple yet; after this
   once, a plain double-click works. (If it still refuses: **System Settings → Privacy & Security →
   Open Anyway**.)
3. It opens in a Terminal window — keep it open while you watch — and the setup page opens in
   your browser. If macOS asks whether to accept incoming network connections, choose **Allow**.

The `.command` ending is what makes a double-click open it in Terminal. The release also carries
the same programs without it (`…-macos-arm64`, `…-macos-intel`) for the built-in updater and for
starting from Terminal (`chmod +x` it first: downloads lose the "executable" permission).

### Linux

Either the **AppImage** (make it executable and double-click it, or run it):

```sh
chmod +x clownsec-bigtop-<version>-x86_64.AppImage
./clownsec-bigtop-<version>-x86_64.AppImage
```

or the plain program, the same way (`clownsec-bigtop-<version>-linux-amd64`). Both are
self-contained — no libraries to install. Started by double-click, the AppImage has no window: use
the setup page's **Quit** button to stop it. (An AppImage needs FUSE, which most desktop
distributions have; if yours doesn't, run it with `--appimage-extract-and-run`, or use the plain
program.) If a firewall is running, open TCP
32500 and UDP 32501 to your local network (e.g. with ufw:
`sudo ufw allow from 192.168.0.0/16 to any port 32500 proto tcp`, and the same for `32501 proto udp`).

### The setup page

It opens by itself at **http://localhost:32500**. Open it again any time in a browser on the same
computer. At the top it shows the server's name and the address a 3DS can use.

**The first time**, a short setup runs in the page:

1. **Speed test** — how fast this computer makes the moflex an Old 3DS plays MP4 / MKV as (about a
   minute); it picks Best quality or Fast encoding.
2. **2D to 3D** (optional) — what it is and what it downloads (about 175 MB, once). Turned on, a
   second test finds the best depth quality this computer keeps up with, for the 3DS and for the
   browser, and saves it.

Everything is saved; **Settings → 2D to 3D → test again** runs it again (after a new graphics card,
say), **turn off** turns it off.

Big Top runs until you quit it — **Quit** in the setup page, *Quit Big Top* in the Windows tray
menu, or closing its Terminal window (macOS) / Ctrl+C. There is no installer and nothing starts by
itself — start Big Top when you want to watch. (To start it with Windows, put a shortcut to it in
the Startup folder: Win+R → `shell:startup`.)

### Updates

Big Top checks GitHub for a new release when it starts and twice a day. When there is one, the
setup page shows a bar at the top — **Update now** downloads the build for your system next to the
current one, checks it against the release's `SHA256SUMS.txt`, starts it and closes the old one
(whose file is then removed). Libraries, edits and watch progress live in the data folder and carry
over. Nothing updates without the click: a restart would interrupt anyone watching.
**Settings → check for updates** checks straight away.

On Windows the tray icon is simply replaced by the new version's, and the update is saved as
`ClownsecBigTop.exe` in the same folder — always that name, because Windows Firewall remembers its
permission per file: after the first update it stops asking, and shortcuts to it keep working. On a Mac running Big Top in
Terminal, the new version opens in a new Terminal window. Otherwise (Linux, an AppImage) it carries
on in the background, logging to `bigtop.log` in the data folder — stop it with **Quit**. The tray
menu's *Update to …* does the same as the setup page's button.

---

## Connecting the 3DS

1. Start Big Top on the computer.
2. On the 3DS: **Open Video → BIG TOP** (the right half of the bottom row, next to PLEX).
3. The first time it offers **SET UP**:
   - **FIND SERVERS ON THIS WI-FI** — the 3DS asks the network who runs Big Top. With more than one
     server it lets you choose.
   - **ENTER ADDRESS** — type the address from the top of the setup page, e.g. `192.168.1.20`
     (`:32500` is added for you).
4. Browse: libraries → movies and shows → seasons → episodes. **X** searches the current list.
   The top screen shows the poster and info of the highlighted title.

Change or forget the server later in **Settings (X on the home screen) → BIG TOP SERVER**.

**While watching**, everything works as for a file on the SD card. Over the network a seek or an
audio-track switch takes a moment to buffer; while it does, touching the progress bar again,
pressing left/right or pressing B changes your mind straight away instead of waiting.

---

## Browsing and watching in a browser

The setup page is also a way to browse and watch, on the computer or on any phone, tablet or computer
on your network (open `http://<the Big Top computer's address>:32500/`).

- **Browse** by type (movies, TV shows, music videos, shorts), 3D or 2D, super moflex, genre and
  first letter, sorted by date added, name, year or length, with a search over titles, genres and
  years. Click a title for its details.
- **Watch**: any video plays in the browser — moflex, MP4, MKV, AVI. Big Top converts it to H.264 as
  it plays (FFmpeg), starting where you ask, so seeking anywhere is quick. Resume works per browser.
  TV episodes go on to the next one.
- **3D**: the **3D** button picks how 3D is shown — **Anaglyph** (red/cyan glasses; the default),
  **Side by side — full** or **— half** for 3D TVs, projectors and VR viewers (go full screen), or
  **2D**. *This video is* sets the file's own layout (2D / Half SBS / Full SBS) when Big Top's guess
  from its name or super moflex info is wrong; *Swap eyes* if the depth looks inside out.
- **2D → 3D**: with [2D to 3D](#2d-to-3d) set up, *This video is* also offers **2D → 3D** for a 2D
  video: Big Top converts it as it plays, and every 3D view above works with it.
- **VLC**: the **VLC** button hands the stream to VLC, from where you are, in the 3D view you picked
  drawn into the picture — on a phone it opens the VLC app, on a computer it gives the link to paste
  into *Media → Open Network Stream*. Handy for AR glasses and 3D TVs, which want VLC's own full
  screen. Keep Big Top running while VLC plays.
- **Subtitles** (CC): a super moflex's own, files beside the video and an MKV/MP4's text tracks. In
  3D each eye gets its own copy (side by side), drawn at the screen's depth. **Audio** picks the
  audio track — a super moflex's extra languages too — and the video **version** of a super moflex
  that has several. Every choice is remembered per video.
- On an **iPhone**, full screen is the phone's own video player: Big Top draws the chosen 3D view
  into the video itself, and its scrubber and skip buttons work across the whole film. Or add the
  page to the Home Screen (Share → Add to Home Screen) to watch without Safari's bars.
- On other devices the page is **browse-and-watch only**: libraries, scanning, settings and editing
  stay on the Big Top computer (unless *Allow this setup page from other computers* is on).

---

## Libraries and scanning

A **library** is a name plus one or more folders — for example "Movies", "3D Movies" or "Cartoons".
Each library has a **type** (set when you add it, or with **Edit**):

- **Mixed** (the default) — movies and TV shows together; Big Top tells them apart by file name.
- **TV shows** — one folder per show (`TV\Show Name\Season 1\…`). Every video is an episode of the
  show whose folder it is in, whatever its name looks like, and is never matched to a film. Season
  folders (`Season 5`, `S10`, `Series 3`, `Specials`) give the season; the episode number is read
  from names like `S05E20`, `6x19`, `725` (season 7 episode 25), `Episode 28`, `Ep. 45`,
  `Show - 56 - Title`, `5of9` or `[3x22]`. A video with no number it can read is still listed under
  its show (with the specials, or in its season folder's season).
- **Movies** — nothing in it is taken for an episode, even with `S01E02` in its name.

Changing a library's type reads its files again on the next scan (started straight away).

- **Add a library** in the setup page, name it (that's what the 3DS shows) and pick its folders with
  the folder browser (drives and network shares included).
- **Subfolders are included.** Organise the folders however you like; the folder structure doesn't
  decide anything (except that a subtitle file must sit next to its video).
- Skipped: hidden folders (`.something`), `$RECYCLE.BIN`, `System Volume Information`, `@eaDir`,
  macOS `._` files, and release `sample` clips under 300 MB.
- Big Top **scans on start, every 30 minutes, and when you press Scan for new videos.** Unchanged
  files cost almost nothing to rescan.
- **Moved or renamed files keep their info and watch history** — a file is recognised by its size
  and a fingerprint of its first and last 64 KB, not by its name.
- A file that disappears is marked missing rather than forgotten (a USB drive may just be
  unplugged); it comes back with everything intact when the file does.
- **Removing a library** forgets its videos. **Your files are never moved, renamed or deleted** —
  Big Top only reads them, and keeps everything it learns in its own data folder.
- Listed: `.moflex`, `.mp4`, `.m4v`, `.mkv`, `.avi`, `.mov`, `.webm`, `.wmv`, `.mpg` / `.mpeg`,
  `.ts` / `.m2ts`, `.flv`, `.ogv`, `.3gp`, `.vob`.

---

## Naming your files

Big Top works out what a file is from a SUPER MOFLEX file's own info, the Clownsec catalog, and the
file name. Files from clownsec.com, and super moflex files, need no particular name. For anything
else the name matters:

### Movies

```
Title (Year).moflex                         Cabaret (1972).moflex
Title.Year.whatever.moflex                  Cabaret.1972.1080p.BluRay.x265.moflex
```

Release-group junk after the year (`1080p`, `BluRay`, `x265`, `WEB-DL`, `[Group]` ...) is ignored, and
so is what the moflex encoder appends (`Lrf Full Sbs_400x240_1400kbps_23.976fps_2pass`).

### TV episodes

In a **TV shows** library the show's folder names the show (see [Libraries](#libraries-and-scanning));
in a mixed library, any of these, with the show's name first:

```
Show - S01E02 - Episode Title.moflex        Aeon Flux (1991) - S02e01 - Gravity.moflex
Show S01E02 Episode Title.moflex            Thundercats 1985 S01e01 1080p.moflex
Show 1x02 Episode Title.moflex
Show 1-02 Episode Title.moflex              Danny Phantom 1-01 Mystery Meat.moflex
```

Episodes with the same show name in a library become one show, with seasons. Write the show's name
the same way on every episode (or fix it in the setup page — see [Editing info](#editing-info)).
Case, punctuation, the year and `(3D)` / `(S)` don't matter.

**Specials:** season 0 holds a show's specials (`S00E03`) and is listed as **Specials**, after the
numbered seasons. Episode 0 is a season's special (`S04E00`) and comes before that season's
episode 1.

### Extras

Big Top follows **Plex's rules**: in a movie's folder, a video named `Title-trailer` (or
`-featurette`, `-behindthescenes`, `-interview`, `-deleted`, `-scene`, `-short`, `-other`), or any
video in an `Extras`, `Trailers`, `Featurettes`, `Behind The Scenes`, `Interviews`, `Deleted Scenes`,
`Scenes`, `Shorts` or `Other` folder inside it, is an **extra** of that movie:

```
Angel (1983) {imdb-tt0086896}/Angel (1983) {imdb-tt0086896}.mkv
Angel (1983) {imdb-tt0086896}/Chance Meeting-behindthescenes.mkv
Angel (1983) {imdb-tt0086896}/Trailers/Angel.mkv
```

The movie is then one entry (on the 3DS "Angel (1983) +2"), its extras inside it — they are not
listed as movies of their own. Extras find their movie by the **title and year of their folder** in
*any* library, so a movie kept on another drive or share still gets the extras from its old folder.
A folder name without a year only groups the files in that folder.

### 3D hints

`(3D)`, `3D`, `Full SBS`, `Half SBS`, `FSBS`, `HSBS` in the name mark a video as 3D (this only labels
it in the lists — a 3D moflex plays in 3D regardless).

---

## Where the info and posters come from

For each video these sources are combined; each fills in what the previous didn't, and a later one
wins where both have a value:

1. **The file name** — title and year, or show / season / episode / episode title.
2. **The [Clownsec catalog](https://clownsec.com/3ds)** — downloaded once a day.
   - A file that **is** a catalog file (same exact size) is recognised exactly.
   - Anything else is matched by title and year (a year off by one still matches), shows by title.
   - Gives the title, year, genres, runtime, description, the "in 3D" poster, and episode titles.
3. **[TMDB](https://www.themoviedb.org)** (The Movie Database) — for a video neither the catalog
   nor its own info identifies: found by the **IMDb id** in its folder or file name
   (`{imdb-tt0086896}`, `tt0086896`) when there is one, else by title and year (it must match the
   title or the year — rather nothing than a wrong film). Gives the title, year, summary, genres,
   runtime and poster; shows get their summary, genres and poster. Each video is looked up once (a
   network failure: again on the next scan); the first scan of a big library takes a while, in the
   background. It works without any setup — Big Top asks through clownsec.com, which holds the
   TMDB key — or with **your own TMDB key** (*Settings*: a v3 API key or a v4 read-access token).
   **Settings** turns it off. *This product uses the TMDB API but is not endorsed or certified by TMDB.*
4. **SUPER MOFLEX files** carry their own info in a trailer at the end of the file: title, year,
   genres, runtime, description, 3D flag and poster. Files made with Moflex Studio 1.7 or later also
   say what they are — movie, TV episode, short or music video — and, for an episode, its season,
   episode and episode title, while the title is the show's name (that's what groups episodes). That
   info wins over the file name, so a renamed file stays in its show and keeps its place, and a
   movie stays a movie whatever its name. Older super moflex files fall back to the file name for
   season and episode (or to a summary starting `S1.E2 "Title" - ...`). To fix one's show name, open
   it in Moflex Studio, correct it and save: renaming the file doesn't change what's inside it.
5. **Your edits** in the setup page — always win, and survive every rescan.

**Posters:** your own upload wins; otherwise the catalog's artwork (it's larger than the poster
inside a super moflex) when the file is the catalog's own or nothing better exists; otherwise the
super moflex's built-in poster; otherwise TMDB's. Posters are stretched to fit, never cropped.

In the setup page, a tile marked **CHECK** was recognised by its file name only — worth a look.
**SUPER** marks a super moflex.

---

## Editing info

Click any poster in the setup page.

- **Movies and episodes:** title, year, summary, genres, runtime and 3D; for episodes also the show,
  season and episode number. Leave a field empty to use what was detected (shown faded).
- **Upload poster…** — any JPEG or PNG.
- **Find it in the Clownsec catalog** — search the catalog and pick the right entry: its info and
  poster are applied as your edits.
- **Clear my edits** — back to what was detected.
- **Shows:** click a show for its title, year, summary, genres and poster, and a list of its
  episodes (each editable). To move an episode to another show, edit the episode's *Show* field.

Edits are kept in Big Top's data, never written into your files.

---

## 3D

- A 3D moflex plays in 3D on the 3DS exactly as from the SD card; the 3D flag in Big Top only labels
  it in the lists.
- The setup page's 3D choice per video: **As detected / 2D / 3D** for moflex; **2D / 3D Half SBS /
  3D Full SBS** for MP4 / MKV, since the 3DS must know how to split the frame.

---

## 2D to 3D

Any 2D MP4 / MKV / AVI can be watched in 3D: Big Top estimates how far away each part of every
picture is — with [Distill-Any-Depth](https://github.com/Westlake-AGI-Lab/Distill-Any-Depth), a
depth model, on your graphics card — and makes the two eyes from it as the video plays.

- **Turn it on** in the first-run setup, or **Settings → 2D to 3D → set up**. It downloads ONNX
  Runtime (which runs the model) and the depth models once — about 175 MB — and tests which depth
  quality this computer keeps up with: for the 3DS (the 3DS's stream) and for the browser (bigger
  pictures, so it may pick a lighter one). Slower computers get the lighter MiDaS model.
- **On the 3DS**, a 2D video offers **PLAY IN 3D** when the test showed this computer keeps up for
  that kind of 3DS; **VIDEO → Format → 2D → 3D** turns it on or off while it plays, and works even
  where the test said too slow (it warns that it may stutter). An Old 3DS gets a 3D moflex, a New
  3DS side-by-side H.264.
- **In the browser**: the 3D menu's *This video is → 2D → 3D*, then any 3D view.

| | runs the depth model on |
|---|---|
| Windows | any DirectX 12 graphics card (DirectML) — NVIDIA, AMD, Intel |
| macOS | Apple silicon (CoreML) — not Intel Macs |
| Linux | the graphics chip through WebGPU, on any GPU with a Vulkan driver (AMD, Intel, NVIDIA; the usual Mesa drivers work); without one, the processor (a fast one is needed) |

The settings were tuned by eye on an Old 3DS (the strength and screen depth of a well-tested
[iw3](https://github.com/nagadomi/nunif) setup, an edge fix so outlines don't double, smoothing
so depth doesn't wobble). Measured: an RTX 4090 converts at well over real time at the best quality;
an Apple M4 manages the best quality for the 3DS and a lighter one for the browser.

---

## Subtitles

### Inside a SUPER MOFLEX

They work exactly as from the SD card — CC menu → Built-in, every language the file carries.

### Subtitle files beside the video

Put them **next to the video, named after it**, with an optional label after a dot — the same names
Plex and the 3DS player use for local files:

```
Cabaret.moflex
Cabaret.srt              the default track
Cabaret.en.srt           a language
Cabaret.ja.ass           ASS / SSA keep their styling
Cabaret.en.forced.srt    forced lines only
Cabaret.English SDH.srt  the label can be anything
```

- `.srt`, `.ass` and `.ssa` are supported.
- The label must follow a **dot**: `Cabaret 2.srt` belongs to a video called `Cabaret 2`.
- When a video starts, the 3DS downloads its subtitle files. The plain one (`Cabaret.srt`) turns on
  by itself; without one, the track in your **preferred subtitle language** (Playback Defaults,
  forced tracks skipped). The rest are in **CC → Load subtitle file…**
- They're drawn by the player: size, position, font and 3D depth all apply, in both eyes.
- They're looked up each time a video starts, so a file you add shows up next time — no rescan.

---

## Audio tracks

A SUPER MOFLEX with a second language (e.g. English + Japanese) switches in the player's
**AUDIO → Language** row, as from the SD card. Your **Playback Defaults** audio language picks the
starting track for videos you haven't set yourself. Over the network a switch takes a moment to
buffer; the button changes at once and the switch happens when you stop pressing.

---

## Watch progress

- Each 3DS has its own resume points and watched marks (a console is identified by an id the player
  makes once).
- When you stop, the 3DS tells Big Top where you were; next time it offers to resume there.
- Stopping in the last tenth of a video (or its last two minutes), or playing it to the end, marks it
  watched (★ in lists). Shows and seasons count their watched episodes.

---

## Network, ports and security

| Port | Protocol | Used for |
|---|---|---|
| 32500 | TCP (HTTP) | the setup page, the 3DS's lists and posters, and the videos themselves |
| 32501 | UDP | discovery: the 3DS broadcasts "is there a Big Top here?" and Big Top answers |

- **The setup page only changes things from the computer Big Top runs on.** Other devices can browse
  and watch, but not edit, scan or change settings (Settings → *Allow this setup page from other
  computers* changes that).
- **Watching needs no login**: anything on your network that can reach port 32500 can list and play
  your videos. That's normal for a home media box, and why **Big Top must not be exposed to the
  internet** — don't forward its ports on your router.
- Watching in a browser keeps a few minutes of converted video in the data folder (`web/`), deleted
  as it plays and when Big Top starts.
- Big Top's only outgoing requests are to clownsec.com (the catalog and its artwork) and to GitHub
  (checking for, and downloading, updates).

---

## Where Big Top keeps its data

| | |
|---|---|
| Windows | `%APPDATA%\Clownsec Big Top` |
| macOS | `~/Library/Application Support/Clownsec Big Top` |
| Linux | `~/.config/clownsec-bigtop` |

| File | Holds |
|---|---|
| `config.json` | the server's name, id, port, setup-page access |
| `bigtop.json` | libraries, every video's detected info and your edits, shows, watch progress |
| `catalog.json` | the cached Clownsec catalog (refreshed daily) |
| `posters/` | posters, and resized copies in `posters/sized/` |
| `bigtop.log` | what Big Top reports as it runs, on Windows (tray menu → *Show log*) and when it runs in the background (Linux / AppImage) |

Back up the folder to keep your edits and watch history; delete it to start over. If `bigtop.json`
is ever unreadable, it's set aside (renamed `.broken-<date>`) rather than overwritten.

---

## Command-line options

```
-data <folder>   keep settings, library and posters in this folder instead
-port <number>   HTTP port (default 32500; remembered once set)
-no-browser      don't open the setup page on start
-console         Windows: show a console window instead of only the tray icon
```

---

## Troubleshooting

| Problem | Try |
|---|---|
| The 3DS finds no server | Is Big Top running? Allowed through the firewall on *private* networks? Same Wi-Fi? Some routers block broadcasts between Wi-Fi and wired devices — use **ENTER ADDRESS** with the address on the setup page. |
| "Can't reach Big Top" | The computer's address may have changed (routers reassign them): Settings → BIG TOP SERVER → find it again. A fixed address for the computer in your router avoids this. |
| Windows deletes the .exe | Microsoft Defender false positive — see [Windows](#windows). |
| "Port 32500 is in use" | Big Top is already running (check for another window), or another program uses the port: start with `-port 32510` and use that address on the 3DS. |
| A video has the wrong title / poster | Click it in the setup page: edit it, or **Find it in the Clownsec catalog**. |
| An episode shows up as a movie | Its name needs `S01E02` (or `1x02` / `1-02`) — rename it, or set Show / Season / Episode in the setup page. |
| Playback stutters | Usually Wi-Fi: move closer to the router (a 3DS only uses 2.4 GHz, so a crowded channel hurts). The player writes `moflex_player/bigtop_log.txt` on the SD card: many long waits there mean the network couldn't keep up. |
| Subtitles don't appear | The file must be next to the video and start with the video's exact name, then a dot. |
| Videos are listed as missing | The drive or share with them isn't connected — reconnect it and scan. |

---

## Building from source

Needs [Go](https://go.dev) 1.22 or newer, and the four moflex encoder builds (see *The moflex encoder*
below) in `~/moflex-encoder/dist/encoders/` (or the folder `MOFLEX_ENCODERS` names).

```sh
./build.sh
```

`go run .` and `go build .` without `-tags moflexenc` make a Big Top with no encoder built in; it then
uses an encoder placed in its data folder (`moflex/moflex-ffmpeg`) or set by `$BIGTOP_MOFLEX_ENCODER`.

builds Windows, macOS (Apple silicon and Intel) and Linux binaries into `dist/`, the version in each
file name, plus a zip of the Windows build, a Linux AppImage (needs `mksquashfs` — on macOS
`brew install squashfs`; the AppImage runtime is downloaded once into `.cache/`) and
`SHA256SUMS.txt`. `go run .` runs it straight from the source (a `dev` build never updates itself).

A release is a GitHub release tagged `v<version>` carrying every file in `dist/` — the updater
picks its file by name and refuses one whose checksum isn't in `SHA256SUMS.txt`.

```
main.go                  start-up, opening the browser, shutdown
internal/config          settings and the data folder
internal/library         the database, file-name parsing, the metadata model
internal/scan            finding videos, fingerprints, working out what each one is
internal/catalog         the Clownsec catalog: download, matching, artwork
internal/moflex          reading a SUPER MOFLEX trailer (info, poster, track languages)
internal/poster          storing and resizing posters
internal/server          the HTTP API for the 3DS, the setup page's API, subtitle files
internal/discovery       answering the 3DS's "is there a Big Top here?"
internal/update          checking GitHub releases, downloading and verifying an update
web/index.html           the setup page (built into the program)
```

The HTTP interface is described in [docs/API.md](docs/API.md).

---

## Roadmap

- 2D to 3D for moflex files too (today: MP4 / MKV / AVI).
- Your own artwork beside the videos (`Movie.jpg`, `poster.jpg`) picked up automatically.
- Code signing (no more SmartScreen / Defender warnings).
- A sharper icon.

---

## The moflex encoder

An Old 3DS can't decode the H.264 that MP4 and MKV files hold, so Big Top turns them into moflex as
they play. The encoder that does it is **[Mobipeg-MOFLEX](https://github.com/Oldhimaster1/Mobipeg-MOFLEX)
by Movie Bot** — FFmpeg and x264 with a MobiClip encoder and moflex muxer — with Clownsec's
improvements on top: an exact 8×8 luma transform, 4×4 chroma blocks where an edge needs them, a
quantizer per frame that follows the official encoder's choices (finest on dark scenes), and fixes
measured against Nintendo's encoder on 3DS hardware. The patch, its notes and the build script are in
[clownsec-moflex-encoder](https://github.com/brainphreak/clownsec-moflex-encoder/tree/main/patches).

Each Big Top download carries the encoder for its system and writes it into the data folder the
first time it's needed. FFmpeg, x264 and so the encoder are licensed under the **GPL**; their source
is at the links above.

The browser player uses [hls.js](https://github.com/video-dev/hls.js) (Apache-2.0), built in.

### Is this computer fast enough for an Old 3DS?

The encoder has to make the moflex faster than the 3DS plays it, or the video stutters, and 3D (two
eyes) is about twice the work of 2D. **Settings → Old 3DS speed → test this computer** encodes a
built-in 20-second 3D clip in 2D and in 3D and says how much faster than real time this computer
is: 1.3× or more plays smoothly, 1.0–1.3× may stutter in busy scenes, under 1× stutters. If only 3D
is too slow, use **Fast** encoding. **Encoding for an Old 3DS** (same Settings page) is **Auto** by
default: a minute or two after Big Top starts (and after each update) it measures this computer once and
uses Best quality only when 3D encodes at 1.3× or faster, otherwise Fast (2–3× as fast, slightly softer
detail). If even Fast is too slow for 3D, play 3D videos in 2D on the Old 3DS (the player's VIDEO →
Format). A New 3DS gets H.264 and isn't affected. For comparison, an Apple M4 Mac measures about 4.9× in
2D and 3.4× in 3D at best quality, and 10× and 5× with Fast.

The 3DS player's **Big Top server → Test connection** checks the other half: how fast and how steadily
this computer's data reaches the 3DS over Wi-Fi (a 3D stream averages about 150–250 KB/s).

The clip is from *Big Buck Bunny* (stereoscopic edition), © copyright 2008 Blender Foundation /
[www.bigbuckbunny.org](https://www.bigbuckbunny.org), licensed under
[CC BY 3.0](https://creativecommons.org/licenses/by/3.0/).

---

## License

Big Top: LGPL-2.1-or-later, like the player. See [LICENSE](LICENSE). The built-in moflex encoder is
a separate program under the GPL (see *The moflex encoder*).

2D to 3D downloads, only when turned on: [ONNX Runtime](https://onnxruntime.ai) (MIT, Microsoft; on
Windows with DirectML, redistributed with Big Top under its licence),
[Distill-Any-Depth](https://github.com/Westlake-AGI-Lab/Distill-Any-Depth) (MIT, © 2025 AGI Lab,
Westlake University) and [MiDaS](https://github.com/isl-org/MiDaS) v2.1 small (MIT, © 2019 Intel
ISL), from this repository's `depth3d-v1` release. Info and posters from
[TMDB](https://www.themoviedb.org): *this product uses the TMDB API but is not endorsed or certified
by TMDB.*
