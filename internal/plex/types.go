// Package plex is the wire layer: an HTTP client for the Plex Media Server API
// and DTOs that mirror its JSON. It contains no domain logic.
package plex

import (
	"bytes"
	"encoding/json"
	"strconv"
)

type envelope struct {
	MediaContainer MediaContainer `json:"MediaContainer"`
}

type MediaContainer struct {
	Size                int         `json:"size"`
	TotalSize           *int        `json:"totalSize"`
	Offset              int         `json:"offset"`
	LibrarySectionID    FlexString  `json:"librarySectionID"`
	LibrarySectionTitle string      `json:"librarySectionTitle"`
	MachineIdentifier   string      `json:"machineIdentifier"`
	Version             string      `json:"version"`
	Metadata            []Metadata  `json:"Metadata"`
	Directory           []Directory `json:"Directory"`
	Setting             []Setting   `json:"Setting"`
}

// Setting is a server preference from /:/prefs.
type Setting struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}

// Metadata is any item Plex returns: movie, episode, track, playlist, collection...
type Metadata struct {
	RatingKey           FlexString `json:"ratingKey"`
	Key                 string     `json:"key"`
	Type                string     `json:"type"`
	Subtype             string     `json:"subtype"`
	Title               string     `json:"title"`
	TitleSort           string     `json:"titleSort"`
	ParentTitle         string     `json:"parentTitle"`
	GrandparentTitle    string     `json:"grandparentTitle"`
	GrandparentKey      FlexString `json:"grandparentRatingKey"`
	ParentIndex         FlexInt    `json:"parentIndex"`
	Index               FlexInt    `json:"index"`
	Year                FlexInt    `json:"year"`
	LibrarySectionID    FlexString `json:"librarySectionID"`
	LibrarySectionTitle string     `json:"librarySectionTitle"`
	ViewCount           FlexInt    `json:"viewCount"`
	ViewOffset          FlexInt    `json:"viewOffset"`
	LastViewedAt        FlexInt    `json:"lastViewedAt"`
	AddedAt             FlexInt    `json:"addedAt"`
	UpdatedAt           FlexInt    `json:"updatedAt"`
	Duration            FlexInt    `json:"duration"`
	PlaylistItemID      FlexString `json:"playlistItemID"`
	PlaylistType        string     `json:"playlistType"`
	Smart               FlexBool   `json:"smart"`
	LeafCount           FlexInt    `json:"leafCount"`
	ChildCount          FlexInt    `json:"childCount"`
	CollectionSort      FlexString `json:"collectionSort"`
	ContentRating       string     `json:"contentRating"`
	Studio              string     `json:"studio"`
	Media               []Media    `json:"Media"`
	Label               []Tag      `json:"Label"`
	Genre               []Tag      `json:"Genre"`
}

type Media struct {
	ID              FlexInt `json:"id"`
	Duration        FlexInt `json:"duration"`
	Bitrate         FlexInt `json:"bitrate"`
	Width           FlexInt `json:"width"`
	Height          FlexInt `json:"height"`
	VideoResolution string  `json:"videoResolution"`
	VideoCodec      string  `json:"videoCodec"`
	AudioCodec      string  `json:"audioCodec"`
	Container       string  `json:"container"`
	Part            []Part  `json:"Part"`
}

type Part struct {
	ID       FlexInt `json:"id"`
	Size     FlexInt `json:"size"`
	File     string  `json:"file"`
	Duration FlexInt `json:"duration"`
}

type Tag struct {
	ID  FlexInt `json:"id"`
	Tag string  `json:"tag"`
}

// Directory is a section, or a tag listing entry (e.g. /library/sections/1/label).
type Directory struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type"`
	UUID  string `json:"uuid"`
}

// Plex is inconsistent about JSON types: IDs are sometimes strings and
// sometimes numbers, booleans are sometimes true/false and sometimes "1"/1.
// The Flex types decode any of those forms and treat null as the zero value.

type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*f = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	*f = FlexString(string(b))
	return nil
}

type FlexInt int64

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*f = 0
		return nil
	}
	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*f = 0
			return nil
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = FlexInt(n)
		return nil
	}
	fl, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = FlexInt(fl)
	return nil
}

type FlexBool bool

func (f *FlexBool) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true", "1", `"1"`, `"true"`:
		*f = true
	default:
		*f = false
	}
	return nil
}
