package tui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ashleyconnor/plx/internal/plan"
)

// The sidebar is the browse tree (Libraries, Playlists, Collections, Labels,
// Queries) as an expandable outline. Groups load their children on first
// expand; leaves are sources, shown in the list pane when the cursor rests
// on them.

type node struct {
	row      row
	view     *view // what row.push returns: a dirView (group) or an itemView (source)
	parent   *node
	depth    int
	expanded bool
	loading  bool
	loaded   bool
	err      error
	children []*node
}

func (n *node) group() bool  { return n.view != nil && n.view.kind == dirView }
func (n *node) leaf() bool   { return n.view != nil && n.view.kind == itemView }
func (n *node) name() string { return strings.TrimSuffix(n.row.label, "/") }

type sidebar struct {
	roots          []*node
	flat           []*node // visible nodes, in order
	cursor, offset int
	active         *node // the source shown in the list pane
}

type sideLoadedMsg struct {
	n    *node
	rows []row
	err  error
}

type sideTickMsg struct{ n *node }

// sidePreviewDelay is how long the cursor rests on a source before the list
// pane loads it, so scrolling past sources doesn't fetch each one.
const sidePreviewDelay = 200 * time.Millisecond

func newNode(r row, parent *node) *node {
	n := &node{row: r, parent: parent}
	if parent != nil {
		n.depth = parent.depth + 1
	}
	if r.push != nil {
		n.view = r.push()
	}
	return n
}

func (m *Model) newSidebar() *sidebar {
	s := &sidebar{}
	rows, _ := m.rootView().loader(m.ctx) // static
	for _, r := range rows {
		s.roots = append(s.roots, newNode(r, nil))
	}
	s.flatten()
	return s
}

// sideInit expands the groups that start open.
func (m *Model) sideInit() tea.Cmd {
	var cmds []tea.Cmd
	for _, n := range m.side.roots {
		if n.row.expand {
			cmds = append(cmds, m.expand(n))
		}
	}
	return tea.Batch(cmds...)
}

func (s *sidebar) flatten() {
	s.flat = s.flat[:0]
	var walk func([]*node)
	walk = func(ns []*node) {
		for _, n := range ns {
			s.flat = append(s.flat, n)
			if n.expanded {
				walk(n.children)
			}
		}
	}
	walk(s.roots)
	s.clamp()
}

func (s *sidebar) clamp() {
	s.cursor = max(0, min(s.cursor, len(s.flat)-1))
}

func (s *sidebar) current() *node {
	if s.cursor < len(s.flat) {
		return s.flat[s.cursor]
	}
	return nil
}

func (s *sidebar) moveTo(n *node) {
	if i := slices.Index(s.flat, n); i >= 0 {
		s.cursor = i
	}
}

// contains reports whether n is still in the tree (a reload can drop it).
func (s *sidebar) contains(n *node) bool {
	for ; n.parent != nil; n = n.parent {
		if !slices.Contains(n.parent.children, n) {
			return false
		}
	}
	return slices.Contains(s.roots, n)
}

func (m *Model) expand(n *node) tea.Cmd {
	if !n.group() {
		return nil
	}
	n.expanded = true
	m.side.flatten()
	if n.loaded || n.loading {
		return nil
	}
	return m.loadNode(n)
}

func (m *Model) loadNode(n *node) tea.Cmd {
	n.loading = true
	loader := n.view.loader
	return func() tea.Msg {
		rows, err := loader(m.ctx)
		return sideLoadedMsg{n: n, rows: rows, err: err}
	}
}

// sideLoaded fills in a group's children. Children that were there before
// keep their node, so expanded groups and each source's filter, sort and
// cursor survive a reload.
func (m *Model) sideLoaded(msg sideLoadedMsg) {
	n := msg.n
	n.loading, n.err = false, msg.err
	if msg.err == nil {
		old := map[string]*node{}
		for _, c := range n.children {
			old[c.row.label] = c
		}
		n.children = nil
		for _, r := range msg.rows {
			c := old[r.label]
			if c == nil {
				c = newNode(r, n)
			} else {
				c.row = r
			}
			n.children = append(n.children, c)
		}
		if len(n.children) == 0 {
			n.children = []*node{newNode(row{label: "(empty)"}, n)}
		}
		n.loaded = true
	}
	s := m.side
	cur := s.current()
	s.flatten()
	if cur != nil {
		s.moveTo(cur)
	}
	if s.active != nil && !s.contains(s.active) { // e.g. the playlist was deleted
		s.active = nil
		m.stack = []*view{emptyView()}
	}
}

