package tui

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/query"
	"github.com/ashleyconnor/plx/internal/source"
)

type viewKind int

const (
	dirView  viewKind = iota // a list of places to go
	itemView                 // the items of a source
)

// row is one line in a view: a directory entry, a folder (folder lens) or an item.
type row struct {
	icon          string
	label, detail string
	push          func() *view // directory entries

	item *model.Item

	folder string        // folder rows: full path
	under  []*model.Item // folder rows: every visible item below it
	size   int64
}

func (r row) isFolder() bool { return r.folder != "" }

type view struct {
	kind  viewKind
	title string

	loader  func(ctx context.Context) ([]row, error)         // dir views
	resolve func(ctx context.Context) (source.Source, error) // item views without a src yet
	src     source.Source                                    // item views
	columns []*field.Field

	all   []row         // dir views: every entry
	items []*model.Item // item views: every item, in source order

	filterText string
	filter     *query.Query
	filterErr  string
	sortKeys   []query.SortKey

	lens       bool // folder lens
	folderRoot string
	folder     string

	rows           []row
	cursor, offset int

	selection bool // the selection, not a source
	indexing  bool // collection/playlist membership is being indexed

	loading, stale bool
	err            error
}

func (v *view) container() *model.Container {
	if v.src == nil {
		return nil
	}
	return v.src.Container()
}

func (v *view) setFilter(text string) {
	v.filterText = text
	if v.kind == itemView {
		q, err := query.Parse(text)
		if err != nil {
			v.filterErr = err.Error()
			return
		}
		v.filterErr = ""
		v.filter = q
		if strings.TrimSpace(text) == "" {
			v.filter = nil
		}
	}
	v.rebuild()
}

// visible is the filtered, sorted item list (ignoring the folder lens).
func (v *view) visible() []*model.Item {
	var items []*model.Item
	if v.filter != nil {
		items = v.filter.Filter(v.items)
	} else {
		items = append([]*model.Item(nil), v.items...)
	}
	if len(v.sortKeys) > 0 {
		query.SortItems(items, v.sortKeys)
	}
	return items
}

func (v *view) rebuild() {
	if v.kind == dirView {
		needle := strings.ToLower(v.filterText)
		v.rows = v.rows[:0]
		for _, r := range v.all {
			if needle == "" || strings.Contains(strings.ToLower(r.label), needle) {
				v.rows = append(v.rows, r)
			}
		}
	} else if v.indexing && v.filter != nil && v.filter.UsesMembership() {
		v.rows = nil // results would be wrong until membership is indexed
	} else {
		items := v.visible()
		if v.lens {
			v.rows = v.folderRows(items)
		} else {
			v.rows = make([]row, len(items))
			for i, it := range items {
				v.rows[i] = row{item: it}
			}
		}
	}
	v.clamp()
}

func (v *view) clamp() {
	if v.cursor >= len(v.rows) {
		v.cursor = len(v.rows) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *view) current() (row, bool) {
	if v.cursor < len(v.rows) {
		return v.rows[v.cursor], true
	}
	return row{}, false
}

// folderRows shows the immediate subfolders of the current folder (with the
// count and size of everything below them) followed by the items directly in
// it. The root is the deepest folder containing every visible item, so
// filtering narrows the tree automatically.
func (v *view) folderRows(items []*model.Item) []row {
	v.folderRoot = commonDir(items)
	if v.folder == "" || !under(v.folder, v.folderRoot) {
		v.folder = v.folderRoot
	}
	cwd := v.folder
	subs := map[string]*row{}
	var names []string
	var direct []row
	for _, it := range items {
		d := it.Dir()
		if d == cwd || d == "" {
			direct = append(direct, row{item: it})
			continue
		}
		if !under(d, cwd) {
			continue
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(d, cwd), "/")
		name, _, _ := strings.Cut(rest, "/")
		r := subs[name]
		if r == nil {
			r = &row{label: name + "/", folder: path.Join(cwd, name)}
			subs[name] = r
			names = append(names, name)
		}
		r.under = append(r.under, it)
		r.size += it.Size
	}
	bySize := len(v.sortKeys) > 0 && v.sortKeys[0].Field == field.Size
	sort.Slice(names, func(i, j int) bool {
		if bySize {
			a, b := subs[names[i]].size, subs[names[j]].size
			if v.sortKeys[0].Desc {
				return a > b
			}
			return a < b
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	out := make([]row, 0, len(names)+len(direct))
	for _, n := range names {
		out = append(out, *subs[n])
	}
	return append(out, direct...)
}

func under(p, dir string) bool {
	return p == dir || dir == "/" || strings.HasPrefix(p, dir+"/")
}

func commonDir(items []*model.Item) string {
	var common []string
	first := true
	for _, it := range items {
		d := it.Dir()
		if d == "" {
			continue
		}
		parts := strings.Split(d, "/")
		if first {
			common, first = parts, false
			continue
		}
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) == 0 {
		return "/"
	}
	if j := strings.Join(common, "/"); j != "" {
		return j
	}
	return "/"
}
