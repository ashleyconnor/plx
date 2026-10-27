package tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestAddToPlaylistPreviewCancelSendsNothing(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";")
	h.wantMode(modeMenu)
	h.wantView("1 item", "Add to playlist", "Delete from disk")
	h.press("p")
	h.wantMode(modePicker)
	h.wantView("Alpha")
	if strings.Contains(h.view(), "Smart") {
		t.Error("smart playlists are read-only and must not be offered")
	}
	h.press("enter")
	h.wantMode(modePreview)
	h.wantView("Add to playlist \"Alpha\"", "add 1 items")
	h.press("esc")
	h.wantMode(modeNormal)
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("cancelled preview sent %v", w)
	}
}

func TestAddToPlaylistConfirmApplies(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "p", "enter")
	h.wantMode(modePreview)
	if !h.m.confirmYes {
		t.Error("non-destructive previews start on Yes")
	}
	h.press("enter")
	h.wantMode(modeNormal)
	if got, want := h.sv.writes(), []string{"PUT /playlists/50/items"}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
	if !strings.Contains(h.m.status, "done") || len(h.m.sel) != 0 {
		t.Errorf("status %q", h.m.status)
	}
}

func TestNewPlaylistFromTypedName(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "p", "Fresh")
	h.wantView(`New playlist "Fresh"`)
	h.press("enter")
	h.wantMode(modePreview)
	h.wantView(`create video playlist "Fresh" with 1 items`)
	h.press("n")
	h.wantMode(modeNormal)
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("n sent %v", w)
	}
}

func TestDeleteDefaultsToNoAndEnterSendsNoDelete(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "D")
	h.wantMode(modePreview)
	if h.m.confirmYes {
		t.Fatal("deleting files must start focused on No")
	}
	h.wantView("Delete 1 items", "permanently deleted")
	h.press("enter")
	h.wantMode(modeNormal)
	h.press(" ") // enter space on a fresh preview too
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("reflexive enter sent %v", w)
	}
}

func TestDeleteSpaceOnNoAlsoCancels(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "D", " ")
	h.wantMode(modeNormal)
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("sent %v", w)
	}
}

func TestDeleteConfirmedByChoosingYes(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "D", "right")
	if !h.m.confirmYes {
		t.Fatal("right should move focus to Yes")
	}
	h.press("enter")
	h.wantMode(modeNormal)
	if got, want := h.sv.writes(), []string{"DELETE /library/metadata/10"}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
}

func TestDeleteYKeyAppliesImmediately(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "D", "y")
	if got := h.sv.writes(); len(got) != 1 || got[0] != "DELETE /library/metadata/10" {
		t.Errorf("writes = %v", got)
	}
}

func TestDeleteRefusedWhenServerForbidsIt(t *testing.T) {
	h := newHarness(t)
	h.sv.allowDel = false
	h.open()
	h.press(";", "D")
	h.wantMode(modePreview)
	h.wantView("doesn't allow media deletion")
	h.press("y") // a blocked plan can't be confirmed
	h.wantMode(modePreview)
	h.press("enter")
	h.wantMode(modeNormal)
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("blocked delete sent %v", w)
	}
}

func TestDeleteVersionOffersEachVersionAndBulkRules(t *testing.T) {
	h := newHarness(t)
	h.open() // cursor on Alien, which has two versions
	h.press(";")
	h.wantView("Delete a version", "all versions")
	h.press("V")
	h.wantMode(modePicker)
	h.wantView("Delete which version?", "sample.mkv", "smallest file")
	h.press("sample")
	h.press("enter")
	h.wantMode(modePreview)
	if h.m.confirmYes {
		t.Error("deleting a version is destructive: default No")
	}
	h.press("right", "enter")
	if got := h.sv.writes(); len(got) != 1 || got[0] != "DELETE /library/metadata/10/media/101" {
		t.Errorf("writes = %v", got)
	}
}

func TestMenuOnlyOffersVersionDeleteForMultiVersionItems(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("down", ";") // Brazil: one version
	h.wantView("Delete from disk…")
	if strings.Contains(h.view(), "Delete a version") || strings.Contains(h.view(), "all versions") {
		t.Errorf("single-version item offered version delete:\n%s", h.view())
	}
}

func TestMenuEscAndNavigation(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "down", "up", "esc")
	h.wantMode(modeNormal)
	h.press(";", "q")
	h.wantMode(modeNormal)
}

func TestMenuWithNothingTargetedSaysSo(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("/", "zzz", "enter", ";")
	h.wantMode(modeNormal)
	if h.m.status != "nothing selected" {
		t.Errorf("status = %q", h.m.status)
	}
}

func TestMovesAndRemovesOnlyOfferedInsideRegularContainers(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";")
	if strings.Contains(h.view(), "Remove from this") {
		t.Error("a library has no remove action")
	}
	h.press("esc")
	// open the regular playlist: remove and move are available
	h.m.stack = []*view{h.m.itemsView(sourceMembers(h, "50", false), nil)}
	h.drive(h.m.load(h.m.top()))
	h.press(";")
	h.wantView("Remove from this playlist", "Move from this playlist to playlist", "Reorder this playlist")
	h.press("d")
	h.wantMode(modePreview)
	h.wantView("remove 1 items from playlist")
	h.press("enter")
	if got := h.sv.writes(); len(got) != 1 || got[0] != "DELETE /playlists/50/items/900" {
		t.Errorf("writes = %v", got)
	}
}

