package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ashleyconnor/plx/internal/exec"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
	"github.com/ashleyconnor/plx/internal/query"
	"github.com/ashleyconnor/plx/internal/source"
	"github.com/ashleyconnor/plx/internal/store"
)

// --- action menu -------------------------------------------------------------

type menuEntry struct {
	key, label string
	run        func() tea.Cmd
}

type menu struct {
	title   string
	entries []menuEntry
	cursor  int
}

func (m *Model) openMenu() tea.Cmd {
	v := m.top()
	if v.kind != itemView || v.loading {
		return nil
	}
	items := m.targets()
	if len(items) == 0 {
		m.setStatus("nothing selected", true)
		return nil
	}
	src := v.container()
	var es []menuEntry
	add := func(key, label string, run func() tea.Cmd) { es = append(es, menuEntry{key, label, run}) }

	add("p", "Add to playlist…", func() tea.Cmd { return m.pickPlaylist(items, nil) })
	add("c", "Add to collection…", func() tea.Cmd { return m.pickCollection(items, nil) })
	add("l", "Add label…", func() tea.Cmd { return m.pickLabel(items, false) })
	add("L", "Remove label…", func() tea.Cmd { return m.pickLabel(items, true) })
	if src != nil && !src.Caps().ReadOnly {
		add("P", fmt.Sprintf("Move from this %s to playlist…", src.Kind), func() tea.Cmd { return m.pickPlaylist(items, src) })
		add("C", fmt.Sprintf("Move from this %s to collection…", src.Kind), func() tea.Cmd { return m.pickCollection(items, src) })
		add("d", fmt.Sprintf("Remove from this %s", src.Kind), func() tea.Cmd {
			return planCmd(plan.RemoveFrom(src, items, v.items))
		})
		if src.Caps().Ordered {
			add("o", fmt.Sprintf("Reorder this %s by current sort", src.Kind), func() tea.Cmd { return m.reorder() })
		}
	}
	m.menu = &menu{title: fmt.Sprintf("%s · %s", plural(len(items), "item"), humanSize(items)), entries: es}
	m.mode = modeMenu
	return nil
}

func (m *Model) menuKey(msg tea.KeyMsg) tea.Cmd {
	mu := m.menu
	switch k := msg.String(); k {
	case "esc", "q":
		m.mode = modeNormal
	case "j", "down":
		mu.cursor = min(mu.cursor+1, len(mu.entries)-1)
	case "k", "up":
		mu.cursor = max(mu.cursor-1, 0)
	case "enter":
		m.mode = modeNormal
		return mu.entries[mu.cursor].run()
	default:
		for _, e := range mu.entries {
			if e.key == k {
				m.mode = modeNormal
				return e.run()
			}
		}
	}
	return nil
}

func planCmd(p *plan.Plan) tea.Cmd {
	return func() tea.Msg { return planMsg{plan: p} }
}

// --- picker ------------------------------------------------------------------

type option struct {
	label, detail string
	value         any
}

// picker is a fuzzy list. With newLabel set, typed text that is not an
// existing option can be picked as a new name.
type picker struct {
	title    string
	options  []option
	newLabel string
	input    textinput.Model
	cursor   int
	pick     func(opt option, isNew bool) tea.Cmd
}

type pickerMsg struct{ p *picker }

func newPicker(title, newLabel string, opts []option, pick func(option, bool) tea.Cmd) *picker {
	in := textinput.New()
	in.Prompt = "› "
	in.Width = 60
	in.Placeholder = "type to filter"
	if newLabel != "" {
		in.Placeholder = "type to filter, or a new name"
	}
	return &picker{title: title, options: opts, newLabel: newLabel, input: in, pick: pick}
}

type pickRow struct {
	opt   option
	isNew bool
}

func (p *picker) rows() []pickRow {
	needle := strings.ToLower(strings.TrimSpace(p.input.Value()))
	var out []pickRow
	exact := false
	for _, o := range p.options {
		l := strings.ToLower(o.label)
		if needle == "" || strings.Contains(l, needle) {
			out = append(out, pickRow{opt: o})
		}
		exact = exact || l == needle
	}
	if p.newLabel != "" && needle != "" && !exact {
		name := strings.TrimSpace(p.input.Value())
		out = append([]pickRow{{opt: option{label: fmt.Sprintf("%s %q", p.newLabel, name), value: name}, isNew: true}}, out...)
	}
	return out
}

