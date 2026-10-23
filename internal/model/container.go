package model

import "fmt"

type ContainerKind int

const (
	Playlist ContainerKind = iota
	Collection
	Label
)

func (k ContainerKind) String() string {
	switch k {
	case Playlist:
		return "playlist"
	case Collection:
		return "collection"
	case Label:
		return "label"
	}
	return "?"
}

// Container is anything items can be members of. Playlists, collections and
// labels look the same to the user; their differences are expressed as Caps
// and enforced by the planner.
type Container struct {
	Kind      ContainerKind
	ID        string // ratingKey for playlists/collections, label name for labels
	Title     string
	SectionID string // collections and labels belong to one section
	ListType  string // playlists: video, audio or photo
	Subtype   Kind   // collections: the item type they hold
	Smart     bool
	Count     int
}

// Caps describes what a container allows.
type Caps struct {
	Ordered      bool // has a manual order that can be changed
	ReadOnly     bool // membership is computed (smart)
	CrossSection bool // may hold items from several sections
	MixedKinds   bool // may hold different item kinds
}

func (c *Container) Caps() Caps {
	switch c.Kind {
	case Playlist:
		return Caps{Ordered: true, ReadOnly: c.Smart, CrossSection: true}
	case Collection:
		return Caps{Ordered: true, ReadOnly: c.Smart, MixedKinds: true}
	default:
		return Caps{MixedKinds: true}
	}
}

func (c *Container) String() string {
	return fmt.Sprintf("%s %q", c.Kind, c.Title)
}
