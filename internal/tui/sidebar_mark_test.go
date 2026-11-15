package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
)

func TestMarkModeToggle(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m")
	if !h.m.side.marking {
		t.Fatal("m should enter mark mode")
	}
	h.wantView("MARK 0", "space mark")
	h.press("m")
	if h.m.side.marking {
		t.Fatal("m again should leave mark mode")
	}
	h.press("m", " ", "esc")
	if h.m.side.marking || len(h.m.side.markedList()) != 0 {
		t.Fatal("esc should leave mark mode and clear marks")
	}
}

func TestMarkSpace(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	at := h.m.side.cursor
	h.press("m", " ")
	h.wantView("MARK 1", "✓")
	if h.m.side.cursor != at+1 {
		t.Errorf("cursor = %d, want %d", h.m.side.cursor, at+1)
	}
	for i, n := range h.m.side.flat {
		if n.name() == "Movies" && n.leaf() {
			h.m.side.cursor = i
		}
	}
	h.press(" ")
	if got := len(h.m.side.markedList()); got != 1 {
		t.Errorf("marks = %d, want 1 (a library is not markable)", got)
	}
}

func TestMarkGroupAll(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m", "a")
	if got := len(h.m.side.markedList()); got != 2 {
		t.Fatalf("marks = %d, want 2", got)
	}
	h.press("a")
	if got := len(h.m.side.markedList()); got != 0 {
		t.Fatalf("marks = %d, want 0", got)
	}
}

func TestMarksSurviveReload(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m", " ")
	h.press("r")
	h.drive(h.m.reloadSide())
	if got := len(h.m.side.markedList()); got != 1 {
		t.Errorf("marks = %d, want 1", got)
	}
}

func TestKeysOutsideMarkMode(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press(" ")
	if got := len(h.m.side.markedList()); got != 0 || h.m.side.marking {
		t.Errorf("space marked %d outside mark mode", got)
	}
}

func TestBulkDeleteMarked(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m", "a", "D")
	h.wantMode(modePreview)
	h.wantView("Delete 2 playlists", `delete playlist "Alpha"`, `delete playlist "Smart"`)
	if h.m.confirmYes {
		t.Error("bulk delete should default to No")
	}
	h.press("y")
	h.wantMode(modeNormal)
	if got, want := h.sv.writes(), []string{"DELETE /playlists/50", "DELETE /playlists/51"}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
	if h.m.side.marking || len(h.m.side.markedList()) != 0 {
		t.Error("a successful bulk delete should leave mark mode")
	}
}

func TestBulkDeleteMovesCursorToParent(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m", "a", "D")
	h.sv.mu.Lock()
	h.sv.hidePlaylists = map[string]bool{"50": true, "51": true} // gone after the delete
	h.sv.mu.Unlock()
	h.press("y")
	h.wantMode(modeNormal)
	if n := h.m.side.current(); n == nil || n.name() != "Playlists" {
		name := "<nil>"
		if n != nil {
			name = n.name()
		}
		t.Errorf("cursor on %q, want the Playlists group the deleted rows were in", name)
	}
}

func TestBulkDeleteNothingMarked(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m", "D")
	h.wantMode(modeNormal)
	if !strings.Contains(h.m.status, "nothing marked") {
		t.Errorf("status %q", h.m.status)
	}
	if w := h.sv.writes(); len(w) != 0 {
		t.Errorf("sent %v", w)
	}
}

func TestBulkDeleteFailureKeepsMarks(t *testing.T) {
	h := newHarness(t)
	h.sv.denyDelete = true
	sideTo(h, "Alpha")
	h.press("m", " ", "D", "y")
	h.wantMode(modeNormal)
	if !h.m.side.marking || len(h.m.side.markedList()) != 1 {
		t.Errorf("marking=%v marks=%d, want mark mode kept with 1 mark", h.m.side.marking, len(h.m.side.markedList()))
	}
	if got, want := h.sv.writes(), []string{"DELETE /playlists/50"}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
	if !strings.HasPrefix(h.m.status, "failed") {
		t.Errorf("status %q, want a failure", h.m.status)
	}
}

func TestMarksPrunedAfterReload(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m", " ", " ")
	if got := len(h.m.side.markedList()); got != 2 {
		t.Fatalf("marks = %d, want 2", got)
	}
	h.sv.mu.Lock()
	h.sv.hidePlaylists = map[string]bool{"51": true} // Smart was deleted elsewhere
	h.sv.mu.Unlock()
	h.press("r")
	h.drive(h.m.reloadSide())
	marked := h.m.side.markedList()
	if len(marked) != 1 || marked[0].Title != "Alpha" {
		var titles []string
		for _, c := range marked {
			titles = append(titles, c.Title)
		}
		t.Errorf("marks = %v, want [Alpha]", titles)
	}
	h.wantView("MARK 1")
}

func TestOtherPlanKeepsMarks(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	h.press("m", " ", "k", "R")
	h.wantMode(modePrompt)
	h.press("backspace", "backspace", "backspace", "backspace", "backspace", "Beta", "enter", "enter")
	h.wantMode(modeNormal)
	if got, want := h.sv.writes(), []string{"PUT /playlists/50"}; !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
	if !h.m.side.marking || len(h.m.side.markedList()) != 1 {
		t.Errorf("marking=%v marks=%d, want mark mode kept with 1 mark", h.m.side.marking, len(h.m.side.markedList()))
	}
}

func TestMarkAllNothingMarkable(t *testing.T) {
	h := newHarness(t)
	sideTo(h, "Alpha")
	for i, n := range h.m.side.flat {
		if n.name() == "Libraries" && n.depth == 0 {
			h.m.side.cursor = i
		}
	}
	h.press("m", "a")
	if h.m.status != "nothing to mark here" || h.m.statusErr {
		t.Errorf("status %q (err %v)", h.m.status, h.m.statusErr)
	}
	if got := len(h.m.side.markedList()); got != 0 {
		t.Errorf("marks = %d, want 0", got)
	}
}

func TestPreviewCapsLongOpList(t *testing.T) {
	h := newHarness(t)
	var cs []*model.Container
	for i := 0; i < 30; i++ {
		cs = append(cs, &model.Container{Kind: model.Collection, ID: fmt.Sprint(100 + i), Title: fmt.Sprintf("Coll %02d", i)})
	}
	h.m.preview, h.m.mode = plan.DeleteContainers(cs), modePreview
	h.wantView("… and 15 more (15 collections)", "Yes (y)")
	cs = append(cs[:20:20], &model.Container{Kind: model.Playlist, ID: "200", Title: "Play A"}, &model.Container{Kind: model.Playlist, ID: "201", Title: "Play B"})
	h.m.preview = plan.DeleteContainers(cs)
	h.wantView("… and 7 more (5 collections, 2 playlists)")
	if strings.Contains(h.view(), "Coll 29") {
		t.Error("ops past the cap should not be listed")
	}
}
