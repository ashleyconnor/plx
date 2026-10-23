package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/ashleyconnor/plx/internal/field"
)

type keyGroup struct {
	title string
	keys  [][2]string
}

var keyGroups = []keyGroup{
	{"Move", [][2]string{
		{"j k ↑ ↓", "up / down"},
		{"g G", "top / bottom"},
		{"^d ^u", "page down / up"},
		{"l → enter", "open, or item info"},
		{"h ← ⌫", "back, or up a folder"},
	}},
	{"Select", [][2]string{
		{"space", "select / unselect"},
		{"a", "select all / none"},
		{"A", "invert"},
		{"v", "view selection"},
		{"c", "clear selection"},
	}},
	{"View", [][2]string{
		{"/", "filter"},
		{"s", "sort"},
		{"f", "folder lens"},
		{"i", "item info"},
		{"esc", "clear filter"},
		{"r", "refresh"},
	}},
	{"Act", [][2]string{
		{"x ;", "actions"},
		{"S", "save as query"},
		{"?", "help"},
		{"q", "quit"},
	}},
}

var filterExamples = [][2]string{
	{"alien", `title contains "alien"`},
	{"size>4G  dur<30m", "compare: sizes K M G T, durations m h d"},
	{"label:keep label:fav", "both labels"},
	{"label:keep,fav", "either label"},
	{"-label:trash", "negate any term"},
	{"has:label  -has:label", "has any label / has none"},
	{"labelcount:1 label:keep", "keep is the only label"},
	{"is:watched  -is:watched", "yes/no fields"},
	{"path:/tv/anime/", "path contains, at any depth"},
	{"path:/tv/*/S01/*", "glob"},
	{"added<30d  viewed>1y", "relative age"},
	{"added>2024-01-01", "absolute date"},
	{"sort:size-,title", "sort, - for descending"},
}

func helpTable(header [2]string, rows [][2]string, keyStyle lipgloss.Style) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(stDim).
		Headers(header[0], header[1]).
		StyleFunc(func(row, col int) lipgloss.Style {
			st := lipgloss.NewStyle().Padding(0, 1)
			switch {
			case row == table.HeaderRow:
				return st.Bold(true)
			case col == 0:
				return keyStyle.Padding(0, 1)
			}
			return st
		})
	for _, r := range rows {
		t.Row(r[0], r[1])
	}
	return t.Render()
}

// helpLines lays out the key tables side by side when they fit, then the
// filter language, fields and icon legend.
func (m *Model) helpLines() []string {
	var tables []string
	for _, g := range keyGroups {
		tables = append(tables, helpTable([2]string{g.title, ""}, g.keys, stKey))
	}
	var keys string
	switch {
	case lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, tables...))+3*len(tables) <= m.w:
		keys = joinGap(tables...)
	case lipgloss.Width(joinGap(tables[0], tables[1])) <= m.w:
		keys = lipgloss.JoinVertical(lipgloss.Left, joinGap(tables[0], tables[1]), joinGap(tables[2], tables[3]))
	default:
		keys = lipgloss.JoinVertical(lipgloss.Left, tables...)
	}

	var names []string
	for _, f := range field.All() {
		names = append(names, f.Name)
	}
	wrap := lipgloss.NewStyle().Width(max(20, m.w-2))

	var b strings.Builder
	b.WriteString(stBold.Render("plx") + stDim.Render(" — selection persists while you browse: collect items from anywhere, then act on them all at once") + "\n\n")
	b.WriteString(keys + "\n\n")
	b.WriteString(helpTable([2]string{"Filter", "meaning"}, filterExamples, lipgloss.NewStyle().Foreground(lipgloss.Color("42"))) + "\n\n")
	b.WriteString(stBold.Render("Fields") + stDim.Render("  (tab completes them in the filter bar)") + "\n")
	b.WriteString(wrap.Render(strings.Join(names, " · ")) + "\n")
	if l := legend(); l != "" {
		b.WriteString("\n" + stBold.Render("Icons") + "\n" + wrap.Render(l) + "\n")
	}
	return strings.Split(b.String(), "\n")
}

func joinGap(blocks ...string) string {
	var parts []string
	for i, bl := range blocks {
		if i > 0 {
			parts = append(parts, "  ")
		}
		parts = append(parts, bl)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m *Model) helpView() string {
	lines := m.helpLines()
	h := max(1, m.h-1)
	m.helpScroll = min(m.helpScroll, max(0, len(lines)-h))
	shown := lines[m.helpScroll:min(len(lines), m.helpScroll+h)]
	footer := renderHints([]hint{{"esc", "close"}}, m.w)
	if len(lines) > h {
		footer = renderHints([]hint{{"j/k", "scroll"}, {"esc", "close"}}, m.w)
	}
	return strings.Join(shown, "\n") + "\n" + footer
}

func (m *Model) helpKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		m.helpScroll++
	case "k", "up":
		m.helpScroll = max(0, m.helpScroll-1)
	case "ctrl+d", "pgdown", " ":
		m.helpScroll += m.h / 2
	case "ctrl+u", "pgup":
		m.helpScroll = max(0, m.helpScroll-m.h/2)
	default:
		m.mode, m.helpScroll = modeNormal, 0
	}
	return nil
}
