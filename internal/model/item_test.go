package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/plex"
)

func TestKindListType(t *testing.T) {
	cases := map[Kind]string{
		"movie": "video", "episode": "video", "clip": "video", "show": "video", "season": "video",
		"track": "audio", "album": "audio", "artist": "audio",
		"photo": "photo", "collection": "", "": "", "weird": "",
	}
	for k, want := range cases {
		if got := k.ListType(); got != want {
			t.Errorf("%q.ListType() = %q, want %q", k, got, want)
		}
	}
}

func TestKindSearchType(t *testing.T) {
	if Kind("movie").SearchType() != 1 || Kind("episode").SearchType() != 4 || Kind("nope").SearchType() != 0 {
		t.Error("search types")
	}
}

func TestPrimaryPrefersLongestThenLargest(t *testing.T) {
	var none Item
	if none.Primary() != nil {
		t.Error("no versions, no primary")
	}
	it := &Item{Versions: []Version{
		{MediaID: "a", Duration: time.Hour, Size: 1},
		{MediaID: "b", Duration: time.Hour, Size: 9},
		{MediaID: "c", Duration: time.Minute, Size: 99},
	}}
	if p := it.Primary(); p.MediaID != "b" {
		t.Errorf("primary = %s, want b (tie on duration goes to larger)", p.MediaID)
	}
}

func TestByDurationDoesNotReorderItem(t *testing.T) {
	it := &Item{Versions: []Version{{MediaID: "long", Duration: time.Hour}, {MediaID: "short", Duration: time.Minute}}}
	got := it.ByDuration()
	if got[0].MediaID != "short" || it.Versions[0].MediaID != "long" {
		t.Errorf("sorted %v, original %v", got, it.Versions)
	}
}

func TestItemHelpers(t *testing.T) {
	it := &Item{Files: []string{"/tv/Show/S01/e1.mkv", "/other"}, ViewCount: 1, ViewOffset: time.Second, Labels: []string{"keep"}}
	if it.Path() != "/tv/Show/S01/e1.mkv" || it.Dir() != "/tv/Show/S01" {
		t.Errorf("path %q dir %q", it.Path(), it.Dir())
	}
	if !it.Watched() || !it.InProgress() || !it.HasLabel("keep") || it.HasLabel("Keep") {
		t.Error("flags (labels are case-sensitive)")
	}
	var empty Item
	if empty.Path() != "" || empty.Dir() != "" || empty.Watched() || empty.InProgress() {
		t.Error("zero item")
	}
}

