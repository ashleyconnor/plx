# Sidebar Bulk Delete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A sidebar mark mode (`m`) to tick playlists/collections and delete them all behind one confirmation.

**Architecture:** A new planner `plan.DeleteContainers` turns many containers into one plan of `OpDeleteContainer` ops (exec already handles that op). The sidebar gets `marking` state plus a mark set keyed by kind+ID; `sideKey` routes `space`/`a`/`D`/`esc`/`m` while marking. The existing preview/confirm/exec/finished pipeline does the rest.

**Tech Stack:** Go, Bubble Tea, lipgloss; tests with `go test` and the fake-Plex harness in `internal/tui/harness_test.go`.

**Spec:** `docs/superpowers/specs/2026-11-14-sidebar-bulk-delete-design.md`

## Global Constraints

- Container deletion keeps items in the library; the plan must never contain `OpDelete`/`OpDeleteVersion`.
- Outside mark mode, every existing sidebar key behaves exactly as today (`D` deletes the row under the cursor).
- Destructive confirm defaults to No (already true via `Plan.Destructive()`).

## Review Focus

- **Long mark lists:** marking 50+ collections must not push the confirm buttons off-screen → preview caps the op list at 15 lines plus "… and N more". (Task 3 test.)
- **Sidebar reload mid-mode** (`r`, or a lazy group load) rebuilds nodes → marks are keyed by `Kind+ID`, not `*node`, so they survive. (Task 2 test.)
- **Failed apply** (e.g. server 403 on one DELETE) → stay in mark mode with marks kept so the user can retry; success exits and clears. (Task 3 test.)
- **`space`/`a` on non-container rows** (libraries, labels, queries, group headers) → no-op, no mark. (Task 2 test.)
- **Duplicates** (same container marked twice via `a` then `space`) → one op per container. (Task 1 test.)

---

### Task 1: `plan.DeleteContainers`

**Files:**
- Modify: `internal/plan/plan.go` (next to `DeleteContainer`, ~line 480)
- Test: `internal/plan/plan_test.go`

**Interfaces:**
- Produces: `func DeleteContainers(cs []*model.Container) *Plan`

- [ ] **Step 1: Write the failing test `TestDeleteContainers`**
  - `DeleteContainers([]*model.Container{videoPL, coll, smartPL, coll})` → not blocked, 3 ops all `OpDeleteContainer`, targets in input order, `Destructive()` true, `DeletesFiles()` false, `Title == "Delete 3 playlists and collections"`.
  - `DeleteContainers([]*model.Container{coll})` → `Title == DeleteContainer(coll).Title`.
  - `DeleteContainers(nil)` → blocked.
  - A label container in the list → blocked.
- [ ] **Step 2:** `go test ./internal/plan -run TestDeleteContainers` → FAIL (undefined).
- [ ] **Step 3: Implement.** Dedupe by `Kind`+`ID` keeping first occurrence; empty → `block("nothing marked")`; any non-playlist/collection → reuse `editable` to block; single → delegate to `DeleteContainer`. Title for many: `"Delete %d playlists and collections"`, or `"Delete %d playlists"` / `"Delete %d collections"` when all one kind (add those cases to the test).
- [ ] **Step 4:** `go test ./internal/plan` → PASS.
- [ ] **Step 5:** commit `feat(plan): delete several playlists and collections at once`.

### Task 2: Mark mode in the sidebar

**Files:**
- Modify: `internal/tui/sidebar.go` (state, `sideKey`, `sideRow`, `sideView` title)
- Modify: `internal/tui/render.go` (`hintsLine` focusSide branch)
- Modify: `internal/tui/help.go` (new "Sidebar" key group or entries)
- Test: `internal/tui/sidebar_mark_test.go` (new)

**Interfaces:**
- Consumes: `m.sideContainer()`, `node.view.container()`.
- Produces on `sidebar`: `marking bool`, `marks map[string]*model.Container`, `markOrder []string`; helpers `markKey(c *model.Container) string` (`c.Kind.String()+":"+c.ID`), `(s *sidebar) toggleMark(c)`, `(s *sidebar) marked(c) bool`, `(s *sidebar) markedList() []*model.Container` (in mark order), `(s *sidebar) exitMarking()` (clears marks).

