// Package plex is the wire layer: an HTTP client for the Plex Media Server API
// and DTOs that mirror its JSON. It contains no domain logic.
package plex

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
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

	// Detail fields, complete only from /library/metadata/{key}.
	Guid                  FlexString `json:"guid"` // "Guid" is also a list of external IDs
	Summary               string     `json:"summary"`
	Tagline               string     `json:"tagline"`
	OriginallyAvailableAt string     `json:"originallyAvailableAt"`
	Rating                FlexFloat  `json:"rating"`
	AudienceRating        FlexFloat  `json:"audienceRating"`
	UserRating            FlexFloat  `json:"userRating"`
	Thumb                 string     `json:"thumb"`
	ParentThumb           string     `json:"parentThumb"`
	GrandparentThumb      string     `json:"grandparentThumb"`
	Director              []Tag      `json:"Director"`
	Writer                []Tag      `json:"Writer"`
	Role                  []Tag      `json:"Role"`
	Country               []Tag      `json:"Country"`
	Collection            []Tag      `json:"Collection"`
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
	AudioChannels   FlexInt `json:"audioChannels"`
	VideoFrameRate  string  `json:"videoFrameRate"`
	Part            []Part  `json:"Part"`
}

type Part struct {
	ID       FlexInt  `json:"id"`
	Size     FlexInt  `json:"size"`
	File     string   `json:"file"`
	Duration FlexInt  `json:"duration"`
	Stream   []Stream `json:"Stream"`
}

// Stream types.
const (
	StreamVideo    = 1
	StreamAudio    = 2
	StreamSubtitle = 3
)

type Stream struct {
	StreamType   FlexInt `json:"streamType"`
	Codec        string  `json:"codec"`
	Language     string  `json:"language"`
	DisplayTitle string  `json:"displayTitle"`
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
//
// They also ignore arrays and objects: encoding/json matches keys
// case-insensitively, and full metadata has both "rating" (a number) and
// "Rating" (a list of rating sources), so a scalar field can be handed either.

func composite(b []byte) bool { return len(b) > 0 && (b[0] == '[' || b[0] == '{') }

type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	if composite(b) {
		return nil
	}
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
	if composite(b) {
		return nil
	}
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

type FlexFloat float64

func (f *FlexFloat) UnmarshalJSON(b []byte) error {
	if composite(b) {
		return nil
	}
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	*f = FlexFloat(v)
	return err
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
