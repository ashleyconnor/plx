// Package tui is plx's Bubble Tea interface: an nnn-style browser where every
// playlist, collection, label, library and saved query is a list of items
// with the same filter, sort, select and act workflow.
package tui

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ashleyconnor/plx/internal/exec"
	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
	"github.com/ashleyconnor/plx/internal/query"
	"github.com/ashleyconnor/plx/internal/source"
)

type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeSort
	modeMenu
	modePicker
	modePrompt
	modePreview
	modeRunning
	modeHelp
	modeInfo
)

// focus is the pane that takes keys.
type focus int

const (
	focusSide focus = iota
	focusList
	focusDetail
)

type Model struct {
	cat *source.Catalog
	ctx context.Context

	side       *sidebar
	stack      []*view // the list pane: a source, plus the selection view on top
	focus      focus
	hideDetail bool
	w, h       int
	mode       mode

	input     textinput.Model // filter and prompt line
	inputPrev string

	sel      map[string]*model.Item
	selOrder []string

	status    string
	statusErr bool
	statusAt  time.Time

	menu    *menu
	info    *info            // the item in the details pane or overlay
	infos   map[string]*info // details already fetched, by item key
	comp    completion       // filter autocomplete
	compSel int              // highlighted suggestion; -1 until ↑/↓ is used

	helpScroll int
	picker     *picker
	prompt     *prompt
	preview    *plan.Plan
	confirmYes bool // destructive previews: focus is on Yes

	progress exec.Progress
	progCh   chan tea.Msg
	spinner  spinner.Model
}

type Options struct {
	Icons string // "emoji" (default), "ascii" or "none"
}