func (m *Model) pickerKey(msg tea.KeyMsg) tea.Cmd {
	p := m.picker
	rows := p.rows()
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		return nil
	case "down", "ctrl+n", "ctrl+j":
		p.cursor = min(p.cursor+1, len(rows)-1)
		return nil
	case "up", "ctrl+p", "ctrl+k":
		p.cursor = max(p.cursor-1, 0)
		return nil
	case "enter":
		if p.cursor >= len(rows) {
			return nil
		}
		r := rows[p.cursor]
		m.mode = modeRunning // building the plan may need a fetch
		m.progress = exec.Progress{Step: "planning…"}
		return p.pick(r.opt, r.isNew)
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.cursor = 0
	return cmd
}

// --- pickers for each action --------------------------------------------------

func errMsg(err error) tea.Msg { return statusMsg{text: err.Error(), err: true} }

// members fetches a container's current items (for duplicate checks).
func (m *Model) members(c *model.Container) ([]*model.Item, error) {
	return m.cat.Items(m.ctx, source.Members{C: c})
}

// pickPlaylist adds items to a playlist, or moves them there from src.
func (m *Model) pickPlaylist(items []*model.Item, src *model.Container) tea.Cmd {
	srcMembers := m.top().items
	return func() tea.Msg {
		pls, err := m.cat.Playlists(m.ctx)
		if err != nil {
			return errMsg(err)
		}
		var opts []option
		for _, p := range pls {
			if !p.Smart && (src == nil || p.ID != src.ID || src.Kind != model.Playlist) {
				opts = append(opts, option{label: p.Title, detail: containerDetail(p), value: p})
			}
		}
		title := "Add to playlist"
		if src != nil {
			title = "Move to playlist"
		}
		return pickerMsg{newPicker(title, "New playlist", opts, func(o option, isNew bool) tea.Cmd {
			return func() tea.Msg {
				if isNew {
					name := o.value.(string)
					if src != nil {
						return planMsg{plan: plan.NewPlaylistFrom(src, name, items, srcMembers)}
					}
					return planMsg{plan: plan.NewPlaylist(name, items)}
				}
				dst := o.value.(*model.Container)
				existing, err := m.members(dst)
				if err != nil {
					return planMsg{err: err}
				}
				if src != nil {
					return planMsg{plan: plan.MoveTo(src, dst, items, srcMembers, existing)}
				}
				return planMsg{plan: plan.AddTo(dst, items, existing)}
			}
		})}
	}
}

func sectionsOf(items []*model.Item) map[string]bool {
	s := map[string]bool{}
	for _, it := range items {
		s[it.SectionID] = true
	}
	return s
}

// pickCollection only offers collections in the items' libraries.
func (m *Model) pickCollection(items []*model.Item, src *model.Container) tea.Cmd {
	srcMembers := m.top().items
	return func() tea.Msg {
		secs := sectionsOf(items)
		all, err := m.cat.AllCollections(m.ctx)
		if err != nil {
			return errMsg(err)
		}
		var opts []option
		for _, c := range all {
			if !c.Smart && secs[c.SectionID] && (src == nil || c.ID != src.ID || src.Kind != model.Collection) {
				opts = append(opts, option{label: c.Title, detail: containerDetail(c), value: c})
			}
		}
		title := "Add to collection"
		if src != nil {
			title = "Move to collection"
		}
		return pickerMsg{newPicker(title, "New collection", opts, func(o option, isNew bool) tea.Cmd {
			return func() tea.Msg {
				if isNew {
					name := o.value.(string)
					if src != nil {
						return planMsg{plan: plan.NewCollectionFrom(src, name, items, srcMembers)}
					}
					return planMsg{plan: plan.NewCollection(name, items)}
				}
				dst := o.value.(*model.Container)
				existing, err := m.members(dst)
				if err != nil {
					return planMsg{err: err}
				}
				if src != nil {
					return planMsg{plan: plan.MoveTo(src, dst, items, srcMembers, existing)}
				}
				return planMsg{plan: plan.AddTo(dst, items, existing)}
			}
		})}
	}
}

// pickLabel offers the labels used in the items' libraries (to add), or the
// labels the items carry (to remove).
func (m *Model) pickLabel(items []*model.Item, remove bool) tea.Cmd {
	return func() tea.Msg {
		counts := map[string]int{}
		if remove {
			for _, it := range items {
				for _, l := range it.Labels {
					counts[l]++
				}
			}
		} else {
			for s := range sectionsOf(items) {
				ls, err := m.cat.Labels(m.ctx, s)
				if err != nil {
					return errMsg(err)
				}
				for _, l := range ls {
					counts[l] += 0
				}
			}
			for _, it := range items {
				for _, l := range it.Labels {
					counts[l]++
				}
			}
		}
		names := make([]string, 0, len(counts))
		for l := range counts {
			names = append(names, l)
		}
		sort.Strings(names)
		var opts []option
		for _, l := range names {
			opts = append(opts, option{label: l, detail: fmt.Sprintf("on %d of %d", counts[l], len(items)), value: l})
		}
		title, newLabel := "Add label", "New label"
		if remove {
			title, newLabel = "Remove label", ""
		}
		return pickerMsg{newPicker(title, newLabel, opts, func(o option, _ bool) tea.Cmd {
			label := o.value.(string)
			if remove {
				return planCmd(plan.RemoveLabel(label, items))
			}
			return planCmd(plan.AddLabel(label, items))
		})}
	}
}

