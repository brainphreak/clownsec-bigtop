# Clownsec Big Top

A small media server that streams your **moflex** library to the
[Clownsec 3DS video player](https://github.com/brainphreak/clownsec-moflex-player) — like Plex or
Jellyfin, but for the format they can't play. Point it at the folders where your `.moflex` files
are, and the player's **BIG TOP** button browses them with posters and info and plays them over
your home network.

> **Status:** early. The server side works (libraries, metadata, posters, streaming with seeking,
> watch progress, discovery). The 3DS side — the BIG TOP button — is being built next.

## Running it

**Windows:** download `ClownsecBigTop.exe` and double-click it. When Windows Firewall asks, allow
it on **private networks** — the 3DS needs to reach it. A window stays open while Big Top runs;
close it to stop the server.

**macOS / Linux:** run `clownsec-bigtop-macos-arm64` (Apple silicon), `clownsec-bigtop-macos-intel`
or `clownsec-bigtop-linux-amd64` from a terminal.

The setup page opens in your browser (`http://localhost:32500`). Add a library, pick its folders,
and Big Top scans them. It rescans every half hour, or press **Scan for new videos**.

The setup page only works on the computer Big Top runs on (Settings can allow other computers).
Watching from a 3DS works either way.

## Where the info comes from

For each video, in order — a later source fills in what an earlier one lacks:

1. **The file name** — `Cabaret.1972.1080p...moflex`, `Show 1-01 Title`, `Show - S01e01 - Title`
2. **The Clownsec catalog** — a file downloaded from clownsec.com is recognised exactly (by size);
   anything else is matched by title and year. Gives the summary, genres and the "in 3D" poster.
3. **SUPER MOFLEX files** carry their own info and poster, which is used first.
4. **Your edits** in the setup page always win and survive rescans. Click any poster to edit the
   title, year, summary, 3D flag or poster, or to pick the right catalog entry.

Big Top keeps all of this in its own data folder and never writes next to your videos:

| | |
|---|---|
| Windows | `%APPDATA%\Clownsec Big Top` |
| macOS | `~/Library/Application Support/Clownsec Big Top` |
| Linux | `~/.config/clownsec-bigtop` |

A moved or renamed file is recognised and keeps its info and watch history.

## Network

| Port | |
|---|---|
| 32500 TCP | the setup page, the 3DS API and the videos (`-port` changes it) |
| 32501 UDP | discovery: the 3DS finds Big Top without typing an address |

## Building

Needs Go 1.22+. `./build.sh` builds Windows, macOS and Linux binaries into `dist/`.
No other dependencies.

## License

LGPL-2.1-or-later, like the player.
