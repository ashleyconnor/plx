# plx

plx manages a Plex Media Server library from the terminal: bulk changes to
playlists, collections and labels, finding and deleting files, and
inspecting what's on disk. It talks to the server only through the Plex API.

The Plex web UI handles one item at a time and treats each place differently:
playlists can't be filtered or sorted, collections can't be split, you can't
filter on two labels at once or drill into folders, and duplicate versions
are hard to find. In plx every library, playlist, collection, label and saved
query is a list of items with the same tools: filter, sort, browse by
folder, select, and act on the selection.

## Goals

1. **Manage media in bulk.** Select many items from anywhere and act on all
   of them at once: playlists, collections, labels, deleting files and
   versions, and inspecting files, streams and metadata.
2. **One model for every source.** Libraries, playlists, collections, labels
   and saved queries behave the same way; an action that works on one works
   on all, within Plex's rules.
3. **Safe by default.** Every change is previewed before it runs, file
   deletion defaults to No, Plex's restrictions are checked before anything
   is sent, and every change is logged.
4. **Plex API only.** No SSH or filesystem access; plx works wherever the
   server is reachable and can do only what the API allows.
5. **A small terminal tool.** One Go binary with few dependencies, usable
   from the TUI or scripted from the shell.

[CONTRIBUTING.md](CONTRIBUTING.md) covers what's in scope.

## Features

- Three-pane TUI: sources, the item list, and the highlighted item's details
  with its poster.
- A filter language with autocomplete: several labels at once, sizes,
  durations, ages, paths at any depth, membership (`-has:playlist`), and
  versions (`dur.min<5m` finds samples).
- Sort any list, including playlists and collections, by any field; save
  the order back to Plex.
- Folder lens: browse any list as its folder tree at any depth.
- Selection that persists across lists; review it as a list of its own.
- Actions: add to or remove from playlists and collections, create new ones
  from the selection, move items between them, add and remove labels,
  delete items or individual versions from disk.
- Saved queries in a TOML file, shown as sources.
- `plx q` prints any query's results for scripts.
- Sign in with a plex.tv/link code or QR code; no token hunting.

## Install

Requires Go 1.24 or later and a Plex Media Server.

```sh
go install github.com/ashleyconnor/plx/cmd/plx@latest
```

Deleting files needs the server setting Settings → Library → *Allow media
deletion*; plx checks it before offering a deletion.

## Sign in

