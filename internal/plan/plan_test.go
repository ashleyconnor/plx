package plan

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/model"
)

func movie(key, section string) *model.Item {
	return &model.Item{Key: key, Kind: "movie", SectionID: section, Title: "m" + key}
}

func track(key string) *model.Item {
	return &model.Item{Key: key, Kind: "track", SectionID: "9", Title: "t" + key}
}

func member(it *model.Item, id string) *model.Item {
	cp := *it
	cp.MemberID = id
	return &cp
}

var (
	videoPL = &model.Container{Kind: model.Playlist, ID: "100", Title: "Watch", ListType: "video"}
	otherPL = &model.Container{Kind: model.Playlist, ID: "200", Title: "Later", ListType: "video"}
	smartPL = &model.Container{Kind: model.Playlist, ID: "300", Title: "Smart", ListType: "video", Smart: true}
	coll    = &model.Container{Kind: model.Collection, ID: "400", Title: "Sci-fi", SectionID: "1", Subtype: "movie"}
)

func TestAddToPlaylistSkipsWrongTypeAndDuplicates(t *testing.T) {
	a, b, c := movie("1", "1"), movie("2", "1"), track("3")
	p := AddTo(videoPL, []*model.Item{a, b, c, a}, []*model.Item{member(b, "pb")})
	if p.Blocked() {
		t.Fatalf("blocked: %+v", p.Violations)
	}
	if len(p.Ops) != 1 || len(p.Ops[0].Items) != 1 || p.Ops[0].Items[0].Key != "1" {
		t.Fatalf("ops = %+v", p.Ops)
	}
	got := strings.Join(p.SkipSummary(), "; ")
	want := "1 × already in playlist; 1 × track can't go in a video playlist"
	if got != want {
		t.Errorf("skips = %q, want %q", got, want)
	}
}

func TestSmartIsReadOnly(t *testing.T) {
	a := movie("1", "1")
	for name, p := range map[string]*Plan{
		"add":     AddTo(smartPL, []*model.Item{a}, nil),
		"remove":  RemoveFrom(smartPL, []*model.Item{a}, []*model.Item{member(a, "x")}),
		"move":    MoveTo(smartPL, videoPL, []*model.Item{a}, nil, nil),
		"reorder": Reorder(smartPL, []*model.Item{member(a, "x")}, []*model.Item{member(a, "x")}),
	} {
		if !p.Blocked() {
			t.Errorf("%s on smart playlist not blocked", name)
		}
	}
}

func TestCollectionRejectsOtherSections(t *testing.T) {
	p := AddTo(coll, []*model.Item{movie("1", "1"), movie("2", "2")}, nil)
	if len(p.Ops) != 1 || len(p.Ops[0].Items) != 1 || len(p.Skipped) != 1 {
		t.Fatalf("plan = %+v", p)
	}
}

func TestRemoveUsesEveryDuplicateMember(t *testing.T) {
	a, b := movie("1", "1"), movie("2", "1")
	members := []*model.Item{member(a, "p1"), member(b, "p2"), member(a, "p3")}
	p := RemoveFrom(videoPL, []*model.Item{a}, members)
	var ids []string
	for _, it := range p.Ops[0].Items {
		ids = append(ids, it.MemberID)
	}
	if strings.Join(ids, ",") != "p1,p3" {
		t.Errorf("removed members %v, want p1,p3", ids)
	}
}

func TestMoveOnlyRemovesWhatWasAdded(t *testing.T) {
	a, b, c := movie("1", "1"), movie("2", "1"), track("3")
	srcMembers := []*model.Item{member(a, "s1"), member(b, "s2"), member(c, "s3")}
	dstMembers := []*model.Item{member(b, "d1")}
	p := MoveTo(videoPL, otherPL, []*model.Item{a, b, c}, srcMembers, dstMembers)
	if p.Blocked() {
		t.Fatalf("blocked: %+v", p.Violations)
	}
	if len(p.Ops) != 2 || p.Ops[0].Kind != OpAdd || p.Ops[1].Kind != OpRemove {
		t.Fatalf("ops = %+v", p.Ops)
	}
	if n := len(p.Ops[0].Items); n != 1 { // b already there, c is audio
		t.Errorf("added %d, want 1", n)
	}
	var removed []string
	for _, it := range p.Ops[1].Items {
		removed = append(removed, it.MemberID)
	}
	// a was added, b already in dst: both leave src. c couldn't be added: it stays.
	if strings.Join(removed, ",") != "s1,s2" {
		t.Errorf("removed %v, want s1,s2", removed)
	}
}

