# Big Top HTTP interface

Everything is plain HTTP + JSON on port **32500** (or the `-port` set). The 3DS player uses the
`/api` endpoints; the setup page uses `/admin/api`. Both are described here so other clients can
be written.

- Responses are UTF-8 JSON unless noted. Errors are a status code with a short text body.
- Ids are short hex strings. A season's id is `<show id>/<season number>`.
- The API has no authentication: it's meant for a home network only.

## Discovery (UDP)

A client finds servers by broadcasting the 16 bytes `CLOWNSEC-BIGTOP?` to UDP port **32501**
(the subnet broadcast address, and/or `255.255.255.255`). Every Big Top answers the sender with
one JSON datagram:

```json
{"service":"clownsec-bigtop","name":"Living Room PC","id":"6f0ffc0094a1ac94","port":32500,"version":"261114.0306"}
```

The server's address is the datagram's source address; its HTTP port is `port`. `id` is stable
for the server's lifetime (it survives address changes), so a client can recognise a server it
has seen before.

## Identifying a console

Watch progress is kept per client. Send a stable client id with every `/api` request — either a
header or a query parameter:

```
X-BigTop-Client: 3ds-0a1b2c3d4e5f
GET /api/libraries?client=3ds-0a1b2c3d4e5f
```

Without one, the client shares the `default` progress.

## The Entry object

Lists and item details return Entries — one shape for movies, shows, seasons and episodes, kept
flat and small (the 3DS parses it with a tiny JSON library). Fields that are empty or zero are
left out.

| Field | Type | |
|---|---|---|
| `type` | string | `movie`, `show`, `season` or `episode` |
| `id` | string | |
| `title` | string | an episode's own title (or "Episode N") |
| `year` | int | |
| `summary` | string | at most ~400 bytes in lists, complete in `/api/items/{id}` |
| `genres` | string | `"Animation, Family"` |
| `kind` | string | movies: `movie`, `short` or `musicvideo` (from the Clownsec catalog, or edited) |
| `added` | int | unix time the file was first found |
| `fmt` | string | the file's container: `MOFLEX`, `MP4`, `MKV`… |
| `runtime` | int | minutes |
| `is3d` | bool | |
| `layout` | string | non-moflex 3D only: `hsbs` / `fsbs` |
| `poster` | bool | a poster is available (see the poster endpoints) |
| `season`, `episode` | int | episodes (and seasons: `season`) |
| `pos` | int | this client's resume point, milliseconds |
| `watched` | bool | this client has watched it |
| `size` | int | file size in bytes |
| `super` | bool | a SUPER MOFLEX |
| `episodes`, `seen` | int | shows and seasons: episode count, and how many this client watched |
| `audio`, `altAudio` | string | item details only: in-band and trailer audio languages (`ENG`, `JPN`) |
| `subs` | string | item details only: built-in subtitle languages, space-separated |
| `file` | string | item details only: the file's name |

## 3DS endpoints

### `GET /api/server`
```json
{"service":"clownsec-bigtop","name":"Living Room PC","id":"6f0ffc0094a1ac94","version":"261114.0306"}
```
A client can use this to check that an address really is a Big Top.

### `GET /api/libraries`
```json
[{"id":"dc5a1ee9","name":"3D Movies","movies":96,"shows":9}]
```

### `GET /api/libraries/{id}?start=0&size=200`
A library's movies and shows together, sorted by title (a leading "The" ignored).
`start` / `size` page through a big library (no `size` = everything).
```json
{"total":105,"items":[ {Entry}, ... ]}
```

### `GET /api/shows/{id}`
A show and its seasons.
```json
{"show":{Entry},"seasons":[{"type":"season","id":"a1b2c3d4e5f6/1","title":"Season 1","season":1,"episodes":13,"seen":2,"poster":true}]}
```

### `GET /api/shows/{id}/seasons/{n}`
A season's episodes, in episode order.
```json
{"total":13,"items":[ {Entry}, ... ]}
```

### `GET /api/items/{id}`
One movie or episode with everything known, including this client's `pos` / `watched`. The player
asks for it just before playing, for a fresh resume point.

### `GET /api/items/{id}/poster?w=132&h=188` · `GET /api/shows/{id}/poster?w=&h=`
JPEG, scaled to exactly `w`×`h` (stretched, never cropped); without `w`/`h`, the original. Scaled
copies are cached. A season uses its show's poster.

### `GET /api/items/{id}/file`
The video file itself. Supports `HEAD` (for `Content-Length`) and `Range` requests
(`206 Partial Content`) — the player reads the file in 64 KB–256 KB ranges over one kept-alive
connection, so seeking costs one small request.

### `GET /api/items/{id}/subs`
Subtitle files beside the video (see the README for the naming rules), the default first:
```json
[{"n":0,"file":"Cabaret.srt","label":"","ext":"srt"},
 {"n":1,"file":"Cabaret.en.forced.srt","label":"en.forced","ext":"srt","forced":true}]
```
Looked up on each request, so files added since the last scan appear.

