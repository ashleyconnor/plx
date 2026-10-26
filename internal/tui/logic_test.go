package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/query"
)

func filed(key, dir string, size int64) *model.Item {
	return &model.Item{Key: key, Kind: "movie", Title: "t" + key, Files: []string{dir + "/" + key + ".mkv"}, Size: size}
}

func itemRows(v *view) string {
	var out []string
	for _, r := range v.rows {
		switch {
		case r.item != nil:
			out = append(out, r.item.Key)
		default:
			out = append(out, r.label)
		}
	}
	return strings.Join(out, ",")
}

func newItemView(items ...*model.Item) *view {
	v := &view{kind: itemView, items: items, columns: defaultColumns()}
	v.rebuild()
	return v
}

func TestSetFilterFiltersAndKeepsLastGoodQueryOnError(t *testing.T) {
	a, b := &model.Item{Key: "a", Title: "Alien", Size: 5 << 30}, &model.Item{Key: "b", Title: "Brazil", Size: 1 << 30}
	v := newItemView(a, b)
	v.setFilter("size>2G")
	if itemRows(v) != "a" || v.filterErr != "" {
		t.Fatalf("rows %s err %q", itemRows(v), v.filterErr)
	}
	v.setFilter("nope>1")
	if v.filterErr == "" || itemRows(v) != "a" {
		t.Errorf("a bad filter must report the error and keep showing the last good result: err %q rows %s", v.filterErr, itemRows(v))
	}
	v.setFilter("  ")
	if v.filter != nil || itemRows(v) != "a,b" || v.filterErr != "" {
		t.Errorf("blank filter should show everything: rows %s", itemRows(v))
	}
}

func TestVisibleSortsAndTypedSortWinsOverViewSort(t *testing.T) {
	a, b, c := &model.Item{Key: "a", Title: "a", Size: 2}, &model.Item{Key: "b", Title: "b", Size: 3}, &model.Item{Key: "c", Title: "c", Size: 1}
	v := newItemView(a, b, c)
	v.sortKeys = []query.SortKey{{Field: field.Size, Desc: true}}
	v.rebuild()
	if itemRows(v) != "b,a,c" {
		t.Errorf("view sort: %s", itemRows(v))
	}
	v.setFilter("sort:size")
	if itemRows(v) != "c,a,b" {
		t.Errorf("typed sort should win: %s", itemRows(v))
	}
	if v.items[0] != a {
		t.Error("sorting must not reorder the source items")
	}
}

func TestDirViewFilterIsCaseInsensitiveSubstring(t *testing.T) {
	v := &view{kind: dirView, all: []row{{label: "Movies/"}, {label: "TV Shows/"}, {label: "Music/"}}}
	v.setFilter("m")
	if itemRows(v) != "Movies/,Music/" {
		t.Errorf("rows = %s", itemRows(v))
	}
	v.setFilter("SHOW")
	if itemRows(v) != "TV Shows/" {
		t.Errorf("rows = %s", itemRows(v))
	}
	v.setFilter("")
	if len(v.rows) != 3 {
		t.Errorf("rows = %d", len(v.rows))
	}
}

func TestIndexingHidesMembershipResults(t *testing.T) {
	v := newItemView(&model.Item{Key: "a"})
	v.indexing = true
	v.setFilter("-has:playlist")
	if len(v.rows) != 0 {
		t.Error("results shown before membership was indexed would be wrong")
	}
	// other filters are not blanked while indexing
	v.setFilter("title:a")
	if v.filterErr != "" {
		t.Fatal(v.filterErr)
	}
	v.indexing = true
	v.rebuild()
	if len(v.rows) != 0 { // no item is titled "a" here; rows are empty because nothing matches
		t.Errorf("rows = %s", itemRows(v))
	}
	v.setFilter("")
	v.rebuild()
	if len(v.rows) != 1 {
		t.Errorf("unfiltered while indexing: %d rows", len(v.rows))
	}
}

