# plx

An nnn-style terminal UI for managing a Plex library through the Plex API.

Libraries, playlists, collections, labels and saved queries all work the same
way: each is a list of items you can filter, sort, browse by folder, select
from, and act on in bulk. The Plex web UI can't do most of this.

## Setup

```sh
go install github.com/ashleyconnor/plx/cmd/plx@latest
```

Configure with `~/.config/plx/config.toml`:

```toml
url = "192.168.1.10:32400"   # https:// is assumed if no scheme
token = "…"
insecure = true              # skip TLS verification (self-signed certs)
icons = "emoji"              # emoji (default), ascii or none
```

or with the `PLEX_URL`, `PLEX_TOKEN` and `PLEX_INSECURE` environment variables.

## Use

```sh
plx                                   # TUI
plx ls                                # list sources
plx q playlist:Watchlist -is:watched  # query from the shell
plx fields                            # query fields
```

Keys: `j/k` move, `l/h` open/back, `space` select, `a` select visible,
`a` select all/none, `v` view selection, `/` filter, `s` sort, `f` folder
lens, `x` or `;` actions (including delete from disk), `S` save query,
`?` help.

Selection persists across views, so you can collect items from several places
and act on all of them at once. Every action shows a preview first: what will
change, what gets skipped and why. Changes are logged to
`~/.config/plx/actions.log`.

## Filter language

```
alien                       title contains "alien"
size>4G  dur<30m            comparisons (sizes K M G T, durations m h d)
label:keep label:fav        both labels
label:keep,fav              either label
-label:trash                negate any term
has:label  -has:label       has any label / has none
labelcount:1 label:keep     keep is the only label
is:watched  -is:watched     yes/no fields
path:/tv/anime/             substring at any depth; path:/tv/*/S01/* glob
added<30d  viewed>1y        relative ages; added>2024-01-01 absolute dates
sort:size-,title            sort, - for descending
```

While typing a filter, `tab` completes field names, `is:`/`has:` targets,
sort fields and values such as labels, genres and resolutions (taken from the
list you're filtering, most common first); `↑`/`↓` choose a suggestion.

`enter` (or `i`) on an item opens its details: full metadata, every media
version with files, audio and subtitle streams, and the poster drawn with
half-block characters, which works in any 256-colour or true-colour terminal.

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

## Layout

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
