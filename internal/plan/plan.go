// Package plan turns a user intent ("move these items to that playlist")
// into a Plan: concrete operations plus anything that was skipped or blocked.
// Every restriction Plex imposes lives here, as pure functions, so the UI
// can preview exactly what will happen and the executor never has to decide.
package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ashleyconnor/plx/internal/model"
)

type OpKind int

const (
	OpCreatePlaylist OpKind = iota
	OpCreateCollection
	OpAdd
	OpRemove
	OpMove
	OpSetCollectionSortCustom
	OpAddLabel
	OpRemoveLabel
	OpDelete
	OpDeleteVersion
	OpRename
	OpDeleteContainer
)

// Op is one executable step. Which fields are used depends on Kind.
type Op struct {
	Kind   OpKind
	Target *model.Container // container being changed (nil for creates and labels)
	Items  []*model.Item

	Title      string // creates, OpRename (the new title)
	ListType   string // OpCreatePlaylist
	SectionID  string // OpCreateCollection, labels
	SearchType int    // OpCreateCollection, labels
	Label      string // labels

	// OpMove: Items[0] is moved after AfterMember ("" = to the top).
	AfterMember string

	// OpDeleteVersion: Versions[i] is the version of Items[i] to delete.
	Versions []model.Version
}

func (o Op) Describe() string {
	n := len(o.Items)
	switch o.Kind {
	case OpCreatePlaylist:
		return fmt.Sprintf("create %s playlist %q with %d items", o.ListType, o.Title, n)
	case OpCreateCollection:
		return fmt.Sprintf("create collection %q with %d items", o.Title, n)
	case OpAdd:
		return fmt.Sprintf("add %d items to %s", n, o.Target)
	case OpRemove:
		return fmt.Sprintf("remove %d items from %s", n, o.Target)
	case OpMove:
		return fmt.Sprintf("move %q in %s", o.Items[0].DisplayTitle(), o.Target)
	case OpSetCollectionSortCustom:
		return fmt.Sprintf("set %s to custom sort order", o.Target)
	case OpAddLabel:
		return fmt.Sprintf("add label %q to %d items", o.Label, n)
	case OpRemoveLabel:
		return fmt.Sprintf("remove label %q from %d items", o.Label, n)
	case OpDelete:
		return fmt.Sprintf("delete %d items and their files from disk", n)
	case OpDeleteVersion:
		return fmt.Sprintf("delete %d versions and their files from disk", n)
	case OpRename:
		return fmt.Sprintf("rename %s to %q", o.Target, o.Title)
	case OpDeleteContainer:
		return fmt.Sprintf("delete %s (its items are kept)", o.Target)
	}
	return "?"
}

type Severity int

const (
	Warn Severity = iota
	Block
)

type Violation struct {
	Severity Severity
	Message  string
}

type Skip struct {
	Item   *model.Item
	Reason string
}

type Plan struct {
	Title      string
	Ops        []Op
	Violations []Violation
	Skipped    []Skip
}

func (p *Plan) Blocked() bool {
	for _, v := range p.Violations {
		if v.Severity == Block {
			return true
		}
	}
	return false
}

func (p *Plan) block(format string, a ...any) {
	p.Violations = append(p.Violations, Violation{Block, fmt.Sprintf(format, a...)})
}

func (p *Plan) warn(format string, a ...any) {
	p.Violations = append(p.Violations, Violation{Warn, fmt.Sprintf(format, a...)})
}

func (p *Plan) skip(it *model.Item, format string, a ...any) {
	p.Skipped = append(p.Skipped, Skip{it, fmt.Sprintf(format, a...)})
}

// Destructive reports whether applying the plan can't be undone.
func (p *Plan) Destructive() bool {
	for _, o := range p.Ops {
		if o.Kind == OpDelete || o.Kind == OpDeleteVersion || o.Kind == OpDeleteContainer {
			return true
		}
	}
	return false
}

// DeletesFiles reports whether applying the plan removes files from disk.
func (p *Plan) DeletesFiles() bool {
	for _, o := range p.Ops {
		if o.Kind == OpDelete || o.Kind == OpDeleteVersion {
			return true
		}
	}
	return false
}

