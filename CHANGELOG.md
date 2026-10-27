# Changelog

All notable changes to plx are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

First release.

### Added

- Terminal UI with three panes: sources (libraries, playlists, collections
  and labels by library, saved queries), the item list, and a details pane
  with full metadata, media versions, streams and a half-block poster.
- Filter language: free text, comparisons on sizes, durations, dates and
  counts, several labels at once (AND and OR), negation, `has:` and `is:`,
  path substrings and globs at any depth, playlist and collection membership,
  and `sort:`. Autocomplete for fields and values.
- Sorting any list by any field, including playlists and collections, and
  saving that order back to Plex.
- Folder lens for browsing any list by folder at any depth.
- Selection that persists across lists, with select all, invert, and a view
  of the selection.
- Bulk actions: add to and remove from playlists and collections, move items
  between them, create new ones from the selection, add and remove labels.
- Renaming and deleting playlists and collections from the sources sidebar
  (`R` and `D`). Deleting one keeps its items and defaults to No.
- Deleting items (all versions) or chosen versions from disk, including the
  samples of every selected item. Deleting versions never removes an item's
  last version or a file that another version uses.
- A preview of every change, with what's skipped and why, Yes/No buttons
  with `y`/`n` shortcuts, and No as the default for file deletion.
- Action log of every change in `~/.config/plx/actions.log`.
- Saved queries in `~/.config/plx/queries.toml`, shown as sources.
- Command line: `plx ls`, `plx q <source> [query]` and `plx fields`.
- Sign-in with a plex.tv/link code and QR code (`plx login`), server
  discovery, and choosing the best address that answers.
- Certificate verification for bare-IP HTTPS URLs against the server's
  plex.direct certificate, so `insecure` is rarely needed.
- Emoji, ASCII or no icons.

### Fixed

- A label with the same name in two libraries no longer shows the first
  library's items when you open the second.
- Date filters now mean the whole day: `added>2024-01-15` is after that day,
  `added<=2024-01-15` up to its end, and `=`/`!=` are exact opposites.
- `res>=720`, `res<1080` and the like compare resolutions in order (sd, 480,
  576, 720, 1080, 2k, 4k, 8k) instead of as text, and an unknown resolution
  is reported.
- Sizes and durations such as `size>inf` or `added<nan` are rejected with an
  error instead of silently matching the wrong items.