func TestSmartPlaylistOffersNoMutations(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.m.stack = []*view{h.m.itemsView(sourceMembers(h, "51", true), nil)}
	h.m.top().items = h.m.top().items[:0]
	h.m.top().loading = false
	h.m.top().items = append(h.m.top().items, h.m.side.roots[0].children[0].view.items...)
	h.m.top().rebuild()
	h.press(";")
	h.wantView("Add to playlist") // the menu is open, with only the non-mutating-source actions
	if strings.Contains(h.view(), "Remove from") || strings.Contains(h.view(), "Move from") || strings.Contains(h.view(), "Reorder") {
		t.Errorf("smart playlist menu:\n%s", h.view())
	}
}

func TestReorderNeedsASortFirst(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.m.stack = []*view{h.m.itemsView(sourceMembers(h, "50", false), nil)}
	h.drive(h.m.load(h.m.top()))
	h.press(";", "o")
	h.wantMode(modeNormal)
	if !strings.Contains(h.m.status, "choose a sort first") {
		t.Errorf("status = %q", h.m.status)
	}
}

func TestPromptSaveQueryWritesFileAndRejectsDuplicates(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("/", "alien", "enter", "S")
	h.wantMode(modePrompt)
	h.press("My query", "enter")
	h.wantMode(modeNormal)
	if !strings.Contains(h.m.status, `saved query "My query"`) {
		t.Errorf("status = %q", h.m.status)
	}
	h.press("S", "my query", "enter")
	if !strings.Contains(h.m.status, "already exists") || !h.m.statusErr {
		t.Errorf("duplicate status = %q", h.m.status)
	}
	// empty name does nothing; esc aborts
	h.press("S", "enter")
	h.wantMode(modeNormal)
	h.press("S", "esc")
	h.wantMode(modeNormal)
}

func TestAddLabelFlow(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "l", "keep", "enter")
	h.wantMode(modePreview)
	h.wantView(`add label "keep" to 1 items`)
	h.press("y")
	if got := h.sv.writes(); len(got) != 1 || got[0] != "PUT /library/sections/1/all" {
		t.Errorf("writes = %v", got)
	}
}

func TestRemoveLabelBlockedWhenItemHasNone(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(";", "L")
	h.wantMode(modePicker) // no labels to offer
	h.press("esc")
	h.wantMode(modeNormal)
}

func TestFailedExecutionReportsErrorAndKeepsSelection(t *testing.T) {
	h := newHarness(t)
	h.sv.denyDelete = true
	h.open()
	h.press(" ")
	h.press(";", "D", "y")
	h.wantMode(modeNormal)
	if !h.m.statusErr || !strings.Contains(h.m.status, "failed") || !strings.Contains(h.m.status, "403") {
		t.Errorf("status = %q", h.m.status)
	}
	if len(h.m.sel) != 1 {
		t.Error("a failed action must keep the selection so it can be retried")
	}
}

// sideTo moves the sidebar cursor to the playlist named name.
func sideTo(h *harness, name string) {
	h.t.Helper()
	h.drive(h.m.sideInit())
	h.m.focus = focusSide
	for i, n := range h.m.side.flat {
		if n.name() == name && n.leaf() {
			h.m.side.cursor = i
			return
		}
	}
	h.t.Fatalf("%s not in the sidebar", name)
}

func TestRenamePlaylistFromSidebar(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.wantView("R rename", "D delete")
	h.press("R")
	h.wantMode(modePrompt)
	if h.m.input.Value() != "Alpha" {
		t.Errorf("prompt starts with %q", h.m.input.Value())
	}
	h.press("backspace", "backspace", "backspace", "backspace", "backspace", "Beta", "enter")
	h.wantMode(modePreview)
	h.wantView(`rename playlist "Alpha" to "Beta"`)
	if !h.m.confirmYes {
		t.Error("rename should start on Yes")
	}
	h.press("enter")
	h.wantMode(modeNormal)
	if got, want := h.sv.writes(), []string{"PUT /playlists/50"}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
}

func TestRenameUnchangedOrEsc(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("R", "enter")
	h.wantMode(modeNormal)
	h.press("R", "esc")
	h.wantMode(modeNormal)
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("sent %v", w)
	}
}

func TestDeletePlaylistDefaultsToNo(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Smart")
	h.press("D")
	h.wantMode(modePreview)
	h.wantView(`delete playlist "Smart" (its items are kept)`, "can't be undone")
	if h.m.confirmYes {
		t.Error("deleting a playlist should start on No")
	}
	h.press("enter")
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("enter on No sent %v", w)
	}
	h.press("D", "y")
	if got, want := h.sv.writes(), []string{"DELETE /playlists/51"}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
}

func TestRenameAndDeleteIgnoredOnOtherSources(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Movies")
	h.wantView()
	if strings.Contains(h.view(), "R rename") {
		t.Error("libraries can't be renamed")
	}
	h.press("R", "D")
	h.wantMode(modeNormal)
}