func Run(c *source.Catalog, opts Options) error {
	SetIcons(opts.Icons)
	m := newModel(c)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func newModel(c *source.Catalog) *Model {
	in := textinput.New()
	in.Prompt = ""
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	m := &Model{cat: c, ctx: context.Background(), input: in, sel: map[string]*model.Item{}, infos: map[string]*info{}, spinner: sp, w: 100, h: 30}
	m.side = m.newSidebar()
	m.stack = []*view{emptyView()}
	return m
}

func (m *Model) top() *view { return m.stack[len(m.stack)-1] }

func (m *Model) Init() tea.Cmd { return tea.Batch(m.spinner.Tick, m.sideInit()) }

// --- messages ----------------------------------------------------------------

type loadedMsg struct {
	v     *view
	src   source.Source
	rows  []row
	items []*model.Item
	err   error
}

type planMsg struct {
	plan *plan.Plan
	err  error
}

type progressMsg exec.Progress

type execDoneMsg struct {
	plan *plan.Plan
	err  error
}

type statusMsg struct {
	text string
	err  bool
}

func (m *Model) load(v *view) tea.Cmd {
	if v.placeholder {
		return nil
	}
	if v.selection {
		v.items, v.stale = m.selected(), false
		v.rebuild()
		return nil
	}
	v.loading, v.err, v.stale = true, nil, false
	return func() tea.Msg {
		if v.kind == dirView {
			rows, err := v.loader(m.ctx)
			return loadedMsg{v: v, rows: rows, err: err}
		}
		src := v.src
		if src == nil {
			var err error
			if src, err = v.resolve(m.ctx); err != nil {
				return loadedMsg{v: v, err: err}
			}
		}
		items, err := m.cat.Items(m.ctx, src)
		return loadedMsg{v: v, src: src, items: items, err: err}
	}
}

// Success messages fade after statusTTL; errors stay until the next key.
const statusTTL = 6 * time.Second

func (m *Model) setStatus(text string, isErr bool) {
	m.status, m.statusErr, m.statusAt = text, isErr, time.Now()
}

// --- update ------------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.syncInfo())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case loadedMsg:
		v := msg.v
		v.loading, v.err = false, msg.err
		if msg.err == nil {
			if v.kind == dirView {
				v.all = msg.rows
			} else {
				v.src, v.items, v.fetched = msg.src, msg.items, true
			}
			v.rebuild()
		}
		return m, m.ensureMembership(v, false)
	case membershipMsg:
		msg.v.indexing = false
		if msg.err != nil {
			m.setStatus("indexing collections and playlists: "+msg.err.Error(), true)
			msg.v.rebuild()
			return m, nil
		}
		m.cat.ApplyMembership(msg.items)
		if m.info != nil {
			m.cat.ApplyMembership([]*model.Item{m.info.item})
		}
		msg.v.rebuild()
		if m.mode == modeFilter && msg.v == m.top() {
			return m, m.updateCompletion()
		}
		return m, nil
	case planMsg:
		if msg.err != nil {
			m.mode = modeNormal
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		m.preview, m.mode = msg.plan, modePreview
		m.confirmYes = !msg.plan.Destructive() // file deletions default to No
		return m, nil
	case pickerMsg:
		m.picker, m.mode = msg.p, modePicker
		m.picker.input.Focus()
		return m, textinput.Blink
	case progressMsg:
		m.progress = exec.Progress(msg)
		return m, waitFor(m.progCh)
	case execDoneMsg:
		return m, m.finished(msg)
	case sideLoadedMsg:
		m.sideLoaded(msg)
		return m, nil
	case sideTickMsg:
		if m.focus == focusSide && m.side.current() == msg.n && m.side.active != msg.n {
			return m, m.openSource(msg.n)
		}
		return m, nil
	case detailTickMsg:
		if m.info != nil && m.info.item.Key == msg.key {
			return m, m.fetchInfo(m.info)
		}
		return m, nil
	case infoMsg:
		in := m.infos[msg.key]
		if in == nil {
			return m, nil
		}
		in.meta, in.err = msg.meta, msg.err
		if msg.meta != nil {
			return m, m.loadPoster(in, msg.meta)
		}
		return m, m.loadPoster(in, in.item.Raw) // listing has the thumb too
	case posterMsg:
		if in := m.infos[msg.key]; in != nil {
			in.poster, in.posterDone = msg.art, true
		}
		return m, nil
	case statusMsg:
		m.mode = modeNormal
		m.setStatus(msg.text, msg.err)
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	switch m.mode {
	case modeFilter:
		return m.filterKey(msg)
	case modeSort:
		return m.sortKey(msg)
	case modeMenu:
		return m.menuKey(msg)
	case modePicker:
		return m.pickerKey(msg)
	case modePrompt:
		return m.promptKey(msg)
	case modePreview:
		return m.previewKey(msg)
	case modeRunning:
		return nil
	case modeHelp:
		return m.helpKey(msg)
	case modeInfo:
		return m.infoKey(msg)
	}
	return m.normalKey(msg)
}

// normalKey handles keys that work in every pane, then passes the rest to
// the focused pane.
func (m *Model) normalKey(msg tea.KeyMsg) tea.Cmd {
	if m.statusErr {
		m.status = ""
	}
	switch msg.String() {
	case "q":
		return tea.Quit
	case "?":
		m.mode = modeHelp
		return nil
	case "tab":
		return m.cycleFocus(1)
	case "shift+tab":
		return m.cycleFocus(-1)
	case "p":
		m.hideDetail = !m.hideDetail
		if m.hideDetail && m.focus == focusDetail {
			m.focus = focusList
		}
		return nil
	case "r":
		return m.refresh()
	}
	switch m.focus {
	case focusSide:
		return m.sideKey(msg)
	case focusDetail:
		return m.infoKey(msg)
	}
	return m.listKey(msg)
}

// cycleFocus moves focus to the next pane on screen.
func (m *Model) cycleFocus(dir int) tea.Cmd {
	panes := []focus{focusSide, focusList}
	if m.layout().detail > 0 && m.info != nil {
		panes = append(panes, focusDetail)
	}
	i := (int(m.focus) + dir + len(panes)) % len(panes)
	if int(m.focus) >= len(panes) {
		i = int(focusList)
	}
	if panes[i] == focusDetail {
		return m.showInfo(m.info.item)
	}
	m.focus = panes[i]
	return nil
}

func (m *Model) listKey(msg tea.KeyMsg) tea.Cmd {
	v := m.top()
	page := max(1, m.listHeight()-1)
	switch msg.String() {
	case "j", "down":
		v.cursor++
	case "k", "up":
		v.cursor--
	case "g", "home":
		v.cursor = 0
	case "G", "end":
		v.cursor = len(v.rows) - 1
	case "ctrl+d", "pgdown":
		v.cursor += page
	case "ctrl+u", "pgup":
		v.cursor -= page
	case "l", "right", "enter":
		return m.open()
	case "h", "left", "backspace":
		return m.back()
	case "/":
		m.inputPrev = v.filterText
		m.input.SetValue(v.filterText)
		m.input.CursorEnd()
		m.input.Focus()
		m.mode = modeFilter
		return tea.Batch(textinput.Blink, m.updateCompletion())
	case "i":
		if r, ok := v.current(); ok && r.item != nil {
			return m.showInfo(r.item)
		}
	case "esc":
		if v.filterText != "" {
			v.setFilter("")
		} else if len(m.sel) > 0 {
			m.clearSelection()
		}
	case "s":
		if v.kind == itemView {
			m.mode = modeSort
		}
	case "f":
		if v.kind == itemView {
			v.lens = !v.lens
			v.folder = ""
			v.cursor = 0
			v.rebuild()
		}
	case " ":
		m.toggleCurrent()
		v.cursor++
	case "a":
		m.toggleVisible()
	case "v":
		return m.viewSelection()
	case "A":
		m.invertVisible()
	case "c":
		m.clearSelection()
	case ";", "x":
		return m.openMenu()
	case "S":
		return m.promptSaveQuery()
	}
	v.clamp()
	return nil
}

func (m *Model) open() tea.Cmd {
	v := m.top()
	r, ok := v.current()
	if !ok {
		return nil
	}
	switch {
	case r.push != nil:
		nv := r.push()
		m.stack = append(m.stack, nv)
		return m.load(nv)
	case r.isFolder():
		v.folder, v.cursor = r.folder, 0
		v.rebuild()
	case r.item != nil:
		return m.showInfo(r.item)
	}
	return nil
}

func (m *Model) back() tea.Cmd {
	v := m.top()
	if v.kind == itemView && v.lens && v.folder != v.folderRoot {
		prev := v.folder
		v.folder = path.Dir(v.folder)
		v.rebuild()
		for i, r := range v.rows { // land on the folder we came from
			if r.folder == prev {
				v.cursor = i
			}
		}
		return nil
	}
	if len(m.stack) == 1 {
		m.focus = focusSide
		return nil
	}
	m.stack = m.stack[:len(m.stack)-1]
	if p := m.top(); p.stale {
		return m.load(p)
	}
	return nil
}

func (m *Model) refresh() tea.Cmd { return m.invalidate() }

// invalidate drops everything cached after the server's data changed and
// reloads what's on screen.
func (m *Model) invalidate() tea.Cmd {
	m.cat.Invalidate()
	for _, sv := range m.stack {
		sv.stale = true
	}
	m.infos, m.info = map[string]*info{}, nil
	return tea.Batch(m.reloadSide(), m.load(m.top()))
}

// Filter keys: tab completes the highlighted suggestion, ↑/↓ choose one
// (then enter also accepts it), enter applies the filter, esc reverts.
func (m *Model) filterKey(msg tea.KeyMsg) tea.Cmd {
	v := m.top()
	n := len(m.comp.sugs)
	switch msg.String() {
	case "tab":
		if n > 0 {
			return m.acceptCompletion(max(m.compSel, 0))
		}
		return nil
	case "down", "ctrl+n":
		if n > 0 {
			m.compSel = (m.compSel + 1) % n
		}
		return nil
	case "up", "ctrl+p":
		if n > 0 {
			m.compSel = (m.compSel - 1 + n) % n
		}
		return nil
	case "enter":
		if m.compSel >= 0 && n > 0 {
			return m.acceptCompletion(m.compSel)
		}
		m.mode = modeNormal
		m.input.Blur()
		return nil
	case "esc":
		if m.compSel >= 0 {
			m.compSel = -1
			return nil
		}
		m.mode = modeNormal
		m.input.Blur()
		v.setFilter(m.inputPrev)
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	v.setFilter(m.input.Value())
	return tea.Batch(cmd, m.updateCompletion())
}

// updateCompletion recomputes suggestions. Completing collection or
// playlist values starts indexing membership if it isn't known yet.
func (m *Model) updateCompletion() tea.Cmd {
	v := m.top()
	if v.kind != itemView {
		m.comp = completion{}
		return nil
	}
	val := m.input.Value()
	m.comp = complete(val[:min(m.input.Position(), len(val))], v.items)
	m.compSel = -1
	f := m.comp.field
	return m.ensureMembership(v, f == field.Collection || f == field.Playlist)
}

func (m *Model) acceptCompletion(i int) tea.Cmd {
	val := m.input.Value()
	pos := min(m.input.Position(), len(val))
	head := val[:m.comp.start] + m.comp.sugs[i].insert
	m.input.SetValue(head + val[pos:])
	m.input.SetCursor(len(head))
	m.top().setFilter(m.input.Value())
	return m.updateCompletion()
}

type membershipMsg struct {
	v     *view
	items []*model.Item
	err   error
}

// ensureMembership indexes collection and playlist membership for a view's
// items when something needs it: the filter, sort or columns use those
// fields, or force (autocomplete, info) asks. It costs a request per
// collection and playlist, so it never runs just because a list loaded.
func (m *Model) ensureMembership(v *view, force bool) tea.Cmd {
	if v.kind != itemView || v.indexing || v.loading || !source.NeedsMembership(v.items) {
		return nil
	}
	need := force || (v.filter != nil && v.filter.UsesMembership())
	for _, k := range v.sortKeys {
		need = need || k.Field == field.Collection || k.Field == field.Playlist
	}
	for _, c := range v.columns {
		need = need || c == field.Collection || c == field.Playlist
	}
	if !need {
		return nil
	}
	v.indexing = true
	v.rebuild() // hide results that depend on membership until it's known
	items := v.items
	return func() tea.Msg {
		return membershipMsg{v: v, items: items, err: m.cat.BuildMembership(m.ctx, items)}
	}
}

// sortChoices are the one-key sorts; numeric fields default to descending.
var sortChoices = []struct {
	key   string
	field *field.Field
	desc  bool
}{
	{"t", field.Title, false},
	{"s", field.Size, true},
	{"d", field.Dur, true},
	{"a", field.Added, true},
	{"v", field.Viewed, true},
	{"p", field.Plays, true},
	{"y", field.Year, true},
	{"b", field.Bitrate, true},
	{"f", field.Path, false},
}

func (m *Model) sortKey(msg tea.KeyMsg) tea.Cmd {
	v := m.top()
	m.mode = modeNormal
	k := msg.String()
	switch k {
	case "r":
		if len(v.sortKeys) > 0 {
			v.sortKeys[0].Desc = !v.sortKeys[0].Desc
		}
	case "0":
		v.sortKeys = nil
	case "c":
		m.prompt = &prompt{label: "sort", value: query.FormatSort(v.sortKeys), submit: func(s string) tea.Cmd {
			keys, err := query.ParseSort(s)
			if err != nil {
				return statusCmd(err.Error(), true)
			}
			v.sortKeys = keys
			v.rebuild()
			return nil
		}}
		return m.startPrompt()
	default:
		for _, c := range sortChoices {
			if c.key == k {
				if len(v.sortKeys) > 0 && v.sortKeys[0].Field == c.field {
					v.sortKeys = []query.SortKey{{Field: c.field, Desc: !v.sortKeys[0].Desc}}
				} else {
					v.sortKeys = []query.SortKey{{Field: c.field, Desc: c.desc}}
				}
			}
		}
	}
	v.rebuild()
	return nil
}

// --- selection ---------------------------------------------------------------

func (m *Model) isSelected(it *model.Item) bool { return m.sel[it.Key] != nil }

func (m *Model) setSelected(it *model.Item, on bool) {
	if on && m.sel[it.Key] == nil {
		m.sel[it.Key] = it
		m.selOrder = append(m.selOrder, it.Key)
	} else if !on && m.sel[it.Key] != nil {
		delete(m.sel, it.Key)
		for i, k := range m.selOrder {
			if k == it.Key {
				m.selOrder = append(m.selOrder[:i], m.selOrder[i+1:]...)
				break
			}
		}
	}
}

func (m *Model) toggleCurrent() {
	r, ok := m.top().current()
	if !ok {
		return
	}
	switch {
	case r.item != nil:
		m.setSelected(r.item, !m.isSelected(r.item))
	case r.isFolder():
		all := true
		for _, it := range r.under {
			all = all && m.isSelected(it)
		}
		for _, it := range r.under {
			m.setSelected(it, !all)
		}
	}
}

func (m *Model) visibleItems() []*model.Item {
	var out []*model.Item
	for _, r := range m.top().rows {
		if r.item != nil {
			out = append(out, r.item)
		}
		out = append(out, r.under...)
	}
	return out
}

// toggleVisible selects every visible item, or unselects them if they are
// all selected already.
func (m *Model) toggleVisible() {
	items := m.visibleItems()
	all := len(items) > 0
	for _, it := range items {
		all = all && m.isSelected(it)
	}
	for _, it := range items {
		m.setSelected(it, !all)
	}
}

// viewSelection opens the selection as an item list. It can be filtered,
// sorted, trimmed with space and acted on like any other list.
func (m *Model) viewSelection() tea.Cmd {
	if len(m.sel) == 0 {
		m.setStatus("nothing selected", true)
		return nil
	}
	if m.top().selection {
		return nil
	}
	v := &view{kind: itemView, title: icons.selection + " Selection", selection: true, columns: defaultColumns()}
	m.stack = append(m.stack, v)
	m.focus = focusList
	return m.load(v)
}

func (m *Model) invertVisible() {
	for _, it := range m.visibleItems() {
		m.setSelected(it, !m.isSelected(it))
	}
}

func (m *Model) clearSelection() {
	m.sel = map[string]*model.Item{}
	m.selOrder = nil
}

func (m *Model) selected() []*model.Item {
	out := make([]*model.Item, 0, len(m.selOrder))
	for _, k := range m.selOrder {
		out = append(out, m.sel[k])
	}
	return out
}

// targets are what an action applies to: the selection, or else the row
// under the cursor (a folder row means everything below it).
func (m *Model) targets() []*model.Item {
	if len(m.sel) > 0 {
		return m.selected()
	}
	r, ok := m.top().current()
	if !ok {
		return nil
	}
	if r.item != nil {
		return []*model.Item{r.item}
	}
	return r.under
}

func statusCmd(text string, isErr bool) tea.Cmd {
	return func() tea.Msg { return statusMsg{text, isErr} }
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
