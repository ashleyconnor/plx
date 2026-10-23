package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
	"github.com/ashleyconnor/plx/internal/query"
)

var (
	stBold   = lipgloss.NewStyle().Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "243"})
	stCursor = lipgloss.NewStyle().Reverse(true)
	stSel    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	stDir    = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	stErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	stWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	stOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	stKey    = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	stBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("39")).Padding(0, 1)
)

// Fixed lines around the list: header, filter line, column header, two
// detail lines, message line, key hints.
const chrome = 7

func (m *Model) listHeight() int { return max(1, m.h-chrome) }

func humanSize(items []*model.Item) string {
	var n int64
	for _, it := range items {
		n += it.Size
	}
	return field.HumanBytes(n)
}

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
}

func fitLeft(s string, w int) string {
	s = runewidth.Truncate(s, w, "…")
	return strings.Repeat(" ", max(0, w-runewidth.StringWidth(s))) + s
}

func (m *Model) View() string {
	switch m.mode {
	case modeHelp:
		return m.helpView()
	case modeMenu:
		return m.overlay(m.menuView())
	case modePicker:
		return m.overlay(m.pickerView())
	case modePreview:
		return m.overlay(m.previewView())
	}
	v := m.top()
	var b strings.Builder
	b.WriteString(m.header(v) + "\n")
	b.WriteString(m.filterLine(v) + "\n")
	b.WriteString(m.columnHeader(v) + "\n")
	b.WriteString(m.list(v))
	b.WriteString(m.detail(v) + "\n")
	b.WriteString(m.messageLine() + "\n")
	b.WriteString(m.hintsLine(v))
	return b.String()
}

func (m *Model) breadcrumb() string {
	var parts []string
	for _, v := range m.stack {
		parts = append(parts, v.title)
	}
	v := m.top()
	if v.kind == itemView && v.lens {
		parts = append(parts, "["+v.folder+"]")
	}
	return strings.Join(parts, " ▸ ")
}

func (m *Model) header(v *view) string {
	right := ""
	if v.kind == itemView && !v.loading {
		vis := v.visible()
		right = fmt.Sprintf("%d/%d · %s", len(vis), len(v.items), humanSize(vis))
	}
	if len(m.sel) > 0 {
		right = stSel.Render(fmt.Sprintf("● %d · %s", len(m.sel), humanSize(m.selected()))) + "  " + right
	}
	left := stBold.Render(runewidth.Truncate(m.breadcrumb(), max(10, m.w-lipgloss.Width(right)-2), "…"))
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) filterLine(v *view) string {
	var parts []string
	if m.mode == modeFilter {
		parts = append(parts, "/"+m.input.View())
	} else if v.filterText != "" {
		parts = append(parts, "filter: "+v.filterText)
	}
	if v.filterErr != "" {
		parts = append(parts, stErr.Render(v.filterErr))
	}
	if v.kind == itemView {
		if len(v.sortKeys) > 0 {
			parts = append(parts, "sort: "+query.FormatSort(v.sortKeys))
		} else if c := v.container(); c != nil && c.Caps().Ordered {
			parts = append(parts, "sort: "+c.Kind.String()+" order")
		}
	}
	if c := v.container(); c != nil && c.Smart {
		parts = append(parts, stWarn.Render("smart "+c.Kind.String()+": read-only"))
	}
	return stDim.Render(runewidth.Truncate(strings.Join(parts, "   "), m.w, "…"))
}

func (m *Model) titleWidth(v *view) int {
	w := m.w - 2
	for _, c := range v.columns {
		w -= colWidth(c) + 1
	}
	return max(10, w)
}

func (m *Model) columnHeader(v *view) string {
	if v.kind != itemView {
		return ""
	}
	var b strings.Builder
	b.WriteString("  " + strings.Repeat(" ", cellWidth()) + fit("TITLE", m.titleWidth(v)-cellWidth()))
	for _, c := range v.columns {
		b.WriteString(" " + fitLeft(strings.ToUpper(columnLabel(c)), colWidth(c)))
	}
	return stDim.Render(b.String())
}

