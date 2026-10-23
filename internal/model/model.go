// Package model holds the domain types the rest of plx works with. Items are
// built once from plex DTOs; derived attributes live in the field registry,
// not here.
package model

import (
	"fmt"
	"path"
	"time"

	"github.com/ashleyconnor/plx/internal/plex"
)

// Kind is the Plex item type: movie, episode, track, photo, show, ...
type Kind string

// ListType is the playlist type an item can go into: video, audio or photo.
func (k Kind) ListType() string {
	switch k {
	case "movie", "episode", "clip", "show", "season":
		return "video"
	case "track", "album", "artist":
		return "audio"
	case "photo":
		return "photo"
	}
	return ""
}

func (k Kind) SearchType() int { return plex.SearchType[string(k)] }

type Item struct {
	Key          string // ratingKey
	Kind         Kind
	SectionID    string
	SectionTitle string

	Title     string
	SortTitle string
	Show      string // grandparent title for episodes, artist for tracks
	ShowKey   string
	SeasonKey string
	Season    string
	SeasonNum int
	Index     int // episode / track number
	Year      int

	Size       int64
	Duration   time.Duration
	Bitrate    int // kbps
	Resolution string
	VideoCodec string
	Container  string
	Files      []string

	ViewCount  int
	ViewOffset time.Duration
	LastViewed time.Time
	Added      time.Time

	Labels      []string
	Genres      []string
	Collections []string // titles of collections containing the item (or its show/season)
	Playlists   []string // titles of playlists containing the item

	// MemberID identifies the item inside the container it was listed from:
	// playlistItemID for playlists, ratingKey for collections. Empty otherwise.
	MemberID string

	Raw *plex.Metadata
}

func (i *Item) Watched() bool    { return i.ViewCount > 0 }
func (i *Item) InProgress() bool { return i.ViewOffset > 0 }

// Path is the first file of the item, or "".
func (i *Item) Path() string {
	if len(i.Files) == 0 {
		return ""
	}
	return i.Files[0]
}

func (i *Item) Dir() string {
	if p := i.Path(); p != "" {
		return path.Dir(p)
	}
	return ""
}

// DisplayTitle includes the show and episode number for episodes.
func (i *Item) DisplayTitle() string {
	switch i.Kind {
	case "episode":
		return fmt.Sprintf("%s S%02dE%02d · %s", i.Show, i.SeasonNum, i.Index, i.Title)
	case "track":
		return fmt.Sprintf("%s · %s", i.Show, i.Title)
	}
	return i.Title
}

func (i *Item) HasLabel(l string) bool {
	for _, x := range i.Labels {
		if x == l {
			return true
		}
	}
	return false
}

// FromPlex converts a playable item. File sizes are summed across distinct
// files: Plex can repeat the same part under multiple Media entries.
func FromPlex(m *plex.Metadata) *Item {
	it := &Item{
		Key:          string(m.RatingKey),
		Kind:         Kind(m.Type),
		SectionID:    string(m.LibrarySectionID),
		SectionTitle: m.LibrarySectionTitle,
		Title:        m.Title,
		SortTitle:    m.TitleSort,
		Show:         m.GrandparentTitle,
		ShowKey:      string(m.GrandparentKey),
		SeasonKey:    string(m.ParentKey),
		Season:       m.ParentTitle,
		SeasonNum:    int(m.ParentIndex),
		Index:        int(m.Index),
		Year:         int(m.Year),
		Duration:     time.Duration(m.Duration) * time.Millisecond,
		ViewCount:    int(m.ViewCount),
		ViewOffset:   time.Duration(m.ViewOffset) * time.Millisecond,
		LastViewed:   unix(int64(m.LastViewedAt)),
		Added:        unix(int64(m.AddedAt)),
		Raw:          m,
	}
	if it.SortTitle == "" {
		it.SortTitle = it.Title
	}
	if it.Kind == "track" && it.Show == "" {
		it.Show = m.ParentTitle
	}
	seen := map[string]bool{}
	for _, md := range m.Media {
		if it.Bitrate == 0 {
			it.Bitrate = int(md.Bitrate)
			it.Resolution = md.VideoResolution
			it.VideoCodec = md.VideoCodec
			it.Container = md.Container
		}
		for _, p := range md.Part {
			if seen[p.File] {
				continue
			}
			seen[p.File] = true
			it.Size += int64(p.Size)
			if p.File != "" {
				it.Files = append(it.Files, p.File)
			}
		}
	}
	for _, t := range m.Label {
		it.Labels = append(it.Labels, t.Tag)
	}
	for _, t := range m.Genre {
		it.Genres = append(it.Genres, t.Tag)
	}
	return it
}

func unix(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0)
}