func TestCursorClamps(t *testing.T) {
	v := newItemView(&model.Item{Key: "a"}, &model.Item{Key: "b"})
	v.cursor = 5
	v.clamp()
	if v.cursor != 1 {
		t.Errorf("cursor = %d", v.cursor)
	}
	r, ok := v.current()
	if !ok || r.item.Key != "b" {
		t.Errorf("current = %+v %v", r, ok)
	}
	empty := newItemView()
	empty.cursor = 3
	empty.clamp()
	if _, ok := empty.current(); ok || empty.cursor != 0 {
		t.Errorf("empty view cursor %d", empty.cursor)
	}
}

func TestCommonDirAndUnder(t *testing.T) {
	cases := []struct {
		items []*model.Item
		want  string
	}{
		{nil, "/"},
		{[]*model.Item{{Key: "x"}}, "/"}, // no files
		{[]*model.Item{filed("1", "/tv/a/s1", 0)}, "/tv/a/s1"},
		{[]*model.Item{filed("1", "/tv/a/s1", 0), filed("2", "/tv/a/s2", 0)}, "/tv/a"},
		{[]*model.Item{filed("1", "/tv/a", 0), filed("2", "/movies", 0)}, "/"},
		{[]*model.Item{filed("1", "/tv/a", 0), {Key: "nofile"}}, "/tv/a"},
	}
	for _, tc := range cases {
		if got := commonDir(tc.items); got != tc.want {
			t.Errorf("commonDir = %q, want %q", got, tc.want)
		}
	}
	for _, tc := range []struct {
		p, dir string
		want   bool
	}{
		{"/tv/a", "/tv", true}, {"/tv", "/tv", true}, {"/tvx", "/tv", false}, {"/anything", "/", true}, {"/tv", "/tv/a", false},
	} {
		if under(tc.p, tc.dir) != tc.want {
			t.Errorf("under(%q, %q) != %v", tc.p, tc.dir, tc.want)
		}
	}
}

func TestFolderLensGroupsByImmediateSubfolder(t *testing.T) {
	items := []*model.Item{
		filed("1", "/tv/Show B/S1", 10),
		filed("2", "/tv/Show B/S2", 20),
		filed("3", "/tv/show a", 100),
		filed("4", "/tv", 1),
	}
	v := newItemView(items...)
	v.lens = true
	v.rebuild()
	if v.folderRoot != "/tv" || v.folder != "/tv" {
		t.Fatalf("root %q folder %q", v.folderRoot, v.folder)
	}
	if itemRows(v) != "show a/,Show B/,4" { // folders first (case-insensitive), then direct files
		t.Errorf("rows = %s", itemRows(v))
	}
	b := v.rows[1]
	if b.folder != "/tv/Show B" || len(b.under) != 2 || b.size != 30 || !b.isFolder() {
		t.Errorf("Show B row = %+v", b)
	}
	// descending by size reorders folders: Show B (30) before show a (100)? no: 100 > 30
	v.sortKeys = []query.SortKey{{Field: field.Size, Desc: true}}
	v.rebuild()
	if itemRows(v) != "show a/,Show B/,4" {
		t.Errorf("size desc rows = %s", itemRows(v))
	}
	v.sortKeys = []query.SortKey{{Field: field.Size}}
	v.rebuild()
	if itemRows(v) != "Show B/,show a/,4" {
		t.Errorf("size asc rows = %s", itemRows(v))
	}
	// descending into a folder, then filtering it away resets to the new root
	v.folder = "/tv/Show B"
	v.rebuild()
	if itemRows(v) != "S1/,S2/" {
		t.Errorf("inside Show B: %s", itemRows(v))
	}
	v.setFilter("title:t3")
	if v.folder != v.folderRoot || itemRows(v) != "3" {
		t.Errorf("after filter: folder %q root %q rows %s", v.folder, v.folderRoot, itemRows(v))
	}
}

func TestViewContainer(t *testing.T) {
	if (&view{}).container() != nil {
		t.Error("view without a source has no container")
	}
}

