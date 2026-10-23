// Package source turns everything browsable (library sections, playlists,
// collections, labels, saved queries) into the same thing: a list of items.
package source

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plex"
)

// Section is a library section.
type Section struct {
	ID, Title, Type string // Type is the Plex section type: movie, show, artist, photo
}

// LeafKind is the kind of playable item the section lists.
func (s Section) LeafKind() model.Kind { return model.Kind(plex.LeafType[s.Type]) }

// Catalog wraps the client with caching. It is safe for concurrent use.
type Catalog struct {
	C *plex.Client

	mu        sync.Mutex
	machineID string
	sections  []Section
	items     map[string][]*model.Item       // by source ID
	labelIdx  map[string]map[string][]string // section ID → ratingKey → labels
	labelList map[string][]string            // section ID → label names
}

func NewCatalog(c *plex.Client) *Catalog {
	return &Catalog{C: c, items: map[string][]*model.Item{}, labelIdx: map[string]map[string][]string{}, labelList: map[string][]string{}}
}

func (c *Catalog) MachineID(ctx context.Context) (string, error) {
	c.mu.Lock()
	id := c.machineID
	c.mu.Unlock()
	if id != "" {
		return id, nil
	}
	id, err := c.C.Identity(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.machineID = id
	c.mu.Unlock()
	return id, nil
}

func (c *Catalog) Sections(ctx context.Context) ([]Section, error) {
	c.mu.Lock()
	ss := c.sections
	c.mu.Unlock()
	if ss != nil {
		return ss, nil
	}
	dirs, err := c.C.Sections(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if _, ok := plex.LeafType[d.Type]; ok {
			ss = append(ss, Section{ID: d.Key, Title: d.Title, Type: d.Type})
		}
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].Title < ss[j].Title })
	c.mu.Lock()
	c.sections = ss
	c.mu.Unlock()
	return ss, nil
}

func (c *Catalog) Section(ctx context.Context, idOrTitle string) (Section, error) {
	ss, err := c.Sections(ctx)
	if err != nil {
		return Section{}, err
	}
	for _, s := range ss {
		if s.ID == idOrTitle {
			return s, nil
		}
	}
	for _, s := range ss {
		if strings.EqualFold(s.Title, idOrTitle) {
			return s, nil
		}
	}
	return Section{}, fmt.Errorf("no library section %q", idOrTitle)
}

// Playlists and collections are listed fresh each time: they are cheap and
// change underneath us when actions run.
func (c *Catalog) Playlists(ctx context.Context) ([]*model.Container, error) {
	ms, err := c.C.Playlists(ctx)
	if err != nil {
		return nil, err
	}
	var out []*model.Container
	for _, m := range ms {
		out = append(out, &model.Container{Kind: model.Playlist, ID: string(m.RatingKey), Title: m.Title,
			ListType: m.PlaylistType, Smart: bool(m.Smart), Count: int(m.LeafCount)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out, nil
}

func (c *Catalog) Collections(ctx context.Context, sectionID string) ([]*model.Container, error) {
	ms, err := c.C.Collections(ctx, sectionID)
	if err != nil {
		return nil, err
	}
	var out []*model.Container
	for _, m := range ms {
		out = append(out, &model.Container{Kind: model.Collection, ID: string(m.RatingKey), Title: m.Title,
			SectionID: sectionID, Subtype: model.Kind(m.Subtype), Smart: bool(m.Smart), Count: int(m.ChildCount)})
	}
	return out, nil
}

// AllCollections lists collections across every section.
func (c *Catalog) AllCollections(ctx context.Context) ([]*model.Container, error) {
	ss, err := c.Sections(ctx)
	if err != nil {
		return nil, err
	}
	var out []*model.Container
	for _, s := range ss {
		cs, err := c.Collections(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

// Labels lists the label names used in a section.
func (c *Catalog) Labels(ctx context.Context, sectionID string) ([]string, error) {
	if err := c.ensureLabels(ctx, sectionID); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.labelList[sectionID], nil
}

// ensureLabels builds the section's ratingKey → labels index. Plex leaves
// labels out of section listings, so the index is built by asking for the
// items carrying each label, for every type that can be labelled.
func (c *Catalog) ensureLabels(ctx context.Context, sectionID string) error {
	c.mu.Lock()
	_, ok := c.labelIdx[sectionID]
	c.mu.Unlock()
	if ok {
		return nil
	}
	sec, err := c.Section(ctx, sectionID)
	if err != nil {
		return err
	}
	tags, err := c.C.SectionTags(ctx, sectionID, "label")
	if err != nil {
		return err
	}
	types := []int{plex.SearchType[string(sec.LeafKind())]}
	if sec.Type == "show" {
		types = append(types, plex.SearchType["show"])
	}
	idx := map[string][]string{}
	var names []string
	for _, t := range tags {
		names = append(names, t.Title)
		for _, typ := range types {
			keys, err := c.C.SectionKeysWithTag(ctx, sectionID, typ, "label", t.Key)
			if err != nil {
				return err
			}
			for _, k := range keys {
				idx[k] = append(idx[k], t.Title)
			}
		}
	}
	sort.Strings(names)
	c.mu.Lock()
	c.labelIdx[sectionID] = idx
	c.labelList[sectionID] = names
	c.mu.Unlock()
	return nil
}

// enrich fills in labels from the index; episodes inherit their show's labels.
func (c *Catalog) enrich(ctx context.Context, items []*model.Item) error {
	secs := map[string]bool{}
	for _, it := range items {
		if it.SectionID != "" {
			secs[it.SectionID] = true
		}
	}
	for s := range secs {
		if err := c.ensureLabels(ctx, s); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, it := range items {
		idx := c.labelIdx[it.SectionID]
		if idx == nil {
			continue
		}
		labels := append([]string(nil), idx[it.Key]...)
		for _, l := range idx[it.ShowKey] {
			if !contains(labels, l) {
				labels = append(labels, l)
			}
		}
		sort.Strings(labels)
		it.Labels = labels
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Items loads a source's items, from cache when possible.
func (c *Catalog) Items(ctx context.Context, s Source) ([]*model.Item, error) {
	id := s.ID()
	c.mu.Lock()
	cached, ok := c.items[id]
	c.mu.Unlock()
	if ok {
		return cached, nil
	}
	items, err := s.load(ctx, c)
	if err != nil {
		return nil, err
	}
	if err := c.enrich(ctx, items); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.items[id] = items
	c.mu.Unlock()
	return items, nil
}

// Invalidate drops cached items for the given sources, or everything if none
// are given. Label indexes are dropped too since actions may change labels.
func (c *Catalog) Invalidate(ids ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(ids) == 0 {
		c.items = map[string][]*model.Item{}
	}
	for _, id := range ids {
		delete(c.items, id)
	}
	c.labelIdx = map[string]map[string][]string{}
	c.labelList = map[string][]string{}
}

func convert(ms []plex.Metadata, memberID func(*plex.Metadata) string) []*model.Item {
	out := make([]*model.Item, 0, len(ms))
	for i := range ms {
		it := model.FromPlex(&ms[i])
		if memberID != nil {
			it.MemberID = memberID(&ms[i])
		}
		out = append(out, it)
	}
	return out
}
