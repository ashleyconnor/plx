package query

import (
	"strings"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
)

func TestParseBytesVariants(t *testing.T) {
	cases := map[string]int64{
		"0": 0, "512": 512, "4K": 4 << 10, "4k": 4 << 10, "4KB": 4 << 10, "4KiB": 4 << 10, "500m": 500 << 20,
		"1.5T": 3 << 39, "2P": 2 << 50, " 4G ": 4 << 30, "0.5G": 1 << 29, "1e3": 1000,
	}
	for in, want := range cases {
		if got, err := ParseBytes(in); err != nil || got != want {
			t.Errorf("ParseBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "B", "G", "-1", "-1G", "4 G", "0x10", "abc", "4X", "4GG"} {
		if _, err := ParseBytes(bad); err == nil || !strings.Contains(err.Error(), "expected a size") {
			t.Errorf("ParseBytes(%q) err = %v", bad, err)
		}
	}
}

func TestParseDurationVariants(t *testing.T) {
	cases := []struct {
		in   string
		bare time.Duration
		want time.Duration
	}{
		{"90", time.Minute, 90 * time.Minute},
		{"90", 24 * time.Hour, 90 * 24 * time.Hour},
		{"1.5", time.Hour, 90 * time.Minute},
		{"30d", time.Minute, 30 * 24 * time.Hour},
		{"2w", time.Minute, 14 * 24 * time.Hour},
		{"1y", time.Minute, 365 * 24 * time.Hour},
		{"0.5d", time.Minute, 12 * time.Hour},
		{"1h30m", time.Minute, 90 * time.Minute},
		{"45s", time.Minute, 45 * time.Second},
		{" 2H ", time.Minute, 2 * time.Hour},
	}
	for _, tc := range cases {
		if got, err := ParseDuration(tc.in, tc.bare); err != nil || got != tc.want {
			t.Errorf("ParseDuration(%q, %v) = %v, %v; want %v", tc.in, tc.bare, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "d", "xd", "1h30x", "abc", "1..5d", "h"} {
		if _, err := ParseDuration(bad, time.Minute); err == nil || !strings.Contains(err.Error(), "expected a duration") {
			t.Errorf("ParseDuration(%q) err = %v", bad, err)
		}
	}
}

func TestNonFiniteNumbersAreRejected(t *testing.T) {
	for _, s := range []string{"nan", "inf", "+inf", "-inf", "NaN", "infd", "1e30", "9999999999P"} {
		if _, err := ParseBytes(s); err == nil {
			t.Errorf("ParseBytes(%q) accepted", s)
		}
		if _, err := ParseDuration(s, time.Minute); err == nil {
			t.Errorf("ParseDuration(%q) accepted", s)
		}
	}
	for _, s := range []string{"1e30d", "99999999999y", "1e30"} {
		if _, err := ParseDuration(s, time.Hour); err == nil {
			t.Errorf("ParseDuration(%q) overflowed silently", s)
		}
	}
	if _, err := Parse("size>inf"); err == nil {
		t.Error("size>inf compiled")
	}
	if _, err := Parse("added<nan"); err == nil {
		t.Error("added<nan compiled")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ q, want string }{
		{`"unterminated`, "unterminated quote"},
		{`label:"a`, "unterminated quote"},
		{"bogusfield:1", `unknown field "bogusfield"`},
		{"titel:x", `did you mean "title"`},
		{"year:abc", "year:abc"},
		{"size>big", "expected a size"},
		{"dur>1h30x", "expected a duration"},
		{"added:2024-13-45", "expected a date"},
		{"added<yesterday", "expected a date (2024-01-31) or an age (30d)"},
		{"viewed>soon", "expected a date"},
		{"is:label", "not a yes/no field"},
		{"is:nope", "not a yes/no field"},
		{"is>watched", "use is:watched"},
		{"has>label", "use has:label"},
		{"has:size", "not a text or list field"},
		{"has:nope", "not a text or list field"},
		{"-sort:size", "use sort:field"},
		{"sort>size", "use sort:field"},
		{"sort:nope", "unknown field"},
		{"sort:-size", "unknown field"},
		{"watched:maybe", `expected yes or no, got "maybe"`},
		{"watched>yes", "use watched:yes"},
		{"watched:yes,no", "one value expected"},
		{"dur:1,x", "expected a duration"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.q)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) err = %v, want %q", tc.q, err, tc.want)
		}
	}
}

func TestParseTolerantInputsMatchEverything(t *testing.T) {
	// half-typed terms must not blank the list while the user is typing
	for _, s := range []string{"label:", "is:", "has:", "sort:", "size>", "year:", "-label:", "  ", ""} {
		q, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(%q): %v", s, err)
			continue
		}
		if len(q.Terms) != 0 && s != "-label:" {
			t.Errorf("Parse(%q) has %d terms", s, len(q.Terms))
		}
		if !q.Match(&model.Item{Title: "anything"}) {
			t.Errorf("%q should match everything", s)
		}
	}
}

func TestBareWordsAndQuoting(t *testing.T) {
	its := []*model.Item{
		{Key: "1", Title: "Star Wars: Holiday Special"},
		{Key: "2", Title: "label:keep"}, // a title that looks like a term
		{Key: "3", Kind: "episode", Show: "Lost", SeasonNum: 1, Index: 2, Title: "Pilot"},
	}
	cases := []struct{ q, want string }{
		{`"star wars"`, "1"},
		{`star wars`, "1"}, // two words, both must match
		{`"label:keep"`, "2"},
		{`-"star wars"`, "2,3"},
		{"lost", "3"}, // episode titles include the show
		{"s01e02", "3"},
		{"-", ""}, // a lone dash is the word "-", not a negation
		{`"-star"`, ""},
		{"-star", "2,3"},
		{"STAR", "1"},
	}
	for _, tc := range cases {
		q, err := Parse(tc.q)
		if err != nil {
			t.Fatalf("%q: %v", tc.q, err)
		}
		if got := keys(q.Filter(its)); got != tc.want {
			t.Errorf("%q = %s, want %s", tc.q, got, tc.want)
		}
	}
}

func TestOperatorsOnStringsAndLists(t *testing.T) {
	its := []*model.Item{
		{Key: "1", Kind: "movie", Resolution: "1080", Labels: []string{"keep"}, Files: []string{"/a/One.mkv"}},
		{Key: "2", Kind: "movie", Resolution: "720", Labels: []string{"fav", "keep"}, Files: []string{"/b/two.mp4"}},
		{Key: "3", Kind: "episode", Resolution: "4k"},
	}
	cases := []struct{ q, want string }{
		{"kind=movie", "1,2"},
		{"kind!=movie", "3"},
		{"kind:mov", "1,2"},
		{"kind=mov", ""},
		{"kind!=m*", "3"},
		{"kind=m*", "1,2"},
		{"kind:*sode", "3"},
		{"res>=1080", "1,3"}, // ordered by resolution rank, not text
		{"res<1080", "2"},
		{"label!=fav", "1,2"}, // any element != fav
		{"label=KEEP", "1,2"},
		{"label:fav,keep", "1,2"},
		{"path:two", "2"},
		{"path=/a/One.mkv", "1"},
		{"path:*.mp4", "2"},
		{"path!=/a/*", "2"}, // any path element not matching; items without paths never match
		{"-has:path", "3"},
		{"has:res", "1,2,3"},
		{"has:kind", "1,2,3"},
		{"-has:label", "3"},
	}
	for _, tc := range cases {
		q, err := Parse(tc.q)
		if err != nil {
			t.Fatalf("%q: %v", tc.q, err)
		}
		if got := keys(q.Filter(its)); got != tc.want {
			t.Errorf("%q = %q, want %q", tc.q, got, tc.want)
		}
	}
}

func TestNumericOperatorsAndLists(t *testing.T) {
	its := []*model.Item{
		{Key: "1", Year: 1979, Index: 1}, {Key: "2", Year: 1986, Index: 2}, {Key: "3", Year: 2001, Index: 3},
	}
	cases := []struct{ q, want string }{
		{"year=1986", "2"},
		{"year:1986", "2"},
		{"year!=1986", "1,3"},
		{"year>1986", "3"},
		{"year>=1986", "2,3"},
		{"year<1986", "1"},
		{"year<=1986", "1,2"},
		{"year:1979,2001", "1,3"},
		{"year:>1980", "2,3"}, // GitHub style
		{"year:<=1986", "1,2"},
		{"ep>=2 year<2000", "2"},
		{"year!=1979 year!=2001", "2"},
	}
	for _, tc := range cases {
		q, err := Parse(tc.q)
		if err != nil {
			t.Fatalf("%q: %v", tc.q, err)
		}
		if got := keys(q.Filter(its)); got != tc.want {
			t.Errorf("%q = %q, want %q", tc.q, got, tc.want)
		}
	}
}

func TestTimeFilters(t *testing.T) {
	now = func() time.Time { return fixedNow }
	defer func() { now = time.Now }()
	day := 24 * time.Hour
	its := []*model.Item{
		{Key: "1", Added: fixedNow.Add(-2 * day), LastViewed: fixedNow.Add(-2 * day)},
		{Key: "2", Added: fixedNow.Add(-45 * day)},
		{Key: "3", Added: time.Date(2024, 1, 15, 9, 0, 0, 0, time.Local)},
		{Key: "4"}, // no dates at all
	}
	cases := []struct{ q, want string }{
		{"added<30d", "1"},
		{"added:30d", "1"}, // ":" with an age means within
		{"added=30d", "1"},
		{"added<=2d", "1"},
		{"added>30d", "2,3,4"}, // older than 30 days
		{"added>=45d", "2,3,4"},
		{"added<1w", "1"},
		{"added<2y", "1,2"},
		{"added<4y", "1,2,3"},
		{"added:2024-01-15", "3"}, // that day
		{"added=2024-01-15", "3"},
		{"added>=2024-01-15", "1,2,3"},
		{"added<2024-01-16", "3"},
		{"viewed>1y", "2,3,4"}, // never viewed = infinitely old
		{"viewed<1y", "1"},
		{"added<30d,2024-01-15", "1"}, // OR of an age and a date
	}
	for _, tc := range cases {
		q, err := Parse(tc.q)
		if err != nil {
			t.Fatalf("%q: %v", tc.q, err)
		}
		if got := keys(q.Filter(its)); got != tc.want {
			t.Errorf("%q = %q, want %q", tc.q, got, tc.want)
		}
	}
}

func TestDateLiteralsMeanTheWholeLocalDay(t *testing.T) {
	day := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, time.Local)
	}
	its := []*model.Item{
		{Key: "before", Added: day(2024, 1, 14, 23, 59)},
		{Key: "start", Added: day(2024, 1, 15, 0, 0)},
		{Key: "noon", Added: day(2024, 1, 15, 12, 0)},
		{Key: "end", Added: day(2024, 1, 15, 23, 59)},
		{Key: "after", Added: day(2024, 1, 16, 0, 0)},
		{Key: "never"},
	}
	cases := []struct{ q, want string }{
		{"added=2024-01-15", "start,noon,end"},
		{"added:2024-01-15", "start,noon,end"},
		{"added!=2024-01-15", "before,after,never"},
		{"added>2024-01-15", "after"},
		{"added>=2024-01-15", "start,noon,end,after"},
		{"added<2024-01-15", "before"},
		{"added<=2024-01-15", "before,start,noon,end"},
	}
	names := func(r []*model.Item) string {
		var out []string
		for _, it := range r {
			out = append(out, it.Key)
		}
		return strings.Join(out, ",")
	}
	for _, tc := range cases {
		q, err := Parse(tc.q)
		if err != nil {
			t.Fatalf("%q: %v", tc.q, err)
		}
		if got := names(q.Filter(its)); got != tc.want {
			t.Errorf("%q = %s, want %s", tc.q, got, tc.want)
		}
	}
	// complements: every dated item is in exactly one of each pair
	for _, pair := range [][2]string{{"added>2024-01-15", "added<=2024-01-15"}, {"added<2024-01-15", "added>=2024-01-15"}, {"added=2024-01-15", "added!=2024-01-15"}} {
		a, _ := Parse(pair[0])
		b, _ := Parse(pair[1])
		for _, it := range its[:5] {
			if a.Match(it) == b.Match(it) {
				t.Errorf("%s and %s both=%v for %s", pair[0], pair[1], a.Match(it), it.Key)
			}
		}
	}
}

func TestDateDayIsCalendarDayAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	old := time.Local
	time.Local = loc
	defer func() { time.Local = old }()
	// 2024-03-10 has 23 hours in New York; 23:30 that evening is still that day
	late := &model.Item{Key: "late", Added: time.Date(2024, 3, 10, 23, 30, 0, 0, loc)}
	next := &model.Item{Key: "next", Added: time.Date(2024, 3, 11, 0, 0, 0, 0, loc)}
	q, _ := Parse("added=2024-03-10")
	if !q.Match(late) || q.Match(next) {
		t.Error("= on a 23-hour day")
	}
	q, _ = Parse("added<=2024-03-10")
	if !q.Match(late) || q.Match(next) {
		t.Error("<= on a 23-hour day")
	}
}

func TestResolutionComparesByRank(t *testing.T) {
	its := []*model.Item{{Key: "1", Resolution: "480"}, {Key: "2", Resolution: "720"}, {Key: "3", Resolution: "1080"},
		{Key: "4", Resolution: "4k"}, {Key: "5", Resolution: "sd"}, {Key: "6", Resolution: "weird"}, {Key: "7"}, {Key: "8", Resolution: "8K"}}
	cases := []struct{ q, want string }{
		{"res>=720", "2,3,4,8"},
		{"res>720", "3,4,8"},
		{"res<1080", "1,2,5"},
		{"res<=720", "1,2,5"},
		{"res>sd", "1,2,3,4,8"},
		{"res<480", "5"},
		{"res>1080 res<8k", "4"},
		{"res>2k", "4,8"},
		{"res>1440", "4,8"}, // numbers rank by height; 2k is 1440
		{"res>=4K", "4,8"},
		{"res:720", "2"}, // equality is still text
		{"res=720", "2"},
		{"res!=720", "1,3,4,5,6,7,8"},
		{"res:720,1080", "2,3"},
		{"res>720,1080", "3,4,8"},
	}
	for _, tc := range cases {
		q, err := Parse(tc.q)
		if err != nil {
			t.Errorf("%q: %v", tc.q, err)
			continue
		}
		if got := keys(q.Filter(its)); got != tc.want {
			t.Errorf("%q = %q, want %q", tc.q, got, tc.want)
		}
	}
	for _, bad := range []string{"res>hd", "res<=big", "res>720,hd"} {
		if _, err := Parse(bad); err == nil || !strings.Contains(err.Error(), "unknown res") || !strings.Contains(err.Error(), "4k") {
			t.Errorf("Parse(%q) err = %v, want helpful error", bad, err)
		}
	}
}

