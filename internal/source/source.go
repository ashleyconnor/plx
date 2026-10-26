package source

import (
	"context"
	"fmt"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/query"
)

// Source is anything that yields a list of items.
type Source interface {
	ID() string    // stable cache key, e.g. "playlist:123"
	Title() string // for breadcrumbs
	// Container is the container the items are members of, or nil.
	Container() *model.Container
	load(ctx context.Context, c *Catalog) ([]*model.Item, error)
}

// Library lists every playable item in a section.
type Library struct{ Section Section }

func (s Library) ID() string                  { return "section:" + s.Section.ID }
func (s Library) Title() string               { return s.Section.Title }
func (s Library) Container() *model.Container { return nil }
func (s Library) load(ctx context.Context, c *Catalog) ([]*model.Item, error) {
	ms, err := c.C.SectionItems(ctx, s.Section.ID, plex.SearchType[string(s.Section.LeafKind())])
	if err != nil {
		return nil, err
	}
	return convert(ms, nil), nil
}

// Members lists a playlist's or collection's items.
type Members struct{ C *model.Container }

func (s Members) ID() string {
	if s.C.Kind == model.Label { // a label name is only unique within its library
		return "label:" + s.C.SectionID + ":" + s.C.ID
	}
	return s.C.Kind.String() + ":" + s.C.ID
}
func (s Members) Title() string               { return s.C.Title }
func (s Members) Container() *model.Container { return s.C }
func (s Members) load(ctx context.Context, c *Catalog) ([]*model.Item, error) {
	switch s.C.Kind {
	case model.Playlist:
		ms, err := c.C.PlaylistItems(ctx, s.C.ID)
		if err != nil {
			return nil, err
		}
		return convert(ms, func(m *plex.Metadata) string { return string(m.PlaylistItemID) }), nil
	case model.Collection:
		ms, err := c.C.CollectionItems(ctx, s.C.ID)
		if err != nil {
			return nil, err
		}
		for i := range ms {
			if ms[i].LibrarySectionID == "" {
				ms[i].LibrarySectionID = plex.FlexString(s.C.SectionID)
			}
		}
		return convert(ms, func(m *plex.Metadata) string { return string(m.RatingKey) }), nil
	case model.Label:
		sec, err := c.Section(ctx, s.C.SectionID)
		if err != nil {
			return nil, err
		}
		all, err := c.Items(ctx, Library{sec})
		if err != nil {
			return nil, err
		}
		var out []*model.Item
		for _, it := range all {
			if it.HasLabel(s.C.ID) {
				cp := *it
				cp.MemberID = it.Key
				out = append(out, &cp)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown container kind %v", s.C.Kind)
}

// Saved is a saved query: the union of its sources, filtered and sorted.
type Saved struct {
	Name    string
	Sources []Source
	Query   *query.Query
}

func (s Saved) ID() string                  { return "query:" + s.Name }
func (s Saved) Title() string               { return s.Name }
func (s Saved) Container() *model.Container { return nil }
func (s Saved) load(ctx context.Context, c *Catalog) ([]*model.Item, error) {
	seen := map[string]bool{}
	var all []*model.Item
	for _, src := range s.Sources {
		items, err := c.Items(ctx, src)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if !seen[it.Key] {
				seen[it.Key] = true
				all = append(all, it)
			}
		}
	}
	if s.Query.UsesMembership() {
		if err := c.EnsureMembership(ctx, all); err != nil {
			return nil, err
		}
	}
	return s.Query.Filter(all), nil
}