func TestDisplayTitle(t *testing.T) {
	cases := []struct {
		it   Item
		want string
	}{
		{Item{Kind: "movie", Title: "Heat"}, "Heat"},
		{Item{Kind: "episode", Show: "Show", SeasonNum: 1, Index: 5, Title: "Pilot"}, "Show S01E05 · Pilot"},
		{Item{Kind: "track", Show: "Artist", Title: "Song"}, "Artist · Song"},
	}
	for _, tc := range cases {
		if got := tc.it.DisplayTitle(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestContainerCaps(t *testing.T) {
	cases := []struct {
		c    Container
		want Caps
	}{
		{Container{Kind: Playlist}, Caps{Ordered: true, CrossSection: true}},
		{Container{Kind: Playlist, Smart: true}, Caps{Ordered: true, ReadOnly: true, CrossSection: true}},
		{Container{Kind: Collection}, Caps{Ordered: true, MixedKinds: true}},
		{Container{Kind: Collection, Smart: true}, Caps{Ordered: true, ReadOnly: true, MixedKinds: true}},
		{Container{Kind: Label}, Caps{MixedKinds: true}},
	}
	for _, tc := range cases {
		if got := tc.c.Caps(); got != tc.want {
			t.Errorf("%v smart=%v: caps %+v, want %+v", tc.c.Kind, tc.c.Smart, got, tc.want)
		}
	}
}

func TestContainerString(t *testing.T) {
	if s := (&Container{Kind: Collection, Title: "Sci-fi"}).String(); s != `collection "Sci-fi"` {
		t.Errorf("got %s", s)
	}
	if ContainerKind(9).String() != "?" || Playlist.String() != "playlist" || Label.String() != "label" {
		t.Error("kind names")
	}
}

func decode(t *testing.T, js string) *plex.Metadata {
	t.Helper()
	var m plex.Metadata
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestFromPlexEpisode(t *testing.T) {
	m := decode(t, `{"ratingKey":123,"type":"episode","title":"Pilot","grandparentTitle":"Show","grandparentRatingKey":"10",
		"parentRatingKey":"11","parentTitle":"Season 1","parentIndex":1,"index":2,"year":2020,"librarySectionID":3,
		"librarySectionTitle":"TV","viewCount":2,"viewOffset":5000,"lastViewedAt":1700000000,"addedAt":0,"duration":60000,
		"Label":[{"tag":"x"}],"Genre":[{"tag":"Drama"}]}`)
	it := FromPlex(m)
	if it.Key != "123" || it.Kind != "episode" || it.SectionID != "3" || it.Show != "Show" || it.ShowKey != "10" ||
		it.SeasonKey != "11" || it.SeasonNum != 1 || it.Index != 2 || it.Year != 2020 {
		t.Errorf("ids/titles: %+v", it)
	}
	if it.SortTitle != "Pilot" {
		t.Errorf("sort title should default to title, got %q", it.SortTitle)
	}
	if it.ViewOffset != 5*time.Second || it.Duration != time.Minute || it.ViewCount != 2 {
		t.Errorf("times: %+v", it)
	}
	if !it.LastViewed.Equal(time.Unix(1700000000, 0)) || !it.Added.IsZero() {
		t.Errorf("last viewed %v added %v (0 must stay the zero time)", it.LastViewed, it.Added)
	}
	if len(it.Labels) != 1 || it.Labels[0] != "x" || len(it.Genres) != 1 || it.Raw != m {
		t.Errorf("tags/raw: %+v", it)
	}
}

func TestFromPlexTrackUsesParentAsArtist(t *testing.T) {
	it := FromPlex(decode(t, `{"ratingKey":"1","type":"track","title":"Song","parentTitle":"Album","titleSort":"song, the"}`))
	if it.Show != "Album" || it.SortTitle != "song, the" {
		t.Errorf("show %q sort %q", it.Show, it.SortTitle)
	}
	it = FromPlex(decode(t, `{"ratingKey":"1","type":"track","title":"Song","grandparentTitle":"Artist","parentTitle":"Album"}`))
	if it.Show != "Artist" {
		t.Errorf("show = %q", it.Show)
	}
}

func TestFromPlexCountsSharedFilesOnce(t *testing.T) {
	// Plex repeats the same part under two Media entries
	m := decode(t, `{"ratingKey":"1","type":"movie","Media":[
		{"id":1,"duration":60000,"Part":[{"file":"/m/a.mkv","size":100}]},
		{"id":2,"duration":60000,"Part":[{"file":"/m/a.mkv","size":100}]}]}`)
	it := FromPlex(m)
	if it.Size != 100 || len(it.Files) != 1 {
		t.Errorf("size %d files %v, want the shared file counted once", it.Size, it.Files)
	}
	if len(it.Versions) != 2 || it.Versions[0].Size != 100 || it.Versions[1].Size != 100 || len(it.Versions[1].Files) != 1 {
		t.Errorf("each version still lists the file: %+v", it.Versions)
	}
}

func TestFromPlexVersionDurationFallsBackToPart(t *testing.T) {
	m := decode(t, `{"ratingKey":"1","type":"movie","duration":1000,"Media":[
		{"id":1,"Part":[{"file":"/m/a.mkv","size":1,"duration":7200000}]}]}`)
	it := FromPlex(m)
	if it.Versions[0].Duration != 2*time.Hour || it.Duration != 2*time.Hour {
		t.Errorf("version %v item %v", it.Versions[0].Duration, it.Duration)
	}
}

func TestFromPlexKeepsItemDurationWhenVersionsHaveNone(t *testing.T) {
	it := FromPlex(decode(t, `{"ratingKey":"1","type":"movie","duration":60000,"Media":[{"id":1}]}`))
	if it.Duration != time.Minute {
		t.Errorf("duration = %v", it.Duration)
	}
}

func TestFromPlexMultiPartVersionSumsSize(t *testing.T) {
	it := FromPlex(decode(t, `{"ratingKey":"1","type":"movie","Media":[{"id":1,"Part":[
		{"file":"/m/cd1.mkv","size":10},{"file":"/m/cd2.mkv","size":20},{"size":5}]}]}`))
	if it.Versions[0].Size != 35 || len(it.Versions[0].Files) != 2 {
		t.Errorf("version = %+v", it.Versions[0])
	}
}
