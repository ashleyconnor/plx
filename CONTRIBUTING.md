# Contributing to plx

Contributions are welcome when they fit the project's goals. Please open an
issue to discuss a feature before writing it; bug fixes can go straight to a
pull request.

## Goals

plx is for **managing a Plex library's contents in bulk, from the terminal,
through the Plex API**. The [README](README.md#goals) lists the goals; in
short:

1. **Manage media in bulk**: playlists, collections, labels, files and
   versions, and inspecting what's there.
2. **One model for every source**: libraries, playlists, collections, labels
   and saved queries share the same filter, sort, select and act workflow.
3. **Safe by default**: preview every change; destructive actions default to
   No; Plex's rules are checked before anything is sent; changes are logged.
4. **Plex API only**: no SSH, mounted filesystems or direct database access.
5. **A small terminal tool**: one Go binary, few dependencies, scriptable.

### In scope

Things that fit and aren't built yet:

- Editing titles, sort titles and other metadata, one item or many.
- Merging items that are the same film or episode, and splitting wrongly
  merged ones.
- Fixing matches, refreshing metadata and analysing media for selected items.
- Smart playlists and collections as editable saved filters.
- More fields for the filter language, more columns, and terminal image
  protocols (kitty, iTerm2) for posters.

### Out of scope

- Playback, streaming or remote control.
- Server administration: users, sharing, transcoder and network settings.
- Anything outside the Plex API: moving or renaming files on disk, SSH, or
  editing Plex's database. If the API can't do it, plx doesn't.
- Fetching metadata from other services.
- A GUI or web interface.

## How the code fits together

The [code layout](README.md#code-layout) in the README describes each
package. A few rules keep it maintainable:

- `internal/plex` mirrors Plex's JSON and endpoints and holds no logic.
- Item attributes are defined once, in the field registry
  (`internal/field`). Filters, sorting, columns and autocomplete all read it,
  so a new field works everywhere.
- Every Plex rule (smart playlists are read-only, a collection belongs to one
  library, never delete a version another one shares files with…) lives in
  `internal/plan`. The UI asks for intents and shows the resulting plan; it
  doesn't decide what's allowed.
- `internal/exec` is the only code that changes the server.
- New dependencies need a good reason; the standard library and the existing
  Charm libraries cover most needs.

## Before opening a pull request

```sh
gofmt -l .        # should print nothing
go vet ./...
go test ./...
```

If your change touches what's sent to the server, run the
[integration tests](README.md#tests) against a server you can safely change.
Never test destructive actions on a library you care about.

Add a line under **Unreleased** in [CHANGELOG.md](CHANGELOG.md) for anything
a user would notice. Commit messages follow
[Conventional Commits](https://www.conventionalcommits.org/) (`feat(tui): …`,
`fix(plex): …`).
