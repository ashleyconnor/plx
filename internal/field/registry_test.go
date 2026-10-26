package field

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/model"
)

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		-5:                   "",
		0:                    "",
		1:                    "1B",
		1023:                 "1023B",
		1024:                 "1.0K",
		1536:                 "1.5K",
		5 << 20:              "5.0M",
		3 << 30:              "3.0G",
		int64(2) << 40:       "2.0T",
		int64(1)<<50 + 1<<49: "1.5P",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTypeStringAndNumeric(t *testing.T) {
	names := map[Type]string{String: "string", List: "list", Int: "int", Bytes: "bytes", Duration: "duration", Time: "time", Bool: "bool"}
	for ty, want := range names {
		if ty.String() != want {
			t.Errorf("%d.String() = %q, want %q", ty, ty.String(), want)
		}
	}
	for ty, want := range map[Type]bool{String: false, List: false, Bool: false, Int: true, Bytes: true, Duration: true, Time: true} {
		if ty.Numeric() != want {
			t.Errorf("%v.Numeric() = %v", ty, !want)
		}
	}
}

func TestLookupIsCaseInsensitiveAndResolvesAliases(t *testing.T) {
	for name, want := range map[string]*Field{
		"title": Title, "TITLE": Title, "artist": Show, "ep": Episode, "track": Episode,
		"library": Section, "ext": Container, "disk": TotalSize, "played": Watched, "dur.max": Dur,
		"size.max": Size, "length": Dur,
	} {
		if got, ok := Lookup(name); !ok || got != want {
			t.Errorf("Lookup(%q) = %v, %v", name, got, ok)
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("unknown name resolved")
	}
	if f, ok := Lookup("bitrate.min"); !ok || f.Type != Int || !strings.Contains(f.Doc, "smallest") {
		t.Errorf("bitrate.min = %+v", f)
	}
}

func TestMustLookupPanicsOnUnknown(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	MustLookup("definitely-not-a-field")
}

func TestAllIsSortedAndComplete(t *testing.T) {
	fs := All()
	if !slices.IsSortedFunc(fs, func(a, b *Field) int { return strings.Compare(a.Name, b.Name) }) {
		t.Error("All() not sorted by name")
	}
	for _, f := range fs {
		if f.Doc == "" {
			t.Errorf("%s has no doc (autocomplete shows it)", f.Name)
		}
		// every field must have the accessor its type needs, or filtering panics
		ok := map[Type]bool{String: f.Str != nil, List: f.List != nil, Bool: f.Bool != nil,
			Int: f.Num != nil, Bytes: f.Num != nil, Duration: f.Num != nil, Time: f.Num != nil}[f.Type]
		if !ok {
			t.Errorf("%s (%v) lacks its accessor", f.Name, f.Type)
		}
		if got, found := Lookup(f.Name); !found || got != f {
			t.Errorf("%s not registered under its own name", f.Name)
		}
	}
	// the generated .min fields are present exactly for versioned fields
	for _, f := range fs {
		_, hasMin := Lookup(f.Name + ".min")
		if (f.Min != nil) != hasMin && !strings.HasSuffix(f.Name, ".min") {
			t.Errorf("%s: Min=%v but .min registered=%v", f.Name, f.Min != nil, hasMin)
		}
	}
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("duplicate name accepted")
		}
	}()
	register(&Field{Name: "extra-title", Aliases: []string{"title"}, Type: String})
}

func TestFormatByType(t *testing.T) {
	added := time.Date(2024, 3, 9, 12, 0, 0, 0, time.UTC)
	it := &model.Item{
		Kind: "episode", Show: "Show", SeasonNum: 2, Index: 3, Title: "Ep", Year: 0,
		Size: 3 << 30, Duration: 90 * time.Minute, Labels: []string{"a", "b"}, ViewCount: 2,
		Added: added.Local(), Files: []string{"/tv/a.mkv"},
	}
	cases := []struct {
		f    *Field
		want string
	}{
		{Title, "Show S02E03 · Ep"},
		{Year, ""}, // zero ints are blank, not "0"
		{Season, "2"},
		{Size, "3.0G"},
		{Dur, "1h30m"},
		{Label, "a,b"},
		{Watched, "✓"},
		{InProgress, ""},
		{Plays, "2"},
		{Added, "2024-03-09"},
		{Viewed, ""},
		{Folder, "/tv"},
		{Path, "/tv/a.mkv"},
	}
	for _, tc := range cases {
		if got := tc.f.Format(it); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.f.Name, got, tc.want)
		}
	}
}