// reloadSide refetches every group that has loaded, after the server's
// data changed. Collapsed groups reload when next expanded.
func (m *Model) reloadSide() tea.Cmd {
	var cmds []tea.Cmd
	var walk func([]*node)
	walk = func(ns []*node) {
		for _, n := range ns {
			if n.leaf() {
				n.view.stale = true
			}
			if n.group() && n.loaded {
				n.loaded = false
				if n.expanded && !n.loading {
					cmds = append(cmds, m.loadNode(n))
				}
				walk(n.children)
			}
		}
	}
	walk(m.side.roots)
	return tea.Batch(cmds...)
}

// openSource shows a source in the list pane, loading it if needed.
func (m *Model) openSource(n *node) tea.Cmd {
	m.side.active = n
	v := n.view
	m.stack = []*view{v}
	if v.loading || (v.fetched && !v.stale) {
		return nil
	}
	return m.load(v)
}

func (m *Model) sideKey(msg tea.KeyMsg) tea.Cmd {
	s := m.side
	n := s.current()
	page := max(1, m.bodyHeight()-2)
	switch msg.String() {
	case "j", "down":
		s.cursor++
	case "k", "up":
		s.cursor--
	case "g", "home":
		s.cursor = 0
	case "G", "end":
		s.cursor = len(s.flat) - 1
	case "ctrl+d", "pgdown":
		s.cursor += page
	case "ctrl+u", "pgup":
		s.cursor -= page
	case "l", "right", "enter":
		switch {
		case n == nil:
		case n.group() && !n.expanded:
			return m.expand(n)
		case n.group() && msg.String() == "enter":
			n.expanded = false
			s.flatten()
		case n.group() && len(n.children) > 0:
			s.cursor++
		case n.leaf():
			m.focus = focusList
			return m.openSource(n)
		}
	case "h", "left", "backspace":
		switch {
		case n == nil:
		case n.group() && n.expanded:
			n.expanded = false
			s.flatten()
		case n.parent != nil:
			s.moveTo(n.parent)
		}
	case "/", "v":
		m.focus = focusList
		return m.listKey(msg)
	case "x", ";":
		if len(m.sel) > 0 {
			return m.openMenu()
		}
	case "R":
		if c := m.sideContainer(); c != nil {
			return m.promptRename(c)
		}
	case "D":
		if c := m.sideContainer(); c != nil {
			return planCmd(plan.DeleteContainer(c))
		}
	case "esc":
		if len(m.sel) > 0 {
			m.clearSelection()
		}
	}
	s.clamp()
	if c := s.current(); c != nil && c.leaf() && c != s.active {
		return tea.Tick(sidePreviewDelay, func(time.Time) tea.Msg { return sideTickMsg{c} })
	}
	return nil
}

// sideView renders the sidebar as h lines of width w.
func (m *Model) sideView(w, h int) []string {
	s := m.side
	focused := m.focus == focusSide
	lines := []string{paneTitle("SOURCES", w, focused)}
	rows := h - 1
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+rows {
		s.offset = s.cursor - rows + 1
	}
	for i := s.offset; i < len(s.flat) && i < s.offset+rows; i++ {
		lines = append(lines, m.sideRow(s.flat[i], w, focused && i == s.cursor))
	}
	return lines
}

func (m *Model) sideRow(n *node, w int, cursor bool) string {
	prefix := strings.Repeat("  ", n.depth)
	switch {
	case n.group() && n.expanded:
		prefix += "▾ "
	case n.group():
		prefix += "▸ "
	default:
		prefix += "  "
	}
	if n.depth == 0 {
		prefix += cell(n.row.icon)
	}
	badge := n.row.badge
	switch {
	case n.loading:
		badge = m.spinner.View()
	case n.err != nil:
		badge = "!"
	}
	nameW := w - lipgloss.Width(prefix) - lipgloss.Width(badge) - 1
	line := prefix + fit(n.name(), nameW) + " " + badge
	if cursor {
		return stCursor.Render(fit(stripANSI(line), w))
	}
	name := fit(n.name(), nameW)
	switch {
	case n == m.side.active:
		name = stDir.Render(name)
	case n.view == nil: // a placeholder like "(none yet…)"
		name = stDim.Render(name)
	case n.depth == 0:
		name = stBold.Render(name)
	}
	if n.err != nil {
		badge = stErr.Render(badge)
	} else {
		badge = stDim.Render(badge)
	}
	return prefix + name + " " + badge
}
