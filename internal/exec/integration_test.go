//go:build integration

// Integration test against a real Plex server. It creates scratch playlists
// (deleted afterwards) and adds then removes a scratch label on a few items.
//
//	PLEX_URL=… PLEX_TOKEN=… PLEX_INSECURE=1 PLX_TEST_SECTION=Movies \
//	  go test -tags integration ./internal/exec/ -v
package exec_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ashleyconnor/plx/internal/exec"
	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/query"
	"github.com/ashleyconnor/plx/internal/source"
)

const (
	scratchPL    = "plx-test"
	scratchPL2   = "plx-test-2"
	scratchLabel = "plx-test-label"
)

func TestWriteActions(t *testing.T) {
	url, token := os.Getenv("PLEX_URL"), os.Getenv("PLEX_TOKEN")
	if url == "" || token == "" {
		t.Skip("PLEX_URL and PLEX_TOKEN not set")
	}
	sectionName := os.Getenv("PLX_TEST_SECTION")
	if sectionName == "" {
		sectionName = "Movies"
	}
	ctx := context.Background()
	client := plex.New(url, token, plex.Options{Insecure: os.Getenv("PLEX_INSECURE") == "1"})
	cat := source.NewCatalog(client)
	mid, err := cat.MachineID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ex := &exec.Executor{C: client, MachineID: mid}

	// Refuse to run if scratch names are already taken: never touch real data.
	pls, err := cat.Playlists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pls {
		if p.Title == scratchPL || p.Title == scratchPL2 {
			t.Fatalf("a playlist named %q already exists; delete it first", p.Title)
		}
	}
	t.Cleanup(func() { deleteScratch(t, cat) })

	sec, err := cat.Section(ctx, sectionName)
	if err != nil {
		t.Fatal(err)
	}
	all, err := cat.Items(ctx, source.Library{Section: sec})
	if err != nil {
		t.Fatal(err)
	}
	var picks []*model.Item
	for _, it := range all {
		if !it.HasLabel(scratchLabel) {
			picks = append(picks, it)
		}
		if len(picks) == 5 {
			break
		}
	}
	if len(picks) < 5 {
		t.Fatalf("need 5 items in %s", sectionName)
	}
	a, b, c, d, e := picks[0], picks[1], picks[2], picks[3], picks[4]

	apply := func(p *plan.Plan) {
		t.Helper()
		if p.Blocked() {
			t.Fatalf("%s blocked: %+v", p.Title, p.Violations)
		}
		for _, op := range p.Ops {
			if op.Kind == plan.OpMove {
				t.Logf("  move %s (%s) after %q", op.Items[0].MemberID, op.Items[0].Title, op.AfterMember)
			}
		}
		if err := ex.Run(ctx, p, nil); err != nil {
			t.Fatalf("%s: %v", p.Title, err)
		}
		cat.Invalidate()
	}
	find := func(title string) *model.Container {
		t.Helper()
		pls, err := cat.Playlists(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pls {
			if p.Title == title {
				return p
			}
		}
		t.Fatalf("playlist %q not found", title)
		return nil
	}
	members := func(pl *model.Container) []*model.Item {
		t.Helper()
		ms, err := cat.Items(ctx, source.Members{C: pl})
		if err != nil {
			t.Fatal(err)
		}
		return ms
	}
	expect := func(pl *model.Container, want ...*model.Item) {
		t.Helper()
		var got, w []string
		for _, it := range members(pl) {
			got = append(got, it.Key)
		}
		for _, it := range want {
			w = append(w, it.Key)
		}
		if !slices.Equal(got, w) {
			t.Fatalf("%s = %v, want %v", pl, got, w)
		}
	}

	t.Log("create playlist with a, b, c")
	apply(plan.NewPlaylist(scratchPL, []*model.Item{a, b, c}))
	pl := find(scratchPL)
	expect(pl, a, b, c)

	t.Log("add c, d, e: c is skipped as a duplicate")
	p := plan.AddTo(pl, []*model.Item{c, d, e}, members(pl))
	if len(p.Skipped) != 1 {
		t.Fatalf("skipped = %+v", p.Skipped)
	}
	apply(p)
	expect(pl, a, b, c, d, e)

	t.Log("reorder by title descending")
	cur := members(pl)
	desired := slices.Clone(cur)
	query.SortItems(desired, []query.SortKey{{Field: field.Title, Desc: true}})
	apply(plan.Reorder(pl, cur, desired))
	expect(pl, desired...)

	t.Log("reorder back to a..e (moves to the top and after)")
	cur = members(pl)
	byKey := map[string]*model.Item{}
	for _, it := range cur {
		byKey[it.Key] = it
	}
	apply(plan.Reorder(pl, cur, []*model.Item{byKey[a.Key], byKey[b.Key], byKey[c.Key], byKey[d.Key], byKey[e.Key]}))
	expect(pl, a, b, c, d, e)

	t.Log("move a, b into a new playlist")
	apply(plan.NewPlaylistFrom(pl, scratchPL2, []*model.Item{a, b}, members(pl)))
	pl2 := find(scratchPL2)
	expect(pl, c, d, e)
	expect(pl2, a, b)

	t.Log("move a back into the first playlist")
	apply(plan.MoveTo(pl2, pl, []*model.Item{a}, members(pl2), members(pl)))
	expect(pl, c, d, e, a)
	expect(pl2, b)

	t.Log("remove d")
	apply(plan.RemoveFrom(pl, []*model.Item{d}, members(pl)))
	expect(pl, c, e, a)

	t.Log("add a label to the playlist's items, then remove it")
	labelled := func() []string {
		var ks []string
		for _, it := range members(pl) {
			if it.HasLabel(scratchLabel) {
				ks = append(ks, it.Key)
			}
		}
		return ks
	}
	apply(plan.AddLabel(scratchLabel, members(pl)))
	if got := labelled(); len(got) != 3 {
		t.Fatalf("labelled after add = %v, want 3 items", got)
	}
	apply(plan.RemoveLabel(scratchLabel, members(pl)))
	if got := labelled(); len(got) != 0 {
		t.Fatalf("labelled after remove = %v, want none", got)
	}

	t.Log("move from a label: labelling, then moving out removes the label")
	apply(plan.AddLabel(scratchLabel, []*model.Item{b}))
	lab := &model.Container{Kind: model.Label, ID: scratchLabel, Title: scratchLabel, SectionID: sec.ID}
	labMembers := members(lab)
	if len(labMembers) != 1 {
		t.Fatalf("label members = %d, want 1", len(labMembers))
	}
	apply(plan.MoveTo(lab, pl, labMembers, labMembers, members(pl)))
	expect(pl, c, e, a, b)
	if n := len(members(lab)); n != 0 {
		t.Fatalf("label still on %d items", n)
	}
}

// deleteScratch removes the scratch playlists and any leftover scratch label.
func deleteScratch(t *testing.T, cat *source.Catalog) {
	ctx := context.Background()
	cat.Invalidate()
	pls, err := cat.Playlists(ctx)
	if err != nil {
		t.Errorf("cleanup: %v", err)
		return
	}
	for _, p := range pls {
		if p.Title == scratchPL || p.Title == scratchPL2 {
			if err := cat.C.DeletePlaylist(ctx, p.ID); err != nil {
				t.Errorf("cleanup: delete %s: %v", p, err)
			} else {
				t.Logf("cleanup: deleted %s", p)
			}
		}
	}
	secs, _ := cat.Sections(ctx)
	for _, s := range secs {
		labels, _ := cat.Labels(ctx, s.ID)
		if !slices.Contains(labels, scratchLabel) {
			continue
		}
		items, _ := cat.Items(ctx, source.Members{C: &model.Container{Kind: model.Label, ID: scratchLabel, SectionID: s.ID}})
		if p := plan.RemoveLabel(scratchLabel, items); !p.Blocked() {
			ex := &exec.Executor{C: cat.C}
			if err := ex.Run(ctx, p, nil); err != nil && !strings.Contains(err.Error(), "nothing") {
				t.Errorf("cleanup: remove label: %v", err)
			}
		}
	}
}
