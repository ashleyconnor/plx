package tui

import "testing"

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
