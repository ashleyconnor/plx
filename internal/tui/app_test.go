package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSidebarListsSourcesAndOpensALibrary(t *testing.T) {
	h := newHarness(t)
	h.drive(h.m.sideInit())
	h.wantView("SOURCES", "Libraries", "Playlists", "Collections", "Labels", "Queries", "Movies", "Alpha")
	if h.m.focus != focusSide {
		t.Fatal("sidebar should start focused")
	}
	// move to Movies and open it
	for h.m.side.current().name() != "Movies" {
		h.press("j")
		if h.m.side.cursor > 20 {
			t.Fatal("Movies not found")
		}
	}
	h.press("enter")
	if h.m.focus != focusList || h.m.side.active == nil || h.m.side.active.name() != "Movies" {
		t.Fatalf("focus %v active %v", h.m.focus, h.m.side.active)
	}
	h.wantView("Libraries ▸ Movies", "Alien", "Brazil", "2/2")
}

func TestSidebarCollapseExpandAndParentJump(t *testing.T) {
	h := newHarness(t)
	h.drive(h.m.sideInit())
	n := len(h.m.side.flat)
	h.press("enter") // Libraries is expanded: enter collapses it
	if len(h.m.side.flat) >= n {
		t.Errorf("collapse: %d -> %d rows", n, len(h.m.side.flat))
	}
	h.press("enter")
	if len(h.m.side.flat) != n {
		t.Errorf("expand: %d rows, want %d", len(h.m.side.flat), n)
	}
	h.press("j", "h") // from a child, h jumps to the parent
	if h.m.side.current().name() != "Libraries" {
		t.Errorf("current = %s", h.m.side.current().name())
	}
	h.press("G")
	last := h.m.side.cursor
	h.press("g")
	if h.m.side.cursor != 0 || last == 0 {
		t.Errorf("g/G cursor %d / %d", h.m.side.cursor, last)
	}
}

func TestEmptyGroupShowsPlaceholder(t *testing.T) {
	h := newHarness(t)
	h.drive(h.m.sideInit())
	h.wantView("(none yet") // no saved queries
}

func TestTabCyclesFocusAndPTogglesDetails(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.m.focus = focusSide
	h.press("tab")
	if h.m.focus != focusList {
		t.Errorf("focus = %v", h.m.focus)
	}
	h.press("shift+tab")
	h.press("p")
	if !h.m.hideDetail {
		t.Error("p should hide details")
	}
	h.press("p")
	if h.m.hideDetail {
		t.Error("p should show details again")
	}
}

func TestFilterEntryAppliesAndEscRevertsOrClears(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("/")
	h.wantMode(modeFilter)
	h.press("brazil")
	h.wantView("/brazil", "1/2")
	h.press("enter")
	h.wantMode(modeNormal)
	h.wantView("filter: brazil")
	if len(h.m.top().rows) != 1 {
		t.Fatalf("rows = %d", len(h.m.top().rows))
	}
	// esc in normal mode clears the filter
	h.press("esc")
	if len(h.m.top().rows) != 2 || h.m.top().filterText != "" {
		t.Error("esc did not clear the filter")
	}
	// esc inside the prompt restores the previous filter
	h.press("/", "alien", "enter", "/", "zzz", "esc")
	if h.m.top().filterText != "alien" || len(h.m.top().rows) != 1 {
		t.Errorf("after cancelled edit: %q, %d rows", h.m.top().filterText, len(h.m.top().rows))
	}
}

func TestFilterErrorIsShown(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("/", "nofield>1")
	h.wantView("unknown field")
	h.press("enter")
	h.wantView("unknown field")
}

func TestFilterCompletionDropdown(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("/", "siz")
	h.wantView("size:")
	h.press("tab")
	if !strings.HasPrefix(h.m.input.Value(), "size") {
		t.Errorf("input = %q", h.m.input.Value())
	}
	h.press("esc")
	h.press("esc")
	h.wantMode(modeNormal)
}

