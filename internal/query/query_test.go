package query

import (
	"strings"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/model"
)

var fixedNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.Local)

func items() []*model.Item {
	day := 24 * time.Hour
	return []*model.Item{
		{Key: "1", Kind: "movie", Title: "Alien", Size: 5 << 30, Duration: 117 * time.Minute, ViewCount: 2,
			Labels: []string{"keep", "fav"}, Files: []string{"/movies/scifi/Alien (1979)/Alien.mkv"},
			Added: fixedNow.Add(-400 * day), LastViewed: fixedNow.Add(-10 * day)},
		{Key: "2", Kind: "movie", Title: "Aliens", Size: 8 << 30, Duration: 137 * time.Minute,
			Labels: []string{"keep"}, Files: []string{"/movies/scifi/Aliens (1986)/Aliens.mkv"},
			Added: fixedNow.Add(-5 * day)},
		{Key: "3", Kind: "episode", Title: "Pilot", Show: "Lost", SeasonNum: 1, Index: 1, Size: 1 << 30,
			Labels: []string{"trash"}, Files: []string{"/tv/Lost/S01/e01.mkv"}, ViewOffset: time.Minute,
			Added: fixedNow.Add(-40 * day)},
		{Key: "4", Kind: "episode", Title: "Star Wars: Holiday Special", Show: "Specials", Size: 700 << 20,
			Files: []string{"/tv/my show/x.avi"}, Added: fixedNow.Add(-1 * day)},
	}
}

func keys(its []*model.Item) string {
	var ks []string
	for _, it := range its {
		ks = append(ks, it.Key)
	}
	return strings.Join(ks, ",")
}

func TestFilter(t *testing.T) {
	now = func() time.Time { return fixedNow }
	defer func() { now = time.Now }()

	cases := []struct{ q, want string }{
		{"", "1,2,3,4"},
		{"alien", "1,2"},
		{"-is:watched", "2,3,4"},
		{"is:watched", "1"},
		{"watched:no", "2,3,4"},
		{"is:inprogress", "3"},
		{"size>4G", "1,2"},
		{"size<=1G", "3,4"},
		{"size>4gb size<6G", "1"},
		{"label:keep label:fav", "1"},  // AND of two tags
		{"label:fav,trash", "1,3"},     // OR within a field
		{"-label:trash", "1,2,4"},      // NOT
		{"label:kee", ""},              // labels match exactly
		{"label:k*", "1,2"},            // glob
		{"path:/movies/scifi/", "1,2"}, // substring at any depth
		{"path:/tv/*/S01/*", "3"},      // glob
		{`path:"/tv/my show/"`, "4"},   // quoted value with space
		{"folder:/tv/lost/s01", "3"},   // case-insensitive
		{"added<30d", "2,4"},           // newer than 30 days
		{"added>30d", "1,3"},           // older than 30 days
		{"viewed>30d", "2,3,4"},        // never viewed counts as old
		{"viewed<30d", "1"},
		{"dur>2h", "2"},
		{"dur>=117", "1,2"}, // bare number = minutes
		{"kind:episode show:lost", "3"},
		{`"star wars"`, "4"},
		{"Star Wars: Holiday", "4"}, // trailing colon is a word, not a field
		{"-alien -lost", "4"},
		{"season:1 episode:1", "3"},
	}
	for _, c := range cases {
		q, err := Parse(c.q)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.q, err)
			continue
		}
		if got := keys(q.Filter(items())); got != c.want {
			t.Errorf("%q = [%s], want [%s]", c.q, got, c.want)
		}
	}
}

func TestSort(t *testing.T) {
	cases := []struct{ q, want string }{
		{"sort:size", "4,3,1,2"},
		{"sort:size-", "2,1,3,4"},
		{"sort:kind,size-", "3,4,2,1"},
		{"sort:title", "1,2,3,4"},
		{"alien sort:dur-", "2,1"},
	}
	for _, c := range cases {
		q, err := Parse(c.q)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.q, err)
		}
		if got := keys(q.Filter(items())); got != c.want {
			t.Errorf("%q = [%s], want [%s]", c.q, got, c.want)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"szie>4G":     `did you mean "size"`,
		"size>lots":   "expected a size",
		"is:alien":    "not a yes/no field",
		"added<soon":  "expected a date",
		"sort:nope":   "unknown field",
		`"unclosed`:   "unterminated quote",
		"watched:may": "expected yes or no",
	}
	for q, want := range cases {
		_, err := Parse(q)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want containing %q", q, err, want)
		}
	}
}

func TestParseBytes(t *testing.T) {
	cases := map[string]int64{"4G": 4 << 30, "500m": 500 << 20, "1.5T": 3 << 39, "123": 123, "2GiB": 2 << 30}
	for in, want := range cases {
		got, err := ParseBytes(in)
		if err != nil || got != want {
			t.Errorf("ParseBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}
