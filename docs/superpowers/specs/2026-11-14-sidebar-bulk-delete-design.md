# Sidebar mark mode: bulk delete playlists and collections

## Why

Plex's automatic movie collections persist after their movies are removed.
Clearing them out one `D` + confirm at a time is slow. The user wants to tick
any number of playlists and collections in the sidebar and delete them in one
go.

## Goals

- Mark any mix of playlists and collections in the sidebar, across libraries.
- Delete all marked containers behind a single confirmation.

## Non-goals

- Rule-based cleanup (e.g. "all empty collections").
- Bulk rename or other bulk container actions.

## Anti-goals

- Deleting media: container deletion must leave items in the library.
- Changing what `D` does outside the mode.

## Approaches

1. **Explicit mark mode (chosen).** `m` toggles a sidebar mode where `space`
   marks containers and `D` deletes the marked set. Explicit: `D` never
   silently widens from "this one" to "all marked".
2. Modeless sidebar marks (`space` marks anytime, `D` acts on marks if any).
   One fewer key, but stale marks change what `D` deletes.
3. Reuse item selection (`m.sel`). Rejected: it holds media items and drives
   item actions/`v` view; mixing containers in would muddle both.

## Design

- `m` in the sidebar toggles mark mode. The pane title reads
  `SOURCES · MARK n`.
- In mark mode:
  - `space` toggles a mark on the playlist/collection under the cursor; other
    rows (libraries, labels, queries, groups) are ignored.
  - `a` marks every playlist/collection among the children of the group under
    the cursor (or of the cursor's parent group when on a leaf); if they are
    all already marked, it unmarks them.
  - Marked rows show a ✓; navigation and expand/collapse work as normal.
  - `D` builds one plan deleting all marked containers (`plan.DeleteContainers`);
    with nothing marked, a status hint says so instead.
  - `esc` or `m` leaves the mode and clears marks.
- The confirm screen lists each container to be deleted; focus defaults to No
  (it is destructive). Smart containers are allowed, as with single delete.
- After a successful apply the mode exits and the sidebar reloads (existing
  invalidate path).
- Help (`?`) and the sidebar hint bar document `m`, and in mode `space`, `a`,
  `D`, `esc`.
- Outside mark mode every existing key behaves as before.

## Left to the builder

- Exact styling of the ✓ and title badge (reuse existing styles).
- Marks are keyed by container kind + ID so a sidebar reload doesn't lose them
  mid-mode.
