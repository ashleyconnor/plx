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

// versionMax and versionMin aggregate a per-version number. Items without
// version data fall back to the item-level value.
func versionMax(get func(*model.Version) int64, item func(*model.Item) int64) func(*model.Item) int64 {
	return func(i *model.Item) int64 {
		if len(i.Versions) == 0 {
			return item(i)
		}
		var n int64
		for k := range i.Versions {
			n = max(n, get(&i.Versions[k]))
		}
		return n
	}
}

func versionMin(get func(*model.Version) int64, item func(*model.Item) int64) func(*model.Item) int64 {
	return func(i *model.Item) int64 {
		if len(i.Versions) == 0 {
			return item(i)
		}
		var n int64
		for k := range i.Versions {
			if v := get(&i.Versions[k]); k == 0 || v < n {
				n = v
			}
		}
		return n
	}
}

func vSize(v *model.Version) int64 { return v.Size }
func iSize(i *model.Item) int64    { return i.Size }
func vDur(v *model.Version) int64  { return int64(v.Duration) }
func iDur(i *model.Item) int64     { return int64(i.Duration) }
func vRate(v *model.Version) int64 { return int64(v.Bitrate) }
func iRate(i *model.Item) int64    { return int64(i.Bitrate) }

func versionStrs(get func(*model.Version) string) func(*model.Item) []string {
	return func(i *model.Item) []string {
		var out []string
		for _, v := range i.ByDuration() {
			out = append(out, get(&v))
		}
		return out
	}
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

	Size = register(&Field{Name: "size", Type: Bytes, Doc: "file size, largest version (e.g. size>4G; size.min: smallest)", Width: 7,
		Num: versionMax(vSize, iSize), Min: versionMin(vSize, iSize)})
	TotalSize = register(&Field{Name: "totalsize", Aliases: []string{"disk"}, Type: Bytes, Doc: "disk used by all versions",
		Num: func(i *model.Item) int64 { return i.Size }})
	Dur = register(&Field{Name: "duration", Aliases: []string{"dur", "length"}, Type: Duration, Doc: "runtime of the longest version (dur.min: shortest)", Width: 6,
		Num: versionMax(vDur, iDur), Min: versionMin(vDur, iDur)})
	Bitrate = register(&Field{Name: "bitrate", Type: Int, Doc: "bitrate in kbps, highest version (bitrate.min: lowest)",
		Num: versionMax(vRate, iRate), Min: versionMin(vRate, iRate)})
	Versions = register(&Field{Name: "versions", Type: Int, Doc: "number of versions (versions>1: has duplicates or samples)",
		Num: func(i *model.Item) int64 { return int64(len(i.Versions)) }})
	Res = register(&Field{Enum: true, Name: "res", Aliases: []string{"resolution"}, Type: String, Doc: "video resolution: sd, 720, 1080, 4k (any version)", Width: 5,
		Str: func(i *model.Item) string { return i.Resolution }, Strs: versionStrs(func(v *model.Version) string { return v.Resolution })})
	Codec = register(&Field{Enum: true, Name: "codec", Type: String, Doc: "video codec (any version)",
		Str: func(i *model.Item) string { return i.VideoCodec }, Strs: versionStrs(func(v *model.Version) string { return v.VideoCodec })})
	Container = register(&Field{Enum: true, Name: "container", Aliases: []string{"ext"}, Type: String, Doc: "file container: mkv, mp4, avi (any version)",
		Str: func(i *model.Item) string { return i.Container }, Strs: versionStrs(func(v *model.Version) string { return v.Container })})
	Path = register(&Field{Name: "path", Aliases: []string{"file"}, Type: List, Doc: "file path; substring, or glob with *",
		List: func(i *model.Item) []string { return i.Files }})
	Folder = register(&Field{Name: "folder", Aliases: []string{"dir"}, Type: String, Doc: "directory of the first file",
		Str: func(i *model.Item) string { return i.Dir() }})

	Label = register(&Field{Enum: true, Name: "label", Aliases: []string{"labels"}, Type: List, Doc: "labels (exact; label:a,b is OR)", Width: 16,
		List: func(i *model.Item) []string { return i.Labels }})
	LabelCount = register(&Field{Name: "labelcount", Aliases: []string{"nlabels"}, Type: Int, Doc: "number of labels (labelcount:1 label:keep = only keep)",
		Num: func(i *model.Item) int64 { return int64(len(i.Labels)) }})
	Collection = register(&Field{Enum: true, Name: "collection", Aliases: []string{"collections"}, Type: List, Doc: "collections containing the item (-has:collection = in none)",
		List: func(i *model.Item) []string { return i.Collections }})
	Playlist = register(&Field{Enum: true, Name: "playlist", Aliases: []string{"playlists"}, Type: List, Doc: "playlists containing the item (-has:playlist = in none)",
		List: func(i *model.Item) []string { return i.Playlists }})
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