func TestCompletingPlaylistValuesIndexesMembership(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("/", "playlist:")
	if !h.m.top().items[0].MembershipKnown {
		t.Error("membership should be indexed on demand")
	}
	h.wantView("Alpha")
}

func TestMembershipFilterShowsResultsOnceIndexed(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("/", "playlist:alpha", "enter")
	if len(h.m.top().rows) != 1 || h.m.top().rows[0].item.Title != "Brazil" {
		t.Errorf("rows = %s", itemRows(h.m.top()))
	}
}

func TestSortKeys(t *testing.T) {
	h := newHarness(t)
	h.open()
	order := func() string { return itemRows(h.m.top()) }
	h.press("s", "s") // size, descending
	h.wantMode(modeNormal)
	if order() != "11,10" {
		t.Errorf("size desc: %s", order())
	}
	h.wantView("sort: size-")
	h.press("s", "s") // same key flips
	if order() != "10,11" {
		t.Errorf("size asc: %s", order())
	}
	h.press("s", "r")
	if order() != "11,10" {
		t.Errorf("reverse: %s", order())
	}
	h.press("s", "0")
	if len(h.m.top().sortKeys) != 0 {
		t.Error("0 clears the sort")
	}
	h.press("s", "t")
	if order() != "10,11" {
		t.Errorf("title: %s", order())
	}
}

func TestCustomSortPrompt(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("s", "c")
	h.wantMode(modePrompt)
	if h.m.input.Value() != "title" {
		t.Errorf("prompt should start with the current sort, got %q", h.m.input.Value())
	}
	h.m.input.SetValue("")
	h.press("year-", "enter")
	if itemRows(h.m.top()) != "11,10" {
		t.Errorf("rows = %s", itemRows(h.m.top()))
	}
	h.press("s", "c")
	h.m.input.SetValue("bogus")
	h.press("enter")
	if !h.m.statusErr {
		t.Errorf("bad sort should report an error, status %q", h.m.status)
	}
}

func TestSelectionPersistsAcrossSourcesAndIsShownInHeader(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press(" ")
	if len(h.m.sel) != 1 || !h.m.isSelected(h.m.top().items[0]) {
		t.Fatal("space did not select")
	}
	if h.m.top().cursor != 1 {
		t.Errorf("space advances the cursor, at %d", h.m.top().cursor)
	}
	h.wantView("● 1")
	// open a different source: the selection survives
	h.m.stack = []*view{h.m.itemsView(sourceMembers(h, "50", false), nil)}
	h.drive(h.m.load(h.m.top()))
	if len(h.m.sel) != 1 {
		t.Fatalf("selection lost: %d", len(h.m.sel))
	}
	// and actions apply to it even though it isn't in this list
	h.press(";")
	h.wantView("1 item", "Add to playlist")
	h.press("esc")
	// view the selection
	h.press("v")
	if !h.m.top().selection || itemRows(h.m.top()) != "10" {
		t.Errorf("selection view rows = %s", itemRows(h.m.top()))
	}
	h.wantView("Selection")
	h.press("h") // back to the playlist
	h.press("c")
	if len(h.m.sel) != 0 {
		t.Error("c clears the selection")
	}
}

func TestSelectionKeysInList(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("a")
	if len(h.m.sel) != 2 {
		t.Fatalf("a selected %d", len(h.m.sel))
	}
	h.press("A")
	if len(h.m.sel) != 0 {
		t.Errorf("A inverts: %d", len(h.m.sel))
	}
	h.press("a", "esc")
	if len(h.m.sel) != 0 {
		t.Error("esc clears selection when no filter is set")
	}
	h.press("v")
	if h.m.status != "nothing selected" {
		t.Errorf("status = %q", h.m.status)
	}
}

