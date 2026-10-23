package field

import (
	"github.com/ashleyconnor/plx/internal/model"
)

func unixOrZero(t interface{ Unix() int64 }, zero bool) int64 {
	if zero {
		return 0
	}
	return t.Unix()
}

var (
	Title = register(&Field{Name: "title", Type: String, Doc: "title (episodes include show)",
		Str: func(i *model.Item) string { return i.DisplayTitle() }})
	Show = register(&Field{Enum: true, Name: "show", Aliases: []string{"artist"}, Type: String, Doc: "show or artist",
		Str: func(i *model.Item) string { return i.Show }})
	Season = register(&Field{Enum: true, Name: "season", Type: Int, Doc: "season number",
		Num: func(i *model.Item) int64 { return int64(i.SeasonNum) }})
	Episode = register(&Field{Name: "episode", Aliases: []string{"ep", "track"}, Type: Int, Doc: "episode or track number",
		Num: func(i *model.Item) int64 { return int64(i.Index) }})
	Year = register(&Field{Enum: true, Name: "year", Type: Int, Doc: "release year",
		Num: func(i *model.Item) int64 { return int64(i.Year) }})
	Kind = register(&Field{Enum: true, Name: "kind", Aliases: []string{"type"}, Type: String, Doc: "movie, episode, track, photo",
		Str: func(i *model.Item) string { return string(i.Kind) }})
	Section = register(&Field{Enum: true, Name: "section", Aliases: []string{"library"}, Type: String, Doc: "library section",
		Str: func(i *model.Item) string { return i.SectionTitle }})

	Size = register(&Field{Name: "size", Type: Bytes, Doc: "total file size (e.g. size>4G)", Width: 7,
		Num: func(i *model.Item) int64 { return i.Size }})
	Dur = register(&Field{Name: "duration", Aliases: []string{"dur", "length"}, Type: Duration, Doc: "runtime (e.g. dur>90m)", Width: 6,
		Num: func(i *model.Item) int64 { return int64(i.Duration) }})
	Bitrate = register(&Field{Name: "bitrate", Type: Int, Doc: "bitrate in kbps",
		Num: func(i *model.Item) int64 { return int64(i.Bitrate) }})
	Res = register(&Field{Enum: true, Name: "res", Aliases: []string{"resolution"}, Type: String, Doc: "video resolution: sd, 720, 1080, 4k", Width: 5,
		Str: func(i *model.Item) string { return i.Resolution }})
	Codec = register(&Field{Enum: true, Name: "codec", Type: String, Doc: "video codec",
		Str: func(i *model.Item) string { return i.VideoCodec }})
	Container = register(&Field{Enum: true, Name: "container", Aliases: []string{"ext"}, Type: String, Doc: "file container: mkv, mp4, avi",
		Str: func(i *model.Item) string { return i.Container }})
	Path = register(&Field{Name: "path", Aliases: []string{"file"}, Type: List, Doc: "file path; substring, or glob with *",
		List: func(i *model.Item) []string { return i.Files }})
	Folder = register(&Field{Name: "folder", Aliases: []string{"dir"}, Type: String, Doc: "directory of the first file",
		Str: func(i *model.Item) string { return i.Dir() }})

	Label = register(&Field{Enum: true, Name: "label", Aliases: []string{"labels"}, Type: List, Doc: "labels (exact; label:a,b is OR)", Width: 16,
		List: func(i *model.Item) []string { return i.Labels }})
	LabelCount = register(&Field{Name: "labelcount", Aliases: []string{"nlabels"}, Type: Int, Doc: "number of labels (labelcount:1 label:keep = only keep)",
		Num: func(i *model.Item) int64 { return int64(len(i.Labels)) }})
	Genre = register(&Field{Enum: true, Name: "genre", Type: List, Doc: "genres (movies)",
		List: func(i *model.Item) []string { return i.Genres }})

	Watched = register(&Field{Name: "watched", Aliases: []string{"played"}, Type: Bool, Doc: "played at least once (also is:watched)",
		Bool: func(i *model.Item) bool { return i.Watched() }})
	InProgress = register(&Field{Name: "inprogress", Aliases: []string{"partial"}, Type: Bool, Doc: "partially watched",
		Bool: func(i *model.Item) bool { return i.InProgress() }})
	Plays = register(&Field{Name: "plays", Aliases: []string{"viewcount"}, Type: Int, Doc: "play count", Width: 5,
		Num: func(i *model.Item) int64 { return int64(i.ViewCount) }})
	Added = register(&Field{Name: "added", Type: Time, Doc: "date added (added<30d, added>2024-01-01)",
		Num: func(i *model.Item) int64 { return unixOrZero(i.Added, i.Added.IsZero()) }})
	Viewed = register(&Field{Name: "viewed", Aliases: []string{"lastviewed"}, Type: Time, Doc: "last viewed date",
		Num: func(i *model.Item) int64 { return unixOrZero(i.LastViewed, i.LastViewed.IsZero()) }})
)