// SkipSummary groups skipped items by reason: "3 × already in playlist".
func (p *Plan) SkipSummary() []string {
	counts := map[string]int{}
	var order []string
	for _, s := range p.Skipped {
		if counts[s.Reason] == 0 {
			order = append(order, s.Reason)
		}
		counts[s.Reason]++
	}
	out := make([]string, len(order))
	for i, r := range order {
		out[i] = fmt.Sprintf("%d × %s", counts[r], r)
	}
	return out
}

// MoveCount is the number of OpMove steps, for progress and previews.
func (p *Plan) MoveCount() int {
	n := 0
	for _, o := range p.Ops {
		if o.Kind == OpMove {
			n++
		}
	}
	return n
}

// dedupe drops repeated items, keeping the first.
func dedupe(items []*model.Item) []*model.Item {
	seen := map[string]bool{}
	out := make([]*model.Item, 0, len(items))
	for _, it := range items {
		if !seen[it.Key] {
			seen[it.Key] = true
			out = append(out, it)
		}
	}
	return out
}

func memberSet(items []*model.Item) map[string]*model.Item {
	m := make(map[string]*model.Item, len(items))
	for _, it := range items {
		m[it.Key] = it
	}
	return m
}

// --- membership --------------------------------------------------------------

// AddTo adds items to a container. existing is the container's current
// members (used to skip duplicates); it may be nil for labels.
func AddTo(dst *model.Container, items, existing []*model.Item) *Plan {
	p := &Plan{Title: "Add to " + dst.String()}
	if dst.Kind == model.Label {
		return addLabel(p, dst.ID, items)
	}
	if dst.Caps().ReadOnly {
		p.block("%s is smart: its contents are computed by Plex", dst)
		return p
	}
	have := memberSet(existing)
	var ok []*model.Item
	for _, it := range dedupe(items) {
		if reason := admits(dst, it); reason != "" {
			p.skip(it, "%s", reason)
			continue
		}
		if have[it.Key] != nil {
			p.skip(it, "already in %s", dst.Kind)
			continue
		}
		ok = append(ok, it)
	}
	if len(ok) == 0 {
		p.block("nothing to add")
		return p
	}
	p.Ops = append(p.Ops, Op{Kind: OpAdd, Target: dst, Items: ok})
	return p
}

// admits returns why an item can't go in a container, or "".
func admits(dst *model.Container, it *model.Item) string {
	switch dst.Kind {
	case model.Playlist:
		if lt := it.Kind.ListType(); lt != dst.ListType {
			return fmt.Sprintf("%s can't go in a %s playlist", it.Kind, dst.ListType)
		}
	case model.Collection:
		if it.SectionID != dst.SectionID {
			return "collection is in a different library"
		}
	}
	return ""
}

// RemoveFrom removes items from the container they were listed from. Items
// must come from that container's listing so they carry a member ID.
func RemoveFrom(src *model.Container, items, members []*model.Item) *Plan {
	p := &Plan{Title: "Remove from " + src.String()}
	if src.Kind == model.Label {
		return removeLabel(p, src.ID, items)
	}
	if src.Caps().ReadOnly {
		p.block("%s is smart: its contents are computed by Plex", src)
		return p
	}
	ok := resolveMembers(p, src, items, members)
	if len(ok) == 0 {
		p.block("nothing to remove")
		return p
	}
	p.Ops = append(p.Ops, Op{Kind: OpRemove, Target: src, Items: ok})
	return p
}

// resolveMembers maps items onto the container's member entries (which
// carry member IDs). For playlists every duplicate entry is included.
func resolveMembers(p *Plan, src *model.Container, items, members []*model.Item) []*model.Item {
	byKey := map[string][]*model.Item{}
	for _, m := range members {
		byKey[m.Key] = append(byKey[m.Key], m)
	}
	var out []*model.Item
	for _, it := range dedupe(items) {
		ms := byKey[it.Key]
		if len(ms) == 0 {
			p.skip(it, "not in %s", src.Kind)
			continue
		}
		out = append(out, ms...)
	}
	return out
}

