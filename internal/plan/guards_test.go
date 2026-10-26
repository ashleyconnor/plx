package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/model"
)

var (
	labelC = &model.Container{Kind: model.Label, ID: "keep", Title: "keep", SectionID: "1"}
	smartC = &model.Container{Kind: model.Collection, ID: "500", Title: "SmartC", SectionID: "1", Smart: true}
)

func hasViolation(p *Plan, sub string) bool {
	for _, v := range p.Violations {
		if strings.Contains(v.Message, sub) {
			return true
		}
	}
	return false
}

func TestAddToGuards(t *testing.T) {
	a := movie("1", "1")
	tests := []struct {
		name string
		p    *Plan
		want string
	}{
		{"smart playlist", AddTo(smartPL, []*model.Item{a}, nil), "smart"},
		{"smart collection", AddTo(smartC, []*model.Item{a}, nil), "smart"},
		{"nothing admissible", AddTo(videoPL, []*model.Item{track("9")}, nil), "nothing to add"},
		{"all duplicates", AddTo(videoPL, []*model.Item{a}, []*model.Item{member(a, "x")}), "nothing to add"},
		{"no items", AddTo(videoPL, nil, nil), "nothing to add"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.p.Blocked() || !hasViolation(tc.p, tc.want) {
				t.Errorf("violations = %+v, want block containing %q", tc.p.Violations, tc.want)
			}
			if len(tc.p.Ops) != 0 {
				t.Errorf("blocked plan has ops: %+v", tc.p.Ops)
			}
		})
	}
}

func TestAddToCollectionSkipReasonAndSameLibrary(t *testing.T) {
	p := AddTo(coll, []*model.Item{movie("2", "2")}, nil)
	if !p.Blocked() || len(p.Skipped) != 1 || p.Skipped[0].Reason != "collection is in a different library" {
		t.Fatalf("plan = %+v", p)
	}
}

func TestAddToLabelDelegatesToLabels(t *testing.T) {
	a := movie("1", "1")
	p := AddTo(labelC, []*model.Item{a}, nil)
	if p.Blocked() || p.Ops[0].Kind != OpAddLabel || p.Ops[0].Label != "keep" {
		t.Fatalf("plan = %+v", p)
	}
	rm := RemoveFrom(labelC, []*model.Item{a}, nil)
	if !rm.Blocked() || !hasViolation(rm, "nothing to unlabel") {
		t.Errorf("removing a label the item lacks: %+v", rm)
	}
	a.Labels = []string{"keep"}
	rm = RemoveFrom(labelC, []*model.Item{a}, nil)
	if rm.Blocked() || rm.Ops[0].Kind != OpRemoveLabel {
		t.Errorf("remove label plan = %+v", rm)
	}
}