func TestResolutionRankAppliesToAnyVersion(t *testing.T) {
	it := &model.Item{Key: "1", Resolution: "480", Versions: []model.Version{{Resolution: "480", Duration: time.Minute}, {Resolution: "4k", Duration: time.Hour}}}
	if q, _ := Parse("res>=4k"); !q.Match(it) {
		t.Error("any version satisfies an ordering comparison")
	}
	if q, _ := Parse("res>4k"); q.Match(it) {
		t.Error("res>4k")
	}
}

func TestUsesAndMembership(t *testing.T) {
	q, _ := Parse("size>1G -has:playlist")
	if !q.Uses(field.Size) || q.Uses(field.Collection) || !q.UsesMembership() {
		t.Error("Uses")
	}
	q, _ = Parse("alien sort:collection")
	if !q.UsesMembership() {
		t.Error("a sort key on collection needs membership too")
	}
	q, _ = Parse("collection:x")
	if !q.UsesMembership() || !q.Uses(field.Playlist, field.Collection) {
		t.Error("collection term")
	}
	q, _ = Parse("alien")
	if q.UsesMembership() || q.Uses() {
		t.Error("bare words use no fields")
	}
}

func TestSortParseFormatAndStability(t *testing.T) {
	keysIn, err := ParseSort("size-, title,year+,")
	if err != nil || len(keysIn) != 3 || !keysIn[0].Desc || keysIn[1].Desc || keysIn[2].Desc {
		t.Fatalf("keys = %+v, %v", keysIn, err)
	}
	if got := FormatSort(keysIn); got != "size-,title,year" {
		t.Errorf("FormatSort = %q", got)
	}
	if FormatSort(nil) != "" {
		t.Error("empty sort formats to empty string")
	}
	if ks, err := ParseSort(""); err != nil || len(ks) != 0 {
		t.Errorf("empty = %v %v", ks, err)
	}
	if _, err := ParseSort("size,nope"); err == nil {
		t.Error("unknown key accepted")
	}
	a, b, c := &model.Item{Key: "a", Year: 1}, &model.Item{Key: "b", Year: 1}, &model.Item{Key: "c", Year: 0}
	its := []*model.Item{a, b, c}
	SortItems(its, []SortKey{{Field: field.Year, Desc: true}})
	if keys(its) != "a,b,c" {
		t.Errorf("ties must keep source order: %s", keys(its))
	}
	SortItems(its, []SortKey{{Field: field.Year}})
	if keys(its) != "c,a,b" {
		t.Errorf("asc: %s", keys(its))
	}
}