func TestNewPlaylistUsesFirstItemsType(t *testing.T) {
	p := NewPlaylist("x", []*model.Item{track("1"), movie("2", "1"), track("3")})
	if p.Ops[0].ListType != "audio" || len(p.Ops[0].Items) != 2 || len(p.Skipped) != 1 {
		t.Fatalf("plan = %+v", p)
	}
}

func TestLabelsPartitionBySectionAndType(t *testing.T) {
	ep := &model.Item{Key: "3", Kind: "episode", SectionID: "2"}
	have := movie("4", "1")
	have.Labels = []string{"keep"}
	p := AddLabel("keep", []*model.Item{movie("1", "1"), movie("2", "1"), ep, have})
	if len(p.Ops) != 2 || len(p.Ops[0].Items) != 2 || p.Ops[1].SearchType != 4 || len(p.Skipped) != 1 {
		t.Fatalf("plan = %+v", p)
	}
}

func TestReorderProducesDesiredOrder(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 40; n++ {
		for trial := 0; trial < 20; trial++ {
			var cur []*model.Item
			var ids []string
			for i := 0; i < n; i++ {
				id := fmt.Sprint("m", i)
				cur = append(cur, &model.Item{Key: id, MemberID: id})
				ids = append(ids, id)
			}
			want := slices.Clone(cur)
			r.Shuffle(len(want), func(i, j int) { want[i], want[j] = want[j], want[i] })
			p := Reorder(videoPL, cur, want)
			if p.Blocked() {
				if slices.Equal(cur, want) {
					continue
				}
				t.Fatalf("blocked: %+v", p.Violations)
			}
			got, err := Simulate(ids, p.Ops)
			if err != nil {
				t.Fatal(err)
			}
			var wantIDs []string
			for _, it := range want {
				wantIDs = append(wantIDs, it.MemberID)
			}
			if !slices.Equal(got, wantIDs) {
				t.Fatalf("n=%d: got %v want %v", n, got, wantIDs)
			}
		}
	}
}

func TestReorderIsMinimal(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	var cur []*model.Item
	for _, id := range ids {
		cur = append(cur, &model.Item{Key: id, MemberID: id})
	}
	// move e to the front: one move
	want := []*model.Item{cur[4], cur[0], cur[1], cur[2], cur[3]}
	if n := Reorder(videoPL, cur, want).MoveCount(); n != 1 {
		t.Errorf("moves = %d, want 1", n)
	}
	// reverse: n-1 moves
	slices.Reverse(want)
	want = []*model.Item{cur[4], cur[3], cur[2], cur[1], cur[0]}
	if n := Reorder(videoPL, cur, want).MoveCount(); n != 4 {
		t.Errorf("moves = %d, want 4", n)
	}
}

func TestReorderCollectionSwitchesToCustom(t *testing.T) {
	a, b := member(movie("1", "1"), "1"), member(movie("2", "1"), "2")
	p := Reorder(coll, []*model.Item{a, b}, []*model.Item{b, a})
	if p.Ops[0].Kind != OpSetCollectionSortCustom || p.MoveCount() != 1 {
		t.Fatalf("ops = %+v", p.Ops)
	}
}

func TestDelete(t *testing.T) {
	a, b := movie("1", "1"), movie("2", "1")
	a.Files, a.Size = []string{"/m/a.mkv", "/m/a.srt"}, 3<<30
	if p := Delete([]*model.Item{a}, false); !p.Blocked() {
		t.Error("delete allowed when the server forbids it")
	}
	p := Delete([]*model.Item{a, b, a}, true)
	if p.Blocked() || !p.Destructive() || len(p.Ops[0].Items) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	if p.Title != "Delete 2 items (2 files, 3.0G) from disk" {
		t.Errorf("title = %q", p.Title)
	}
}