func TestRemoveFromGuards(t *testing.T) {
	a, b := movie("1", "1"), movie("2", "1")
	p := RemoveFrom(videoPL, []*model.Item{b}, []*model.Item{member(a, "p1")})
	if !p.Blocked() || !hasViolation(p, "nothing to remove") {
		t.Errorf("item not in container: %+v", p.Violations)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Reason != "not in playlist" {
		t.Errorf("skipped = %+v", p.Skipped)
	}
}

func TestMoveToGuards(t *testing.T) {
	a := movie("1", "1")
	m := []*model.Item{member(a, "s1")}
	if p := MoveTo(videoPL, videoPL, []*model.Item{a}, m, nil); !hasViolation(p, "same") {
		t.Errorf("same container: %+v", p.Violations)
	}
	// destination smart: the add plan's block is returned and nothing is removed
	p := MoveTo(videoPL, smartPL, []*model.Item{a}, m, nil)
	if !p.Blocked() || len(p.Ops) != 0 || !hasViolation(p, "smart") {
		t.Errorf("smart destination: %+v", p)
	}
	// nothing can be added and nothing is already there: nothing leaves src
	p = MoveTo(videoPL, otherPL, []*model.Item{track("3")}, []*model.Item{member(track("3"), "s")}, nil)
	if !p.Blocked() || !hasViolation(p, "nothing to move") || len(p.Ops) != 0 {
		t.Errorf("unmovable: %+v", p)
	}
	if len(p.Skipped) != 1 {
		t.Errorf("skip reason lost: %+v", p.Skipped)
	}
}

func TestMoveToAlreadyInDestinationStillLeavesSource(t *testing.T) {
	a := movie("1", "1")
	p := MoveTo(videoPL, otherPL, []*model.Item{a}, []*model.Item{member(a, "s1")}, []*model.Item{member(a, "d1")})
	if p.Blocked() || len(p.Ops) != 1 || p.Ops[0].Kind != OpRemove {
		t.Fatalf("plan = %+v", p)
	}
}

func TestMoveFromLabelOrCollectionSources(t *testing.T) {
	a := movie("1", "1")
	a.Labels = []string{"keep"}
	p := MoveTo(labelC, videoPL, []*model.Item{a}, nil, nil)
	if p.Blocked() || len(p.Ops) != 2 || p.Ops[1].Kind != OpRemoveLabel {
		t.Fatalf("label -> playlist: %+v", p)
	}
	if p := MoveTo(smartC, videoPL, []*model.Item{a}, nil, nil); !p.Blocked() {
		t.Error("smart collection source not blocked")
	}
}

func TestNewPlaylistGuards(t *testing.T) {
	a := movie("1", "1")
	for name, p := range map[string]*Plan{
		"blank title":  NewPlaylist("  ", []*model.Item{a}),
		"no items":     NewPlaylist("x", nil),
		"unlistable":   NewPlaylist("x", []*model.Item{{Key: "9", Kind: "weird"}}),
		"coll blank":   NewCollection("", []*model.Item{a}),
		"coll no item": NewCollection("x", nil),
	} {
		if !p.Blocked() || len(p.Ops) != 0 {
			t.Errorf("%s: %+v", name, p)
		}
	}
}

func TestNewPlaylistSkipsUnlistableKinds(t *testing.T) {
	p := NewPlaylist("x", []*model.Item{{Key: "9", Kind: "weird"}, movie("1", "1"), movie("1", "1")})
	if p.Blocked() || len(p.Ops[0].Items) != 1 || len(p.Skipped) != 1 {
		t.Fatalf("plan = %+v", p)
	}
	if got := p.SkipSummary(); len(got) != 1 || got[0] != "1 × weird can't go in a playlist" {
		t.Errorf("summary = %v", got)
	}
}

func TestNewCollectionOneLibraryOneType(t *testing.T) {
	show := &model.Item{Key: "5", Kind: "show", SectionID: "1"}
	p := NewCollection("c", []*model.Item{movie("1", "1"), movie("2", "2"), show, movie("3", "1")})
	if p.Blocked() {
		t.Fatalf("blocked: %+v", p.Violations)
	}
	op := p.Ops[0]
	if op.Kind != OpCreateCollection || op.SectionID != "1" || op.SearchType != 1 || len(op.Items) != 2 {
		t.Errorf("op = %+v", op)
	}
	if len(p.Skipped) != 2 {
		t.Errorf("skipped = %+v", p.Skipped)
	}
}

func TestNewFromMovesOutOfSource(t *testing.T) {
	a, b := movie("1", "1"), movie("2", "1")
	members := []*model.Item{member(a, "p1"), member(b, "p2")}

	pl := NewPlaylistFrom(videoPL, "n", []*model.Item{a}, members)
	if pl.Blocked() || len(pl.Ops) != 2 || pl.Ops[0].Kind != OpCreatePlaylist || pl.Ops[1].Kind != OpRemove ||
		pl.Ops[1].Items[0].MemberID != "p1" {
		t.Fatalf("playlist plan = %+v", pl)
	}
	co := NewCollectionFrom(coll, "n", []*model.Item{b}, []*model.Item{member(b, "400b")})
	if co.Blocked() || len(co.Ops) != 2 || co.Ops[1].Kind != OpRemove {
		t.Fatalf("collection plan = %+v", co)
	}
	// smart source: items can't leave it, so the create is blocked too
	for name, p := range map[string]*Plan{
		"playlist":   NewPlaylistFrom(smartPL, "n", []*model.Item{a}, members),
		"collection": NewCollectionFrom(smartC, "n", []*model.Item{a}, members),
	} {
		if !p.Blocked() || !hasViolation(p, "smart") {
			t.Errorf("%s from smart source: %+v", name, p)
		}
	}
	// creation guards propagate
	if p := NewPlaylistFrom(videoPL, "", []*model.Item{a}, members); !p.Blocked() {
		t.Error("blank title not blocked")
	}
	if p := NewCollectionFrom(coll, "", []*model.Item{a}, members); !p.Blocked() {
		t.Error("blank collection title not blocked")
	}
}

func TestLabelGuards(t *testing.T) {
	if p := AddLabel(" ", []*model.Item{movie("1", "1")}); !p.Blocked() || !hasViolation(p, "empty") {
		t.Errorf("blank label: %+v", p)
	}
	have := movie("1", "1")
	have.Labels = []string{"x"}
	if p := AddLabel("x", []*model.Item{have}); !hasViolation(p, "nothing to label") {
		t.Errorf("already labelled: %+v", p)
	}
	p := RemoveLabel("x", []*model.Item{have, movie("2", "1")})
	if p.Blocked() || len(p.Ops) != 1 || len(p.Skipped) != 1 {
		t.Errorf("remove label: %+v", p)
	}
}

func TestLabelOpsAreOrderedBySection(t *testing.T) {
	p := AddLabel("x", []*model.Item{movie("1", "3"), movie("2", "1"), movie("3", "2")})
	var secs []string
	for _, o := range p.Ops {
		secs = append(secs, o.SectionID)
	}
	if strings.Join(secs, ",") != "1,2,3" {
		t.Errorf("sections = %v", secs)
	}
}

func TestReorderGuards(t *testing.T) {
	a := &model.Item{Key: "a", MemberID: "a"}
	b := &model.Item{Key: "b", MemberID: "b"}
	c := &model.Item{Key: "c", MemberID: "c"}
	tests := []struct {
		name string
		p    *Plan
		want string
	}{
		{"label unordered", Reorder(labelC, nil, nil), "no order"},
		{"length mismatch", Reorder(videoPL, []*model.Item{a, b}, []*model.Item{a}), "needs every member"},
		{"unknown member", Reorder(videoPL, []*model.Item{a, b}, []*model.Item{a, c}), "not a member"},
		{"already ordered", Reorder(videoPL, []*model.Item{a, b}, []*model.Item{a, b}), "already in that order"},
		{"smart collection", Reorder(smartC, []*model.Item{a}, []*model.Item{a}), "smart"},
	}
	for _, tc := range tests {
		if !tc.p.Blocked() || !hasViolation(tc.p, tc.want) || len(tc.p.Ops) != 0 {
			t.Errorf("%s: %+v", tc.name, tc.p)
		}
	}
}

func TestReorderWarnsOnManyMoves(t *testing.T) {
	var cur, rev []*model.Item
	for i := 0; i < 250; i++ {
		id := string(rune('A'+i/26)) + string(rune('a'+i%26))
		cur = append(cur, &model.Item{Key: id, MemberID: id})
	}
	for i := len(cur) - 1; i >= 0; i-- {
		rev = append(rev, cur[i])
	}
	p := Reorder(videoPL, cur, rev)
	if p.Blocked() || !hasViolation(p, "moves") || p.MoveCount() != 249 {
		t.Fatalf("moves = %d, violations = %+v", p.MoveCount(), p.Violations)
	}
}

func TestReorderMoveToTopHasNoPredecessor(t *testing.T) {
	a := &model.Item{Key: "a", MemberID: "a"}
	b := &model.Item{Key: "b", MemberID: "b"}
	p := Reorder(videoPL, []*model.Item{a, b}, []*model.Item{b, a})
	if p.MoveCount() != 1 {
		t.Fatalf("ops = %+v", p.Ops)
	}
	// whichever is moved, the simulation must reach the target
	got, err := Simulate([]string{"a", "b"}, p.Ops)
	if err != nil || strings.Join(got, "") != "ba" {
		t.Errorf("got %v err %v", got, err)
	}
}

func TestLISKeepsLongestIncreasingRun(t *testing.T) {
	tests := []struct {
		seq  []int
		kept int
	}{
		{nil, 0},
		{[]int{0, 1, 2, 3}, 4},
		{[]int{3, 2, 1, 0}, 1},
		{[]int{2, 0, 1, 3}, 3},
	}
	for _, tc := range tests {
		n := 0
		for _, k := range lis(tc.seq) {
			if k {
				n++
			}
		}
		if n != tc.kept {
			t.Errorf("lis(%v) keeps %d, want %d", tc.seq, n, tc.kept)
		}
	}
}

func TestSimulateErrors(t *testing.T) {
	x := &model.Item{MemberID: "x"}
	if _, err := Simulate([]string{"a"}, []Op{{Kind: OpMove, Items: []*model.Item{x}}}); err == nil {
		t.Error("moving a missing member should fail")
	}
	a := &model.Item{MemberID: "a"}
	if _, err := Simulate([]string{"a"}, []Op{{Kind: OpMove, Items: []*model.Item{a}, AfterMember: "zz"}}); err == nil {
		t.Error("moving after a missing member should fail")
	}
	got, err := Simulate([]string{"a", "b"}, []Op{{Kind: OpAdd}})
	if err != nil || strings.Join(got, "") != "ab" {
		t.Errorf("non-move ops must be ignored: %v %v", got, err)
	}
}

func TestDeleteGuardsAndNonDestructive(t *testing.T) {
	if p := Delete(nil, true); !p.Blocked() || !hasViolation(p, "nothing to delete") {
		t.Errorf("empty delete: %+v", p)
	}
	if p := Delete([]*model.Item{movie("1", "1")}, false); len(p.Ops) != 0 || p.Destructive() {
		t.Errorf("forbidden delete produced ops: %+v", p)
	}
	p := Delete([]*model.Item{movie("1", "1")}, true)
	warns := 0
	for _, v := range p.Violations {
		if v.Severity == Warn {
			warns++
		}
	}
	if warns != 2 || p.Blocked() {
		t.Errorf("want two warnings and no block: %+v", p.Violations)
	}
}

func TestDeleteVersionsGuards(t *testing.T) {
	v := func(id string, mins int, file string) model.Version {
		return model.Version{MediaID: id, Duration: time.Duration(mins) * time.Minute, Size: int64(mins), Bitrate: mins, Files: []string{file}}
	}
	one := &model.Item{Key: "1", Versions: []model.Version{v("a", 90, "/a")}}
	if p := DeleteVersions([]*model.Item{one}, Picks[1], true); !p.Blocked() || len(p.Ops) != 0 || !hasViolation(p, "nothing to delete") {
		t.Errorf("single version: %+v", p)
	}
	// a pick that selects every version must never delete them all
	all := VersionPick{Name: "all", Pick: func(it *model.Item) []model.Version { return it.Versions }}
	two := &model.Item{Key: "2", Versions: []model.Version{v("a", 90, "/a"), v("b", 91, "/b")}}
	p := DeleteVersions([]*model.Item{two}, all, true)
	if !p.Blocked() || len(p.Skipped) != 1 || p.Skipped[0].Reason != "would delete every version" {
		t.Errorf("delete-all pick: %+v", p)
	}
	// missing media ID
	if p := DeleteVersions([]*model.Item{two}, PickMedia("zz"), true); !p.Blocked() || len(p.Skipped) != 1 {
		t.Errorf("unknown media: %+v", p)
	}
}

func TestVersionPicksByRule(t *testing.T) {
	it := &model.Item{Key: "1", Versions: []model.Version{
		{MediaID: "big", Size: 100, Bitrate: 50, Duration: time.Hour, Files: []string{"/big"}},
		{MediaID: "small", Size: 10, Bitrate: 5, Duration: time.Hour, Files: []string{"/small"}},
		{MediaID: "mid", Size: 50, Bitrate: 20, Duration: time.Hour, Files: []string{"/mid"}},
	}}
	want := map[string]string{
		"smallest file":  "small",
		"largest file":   "big",
		"lowest bitrate": "small",
	}
	for _, pk := range Picks[1:] {
		got := pk.Pick(it)
		if len(got) != 1 || got[0].MediaID != want[pk.Name] {
			t.Errorf("%s picked %+v, want %s", pk.Name, got, want[pk.Name])
		}
		p := DeleteVersions([]*model.Item{it}, pk, true)
		if p.Blocked() || len(p.Ops[0].Versions) != 1 || !strings.Contains(p.Title, pk.Name) {
			t.Errorf("%s plan = %+v", pk.Name, p)
		}
	}
	// samples: nothing is under half the longest
	if got := Picks[0].Pick(it); len(got) != 0 {
		t.Errorf("samples picked %+v", got)
	}
}

func TestDeleteVersionsSharedFileWithSampleStillDeletesOthers(t *testing.T) {
	// two samples (one sharing the movie's file) and a movie: the independent sample goes, the shared one is kept
	it := &model.Item{Key: "1", Versions: []model.Version{
		{MediaID: "m", Duration: 100 * time.Minute, Files: []string{"/m.mkv"}},
		{MediaID: "s1", Duration: time.Minute, Size: 5, Files: []string{"/s1.mkv"}},
		{MediaID: "s2", Duration: time.Minute, Size: 5, Files: []string{"/m.mkv"}},
	}}
	p := DeleteVersions([]*model.Item{it}, Picks[0], true)
	if p.Blocked() || len(p.Ops[0].Versions) != 1 || p.Ops[0].Versions[0].MediaID != "s1" {
		t.Fatalf("plan = %+v", p)
	}
	if len(p.Skipped) != 1 || !strings.Contains(p.Skipped[0].Reason, "also used") {
		t.Errorf("skipped = %+v", p.Skipped)
	}
}

func TestDescribe(t *testing.T) {
	a := &model.Item{Key: "1", Kind: "movie", Title: "Heat"}
	ops := []struct {
		op   Op
		want string
	}{
		{Op{Kind: OpCreatePlaylist, ListType: "video", Title: "P", Items: []*model.Item{a}}, `create video playlist "P" with 1 items`},
		{Op{Kind: OpCreateCollection, Title: "C", Items: []*model.Item{a, a}}, `create collection "C" with 2 items`},
		{Op{Kind: OpAdd, Target: videoPL, Items: []*model.Item{a}}, `add 1 items to playlist "Watch"`},
		{Op{Kind: OpRemove, Target: videoPL, Items: []*model.Item{a}}, `remove 1 items from playlist "Watch"`},
		{Op{Kind: OpMove, Target: videoPL, Items: []*model.Item{a}}, `move "Heat" in playlist "Watch"`},
		{Op{Kind: OpSetCollectionSortCustom, Target: coll}, `set collection "Sci-fi" to custom sort order`},
		{Op{Kind: OpAddLabel, Label: "x", Items: []*model.Item{a}}, `add label "x" to 1 items`},
		{Op{Kind: OpRemoveLabel, Label: "x", Items: []*model.Item{a}}, `remove label "x" from 1 items`},
		{Op{Kind: OpDelete, Items: []*model.Item{a}}, "delete 1 items and their files from disk"},
		{Op{Kind: OpDeleteVersion, Items: []*model.Item{a}}, "delete 1 versions and their files from disk"},
		{Op{Kind: OpKind(99)}, "?"},
	}
	for _, tc := range ops {
		if got := tc.op.Describe(); got != tc.want {
			t.Errorf("Describe = %q, want %q", got, tc.want)
		}
	}
}

func TestSkipSummaryGroupsInFirstSeenOrder(t *testing.T) {
	p := &Plan{}
	a := movie("1", "1")
	p.skip(a, "b")
	p.skip(a, "a")
	p.skip(a, "b")
	if got := strings.Join(p.SkipSummary(), "; "); got != "2 × b; 1 × a" {
		t.Errorf("summary = %q", got)
	}
	if (&Plan{}).Blocked() {
		t.Error("empty plan must not be blocked")
	}
	p.warn("w")
	if p.Blocked() {
		t.Error("warnings must not block")
	}
}