- [ ] **Step 1: Write failing tests** (use `sideTo(h, "Alpha")` from `actions_test.go`):
  - `TestMarkModeToggle`: `press("m")` → `side.marking`, view contains `MARK 0` and hint `space mark`; `press("m")` → not marking. `esc` also exits and clears marks.
  - `TestMarkSpace`: in mode, `space` on Alpha → `MARK 1`, row shows `✓`; cursor advances one row; `space` on a library row (`Movies` under Libraries) → still 1 mark.
  - `TestMarkGroupAll`: cursor on Alpha, `a` → both Alpha and Smart marked; `a` again → none.
  - `TestMarksSurviveReload`: mark Alpha, `press("r")`, drive → `len(markedList()) == 1`.
  - `TestKeysOutsideMarkMode`: without `m`, `space` marks nothing.
- [ ] **Step 2:** `go test ./internal/tui -run 'Mark'` → FAIL.
- [ ] **Step 3: Implement.** In `sideKey`: `m` toggles (exiting clears); when `marking`, handle `" "` (toggle `m.sideContainer()` if non-nil, then `cursor++`), `a` (containers among the children of the current group, or of the current leaf's parent; all marked → unmark all, else mark all), `esc` (exit). Title: `paneTitle("SOURCES · MARK n", …)` while marking. `sideRow`: prefix the name with `✓ ` for marked leaves (style `stKey` when not under cursor). Hints while marking: `space mark`, `a all`, `D delete marked`, `esc done`; otherwise add `m mark` to the focusSide hints. Help: add `{"m", "mark playlists/collections (sidebar)"}` to Act.
- [ ] **Step 4:** `go test ./internal/tui` → PASS (all existing tests too).
- [ ] **Step 5:** commit `feat(tui): mark mode for playlists and collections in the sidebar`.

### Task 3: Bulk delete from mark mode

**Files:**
- Modify: `internal/tui/sidebar.go` (`D` in mark mode)
- Modify: `internal/tui/actions.go` (`finished`)
- Modify: `internal/tui/render.go` (`previewView` op cap)
- Test: `internal/tui/sidebar_mark_test.go`

**Interfaces:**
- Consumes: `plan.DeleteContainers`, `sidebar.markedList()`, `sidebar.exitMarking()`.

- [ ] **Step 1: Write failing tests:**
  - `TestBulkDeleteMarked`: mark Alpha and Smart, `D` → `modePreview`, view contains `Delete 2 playlists`, both `delete playlist "Alpha"` and `"Smart"` lines, `confirmYes == false`; `press("y")` (or `left`/`enter` per existing confirm keys) → writes `DELETE /playlists/50`, `DELETE /playlists/51`; afterwards `!side.marking` and no marks.
  - `TestBulkDeleteNothingMarked`: `m`, `D` → still `modeNormal`, status contains `nothing marked`, no writes.
  - `TestBulkDeleteFailureKeepsMarks`: `h.sv.denyDelete = true`, mark Alpha, `D`, confirm → `side.marking` still true, 1 mark.
  - `TestPreviewCapsLongOpList`: build a plan via `plan.DeleteContainers` of 30 fake collections, set `m.preview`, render → contains `… and 15 more` and `Yes (y)`.
- [ ] **Step 2:** `go test ./internal/tui -run 'BulkDelete|PreviewCaps'` → FAIL.
- [ ] **Step 3: Implement.** In marking `D`: empty → `m.setStatus("nothing marked: space marks a playlist or collection", false)`; else `planCmd(plan.DeleteContainers(s.markedList()))`. In `finished` success branch: `if m.side.marking { m.side.exitMarking() }`. In `previewView`: after 15 non-move op lines, write dim `… and N more` and stop listing.
- [ ] **Step 4:** `go test ./...` and `go vet ./...` → PASS.
- [ ] **Step 5:** commit `feat(tui): bulk delete marked playlists and collections`; add a CHANGELOG entry and README key mention if the README lists sidebar keys.
