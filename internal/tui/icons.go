package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/ashleyconnor/plx/internal/model"
)

type iconSet struct {
	kinds                                map[model.Kind]string
	other                                string
	folder, dir                          string
	library, playlist, collection, label string
	query, selection                     string
}

var emojiIcons = iconSet{
	kinds:  map[model.Kind]string{"movie": "🎬", "episode": "📺", "track": "🎵", "photo": "📷", "clip": "📹"},
	other:  "📄",
	folder: "📁", dir: "📂",
	library: "📚", playlist: "📋", collection: "📦", label: "🔖",
	query: "🔍", selection: "📌",
}

var asciiIcons = iconSet{
	kinds:  map[model.Kind]string{"movie": "m", "episode": "e", "track": "t", "photo": "p", "clip": "c"},
	other:  "-",
	folder: ">", dir: ">",
	library: "L", playlist: "P", collection: "C", label: "#",
	query: "?", selection: "*",
}

// icons is the active set, chosen by the icons config option.
var icons = emojiIcons

// SetIcons picks the icon set: "emoji" (default), "ascii" or "none".
func SetIcons(name string) {
	switch name {
	case "ascii":
		icons = asciiIcons
	case "none":
		icons = iconSet{}
	default:
		icons = emojiIcons
	}
}

func (s iconSet) kind(k model.Kind) string {
	if i, ok := s.kinds[k]; ok {
		return i
	}
	return s.other
}

// cell renders an icon in a fixed two-column cell plus a space, so titles
// line up whatever the icon's width. With no icons it renders nothing.
func cell(icon string) string {
	if icons.other == "" {
		return ""
	}
	return runewidth.FillRight(icon, 2) + " "
}

func cellWidth() int {
	if icons.other == "" {
		return 0
	}
	return 3
}

func legend() string {
	if icons.other == "" {
		return ""
	}
	pairs := []struct{ icon, name string }{
		{icons.kinds["movie"], "movie"}, {icons.kinds["episode"], "episode"}, {icons.kinds["track"], "track"},
		{icons.kinds["photo"], "photo"}, {icons.folder, "folder"}, {icons.library, "library"},
		{icons.playlist, "playlist"}, {icons.collection, "collection"}, {icons.label, "label"},
		{icons.query, "saved query"}, {icons.selection, "selection"},
	}
	var parts []string
	for _, p := range pairs {
		parts = append(parts, p.icon+" "+p.name)
	}
	return strings.Join(parts, "   ")
}