### `GET /api/items/{id}/subs/{n}`
The subtitle file `n` (as listed), as text.

### `POST /api/items/{id}/progress?pos=<ms>&dur=<ms>&state=<state>`
Where this client is in the video. `state` is `playing`, `paused` or `stopped`. When `stopped`
within the last tenth of `dur` (or its last two minutes), the item is marked watched and its
resume point cleared. Without `dur`, the item's runtime is used. → `204 No Content`.

### `POST /api/items/{id}/watched?on=1`
Mark watched (`on=1`) or unwatched (`on=0`) for this client, clearing the resume point. → `204`.

## Browser player endpoints

### `GET /api/items/{id}/web?at=<ms>&audio=<n>&client=<id>`
Start a browser stream of any item at `at` (exact: a moflex starts at the keyframe before it and
drops the gap). Answers once the first segment exists (or `500` with FFmpeg's error), with:

```json
{"url":"/api/web/<sid>/index.m3u8", "start":61000, "dur":5160353, "frame":"fsbs", "view":"3d",
 "audio":[{"n":0,"name":"English (AC3 5.1)","default":true}], "audioN":-1, "title":"…", "pos":0}
```

`url` is an HLS playlist (H.264 + AAC, 2-second MPEG-TS segments; `EVENT` while it's being made)
whose time 0 is `start` in the film. `frame` is the picture: `2d`, or both eyes side by side
(`fsbs`: each eye at full width — a 3D moflex comes as 800×240; `hsbs`: squeezed). `view` is the
item's 3D label. One stream per `client`: a new one stops the one before.

### `GET /api/web/{sid}/{index.m3u8 | sNNNNN.ts}`
The playlist and segments. `410` once the stream has ended (stopped, replaced, or 5 minutes unused).

### `POST /api/web/{sid}/stop`
Stop a stream (the player closed). → `204`.

## Setup page endpoints (`/admin/api`)

These change the server, so they answer **only requests from the same computer** (loopback)
unless *Allow this setup page from other computers* is on; otherwise `403`. Two are open to every
device, for browsing and watching: `GET /admin/api/state` and `GET /admin/api/libraries/{id}/items`
(to another device without that setting: `"viewer": true`, no folders, paths or settings).

| Method | Path | Body / query | |
|---|---|---|---|
| GET | `/admin/api/state` | | name, id, port, version, LAN addresses, libraries (folders, counts), scan status, catalog size |
| PUT | `/admin/api/settings` | `{"name":"…","allowRemoteAdmin":false}` | either field optional |
| POST | `/admin/api/libraries` | `{"name":"…","folders":["…"]}` | add (folders must exist); starts a scan; → `{"id":"…"}` |
| PUT | `/admin/api/libraries/{id}` | same | rename / change folders; starts a scan |
| DELETE | `/admin/api/libraries/{id}` | | forget the library and its videos (files untouched) |
| GET | `/admin/api/browse?path=` | | folders inside `path` and how many moflex it holds; `path` empty = drives (Windows) or top-level places |
| POST | `/admin/api/scan?refresh=1` | | start a scan; `refresh=1` re-reads every file's info; → `{"started":true}` |
| GET | `/admin/api/libraries/{id}/items` | | every item (path, detected info, edits, source, poster) and show, for the grid |
| PUT | `/admin/api/items/{id}` | an Info object | replaces the item's edits (empty fields = use detected) |
| POST | `/admin/api/items/{id}/poster` | JPEG/PNG bytes | upload a poster |
| POST | `/admin/api/items/{id}/match?catalog=<id>` | | apply a Clownsec catalog entry's info and poster as edits |
| PUT | `/admin/api/shows/{id}` | an Info object | a show's edits |
| POST | `/admin/api/shows/{id}/poster` | JPEG/PNG bytes | a show's poster |
| GET | `/admin/api/catalog?q=` | | search the Clownsec catalog (up to 30) |
| POST | `/admin/api/update/check` | | check GitHub now; → the update status |
| POST | `/admin/api/update` | | download, verify and start the newer release, then exit; → `202` |
| POST | `/admin/api/quit` | | stop Big Top; → `204` |

`/admin/api/state` includes `update`: `{"current","latest","available","notes","page","state","error","checked"}`
(`state` is `""`, `checking`, `downloading` or `restarting`).

An **Info** object (detected values and edits use the same shape; empty / zero = unset):

```json
{"title":"","year":0,"summary":"","genres":"","runtime":0,"is3d":"",
 "show":"","showYear":0,"season":0,"episode":0,"epTitle":""}
```

`is3d` is `""` (as detected), `"2d"`, `"3d"` (moflex), or `"hsbs"` / `"fsbs"` (other formats).