func TestCursorMovementClampsInList(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("j", "j", "j")
	if h.m.top().cursor != 1 {
		t.Errorf("cursor = %d", h.m.top().cursor)
	}
	h.press("g")
	h.press("k")
	if h.m.top().cursor != 0 {
		t.Errorf("cursor = %d", h.m.top().cursor)
	}
	h.press("G")
	if h.m.top().cursor != 1 {
		t.Errorf("cursor = %d", h.m.top().cursor)
	}
}

func TestFolderLensToggle(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("f")
	if !h.m.top().lens {
		t.Fatal("f toggles the folder lens")
	}
	h.wantView("[/m]")
	h.press("f")
	if h.m.top().lens {
		t.Error("lens should toggle off")
	}
}

func TestHelpOpensScrollsAndCloses(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.press("?")
	h.wantMode(modeHelp)
	h.wantView("selection persists", "Fields")
	h.press("j", "j")
	if h.m.helpScroll == 0 {
		t.Error("j should scroll")
	}
	h.press("esc")
	h.wantMode(modeNormal)
	if h.m.helpScroll != 0 {
		t.Error("closing resets scroll")
	}
}

func TestQuitAndCtrlC(t *testing.T) {
	h := newHarness(t)
	h.open()
	if _, ok := h.m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})().(tea.QuitMsg); !ok {
		t.Error("q should quit")
	}
	_, cmd := h.m.update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c should quit")
	}
	// but q is just a character while typing a filter
	h.press("/")
	h.m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if h.m.input.Value() != "q" {
		t.Errorf("filter input = %q", h.m.input.Value())
	}
}

func TestWindowSizeAndNarrowViewDoNotPanic(t *testing.T) {
	h := newHarness(t)
	h.open()
	for _, size := range [][2]int{{200, 50}, {100, 30}, {60, 20}, {20, 5}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if h.view() == "" {
			t.Errorf("empty view at %v", size)
		}
	}
	h.send(tea.WindowSizeMsg{Width: 60, Height: 20})
	h.wantView("Alien") // narrow: only the list is shown
}

func TestInfoOverlayShowsFetchedDetails(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.m.hideDetail = true
	h.press("i")
	h.wantMode(modeInfo)
	h.wantView("Alien", "In space no one can hear you scream.")
	if h.sv.count("GET /library/metadata/10") != 1 {
		t.Errorf("metadata fetched %d times", h.sv.count("GET /library/metadata/10"))
	}
	h.press("esc")
	h.wantMode(modeNormal)
	// reopening uses the cached details
	h.press("i")
	h.press("esc")
	if h.sv.count("GET /library/metadata/10") != 1 {
		t.Error("details were refetched")
	}
}

func TestDetailsPaneFollowsCursor(t *testing.T) {
	h := newHarness(t)
	h.open()
	h.send(tea.WindowSizeMsg{Width: 160, Height: 40})
	h.wantView("Alien")
	if h.m.info == nil || h.m.info.item.Title != "Alien" {
		t.Fatalf("info = %+v", h.m.info)
	}
	h.press("j")
	if h.m.info.item.Title != "Brazil" {
		t.Errorf("details follow the cursor, got %s", h.m.info.item.Title)
	}
}

func TestRefreshReloadsFromServer(t *testing.T) {
	h := newHarness(t)
	h.open()
	before := h.sv.count("GET /library/sections/1/all")
	h.press("r")
	if h.sv.count("GET /library/sections/1/all") <= before {
		t.Error("r did not refetch")
	}
	if len(h.m.top().rows) != 2 {
		t.Errorf("rows after refresh = %d", len(h.m.top().rows))
	}
}

func TestLoadErrorIsShown(t *testing.T) {
	h := newHarness(t)
	h.open()
	v := h.m.top()
	h.send(loadedMsg{v: v, err: errTest("boom")})
	if v.err == nil {
		t.Fatal("error not recorded")
	}
	h.wantView("boom")
}

type errTest string

func (e errTest) Error() string { return string(e) }