func TestDeleteVersions(t *testing.T) {
	v := func(id string, mins int, file string) model.Version {
		return model.Version{MediaID: id, Duration: time.Duration(mins) * time.Minute, Size: int64(mins), Files: []string{file}}
	}
	withSample := &model.Item{Key: "1", Title: "a", Versions: []model.Version{v("s", 2, "/a/Sample.mkv"), v("m", 120, "/a/a.mkv")}}
	single := &model.Item{Key: "2", Title: "b", Versions: []model.Version{v("x", 90, "/b/b.mkv")}}
	twoEncodes := &model.Item{Key: "4", Title: "d", Versions: []model.Version{v("e1", 101, "/d/1.mkv"), v("e2", 102, "/d/2.mkv")}}
	sharedFile := &model.Item{Key: "3", Title: "c", Versions: []model.Version{v("p", 100, "/c/c.mkv"), v("q", 101, "/c/c.mkv")}}

	p := DeleteVersions([]*model.Item{withSample, single, sharedFile, twoEncodes}, Picks[0], true)
	if p.Blocked() || !p.Destructive() {
		t.Fatalf("plan = %+v", p)
	}
	op := p.Ops[0]
	if len(op.Versions) != 1 || op.Versions[0].MediaID != "s" {
		t.Errorf("deleting %+v, want only the sample", op.Versions)
	}
	got := strings.Join(p.SkipSummary(), "; ")
	want := "1 × only one version; 2 × no " + Picks[0].Name // sharedFile and twoEncodes
	if got != want {
		t.Errorf("skips = %q, want %q", got, want)
	}
	// a shared file is protected even when the version is explicitly picked
	if p := DeleteVersions([]*model.Item{sharedFile}, PickMedia("p"), true); !p.Blocked() || len(p.Skipped) != 1 {
		t.Errorf("deleting a version sharing a kept file: %+v", p)
	}
	if !DeleteVersions([]*model.Item{withSample}, Picks[0], false).Blocked() {
		t.Error("allowed without server permission")
	}
	if p := DeleteVersions([]*model.Item{withSample}, PickMedia("m"), true); p.Ops[0].Versions[0].MediaID != "m" {
		t.Error("PickMedia picked the wrong version")
	}
}

func TestDeleteNamesAllVersions(t *testing.T) {
	it := movie("1", "1")
	it.Files, it.Size = []string{"/m/Sample.mkv", "/m/m.mkv"}, 2<<30
	it.Versions = []model.Version{{MediaID: "a"}, {MediaID: "b"}}
	p := Delete([]*model.Item{it}, true)
	if want := "Delete 1 items with all 2 versions (2 files, 2.0G) from disk"; p.Title != want {
		t.Errorf("title = %q, want %q", p.Title, want)
	}
}

func TestRenameContainer(t *testing.T) {
	p := Rename(coll, "  Science fiction ")
	if p.Blocked() || len(p.Ops) != 1 || p.Ops[0].Kind != OpRename || p.Ops[0].Title != "Science fiction" || p.Ops[0].Target != coll {
		t.Fatalf("plan = %+v", p)
	}
	if p.Destructive() {
		t.Error("rename is not destructive")
	}
	if Rename(smartPL, "Renamed").Blocked() {
		t.Error("smart playlists can be renamed")
	}
	label := &model.Container{Kind: model.Label, ID: "keep", Title: "keep"}
	for name, p := range map[string]*Plan{
		"empty":     Rename(videoPL, "  "),
		"unchanged": Rename(videoPL, "Watch"),
		"label":     Rename(label, "other"),
	} {
		if !p.Blocked() || len(p.Ops) != 0 {
			t.Errorf("%s: want blocked, got %+v", name, p)
		}
	}
}

func TestDeleteContainer(t *testing.T) {
	for _, c := range []*model.Container{videoPL, smartPL, coll} {
		p := DeleteContainer(c)
		if p.Blocked() || len(p.Ops) != 1 || p.Ops[0].Kind != OpDeleteContainer {
			t.Fatalf("%s: plan = %+v", c, p)
		}
		if !p.Destructive() || p.DeletesFiles() {
			t.Errorf("%s: destructive %v, deletes files %v", c, p.Destructive(), p.DeletesFiles())
		}
	}
	if p := DeleteContainer(&model.Container{Kind: model.Label, ID: "x"}); !p.Blocked() {
		t.Error("labels can't be deleted as containers")
	}
}
