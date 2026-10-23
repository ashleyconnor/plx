package tui

import (
	"fmt"
	"path"
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
	if n == 0 {
		return "0B"
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
	case modeInfo:
		return m.overlay(m.infoView())
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
	if v.kind == itemView && v.indexing && v.filter != nil && v.filter.UsesMembership() {
		right = fmt.Sprintf("…/%d", len(v.items))
	} else if v.kind == itemView && !v.loading {
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
		if v.filter != nil && len(v.filter.Sort) > 0 {
			// shown as part of the filter text
		} else if len(v.sortKeys) > 0 {
			parts = append(parts, "sort: "+query.FormatSort(v.sortKeys))
		} else if c := v.container(); c != nil && c.Caps().Ordered {
			parts = append(parts, "sort: "+c.Kind.String()+" order")
		}
	}
	if c := v.container(); c != nil && c.Smart {
		parts = append(parts, stWarn.Render("smart "+c.Kind.String()+": read-only"))
	}
	if v.indexing {
		parts = append(parts, stWarn.Render(m.spinner.View()+" indexing collections & playlists…"))
	}
	return stDim.Render(runewidth.Truncate(strings.Join(parts, "   "), m.w, "…"))
}

func (m *Model) titleWidth(v *view) int {
	w := m.w - 2
	for _, c := range v.columns {
		w -= v.colWidth(c) + 1
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
		b.WriteString(" " + fitLeft(strings.ToUpper(columnLabel(c)), v.colWidth(c)))
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

// maxColWidth caps how wide a column grows to fit values like "2m|2h30m".
const maxColWidth = 16

// colWidth is the column's width in this view: its default, widened to fit
// the widest visible value.
func (v *view) colWidth(f *field.Field) int {
	if w, ok := v.widths[f]; ok {
		return w
	}
	return colWidth(f)
}

func (v *view) measureColumns() {
	v.widths = map[*field.Field]int{}
	for _, c := range v.columns {
		w := colWidth(c)
		for _, r := range v.rows {
			if r.item != nil {
				w = max(w, min(maxColWidth, runewidth.StringWidth(c.Format(r.item))))
			}
		}
		v.widths[c] = w
	}
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
	if m.mode == modeFilter && len(m.comp.sugs) > 0 {
		drop := m.dropdown(min(8, max(1, h/2)))
		lines = append(lines, drop...)
		h -= len(drop)
	}
	switch {
	case v.loading:
		lines = append(lines, "  "+m.spinner.View()+" loading…")
	case v.indexing && len(v.rows) == 0:
		lines = append(lines, "  "+m.spinner.View()+" indexing collections & playlists…")
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
	for len(lines) < m.listHeight() {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n") + "\n"
}

// dropdown renders filter suggestions as a box of at most n rows, scrolled
// to keep the highlighted one visible.
func (m *Model) dropdown(n int) []string {
	sugs := m.comp.sugs
	start := 0
	if m.compSel >= n {
		start = m.compSel - n + 1
	}
	w := min(70, m.w-6)
	var rows []string
	for i := start; i < len(sugs) && i < start+n; i++ {
		s := sugs[i]
		line := fit(s.label, 28) + " " + stDim.Render(fit(s.detail, w-29))
		if i == m.compSel || (m.compSel < 0 && i == 0) {
			line = stCursor.Render(fit(stripANSI(line), w))
		}
		rows = append(rows, line)
	}
	more := ""
	if len(sugs) > n {
		more = fmt.Sprintf("%d more · ", len(sugs)-start-len(rows))
	}
	rows = append(rows, stDim.Render(more)+stKey.Render("tab")+stDim.Render(" to complete · ")+stKey.Render("↑↓")+stDim.Render(" choose"))
	box := stBox.Padding(0, 1).Render(strings.Join(rows, "\n"))
	var out []string
	for _, l := range strings.Split(box, "\n") {
		out = append(out, " "+l)
	}
	return out
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
			b.WriteString(" " + fitLeft(c.Format(r.item), v.colWidth(c)))
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
	case modeFilter:
		keys := renderHints([]hint{{"tab", "complete"}, {"↑↓", "choose"}, {"enter", "apply"}, {"esc", "revert"}}, m.w/2)
		hint := runewidth.Truncate(m.comp.hint, max(0, m.w-lipgloss.Width(keys)-3), "…")
		gap := max(1, m.w-lipgloss.Width(keys)-runewidth.StringWidth(hint))
		return stDim.Render(hint) + strings.Repeat(" ", gap) + keys
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

var (
	stButton    = lipgloss.NewStyle().Padding(0, 3).Foreground(lipgloss.AdaptiveColor{Light: "235", Dark: "252"}).Background(lipgloss.AdaptiveColor{Light: "252", Dark: "237"})
	stButtonNo  = stButton.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Bold(true)
	stButtonYes = stButton.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("160")).Bold(true)
)

// confirmButtons draws No / Yes buttons with the focused one highlighted;
// Yes turns red when focused since it's destructive.
func confirmButtons(yes bool) string {
	no, ok := stButtonNo.Render("No"), stButton.Render("Yes")
	if yes {
		no, ok = stButton.Render("No"), stButtonYes.Render("Yes")
	}
	return no + "  " + ok
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
			for i, it := range op.Items {
				if op.Kind == plan.OpDeleteVersion {
					v := op.Versions[i]
					for _, f := range v.Files {
						files = append(files, fmt.Sprintf("%s (%s): %s", it.DisplayTitle(), versionLabel(v), path.Base(f)))
					}
					continue
				}
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
		b.WriteString(stBold.Render("Delete these files?") + "\n\n" + confirmButtons(m.confirmYes) + "\n\n" +
			renderHints([]hint{{"←→", "choose"}, {"enter", "confirm"}, {"esc", "cancel"}}, w))
	default:
		b.WriteString(stOK.Render("y apply") + stDim.Render(" · n cancel"))
	}
	return stBox.Width(w + 4).Render(b.String())
}
