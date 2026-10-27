package tui

import tea "github.com/charmbracelet/bubbletea"

// bodyTop is the screen row of the panes' first line, under the header and
// filter line. Each pane's first line is its title or column header.
const bodyTop = 2

// wheelStep is how many rows one wheel notch scrolls.
const wheelStep = 3

// paneAt reports which pane is at screen column x and screen row y, and
// which of its rows (counting from the first one under the pane's title).
func (m *Model) paneAt(x, y int) (f focus, row int, ok bool) {
	row = y - bodyTop - 1
	if row < 0 || row >= m.bodyHeight()-1 {
		return 0, 0, false
	}
	lay := m.layout()
	switch {
	case x < lay.side:
		return focusSide, row, true
	case lay.side > 0 && x == lay.side: // the rule between panes
	case lay.list > 0 && x >= lay.side+1 && x < lay.side+1+lay.list:
		return focusList, row, true
	case lay.side == 0 && lay.list > 0 && x < lay.list:
		return focusList, row, true
	}
	return 0, 0, false
}

// mouse handles clicks and the wheel in the sidebar and list panes. Other
// modes draw over them, so they ignore the mouse.
func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode != modeNormal || msg.Action != tea.MouseActionPress {
		return nil
	}
	f, row, ok := m.paneAt(msg.X, msg.Y)
	if !ok {
		return nil
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		if f == focusSide {
			return m.sideClick(row)
		}
		return m.listClick(row)
	case tea.MouseButtonWheelUp:
		m.wheel(f, -wheelStep)
	case tea.MouseButtonWheelDown:
		m.wheel(f, wheelStep)
	}
	return nil
}

// sideClick selects the clicked node: a group opens or closes, a source
// shows in the list pane.
func (m *Model) sideClick(row int) tea.Cmd {
	s := m.side
	i := s.offset + row
	if i >= len(s.flat) {
		return nil
	}
	m.focus = focusSide
	s.cursor = i
	n := s.flat[i]
	switch {
	case n.group() && n.expanded:
		n.expanded = false
		s.flatten()
	case n.group():
		return m.expand(n)
	case n.leaf() && n != s.active:
		return m.openSource(n)
	}
	return nil
}

func (m *Model) listClick(row int) tea.Cmd {
	v := m.top()
	i := v.offset + row
	if i >= len(v.rows) {
		return nil
	}
	m.focus = focusList
	v.cursor = i
	return nil
}

// wheel scrolls a pane by d rows without moving focus, dragging the cursor
// along so the pane's render keeps the new position.
func (m *Model) wheel(f focus, d int) {
	if f == focusSide {
		s := m.side
		s.offset, s.cursor = scroll(s.offset, s.cursor, d, len(s.flat), m.bodyHeight()-1)
		return
	}
	v := m.top()
	v.offset, v.cursor = scroll(v.offset, v.cursor, d, len(v.rows), m.listHeight())
}

// scroll moves a window of h rows over n by d and keeps cursor inside it.
func scroll(offset, cursor, d, n, h int) (int, int) {
	offset = max(0, min(offset+d, n-h))
	cursor = max(offset, min(cursor, offset+h-1, n-1))
	return offset, cursor
}
