package tui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func (h *harness) click(x, y int) {
	h.t.Helper()
	h.send(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
}

func (h *harness) wheel(x, y int, b tea.MouseButton) {
	h.t.Helper()
	h.send(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
}

// sideY is the screen row of the sidebar node with the given name.
func (h *harness) sideY(name string) int {
	h.t.Helper()
	i := slices.IndexFunc(h.m.side.flat, func(n *node) bool { return n.name() == name })
	if i < 0 {
		h.t.Fatalf("%s not in the sidebar", name)
	}
	return bodyTop + 1 + i - h.m.side.offset
}

// listX is a column inside the list pane.
func (h *harness) listX() int { return h.m.layout().side + 3 }

func TestClickSidebarLeafSelectsAndOpensIt(t *testing.T) {
	h := newHarness(t)
	h.drive(h.m.sideInit())
	h.m.focus = focusList
	h.click(2, h.sideY("Movies"))
	if h.m.focus != focusSide || h.m.side.current().name() != "Movies" {
		t.Fatalf("focus %v current %s", h.m.focus, h.m.side.current().name())
	}
	if h.m.side.active == nil || h.m.side.active.name() != "Movies" {
		t.Fatalf("active = %v", h.m.side.active)
	}
	h.wantView("Libraries ▸ Movies", "Alien", "Brazil")
}

func TestClickSidebarGroupTogglesIt(t *testing.T) {
	h := newHarness(t)
	h.drive(h.m.sideInit())
	n := len(h.m.side.flat)
	y := h.sideY("Libraries")
	h.click(2, y)
	if h.m.side.current().name() != "Libraries" || len(h.m.side.flat) >= n {
		t.Fatalf("collapse: current %s, %d -> %d rows", h.m.side.current().name(), n, len(h.m.side.flat))
	}
	h.click(2, y)
	if len(h.m.side.flat) != n {
		t.Errorf("expand: %d rows, want %d", len(h.m.side.flat), n)
	}
}

func TestClickListRowSelectsIt(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.m.focus = focusSide
	h.click(h.listX(), bodyTop+2) // second row, under the column header
	if h.m.focus != focusList || h.m.top().cursor != 1 {
		t.Fatalf("focus %v cursor %d", h.m.focus, h.m.top().cursor)
	}
	if r, _ := h.m.top().current(); r.item == nil || r.item.Title != "Brazil" {
		t.Errorf("current = %+v", r)
	}
}

func TestClicksOutsideRowsChangeNothing(t *testing.T) {
	h := newHarness(t)
	h.open()
	for _, p := range [][2]int{
		{h.listX(), 0},                   // header
		{h.listX(), bodyTop},             // column header
		{h.listX(), bodyTop + 10},        // below the last row
		{h.m.layout().side, bodyTop + 1}, // the pane rule
		{h.m.w - 2, bodyTop + 1},         // details pane
	} {
		h.click(p[0], p[1])
		if h.m.focus != focusList || h.m.top().cursor != 0 {
			t.Errorf("click %v: focus %v cursor %d", p, h.m.focus, h.m.top().cursor)
		}
	}
}

func TestClicksIgnoredOutsideNormalMode(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("?")
	h.click(h.listX(), bodyTop+2)
	if h.m.top().cursor != 0 {
		t.Errorf("cursor moved under help: %d", h.m.top().cursor)
	}
}

func TestWheelScrollsThePaneUnderThePointer(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.m.h = 6 // one list row, three sidebar rows
	h.wheel(h.listX(), bodyTop+1, tea.MouseButtonWheelDown)
	if v := h.m.top(); v.cursor != 1 || v.offset != 1 {
		t.Errorf("list wheel down: cursor %d offset %d", v.cursor, v.offset)
	}
	h.wheel(h.listX(), bodyTop+1, tea.MouseButtonWheelUp)
	if v := h.m.top(); v.cursor != 0 || v.offset != 0 {
		t.Errorf("list wheel up: cursor %d offset %d", v.cursor, v.offset)
	}
	if h.m.focus != focusList {
		t.Errorf("wheel changed focus to %v", h.m.focus)
	}

	s := h.m.side
	s.cursor, s.offset = 0, 0
	h.wheel(2, bodyTop+1, tea.MouseButtonWheelDown)
	if s.offset != wheelStep || s.cursor != wheelStep {
		t.Errorf("sidebar wheel down: cursor %d offset %d", s.cursor, s.offset)
	}
	h.view() // render keeps the scrolled position
	if s.offset != wheelStep {
		t.Errorf("render moved offset to %d", s.offset)
	}
	h.wheel(2, bodyTop+1, tea.MouseButtonWheelUp)
	if s.offset != 0 || s.cursor != 0 {
		t.Errorf("sidebar wheel up: cursor %d offset %d", s.cursor, s.offset)
	}
}