// MoveTo adds items to dst and removes them from src. Only items that are
// actually added (or already present in dst) are removed, so nothing is lost.
func MoveTo(src, dst *model.Container, items, srcMembers, dstMembers []*model.Item) *Plan {
	p := &Plan{Title: fmt.Sprintf("Move from %s to %s", src, dst)}
	if src.Kind == dst.Kind && src.ID == dst.ID && src.SectionID == dst.SectionID {
		p.block("source and destination are the same")
		return p
	}
	if src.Caps().ReadOnly {
		p.block("%s is smart: items can't be removed from it", src)
		return p
	}
	add := AddTo(dst, items, dstMembers)
	if add.Blocked() && dst.Caps().ReadOnly {
		return add
	}
	p.Ops = append(p.Ops, add.Ops...)
	p.Violations = append(p.Violations, nonEmpty(add.Violations)...)

	// Items skipped only because dst already has them are still moved out of src.
	movable := map[string]bool{}
	for _, op := range add.Ops {
		for _, it := range op.Items {
			movable[it.Key] = true
		}
	}
	for _, s := range add.Skipped {
		if strings.HasPrefix(s.Reason, "already ") {
			movable[s.Item.Key] = true
			continue
		}
		p.Skipped = append(p.Skipped, s)
	}
	var toRemove []*model.Item
	for _, it := range items {
		if movable[it.Key] {
			toRemove = append(toRemove, it)
		}
	}
	if len(toRemove) == 0 {
		p.block("nothing to move")
		return p
	}
	rm := RemoveFrom(src, toRemove, srcMembers)
	p.Ops = append(p.Ops, rm.Ops...)
	p.Skipped = append(p.Skipped, rm.Skipped...)
	p.Violations = append(p.Violations, nonEmpty(rm.Violations)...)
	return p
}

// nonEmpty drops "nothing to …" blocks from sub-plans; the caller decides.
func nonEmpty(vs []Violation) []Violation {
	var out []Violation
	for _, v := range vs {
		if !strings.HasPrefix(v.Message, "nothing to") {
			out = append(out, v)
		}
	}
	return out
}

// --- creation ----------------------------------------------------------------

// NewPlaylist creates a playlist from items, in order. A playlist holds one
// media type, so the type of the first item wins and others are skipped.
func NewPlaylist(title string, items []*model.Item) *Plan {
	p := &Plan{Title: fmt.Sprintf("New playlist %q", title)}
	if strings.TrimSpace(title) == "" {
		p.block("playlist needs a title")
		return p
	}
	items = dedupe(items)
	if len(items) == 0 {
		p.block("no items")
		return p
	}
	lt := ""
	var ok []*model.Item
	for _, it := range items {
		t := it.Kind.ListType()
		if t == "" {
			p.skip(it, "%s can't go in a playlist", it.Kind)
			continue
		}
		if lt == "" {
			lt = t
		}
		if t != lt {
			p.skip(it, "%s can't go in a %s playlist", it.Kind, lt)
			continue
		}
		ok = append(ok, it)
	}
	if len(ok) == 0 {
		p.block("no items can go in a playlist")
		return p
	}
	p.Ops = append(p.Ops, Op{Kind: OpCreatePlaylist, Title: title, ListType: lt, Items: ok})
	return p
}

// NewCollection creates a collection. Collections live in one library and
// are created with one item type, so the first item decides both.
func NewCollection(title string, items []*model.Item) *Plan {
	p := &Plan{Title: fmt.Sprintf("New collection %q", title)}
	if strings.TrimSpace(title) == "" {
		p.block("collection needs a title")
		return p
	}
	items = dedupe(items)
	if len(items) == 0 {
		p.block("no items")
		return p
	}
	first := items[0]
	var ok []*model.Item
	for _, it := range items {
		switch {
		case it.SectionID != first.SectionID:
			p.skip(it, "in a different library than %q", first.SectionTitle)
		case it.Kind != first.Kind:
			p.skip(it, "a collection is created with one item type (%s)", first.Kind)
		default:
			ok = append(ok, it)
		}
	}
	p.Ops = append(p.Ops, Op{Kind: OpCreateCollection, Title: title, SectionID: first.SectionID,
		SearchType: first.Kind.SearchType(), Items: ok})
	return p
}

// NewPlaylistFrom creates a playlist and removes the items from src: "move
// these into a new playlist".
func NewPlaylistFrom(src *model.Container, title string, items, srcMembers []*model.Item) *Plan {
	p := NewPlaylist(title, items)
	if p.Blocked() {
		return p
	}
	p.Title = fmt.Sprintf("Move from %s to new playlist %q", src, title)
	if src.Caps().ReadOnly {
		p.block("%s is smart: items can't be removed from it", src)
		return p
	}
	rm := RemoveFrom(src, p.Ops[0].Items, srcMembers)
	p.Ops = append(p.Ops, rm.Ops...)
	p.Skipped = append(p.Skipped, rm.Skipped...)
	return p
}