// reorder rewrites the container's order to match the current sort. It
// reorders every member, not just the ones matching the filter.
func (m *Model) reorder() tea.Cmd {
	v := m.top()
	c := v.container()
	if len(v.sortKeys) == 0 {
		m.setStatus("choose a sort first (s), then reorder", true)
		return nil
	}
	desired := append([]*model.Item(nil), v.items...)
	query.SortItems(desired, v.sortKeys)
	p := plan.Reorder(c, v.items, desired)
	p.Title = fmt.Sprintf("Reorder %s by %s", c, query.FormatSort(v.sortKeys))
	return planCmd(p)
}

// --- preview and execution ---------------------------------------------------

func (m *Model) previewKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "y", "enter":
		if m.preview.Blocked() {
			return nil
		}
		p := m.preview
		m.preview = nil
		m.mode = modeRunning
		m.progress = exec.Progress{Total: exec.Steps(p), Step: "starting…"}
		return m.execute(p)
	case "n", "esc", "q":
		m.preview = nil
		m.mode = modeNormal
		m.setStatus("cancelled", false)
	}
	return nil
}

func (m *Model) execute(p *plan.Plan) tea.Cmd {
	ch := make(chan tea.Msg, 16)
	m.progCh = ch
	go func() {
		defer close(ch)
		mid, err := m.cat.MachineID(m.ctx)
		if err != nil {
			ch <- execDoneMsg{plan: p, err: err}
			return
		}
		ex := &exec.Executor{C: m.cat.C, MachineID: mid, LogPath: filepath.Join(store.Dir(), "actions.log")}
		err = ex.Run(context.Background(), p, func(pr exec.Progress) { ch <- progressMsg(pr) })
		ch <- execDoneMsg{plan: p, err: err}
	}()
	return waitFor(ch)
}

func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// finished reloads everything: an action can change any list on screen.
func (m *Model) finished(msg execDoneMsg) tea.Cmd {
	m.mode = modeNormal
	m.cat.Invalidate()
	for _, v := range m.stack {
		v.stale = true
	}
	if msg.err != nil {
		m.setStatus("failed: "+msg.err.Error()+" (completed steps were kept; see actions.log)", true)
	} else {
		m.setStatus("done: "+msg.plan.Title, false)
		m.clearSelection()
	}
	return m.load(m.top())
}

// --- prompt ------------------------------------------------------------------

type prompt struct {
	label  string
	value  string
	submit func(string) tea.Cmd
}

func (m *Model) startPrompt() tea.Cmd {
	m.input.SetValue(m.prompt.value)
	m.input.CursorEnd()
	m.input.Focus()
	m.mode = modePrompt
	return textinput.Blink
}

func (m *Model) promptKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.mode = modeNormal
		m.input.Blur()
		return m.prompt.submit(strings.TrimSpace(m.input.Value()))
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// promptSaveQuery saves the current source, filter and sort as a query.
func (m *Model) promptSaveQuery() tea.Cmd {
	v := m.top()
	if v.kind != itemView || v.src == nil {
		return nil
	}
	m.prompt = &prompt{label: "save query as", submit: func(name string) tea.Cmd {
		if name == "" {
			return nil
		}
		filter := v.filterText
		if s, ok := v.src.(source.Saved); ok {
			filter = strings.TrimSpace(s.Query.Text + " " + filter)
		}
		var cols []string
		for _, c := range v.columns {
			cols = append(cols, c.Name)
		}
		q := store.SavedQuery{Name: name, Sources: store.SpecFor(v.src), Filter: filter,
			Sort: query.FormatSort(v.sortKeys), Columns: cols}
		if err := store.AppendQuery(q); err != nil {
			return statusCmd(err.Error(), true)
		}
		for _, sv := range m.stack {
			sv.stale = true
		}
		return statusCmd(fmt.Sprintf("saved query %q (Queries/)", name), false)
	}}
	return m.startPrompt()
}
