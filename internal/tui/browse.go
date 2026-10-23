package tui

import (
	"context"
	"fmt"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/query"
	"github.com/ashleyconnor/plx/internal/source"
	"github.com/ashleyconnor/plx/internal/store"
)

// The browse tree:
//
//	Libraries/<section>                 every item in a section
//	Playlists/<playlist>
//	Collections/<section>/<collection>
//	Labels/<section>/<label>
//	Queries/<saved query>

func (m *Model) rootView() *view {
	dir := func(icon, label, detail string, push func() *view) row {
		return row{icon: icon, label: label + "/", detail: detail, push: push}
	}
	return &view{kind: dirView, title: "plx", loader: func(context.Context) ([]row, error) {
		return []row{
			dir(icons.library, "Libraries", "every item in a library", m.librariesView),
			dir(icons.playlist, "Playlists", "", m.playlistsView),
			dir(icons.collection, "Collections", "by library", func() *view { return m.perSectionView("Collections", icons.collection, m.collectionsView) }),
			dir(icons.label, "Labels", "by library", func() *view { return m.perSectionView("Labels", icons.label, m.labelsView) }),
			dir(icons.query, "Queries", "saved queries", m.queriesView),
		}, nil
	}}
}

func defaultColumns() []*field.Field {
	return []*field.Field{field.Size, field.Dur, field.Res, field.Plays, field.Label}
}

func (m *Model) itemsView(src source.Source, sortKeys []query.SortKey) *view {
	return &view{kind: itemView, title: src.Title(), src: src, columns: defaultColumns(), sortKeys: sortKeys}
}

func (m *Model) librariesView() *view {
	return &view{kind: dirView, title: "Libraries", loader: func(ctx context.Context) ([]row, error) {
		secs, err := m.cat.Sections(ctx)
		if err != nil {
			return nil, err
		}
		var rows []row
		for _, s := range secs {
			rows = append(rows, row{icon: icons.library, label: s.Title + "/", detail: string(s.LeafKind()) + "s", push: func() *view {
				return m.itemsView(source.Library{Section: s}, []query.SortKey{{Field: field.Title}})
			}})
		}
		return rows, nil
	}}
}

func containerDetail(c *model.Container) string {
	d := fmt.Sprintf("%d items", c.Count)
	if c.ListType != "" {
		d += " · " + c.ListType
	}
	if c.Smart {
		d += " · smart"
	}
	return d
}

func (m *Model) playlistsView() *view {
	return &view{kind: dirView, title: "Playlists", loader: func(ctx context.Context) ([]row, error) {
		pls, err := m.cat.Playlists(ctx)
		if err != nil {
			return nil, err
		}
		var rows []row
		for _, p := range pls {
			rows = append(rows, row{icon: icons.playlist, label: p.Title + "/", detail: containerDetail(p), push: func() *view {
				return m.itemsView(source.Members{C: p}, nil)
			}})
		}
		return rows, nil
	}}
}

func (m *Model) perSectionView(title, icon string, child func(source.Section) *view) *view {
	return &view{kind: dirView, title: title, loader: func(ctx context.Context) ([]row, error) {
		secs, err := m.cat.Sections(ctx)
		if err != nil {
			return nil, err
		}
		var rows []row
		for _, s := range secs {
			rows = append(rows, row{icon: icon, label: s.Title + "/", push: func() *view { return child(s) }})
		}
		return rows, nil
	}}
}

func (m *Model) collectionsView(s source.Section) *view {
	return &view{kind: dirView, title: s.Title, loader: func(ctx context.Context) ([]row, error) {
		cs, err := m.cat.Collections(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		var rows []row
		for _, c := range cs {
			rows = append(rows, row{icon: icons.collection, label: c.Title + "/", detail: containerDetail(c), push: func() *view {
				return m.itemsView(source.Members{C: c}, nil)
			}})
		}
		return rows, nil
	}}
}

func (m *Model) labelsView(s source.Section) *view {
	return &view{kind: dirView, title: s.Title, loader: func(ctx context.Context) ([]row, error) {
		labels, err := m.cat.Labels(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		var rows []row
		for _, l := range labels {
			c := &model.Container{Kind: model.Label, ID: l, Title: l, SectionID: s.ID}
			rows = append(rows, row{icon: icons.label, label: l + "/", push: func() *view {
				return m.itemsView(source.Members{C: c}, []query.SortKey{{Field: field.Title}})
			}})
		}
		return rows, nil
	}}
}

func (m *Model) queriesView() *view {
	return &view{kind: dirView, title: "Queries", loader: func(ctx context.Context) ([]row, error) {
		qs, err := store.LoadQueries()
		if err != nil {
			return nil, err
		}
		var rows []row
		for _, q := range qs {
			rows = append(rows, row{icon: icons.query, label: q.Name + "/", detail: q.Filter, push: func() *view {
				v := &view{kind: itemView, title: q.Name, columns: defaultColumns()}
				if len(q.Columns) > 0 {
					v.columns = nil
					for _, name := range q.Columns {
						if f, ok := field.Lookup(name); ok && f != field.Title { // title is always shown
							v.columns = append(v.columns, f)
						}
					}
				}
				v.resolve = func(ctx context.Context) (source.Source, error) { return store.Resolve(ctx, m.cat, q) }
				return v
			}})
		}
		if len(rows) == 0 {
			rows = append(rows, row{label: "(none yet: press S in any item list to save one)"})
		}
		return rows, nil
	}}
}