// NewCollectionFrom creates a collection and removes the items from src.
func NewCollectionFrom(src *model.Container, title string, items, srcMembers []*model.Item) *Plan {
	p := NewCollection(title, items)
	if p.Blocked() {
		return p
	}
	p.Title = fmt.Sprintf("Move from %s to new collection %q", src, title)
	if src.Caps().ReadOnly {
		p.block("%s is smart: items can't be removed from it", src)
		return p
	}
	rm := RemoveFrom(src, p.Ops[0].Items, srcMembers)
	p.Ops = append(p.Ops, rm.Ops...)
	p.Skipped = append(p.Skipped, rm.Skipped...)
	return p
}

// --- containers --------------------------------------------------------------

// editable blocks changes to anything but a playlist or collection. Smart
// ones can be renamed and deleted too: only their contents are computed.
func editable(p *Plan, c *model.Container) bool {
	if c == nil || (c.Kind != model.Playlist && c.Kind != model.Collection) {
		p.block("only playlists and collections can be renamed or deleted")
		return false
	}
	return true
}

// Rename gives a playlist or collection a new title.
func Rename(c *model.Container, title string) *Plan {
	title = strings.TrimSpace(title)
	p := &Plan{Title: fmt.Sprintf("Rename %s", c)}
	if !editable(p, c) {
		return p
	}
	switch {
	case title == "":
		p.block("%s needs a title", c.Kind)
	case title == c.Title:
		p.block("the title is unchanged")
	default:
		p.Ops = append(p.Ops, Op{Kind: OpRename, Target: c, Title: title})
	}
	return p
}

// DeleteContainer deletes a playlist or collection. Its items stay in the
// library.
func DeleteContainer(c *model.Container) *Plan {
	p := &Plan{Title: fmt.Sprintf("Delete %s", c)}
	if !editable(p, c) {
		return p
	}
	p.Ops = append(p.Ops, Op{Kind: OpDeleteContainer, Target: c})
	return p
}

// --- labels ------------------------------------------------------------------

// Labels are per library and edited with one request per section and item
// type, so items are partitioned accordingly.
func addLabel(p *Plan, label string, items []*model.Item) *Plan {
	var ok []*model.Item
	for _, it := range dedupe(items) {
		if it.HasLabel(label) {
			p.skip(it, "already labelled")
			continue
		}
		ok = append(ok, it)
	}
	return labelOps(p, OpAddLabel, label, ok, "nothing to label")
}

func removeLabel(p *Plan, label string, items []*model.Item) *Plan {
	var ok []*model.Item
	for _, it := range dedupe(items) {
		if !it.HasLabel(label) {
			p.skip(it, "doesn't have the label")
			continue
		}
		ok = append(ok, it)
	}
	return labelOps(p, OpRemoveLabel, label, ok, "nothing to unlabel")
}

func labelOps(p *Plan, kind OpKind, label string, items []*model.Item, empty string) *Plan {
	if strings.TrimSpace(label) == "" {
		p.block("label can't be empty")
		return p
	}
	type part struct {
		section string
		typ     int
	}
	groups := map[part][]*model.Item{}
	var order []part
	for _, it := range items {
		k := part{it.SectionID, it.Kind.SearchType()}
		if groups[k] == nil {
			order = append(order, k)
		}
		groups[k] = append(groups[k], it)
	}
	if len(order) == 0 {
		p.block("%s", empty)
		return p
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].section < order[j].section })
	for _, k := range order {
		p.Ops = append(p.Ops, Op{Kind: kind, Label: label, SectionID: k.section, SearchType: k.typ, Items: groups[k]})
	}
	return p
}

func AddLabel(label string, items []*model.Item) *Plan {
	return addLabel(&Plan{Title: fmt.Sprintf("Add label %q", label)}, label, items)
}

func RemoveLabel(label string, items []*model.Item) *Plan {
	return removeLabel(&Plan{Title: fmt.Sprintf("Remove label %q", label)}, label, items)
}