Run `plx` and sign in the way a TV app does: it shows a 4-character code and
a QR code; scan it with your phone, or go to plex.tv/link and enter the
code. plx then lists your servers (asking which, if there's more than one),
finds an address that answers (local first, over HTTPS with the server's
plex.direct certificate) and saves it with the server's access token to
`~/.config/plx/config.toml` (mode 0600). `plx login` does it again, for
example to switch servers. plx appears in Plex's Authorized Devices, where
you can revoke it.

Or configure by hand, in `config.toml`:

```toml
url = "192.168.1.10:32400"   # https:// is assumed if no scheme
token = "…"
insecure = true              # skip TLS verification (self-signed certs)
icons = "emoji"              # emoji (default), ascii or none
```

or with the `PLEX_URL`, `PLEX_TOKEN` and `PLEX_INSECURE` environment
variables, which override the file. An `https://` URL with a bare IP works
without `insecure`: plx accepts the server's plex.direct certificate for that
IP, which is what Plex serves. `plx login` rewrites the file, so
comments in it aren't kept.

## Use

```sh
plx                                   # TUI (signs in first if needed)
plx login                             # sign in again or switch servers
plx ls                                # list sources
plx q playlist:Watchlist -is:watched  # query from the shell
plx fields                            # query fields
```

The screen has three panes: sources on the left (libraries, playlists,
collections and labels by library, saved queries), the item list in the
middle, and the highlighted item's details on the right. Resting the cursor
on a source shows it in the list; `tab` moves between panes and `p` hides
the details pane. On narrow terminals the details pane goes first, then the
sidebar.

Keys: `j/k` move, `l/h` open/back (`h` at the top of a list returns to the
sources), `space` select, `a` select all/none, `v` view selection, `/`
filter, `s` sort, `f` folder lens, `x` or `;` actions (including delete from
disk), `S` save query, `?` help.

Selection persists across views, so you can collect items from several places
and act on all of them at once. Every action shows a preview first: what will
change, what gets skipped and why, then **No (n)** / **Yes (y)** buttons
(`y`/`n` act at once; `←→` and `enter` work too). Focus starts on Yes,
except for anything that deletes files, which starts on No. Changes are logged to
`~/.config/plx/actions.log`.

## Filter language

```
alien                       title contains "alien"
size>4G  dur<30m  plays>1   comparisons (sizes K M G T, durations m h d);
                            size:>4G works too
label:keep label:fav        both labels
label:keep,fav              either label
-label:trash                negate any term
has:label  -has:label       has any label / has none
labelcount:1 label:keep     keep is the only label
-has:collection -has:playlist   in no collection or playlist
versions>1                  has more than one version (duplicates, samples)
dur.min<5m                  has a short version, e.g. a Sample.mkv
```

Items with several versions show each value, shortest version first:
`2m|2h08m`, `358.6M|22.7G`, `1080|4k`. Numeric fields filter and sort on the
largest version (`dur`, `size`, `bitrate`; append `.min` for the smallest),
text fields (`res`, `codec`, `container`) match any version, and
`totalsize` is the disk used by all of them. `x` → `V` deletes a chosen
version, or the samples of every selected item.

```
playlist:"Watch later"      in that playlist (collection: likewise)
is:watched  -is:watched     yes/no fields
path:/tv/anime/             substring at any depth; path:/tv/*/S01/* glob
added<30d  viewed>1y        relative ages; added>2024-01-01 absolute dates
sort:size-,title            sort, - for descending
```

While typing a filter, `tab` completes field names, `is:`/`has:` targets,
sort fields and values such as labels, genres and resolutions (taken from the
list you're filtering, most common first); `↑`/`↓` choose a suggestion.

The details pane shows full metadata, every media version with files, audio
and subtitle streams, and the poster drawn with half-block characters, which
works in any 256-colour or true-colour terminal. It's fetched once the cursor
rests on an item. `enter` (or `i`) focuses the pane to scroll it and looks up
the item's playlists; without room for the pane it opens as an overlay.

## Saved queries

`~/.config/plx/queries.toml`, appended by `S` in the TUI or written by hand:

```toml
[[query]]
name    = "Big unwatched anime"
filter  = "-is:watched size>4G path:/tv/anime/"
sort    = "size-"
columns = ["size", "duration", "labels"]

  [[query.sources]]
  section = "TV"

  [[query.sources]]
  playlist = "Watch later"   # title or ratingKey
```

Sources can be `section`, `playlist`, `collection` (with optional `section`),
or `label` with `section`. A query with several sources shows their union.

## Code layout

```
internal/plex    HTTP client and DTOs that mirror Plex's JSON. No logic.
internal/model   Item and Container domain types, built from DTOs.
internal/field   Registry of item attributes, used by filter, sort and columns.
internal/query   Filter language: lexer, parser, evaluation, sorting.
internal/source  Libraries, playlists, collections, labels, saved queries → items.
internal/plan    Turns intents into previewable plans; every Plex rule lives here.
internal/exec    Applies plans through the client and writes the action log.
internal/store   Config, saved queries.
internal/tui     Bubble Tea UI.
```

## Tests

```sh
go test ./...
```

The write path has an opt-in integration test against a real server. It
creates and deletes scratch playlists (`plx-test`, `plx-test-2`) and adds then
removes a `plx-test-label` label on a few items:

```sh
PLEX_URL=… PLEX_TOKEN=… PLEX_INSECURE=1 PLX_TEST_SECTION=Movies \
  go test -tags integration ./internal/exec/ -v
```

Server discovery has a read-only one, with an account token: it lists the
account's servers and connects to the first.

```sh
PLEX_TOKEN=… go test -tags integration ./internal/plex/ -run TestServersAndConnect -v
```

## Built with

| Library | Used for |
| --- | --- |
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | the TUI event loop |
| [Bubbles](https://github.com/charmbracelet/bubbles) | text input and spinner |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | styling, tables and layout |
| [x/ansi](https://github.com/charmbracelet/x) | measuring and trimming styled text |
| [go-runewidth](https://github.com/mattn/go-runewidth) | column widths for wide characters |
| [BurntSushi/toml](https://github.com/BurntSushi/toml) | config and saved queries |
| [rsc.io/qr](https://pkg.go.dev/rsc.io/qr) | the sign-in QR code |

The Plex client is written for plx against Plex's JSON API; endpoints were
worked out with reference to [python-plexapi](https://github.com/pushingkarmaorg/python-plexapi).

## Contributing

Contributions are welcome when they fit the [goals](#goals); see
[CONTRIBUTING.md](CONTRIBUTING.md). Changes are recorded in
[CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