func TestSortByMultipleKeysAndBoolList(t *testing.T) {
	its := []*model.Item{
		{Key: "1", Title: "b", Labels: []string{"x"}, ViewCount: 1},
		{Key: "2", Title: "a", Labels: []string{"x"}},
		{Key: "3", Title: "c", Labels: []string{"a"}, ViewCount: 1},
	}
	SortItems(its, []SortKey{{Field: field.Label}, {Field: field.Title}})
	if keys(its) != "3,2,1" {
		t.Errorf("label,title = %s", keys(its))
	}
	SortItems(its, []SortKey{{Field: field.Watched, Desc: true}, {Field: field.Title}})
	if keys(its) != "1,3,2" {
		t.Errorf("watched-,title = %s", keys(its))
	}
}

func TestCompareKeysUnknownTypes(t *testing.T) {
	if compareKeys(1.5, 2.5) != 0 {
		t.Error("unsupported key types compare equal")
	}
}

func TestSortKeyString(t *testing.T) {
	if (SortKey{Field: field.Size, Desc: true}).String() != "size-" || (SortKey{Field: field.Size}).String() != "size" {
		t.Error("SortKey.String")
	}
}

func TestSplitOpAndValues(t *testing.T) {
	cases := []struct {
		in, name, op, val string
		ok                bool
	}{
		{"size>4G", "size", ">", "4G", true},
		{"size:>=4G", "size", ">=", "4G", true},
		{"size:!=4G", "size", "!=", "4G", true},
		{"dur.min<5m", "dur.min", "<", "5m", true},
		{"label:a:b", "label", ":", "a:b", true},
		{"label:=x", "label", "=", "x", true},
		{"plain", "", "", "", false},
		{"123:x", "", "", "", false},
		{">x", "", "", "", false},
	}
	for _, tc := range cases {
		n, o, v, ok := splitOp(tc.in)
		if n != tc.name || o != tc.op || v != tc.val || ok != tc.ok {
			t.Errorf("splitOp(%q) = %q %q %q %v", tc.in, n, o, v, ok)
		}
	}
	for in, want := range map[string]string{
		`a,b`: "a|b", `"a,b",c`: "a,b|c", `a,`: "a|", `"x y"`: "x y", ``: "",
	} {
		if got := strings.Join(splitValues(in), "|"); got != want {
			t.Errorf("splitValues(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenizeQuoting(t *testing.T) {
	toks, err := tokenize(`a  "b c" d:"e f,g" "h"i`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range toks {
		got = append(got, tk.text)
	}
	if strings.Join(got, "|") != `a|b c|d:"e f,g"|"h"i` { // only a fully quoted token loses its quotes
		t.Errorf("tokens = %q", got)
	}
	if toks[1].quotedAll != true || toks[0].quotedAll {
		t.Error("quotedAll flags")
	}
	if _, err := tokenize("\t x \t"); err != nil {
		t.Error(err)
	}
}

func TestSuggestAndLevenshtein(t *testing.T) {
	if levenshtein("kitten", "sitting") != 3 || levenshtein("", "abc") != 3 || levenshtein("same", "same") != 0 {
		t.Error("levenshtein")
	}
	if got := suggest("zzzzzzzzzz"); got != "" {
		t.Errorf("distant names get no suggestion, got %q", got)
	}
	if got := suggest("Titel"); !strings.Contains(got, `"title"`) {
		t.Errorf("case-insensitive suggestion: %q", got)
	}
}

func TestVersionAwareFieldQueries(t *testing.T) {
	it := &model.Item{Key: "1", Resolution: "1080", Size: 4<<30 + 10, Versions: []model.Version{
		{Duration: 2 * time.Minute, Resolution: "720", Container: "avi", Size: 10},
		{Duration: 120 * time.Minute, Resolution: "1080", Container: "mkv", Size: 4 << 30},
	}}
	cases := map[string]bool{
		"res:720": true, "res:1080": true, "res:4k": false, "ext:avi": true, "ext:mp4": false,
		"dur.min<5m": true, "dur.max>1h": true, "dur>1h": true, "dur<5m": false,
		"size.min<1K": true, "versions>1": true, "versions=2": true, "totalsize>3G": true,
	}
	for qs, want := range cases {
		q, err := Parse(qs)
		if err != nil {
			t.Fatalf("%q: %v", qs, err)
		}
		if q.Match(it) != want {
			t.Errorf("%q matched %v, want %v", qs, !want, want)
		}
	}
	// a versioned string field with no versions falls back to the item value
	single := &model.Item{Resolution: "720"}
	if q, _ := Parse("res:720"); !q.Match(single) {
		t.Error("fallback to item resolution")
	}
}