func TestFormatSameValuesAcrossVersionsShownOnce(t *testing.T) {
	it := &model.Item{Resolution: "1080", Versions: []model.Version{
		{Duration: 90 * time.Minute, Resolution: "1080", Container: "mkv"},
		{Duration: 90*time.Minute + 4*time.Second, Resolution: "1080", Container: "mp4"},
	}}
	if got := Dur.Format(it); got != "1h30m" {
		t.Errorf("durations equal as displayed: %q", got)
	}
	if got := Res.Format(it); got != "1080" {
		t.Errorf("res = %q", got)
	}
	if got := Container.Format(it); got != "mkv|mp4" {
		t.Errorf("container = %q", got)
	}
}

func TestVersionAggregatesFallBackToItem(t *testing.T) {
	it := &model.Item{Size: 10, Duration: time.Minute, Bitrate: 7}
	if Size.Num(it) != 10 || Dur.Num(it) != int64(time.Minute) || Bitrate.Num(it) != 7 {
		t.Error("max fallback")
	}
	if Size.Min(it) != 10 || Dur.Min(it) != int64(time.Minute) || Bitrate.Min(it) != 7 {
		t.Error("min fallback")
	}
	it.Versions = []model.Version{{Size: 5, Bitrate: 3}, {Size: 9, Bitrate: 1}, {Size: 7, Bitrate: 2}}
	if Size.Num(it) != 9 || Size.Min(it) != 5 || Bitrate.Min(it) != 1 || Bitrate.Num(it) != 3 {
		t.Errorf("max/min = %d/%d", Size.Num(it), Size.Min(it))
	}
	if TotalSize.Num(it) != 10 {
		t.Error("totalsize is the item's disk usage, not a version's")
	}
}

func TestSortKey(t *testing.T) {
	it := &model.Item{Title: "Zed", Labels: []string{"B", "a"}, ViewCount: 1, Year: 1999}
	if k := Title.SortKey(it); k != "zed" {
		t.Errorf("string key = %v", k)
	}
	if k := Label.SortKey(it); k != "b,a" {
		t.Errorf("list key = %v", k)
	}
	if k := Year.SortKey(it); k != int64(1999) {
		t.Errorf("int key = %v (%T)", k, k)
	}
	if Watched.SortKey(it) != int64(1) || InProgress.SortKey(it) != int64(0) {
		t.Error("bool keys")
	}
	if k := Added.SortKey(it); k != int64(0) {
		t.Errorf("never-added sorts as 0, got %v", k)
	}
}

func TestTimeFieldsAreZeroWhenUnset(t *testing.T) {
	var it model.Item
	if Added.Num(&it) != 0 || Viewed.Num(&it) != 0 {
		t.Error("unset times must be 0 (= never), not the year-1 unix value")
	}
	it.LastViewed = time.Unix(1700000000, 0)
	if Viewed.Num(&it) != 1700000000 {
		t.Error("viewed")
	}
}

func TestFormatUnknownTypeAndNeverSeenTimesAreBlank(t *testing.T) {
	odd := &Field{Name: "odd", Type: Type(99)}
	if got := odd.Format(&model.Item{}); got != "" {
		t.Errorf("unknown type formats as %q", got)
	}
	if got := Viewed.Format(&model.Item{}); got != "" {
		t.Errorf("never viewed = %q", got)
	}
	if MustLookup("Size") != Size {
		t.Error("MustLookup is case-insensitive like Lookup")
	}
}

func TestResolutionRank(t *testing.T) {
	order := []string{"sd", "480", "576", "720", "1080", "2k", "4k", "8k"}
	prev := 0
	for _, r := range order {
		n, ok := ResolutionRank(r)
		if !ok || n <= prev {
			t.Errorf("%s rank %d (ok %v) not above %d", r, n, ok, prev)
		}
		prev = n
	}
	a, _ := ResolutionRank(" 4K ")
	b, _ := ResolutionRank("2160")
	if a != b {
		t.Error("4k is 2160, case and space insensitive")
	}
	for _, bad := range []string{"", "hd", "0", "-720", "720p"} {
		if _, ok := ResolutionRank(bad); ok {
			t.Errorf("%q should have no rank", bad)
		}
	}
	if Res.Rank == nil || Res.Hint == "" {
		t.Error("res must be an ordered field with a hint")
	}
}