// columnLabel is the field name, or its shortest alias if the name doesn't fit.
func columnLabel(f *field.Field) string {
	label := f.Name
	for _, a := range f.Aliases {
		if len(label) > colWidth(f) && len(a) < len(label) {
			label = a
		}
	}
	return label
}

func colWidth(f *field.Field) int {
	if f.Width > 0 {
		return f.Width
	}
	return 10
}

func (m *Model) list(v *view) string {
	h := m.listHeight()
	var lines []string
	switch {
	case v.loading:
		lines = append(lines, "  "+m.spinner.View()+" loading…")
	case v.err != nil:
		lines = append(lines, "  "+stErr.Render(v.err.Error()), "  "+stDim.Render("r to retry, h to go back"))
	case len(v.rows) == 0:
		lines = append(lines, stDim.Render("  (empty)"))
	default:
		if v.cursor < v.offset {
			v.offset = v.cursor
		}
		if v.cursor >= v.offset+h {
			v.offset = v.cursor - h + 1
		}
		for i := v.offset; i < len(v.rows) && i < v.offset+h; i++ {
			lines = append(lines, m.renderRow(v, v.rows[i], i == v.cursor))
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n") + "\n"
}

func (m *Model) renderRow(v *view, r row, cursor bool) string {
	tw := m.titleWidth(v) - cellWidth()
	var line string
	selected := false
	switch {
	case r.item != nil:
		selected = m.isSelected(r.item)
		var b strings.Builder
		b.WriteString(cell(icons.kind(r.item.Kind)) + fit(r.item.DisplayTitle(), tw))
		for _, c := range v.columns {
			b.WriteString(" " + fitLeft(c.Format(r.item), colWidth(c)))
		}
		line = b.String()
	case r.isFolder():
		n := 0
		for _, it := range r.under {
			if m.isSelected(it) {
				n++
			}
		}
		selected = n > 0 && n == len(r.under)
		name := fit(r.label, tw)
		if !cursor && !selected {
			name = stDir.Render(name)
		}
		line = cell(icons.folder) + name + " " + fitLeft(field.HumanBytes(r.size), field.Size.Width) + "  " + stDim.Render(plural(len(r.under), "item"))
	default:
		w := m.w - 2 - cellWidth()
		nameW := min(w, 40)
		name := fit(r.label, nameW)
		if r.push != nil && !cursor {
			name = stDir.Render(name)
		}
		line = cell(r.icon) + name + "  " + stDim.Render(runewidth.Truncate(r.detail, max(0, w-nameW-2), "…"))
	}
	marker := "  "
	if selected {
		marker = stSel.Render("● ")
	}
	if cursor {
		return marker + stCursor.Render(fit(stripANSI(line), m.w-2))
	}
	if selected {
		return marker + stSel.Render(stripANSI(line))
	}
	return marker + line
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func dateOr(t time.Time, none string) string {
	if t.IsZero() {
		return none
	}
	return t.Format("2006-01-02")
}

func (m *Model) detail(v *view) string {
	r, ok := v.current()
	if !ok || v.kind != itemView {
		return "\n"
	}
	if r.isFolder() {
		return stDim.Render(fit(r.folder, m.w)) + "\n" + stDim.Render(fmt.Sprintf("%s · %s", plural(len(r.under), "item"), field.HumanBytes(r.size)))
	}
	it := r.item
	if it == nil {
		return "\n"
	}
	var bits []string
	add := func(s string) {
		if s != "" {
			bits = append(bits, s)
		}
	}
	add(string(it.Kind))
	add(it.SectionTitle)
	add(strings.TrimSpace(strings.Join([]string{it.Resolution, it.VideoCodec, it.Container}, " ")))
	if it.Bitrate > 0 {
		add(fmt.Sprintf("%d kbps", it.Bitrate))
	}
	add("added " + dateOr(it.Added, "?"))
	add("viewed " + dateOr(it.LastViewed, "never"))
	if len(it.Labels) > 0 {
		add("labels " + strings.Join(it.Labels, ","))
	}
	path := it.Path()
	if len(it.Files) > 1 {
		path += fmt.Sprintf("  (+%d more)", len(it.Files)-1)
	}
	return stDim.Render(fit(path, m.w)) + "\n" + stDim.Render(fit(strings.Join(bits, " · "), m.w))
}

// messageLine shows the current mode's input, progress, or a recent message.
func (m *Model) messageLine() string {
	switch m.mode {
	case modePrompt:
		return stKey.Render(m.prompt.label+": ") + m.input.View()
	case modeSort:
		return stBold.Render("sort by: ") + renderHints([]hint{
			{"t", "title"}, {"s", "size"}, {"d", "duration"}, {"a", "added"}, {"v", "viewed"}, {"p", "plays"},
			{"y", "year"}, {"b", "bitrate"}, {"f", "file"}, {"r", "reverse"}, {"0", "source order"}, {"c", "custom…"},
		}, m.w-9)
	case modeRunning:
		bar := ""
		if m.progress.Total > 0 {
			const bw = 20
			n := bw * m.progress.Done / m.progress.Total
			bar = fmt.Sprintf("[%s%s] %d/%d ", strings.Repeat("█", n), strings.Repeat("░", bw-n), m.progress.Done, m.progress.Total)
		}
		return m.spinner.View() + " " + bar + runewidth.Truncate(m.progress.Step, max(0, m.w-40), "…")
	}
	if m.status == "" || time.Since(m.statusAt) > statusTTL {
		return ""
	}
	if m.statusErr {
		return stErr.Render(runewidth.Truncate("✗ "+m.status, m.w, "…"))
	}
	return stOK.Render(runewidth.Truncate("✓ "+m.status, m.w, "…"))
}

type hint struct{ key, desc string }

// renderHints draws "key desc" pairs, dropping trailing ones that don't fit.
func renderHints(hs []hint, w int) string {
	var b strings.Builder
	used := 0
	for i, h := range hs {
		gap := 0
		if i > 0 {
			gap = 3
		}
		n := runewidth.StringWidth(h.key) + 1 + runewidth.StringWidth(h.desc)
		if used+gap+n > w {
			break
		}
		if i > 0 {
			b.WriteString(stDim.Render(" · "))
		}
		b.WriteString(stKey.Render(h.key) + " " + stDim.Render(h.desc))
		used += gap + n
	}
	return b.String()
}

func (m *Model) hintsLine(v *view) string {
	var hs []hint
	if v.kind == itemView {
		hs = []hint{{"space", "select"}, {"a", "all/none"}, {"v", "view selected"}, {"x", "actions"},
			{"/", "filter"}, {"s", "sort"}, {"f", "folders"}, {"S", "save"}, {"?", "help"}}
	} else {
		hs = []hint{{"→", "open"}, {"←", "back"}, {"/", "filter"}}
		if len(m.sel) > 0 {
			hs = append(hs, hint{"v", "view selected"})
		}
		hs = append(hs, hint{"?", "help"}, hint{"q", "quit"})
	}
	return renderHints(hs, m.w)
}

// --- overlays ----------------------------------------------------------------

func (m *Model) overlay(box string) string {
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) menuView() string {
	mu := m.menu
	var b strings.Builder
	b.WriteString(stBold.Render(mu.title) + "\n\n")
	for i, e := range mu.entries {
		label := e.label
		if e.danger {
			label = stErr.Render(label)
		}
		line := fmt.Sprintf(" %s  %s ", stKey.Render(e.key), label)
		if i == mu.cursor {
			line = stCursor.Render(stripANSI(line))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + stDim.Render("key or enter · esc cancel"))
	return stBox.Render(b.String())
}

func (m *Model) pickerView() string {
	p := m.picker
	rows := p.rows()
	w := min(70, m.w-6)
	h := max(3, min(15, m.h-10))
	var b strings.Builder
	b.WriteString(stBold.Render(p.title) + "\n" + p.input.View() + "\n\n")
	start := max(0, p.cursor-h+1)
	for i := start; i < len(rows) && i < start+h; i++ {
		r := rows[i]
		label := fit(r.opt.label, w-22) + " " + stDim.Render(fit(r.opt.detail, 20))
		if r.isNew {
			label = stOK.Render(fit("+ "+r.opt.label, w))
		}
		if i == p.cursor {
			label = stCursor.Render(fit(stripANSI(label), w))
		}
		b.WriteString(label + "\n")
	}
	if len(rows) == 0 {
		b.WriteString(stDim.Render("no matches") + "\n")
	}
	b.WriteString("\n" + stDim.Render("↑↓ choose · enter pick · esc cancel"))
	return stBox.Width(w + 4).Render(b.String())
}

func (m *Model) previewView() string {
	p := m.preview
	w := min(90, m.w-6)
	var b strings.Builder
	b.WriteString(stBold.Render(p.Title) + "\n\n")
	moves := 0
	for _, op := range p.Ops {
		if op.Kind == plan.OpMove {
			moves++
			continue
		}
		b.WriteString("  • " + runewidth.Truncate(op.Describe(), w-4, "…") + "\n")
	}
	if moves > 0 {
		b.WriteString(fmt.Sprintf("  • %s (only items out of place are moved)\n", plural(moves, "move")))
	}
	if len(p.Ops) == 0 {
		b.WriteString(stDim.Render("  nothing to do") + "\n")
	}
	if sk := p.SkipSummary(); len(sk) > 0 {
		b.WriteString("\n" + stWarn.Render("skipped:") + "\n")
		for _, s := range sk {
			b.WriteString("  " + s + "\n")
		}
	}
	if len(p.Violations) > 0 {
		b.WriteString("\n")
		for _, vi := range p.Violations {
			if vi.Severity == plan.Block {
				b.WriteString(stErr.Render("✗ "+vi.Message) + "\n")
			} else {
				b.WriteString(stWarn.Render("! "+vi.Message) + "\n")
			}
		}
	}
	if p.Destructive() {
		b.WriteString("\n" + stErr.Render("files that will be deleted:") + "\n")
		const sample = 8
		var files []string
		for _, op := range p.Ops {
			for _, it := range op.Items {
				files = append(files, it.Files...)
			}
		}
		for i, f := range files {
			if i == sample {
				b.WriteString(stDim.Render(fmt.Sprintf("  … and %d more", len(files)-sample)) + "\n")
				break
			}
			b.WriteString("  " + runewidth.Truncate(f, w-2, "…") + "\n")
		}
	}
	b.WriteString("\n")
	switch {
	case p.Blocked():
		b.WriteString(stDim.Render("can't apply · esc back"))
	case p.Destructive():
		b.WriteString(stErr.Render("type delete to confirm: ") + m.input.View() + "\n" + stDim.Render("enter confirm · esc cancel"))
	default:
		b.WriteString(stOK.Render("y apply") + stDim.Render(" · n cancel"))
	}
	return stBox.Width(w + 4).Render(b.String())
}

const helpText = `plx — keys

  j/k ↑/↓      move              g/G      top/bottom        ^d/^u  page
  l/→/enter    open              h/←/⌫    back / up a folder
  space        select            a        select all/none   A      invert
  v            view selection    c        clear selection
  /            filter            s        sort              f      folder lens
  x or ;       actions           S        save as query     r      refresh
  esc          clear filter      q        quit              ?      this help

  Selection persists while you browse: pick items from several places,
  then act on them all at once.

filter language

  alien                  title contains "alien"
  size>4G  dur<30m       compare (sizes: K M G T; durations: m h d)
  label:keep label:fav   both labels         label:keep,fav   either
  -label:trash           negate any term     is:watched  -is:watched
  path:/tv/anime/        substring at any depth    path:/tv/*/S01/*  glob
  added<30d              added in the last 30 days  viewed>1y  added>2024-01-01
  sort:size-,title       sort (- = descending)

fields
`

func (m *Model) helpView() string {
	var names []string
	for _, f := range field.All() {
		names = append(names, f.Name)
	}
	wrap := lipgloss.NewStyle().Width(max(20, m.w-4)).MarginLeft(2)
	out := helpText + "\n" + wrap.Render(strings.Join(names, " ")) + "\n"
	if l := legend(); l != "" {
		out += "\nicons\n\n" + wrap.Render(l) + "\n"
	}
	return out + "\n" + stDim.Render("press any key")
}