func TestContainerDetail(t *testing.T) {
	cases := []struct {
		c    model.Container
		want string
	}{
		{model.Container{Count: 3}, "3 items"},
		{model.Container{Count: 0, ListType: "video"}, "0 items · video"},
		{model.Container{Count: 12, ListType: "audio", Smart: true}, "12 items · audio · smart"},
	}
	for _, tc := range cases {
		if got := containerDetail(&tc.c); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestPlural(t *testing.T) {
	if plural(1, "item") != "1 item" || plural(0, "item") != "0 items" || plural(2, "item") != "2 items" {
		t.Error("plural")
	}
}

func TestFitAndFitLeftUseDisplayWidth(t *testing.T) {
	if got := fit("abc", 5); got != "abc  " {
		t.Errorf("fit pad = %q", got)
	}
	if got := fit("abcdef", 4); got != "abc…" {
		t.Errorf("fit truncate = %q", got)
	}
	if fit("x", 0) != "" || fit("x", -1) != "" {
		t.Error("non-positive width")
	}
	if got := fit("日本語", 5); ansi.StringWidth(got) != 5 {
		t.Errorf("wide runes: %q is %d cells", got, ansi.StringWidth(got))
	}
	if got := fitLeft("ab", 4); got != "  ab" {
		t.Errorf("fitLeft pad = %q", got)
	}
	if got := fitLeft("abcdef", 4); got != "abc…" {
		t.Errorf("fitLeft truncate = %q", got)
	}
}

func TestStripANSIAndPadANSI(t *testing.T) {
	styled := stBold.Render("hi") + " " + stErr.Render("there")
	if stripANSI("\x1b[1;31mred\x1b[0m plain") != "red plain" {
		t.Error("stripANSI")
	}
	if got := stripANSI(styled); got != "hi there" {
		t.Errorf("stripANSI(styled) = %q", got)
	}
	if got := padANSI("\x1b[1mab\x1b[0m", 5); ansi.StringWidth(got) != 5 {
		t.Errorf("pad width = %d", ansi.StringWidth(got))
	}
	if got := padANSI("abcdef", 3); ansi.StringWidth(got) != 3 {
		t.Errorf("truncate width = %d", ansi.StringWidth(got))
	}
}

func TestHumanSizeOfItems(t *testing.T) {
	if humanSize(nil) != "0B" {
		t.Error("empty is 0B, not blank")
	}
	if got := humanSize([]*model.Item{{Size: 1 << 30}, {Size: 1 << 30}}); got != "2.0G" {
		t.Errorf("got %q", got)
	}
}

func TestColumnLabelFallsBackToShortestAlias(t *testing.T) {
	if got := columnLabel(field.Res); got != "res" {
		t.Errorf("res = %q", got)
	}
	long := &field.Field{Name: "verylongname", Aliases: []string{"vln", "other"}, Width: 4}
	if got := columnLabel(long); got != "vln" {
		t.Errorf("long = %q", got)
	}
	short := &field.Field{Name: "ab", Aliases: []string{"a"}, Width: 4}
	if got := columnLabel(short); got != "ab" {
		t.Errorf("fits already: %q", got)
	}
}

func TestMeasureColumnsWidensToFitCappedAtMax(t *testing.T) {
	versions := &model.Item{Key: "v", Versions: []model.Version{{Duration: 2 * 60e9}, {Duration: 150 * 60 * 1e9}}}
	versions.Duration = 150 * 60 * 1e9
	v := newItemView(versions)
	if w := v.colWidth(field.Dur); w != len("2m|2h30m") {
		t.Errorf("duration width = %d", w)
	}
	long := &model.Item{Key: "l", Labels: []string{strings.Repeat("x", 40)}}
	v = newItemView(long)
	if w := v.colWidth(field.Label); w != maxColWidth {
		t.Errorf("label width = %d, want cap %d", w, maxColWidth)
	}
	v = newItemView()
	if v.colWidth(field.Size) != field.Size.Width || v.colWidth(field.Codec) != 10 {
		t.Error("defaults: registered width, else 10")
	}
}

func TestIconSets(t *testing.T) {
	defer SetIcons("emoji")
	SetIcons("ascii")
	if icons.kind("movie") != "m" || icons.kind("weird") != "-" || cellWidth() != 3 {
		t.Error("ascii set")
	}
	if got := cell("m"); got != "m  " {
		t.Errorf("cell = %q", got)
	}
	if !strings.Contains(legend(), "m movie") {
		t.Errorf("legend = %q", legend())
	}
	SetIcons("none")
	if cell("x") != "" || cellWidth() != 0 || legend() != "" {
		t.Error("none set should render nothing")
	}
	SetIcons("")
	if icons.other != emojiIcons.other {
		t.Error("unknown names fall back to emoji")
	}
	// every emoji cell is padded to two columns plus a space
	if got := ansi.StringWidth(cell(icons.kind("movie"))); got != 3 {
		t.Errorf("emoji cell width = %d", got)
	}
}

func testModel() *Model {
	m := newModel(nil)
	return m
}

func TestSelectionKeepsOrderAndDeduplicates(t *testing.T) {
	m := testModel()
	a, b, c := &model.Item{Key: "a"}, &model.Item{Key: "b"}, &model.Item{Key: "c"}
	for _, it := range []*model.Item{b, a, b, c} {
		m.setSelected(it, true)
	}
	keys := func() string {
		var ks []string
		for _, it := range m.selected() {
			ks = append(ks, it.Key)
		}
		return strings.Join(ks, "")
	}
	if keys() != "bac" {
		t.Errorf("order = %s", keys())
	}
	m.setSelected(a, false)
	m.setSelected(a, false)
	if keys() != "bc" || m.isSelected(a) {
		t.Errorf("after unselect = %s", keys())
	}
	m.clearSelection()
	if len(m.selected()) != 0 || m.isSelected(b) {
		t.Error("clear")
	}
}

func TestToggleVisibleInvertAndTargets(t *testing.T) {
	m := testModel()
	a, b, c := &model.Item{Key: "a", Title: "a"}, &model.Item{Key: "b", Title: "b"}, &model.Item{Key: "c", Title: "c"}
	v := newItemView(a, b, c)
	m.stack = []*view{v}

	if got := m.targets(); len(got) != 1 || got[0] != a {
		t.Errorf("no selection targets the row under the cursor, got %v", got)
	}
	m.toggleVisible()
	if len(m.selected()) != 3 {
		t.Fatalf("selected %d", len(m.selected()))
	}
	m.toggleVisible() // all selected -> unselect all
	if len(m.selected()) != 0 {
		t.Fatalf("toggle again selected %d", len(m.selected()))
	}
	m.setSelected(a, true)
	m.invertVisible()
	if m.isSelected(a) || !m.isSelected(b) || !m.isSelected(c) {
		t.Error("invert")
	}
	if got := m.targets(); len(got) != 2 {
		t.Errorf("selection wins over cursor: %v", got)
	}
	m.clearSelection()
	v.cursor = 1
	m.toggleCurrent()
	if !m.isSelected(b) || len(m.selected()) != 1 {
		t.Error("toggleCurrent")
	}
	m.toggleCurrent()
	if m.isSelected(b) {
		t.Error("toggleCurrent twice")
	}
}

func TestToggleFolderRowSelectsEverythingBelow(t *testing.T) {
	m := testModel()
	v := newItemView(filed("1", "/tv/a", 1), filed("2", "/tv/a", 1), filed("3", "/tv/b", 1))
	v.lens = true
	v.rebuild()
	m.stack = []*view{v}
	if !v.rows[0].isFolder() {
		t.Fatalf("rows = %s", itemRows(v))
	}
	m.toggleCurrent()
	if len(m.selected()) != 2 {
		t.Fatalf("selected %d", len(m.selected()))
	}
	// a partly selected folder becomes fully selected, a full one is cleared
	m.setSelected(v.rows[0].under[0], false)
	m.toggleCurrent()
	if len(m.selected()) != 2 {
		t.Errorf("partial -> all: %d", len(m.selected()))
	}
	m.toggleCurrent()
	if len(m.selected()) != 0 {
		t.Errorf("all -> none: %d", len(m.selected()))
	}
	if got := m.targets(); len(got) != 2 {
		t.Errorf("a folder row targets everything below it: %d", len(got))
	}
	if len(m.visibleItems()) != 3 {
		t.Errorf("visibleItems = %d", len(m.visibleItems()))
	}
}

func TestTargetsEmptyView(t *testing.T) {
	m := testModel()
	m.stack = []*view{newItemView()}
	if got := m.targets(); got != nil {
		t.Errorf("targets = %v", got)
	}
}

func TestLayoutDropsDetailThenSidebar(t *testing.T) {
	m := testModel()
	m.w = 200
	l := m.layout()
	if l.side == 0 || l.list == 0 || l.detail == 0 || l.side+l.list+l.detail+2 != 200 {
		t.Errorf("wide layout = %+v", l)
	}
	m.hideDetail = true
	if l := m.layout(); l.detail != 0 || l.side+l.list+1 != 200 {
		t.Errorf("detail hidden = %+v", l)
	}
	m.hideDetail = false
	m.w = 100 // too narrow for three panes: details go first
	if l := m.layout(); l.detail != 0 || l.list < minListW || l.side == 0 {
		t.Errorf("medium layout = %+v", l)
	}
	m.w = 60 // only the focused pane
	m.focus = focusList
	if l := m.layout(); l != (layout{list: 60}) {
		t.Errorf("narrow list = %+v", l)
	}
	m.focus = focusSide
	if l := m.layout(); l != (layout{side: 60}) {
		t.Errorf("narrow side = %+v", l)
	}
}

func TestBodyHeightReservesSummaryWhenNoDetailPane(t *testing.T) {
	m := testModel()
	m.w, m.h = 200, 40
	if m.summaryShown() || m.bodyHeight() != 36 || m.listHeight() != 35 {
		t.Errorf("with details: summary %v body %d", m.summaryShown(), m.bodyHeight())
	}
	m.hideDetail = true
	if !m.summaryShown() || m.bodyHeight() != 34 {
		t.Errorf("no details: summary %v body %d", m.summaryShown(), m.bodyHeight())
	}
	m.h = 3
	if m.bodyHeight() != 2 {
		t.Errorf("tiny terminal body = %d, want floor of 2", m.bodyHeight())
	}
	if m.listW() <= 0 {
		t.Error("listW")
	}
}

func TestDateOr(t *testing.T) {
	if dateOr(testTime(0), "never") != "never" {
		t.Error("zero time")
	}
	if got := dateOr(testTime(1700000000).UTC(), "never"); got != "2023-11-14" {
		t.Errorf("got %q", got)
	}
}

func TestHalfBlocksFitsBoxAndRejectsGarbage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := 0; x < 40; x++ {
		for y := 0; y < 20; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 12), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, err := halfBlocks(buf.Bytes(), 10, 4)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 4 {
		t.Errorf("%d rows, box is 4", len(lines))
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w != 10 {
			t.Errorf("row width %d, want 10 (padded to the box)", w)
		}
		if !strings.Contains(l, "▀") {
			t.Errorf("row has no half blocks: %q", l)
		}
	}
	if _, err := halfBlocks([]byte("not an image"), 10, 4); err == nil {
		t.Error("garbage accepted")
	}
}

func TestHalfBlocksTallImageIsLimitedByHeight(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 100))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	out, err := halfBlocks(buf.Bytes(), 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Errorf("rows = %d, want 5", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) != 20 {
			t.Errorf("width %d", ansi.StringWidth(l))
		}
	}
}

func testTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
