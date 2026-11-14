# Clownsec Big Top

A small media server that streams your **moflex** library from a computer to the
[Clownsec 3DS video player](https://github.com/brainphreak/clownsec-moflex-player) — like Plex or
Jellyfin, but for the 3DS's own video format, which they can't play.

Point Big Top at the folders where your `.moflex` files are. On the 3DS, the player's **BIG TOP**
button browses them — movies, TV shows by season, posters, descriptions, what you've watched — and
plays them over your home Wi-Fi with seeking, resume, audio tracks, subtitles and 3D, exactly as if
the files were on the SD card.

Moflex files are streamed as they are: Big Top converts nothing, so it needs almost no CPU and runs
happily on an old PC, a laptop or a NAS-attached box.

> **Status: early testing.** Moflex streaming works. MP4 / MKV / AVI (converted on the fly) and
> online info lookup (TMDB / TVDB) are planned — see [Roadmap](#roadmap).

---

## Contents

- [Requirements](#requirements)
- [Installing and running](#installing-and-running)
- [Connecting the 3DS](#connecting-the-3ds)
- [Libraries and scanning](#libraries-and-scanning)
- [Naming your files](#naming-your-files)
- [Where the info and posters come from](#where-the-info-and-posters-come-from)
- [Editing info](#editing-info)
- [3D](#3d)
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
| Videos | `.moflex` files on a local drive, a USB drive or a mounted network share |

Nothing else to install: Big Top is a single program file.

**The 3DS**

| | |
|---|---|
| Console | **New 3DS / New 2DS XL** (the Old 3DS can't keep up with streamed moflex, 3D especially) |
| Player | the [Clownsec player](https://github.com/brainphreak/clownsec-moflex-player) with the BIG TOP button (build 61114.31 or newer — it updates itself) |
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
| `clownsec-bigtop-<version>-macos-arm64` | Macs with Apple silicon (M1 and later) |
| `clownsec-bigtop-<version>-macos-intel` | Macs with an Intel processor |
| `clownsec-bigtop-<version>-x86_64.AppImage` | 64-bit Linux, as an AppImage |
| `clownsec-bigtop-<version>-linux-amd64` | 64-bit Linux, the plain program |
| `SHA256SUMS.txt` | checksums of all of the above |

### Windows

1. Unzip `ClownsecBigTop-<version>-windows.zip` and run `ClownsecBigTop-<version>.exe`.
2. **"Windows protected your PC"** (SmartScreen) appears because the program isn't code-signed yet:
   click **More info → Run anyway**.
3. When **Windows Firewall** asks, allow it on **Private networks**. Without this the 3DS can't
   reach it.
4. A black window opens (keep it open while you watch — closing it stops Big Top) and the setup
   page opens in your browser.

**If Microsoft Defender deletes the file:** unsigned programs written in Go are sometimes flagged
as a generic "Trojan" by mistake. To allow it:
- **Windows Security → Virus & threat protection → Protection history** → click the Big Top entry
  → **Actions → Allow on device** (this also restores the file), or
- **Virus & threat protection → Manage settings → Exclusions → Add an exclusion → Folder**, choose
  a folder, and keep Big Top in it.

### macOS

1. Download the `macos-arm64` file (Apple silicon) or the `macos-intel` one.
2. Make it runnable once, in Terminal: `chmod +x clownsec-bigtop-*-macos-*`
3. Double-click it (it opens in a Terminal window), or run it from Terminal.
4. If macOS says it can't check the developer: **System Settings → Privacy & Security → Open
   Anyway**. If it asks whether to accept incoming network connections, choose **Allow**.

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

Big Top runs until you close its window, press Ctrl+C, or press **Quit** in the setup page. There
is no installer and nothing starts by itself — start Big Top when you want to watch.

### Updates

Big Top checks GitHub for a new release when it starts and twice a day. When there is one, the
setup page shows a bar at the top — **Update now** downloads the build for your system next to the
current one, checks it against the release's `SHA256SUMS.txt`, starts it and closes the old one
(whose file is then removed). Libraries, edits and watch progress live in the data folder and carry
over. Nothing updates without the click: a restart would interrupt anyone watching.
**Settings → check for updates** checks straight away.

On Windows the new version opens in a new window; on a Mac running Big Top in Terminal, in a new
Terminal window. Otherwise (Linux, an AppImage) it carries on in the background, logging to
`bigtop.log` in the data folder — stop it with **Quit**.

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

## Libraries and scanning

A **library** is a name plus one or more folders — for example "Movies", "3D Movies" or "Cartoons".
A library can hold movies and TV shows together; Big Top tells them apart by name.

- **Add a library** in the setup page, name it (that's what the 3DS shows) and pick its folders with
  the folder browser (drives and network shares included).
- **Subfolders are included.** Organise the folders however you like; the folder structure doesn't
  decide anything (except that a subtitle file must sit next to its video).
- Skipped: hidden folders (`.something`), `$RECYCLE.BIN`, `System Volume Information`, `@eaDir`, and
  macOS `._` files.
- Big Top **scans on start, every 30 minutes, and when you press Scan for new videos.** Unchanged
  files cost almost nothing to rescan.
- **Moved or renamed files keep their info and watch history** — a file is recognised by its size
  and a fingerprint of its first and last 64 KB, not by its name.
- A file that disappears is marked missing rather than forgotten (a USB drive may just be
  unplugged); it comes back with everything intact when the file does.
- **Removing a library** forgets its videos. **Your files are never moved, renamed or deleted** —
  Big Top only reads them, and keeps everything it learns in its own data folder.
- Only `.moflex` files are listed for now.

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

Any of these, with the show's name first:

```
Show - S01E02 - Episode Title.moflex        Aeon Flux (1991) - S02e01 - Gravity.moflex
Show S01E02 Episode Title.moflex            Thundercats 1985 S01e01 1080p.moflex
Show 1x02 Episode Title.moflex
Show 1-02 Episode Title.moflex              Danny Phantom 1-01 Mystery Meat.moflex
```

Episodes with the same show name in a library become one show, with seasons. Write the show's name
the same way on every episode (or fix it in the setup page — see [Editing info](#editing-info)).

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
3. **SUPER MOFLEX files** carry their own info in a trailer at the end of the file: title, year,
   genres, runtime, description, 3D flag and poster. A super moflex tagged *TV Shows* also names its
   show (that's what groups episodes), the show's description, and its season / episode / episode
   title (in its summary, `S1.E2 "Title" - ...`) — so it's placed right even if the file was renamed.
4. **Your edits** in the setup page — always win, and survive every rescan.

**Posters:** your own upload wins; otherwise the catalog's artwork (it's larger than the poster
inside a super moflex) when the file is the catalog's own or nothing better exists; otherwise the
super moflex's built-in poster. Posters are stretched to fit, never cropped.

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
- The setup page's 3D choice per video: **As detected / 2D / 3D** for moflex. (For MP4 / MKV, when
  they arrive: **2D / 3D Half SBS / 3D Full SBS**, since the 3DS must know how to split the frame.)

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

- **The setup page only works on the computer Big Top runs on.** From another device it refuses
  (Settings → *Allow this setup page from other computers* changes that).
- **Watching needs no login**: anything on your network that can reach port 32500 can list and play
  your videos. That's normal for a home media box, and why **Big Top must not be exposed to the
  internet** — don't forward its ports on your router.
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
| `bigtop.log` | output of a Big Top restarted in the background by an update (Linux / AppImage) |

Back up the folder to keep your edits and watch history; delete it to start over. If `bigtop.json`
is ever unreadable, it's set aside (renamed `.broken-<date>`) rather than overwritten.

---

## Command-line options

```
-data <folder>   keep settings, library and posters in this folder instead
-port <number>   HTTP port (default 32500; remembered once set)
-no-browser      don't open the setup page on start
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

Needs [Go](https://go.dev) 1.22 or newer. No other dependencies.

```sh
./build.sh
```

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

- **MP4 / MKV / AVI**: converted on the fly (FFmpeg) into the H.264 stream the player already plays
  for Plex — with 2D / Half SBS / Full SBS, and embedded subtitles (ASS included) extracted for the
  player to draw, in 3D too.
- **Online info** for files the Clownsec catalog doesn't know: TMDB / TVDB.
- Your own artwork beside the videos (`Movie.jpg`, `poster.jpg`) picked up automatically.
- A tray icon instead of a console window on Windows; code signing.
- A sharper icon.

---

## License

LGPL-2.1-or-later, like the player. See [LICENSE](LICENSE).
