package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/query"
)

// fake is a tiny Plex server: Movies (1) and TV (2) sections, two movies,
// a show with one episode, labels, a playlist and a collection.
type fake struct {
	mu    sync.Mutex
	calls map[string]int // path?query -> count
	fail  map[string]bool
}

func (f *fake) hits(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, v := range f.calls {
		if strings.HasPrefix(k, prefix) {
			n += v
		}
	}
	return n
}

func newFake(t *testing.T) (*Catalog, *fake) {
	t.Helper()
	f := &fake{calls: map[string]int{}, fail: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls[r.URL.Path+"?"+r.URL.Query().Get("type")]++
		bad := f.fail[r.URL.Path]
		f.mu.Unlock()
		if bad {
			http.Error(w, "boom", 400)
			return
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/identity":
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"MID"}}`)
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[
				{"key":"2","title":"TV","type":"show"},{"key":"1","title":"Movies","type":"movie"},{"key":"3","title":"Other","type":"weird"}]}}`)
		case "/library/sections/1/all":
			if l := q.Get("label"); l != "" {
				if l == "L1" {
					fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"10"}]}}`)
				} else {
					fmt.Fprint(w, `{"MediaContainer":{"Metadata":[]}}`)
				}
				return
			}
			fmt.Fprint(w, `{"MediaContainer":{"librarySectionTitle":"Movies","Metadata":[
				{"ratingKey":"10","type":"movie","title":"Alien","Media":[{"id":1,"Part":[{"file":"/m/alien.mkv","size":100}]}]},
				{"ratingKey":"11","type":"movie","title":"Brazil"}]}}`)
		case "/library/sections/2/all":
			switch {
			case q.Get("label") == "L2" && q.Get("type") == "2":
				fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"20"}]}}`)
			case q.Get("label") != "":
				fmt.Fprint(w, `{"MediaContainer":{"Metadata":[]}}`)
			default:
				fmt.Fprint(w, `{"MediaContainer":{"librarySectionTitle":"TV","Metadata":[
					{"ratingKey":"21","type":"episode","title":"Pilot","grandparentRatingKey":"20","parentRatingKey":"25"}]}}`)
			}
		case "/library/sections/1/label":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"L1","title":"keep"},{"key":"LX","title":"orphan"}]}}`)
		case "/library/sections/2/label":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"L2","title":"binge"}]}}`)
		case "/playlists":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[
				{"ratingKey":"51","title":"zeta","playlistType":"video","smart":1,"leafCount":3},
				{"ratingKey":"50","title":"Alpha","playlistType":"video"}]}}`)
		case "/playlists/50/items":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"10","type":"movie","title":"Alien","playlistItemID":"900"},
				{"ratingKey":"10","type":"movie","title":"Alien","playlistItemID":"901"}]}}`)
		case "/playlists/51/items":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[]}}`)
		case "/library/sections/1/collections":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"60","title":"Sci-fi","subtype":"movie","childCount":2}]}}`)
		case "/library/sections/2/collections":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"61","title":"Shows","subtype":"show"}]}}`)
		case "/library/collections/60/children":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"11","type":"movie","title":"Brazil"}]}}`)
		case "/library/collections/61/children":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"20","type":"show","title":"The Show"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return NewCatalog(plex.New(srv.URL, "tok", plex.Options{})), f
}

var ctx = context.Background()

func titles(items []*model.Item) string {
	var ts []string
	for _, it := range items {
		ts = append(ts, it.Title)
	}
	return strings.Join(ts, ",")
}

func TestSectionsFiltersUnknownTypesSortsAndCaches(t *testing.T) {
	c, f := newFake(t)
	ss, err := c.Sections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 || ss[0].Title != "Movies" || ss[1].Title != "TV" {
		t.Fatalf("sections = %+v", ss)
	}
	c.Sections(ctx)
	if n := f.hits("/library/sections?"); n != 1 {
		t.Errorf("listed sections %d times, want cached", n)
	}
	if k := ss[1].LeafKind(); k != "episode" {
		t.Errorf("TV leaf kind = %q", k)
	}
}

func TestSectionLookup(t *testing.T) {
	c, _ := newFake(t)
	for ref, want := range map[string]string{"1": "Movies", "tv": "TV", "MOVIES": "Movies"} {
		s, err := c.Section(ctx, ref)
		if err != nil || s.Title != want {
			t.Errorf("Section(%q) = %+v, %v", ref, s, err)
		}
	}
	if _, err := c.Section(ctx, "3"); err == nil {
		t.Error("section of an unsupported type should not resolve")
	}
}

func TestMachineIDIsCached(t *testing.T) {
	c, f := newFake(t)
	for range 3 {
		id, err := c.MachineID(ctx)
		if err != nil || id != "MID" {
			t.Fatalf("id = %q, %v", id, err)
		}
	}
	if n := f.hits("/identity"); n != 1 {
		t.Errorf("identity fetched %d times", n)
	}
}

func TestPlaylistsSortedCaseInsensitively(t *testing.T) {
	c, _ := newFake(t)
	pls, err := c.Playlists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pls) != 2 || pls[0].Title != "Alpha" || pls[1].Title != "zeta" {
		t.Fatalf("playlists = %+v", pls)
	}
	if !pls[1].Smart || pls[1].Count != 3 || pls[0].Kind != model.Playlist || pls[0].ListType != "video" {
		t.Errorf("fields = %+v %+v", pls[0], pls[1])
	}
}

func TestCollectionsCarrySection(t *testing.T) {
	c, _ := newFake(t)
	cs, err := c.Collections(ctx, "1")
	if err != nil || len(cs) != 1 {
		t.Fatalf("%v %v", cs, err)
	}
	if cs[0].SectionID != "1" || cs[0].Subtype != "movie" || cs[0].Count != 2 || cs[0].Kind != model.Collection {
		t.Errorf("collection = %+v", cs[0])
	}
	all, err := c.AllCollections(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %v %v", all, err)
	}
}

func TestLabelsListOnlyLabelsWithItems(t *testing.T) {
	c, _ := newFake(t)
	ls, err := c.Labels(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ls, []string{"keep"}) {
		t.Errorf("labels = %v, want orphan label hidden", ls)
	}
	// show sections also look at show-level labels
	ls, err = c.Labels(ctx, "2")
	if err != nil || !slices.Equal(ls, []string{"binge"}) {
		t.Errorf("tv labels = %v, %v", ls, err)
	}
}

func TestLibraryItemsGetLabelsAndSection(t *testing.T) {
	c, _ := newFake(t)
	items, err := c.Items(ctx, Library{Section: Section{ID: "1", Title: "Movies", Type: "movie"}})
	if err != nil {
		t.Fatal(err)
	}
	if titles(items) != "Alien,Brazil" {
		t.Fatalf("items = %s", titles(items))
	}
	if items[0].SectionID != "1" || items[0].SectionTitle != "Movies" {
		t.Errorf("section not set: %+v", items[0])
	}
	if !items[0].HasLabel("keep") || len(items[1].Labels) != 0 {
		t.Errorf("labels = %v / %v", items[0].Labels, items[1].Labels)
	}
	if items[0].Size != 100 || len(items[0].Versions) != 1 {
		t.Errorf("media not converted: %+v", items[0])
	}
}

func TestEpisodesInheritShowLabels(t *testing.T) {
	c, _ := newFake(t)
	items, err := c.Items(ctx, Library{Section: Section{ID: "2", Title: "TV", Type: "show"}})
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %v", items, err)
	}
	if !slices.Equal(items[0].Labels, []string{"binge"}) {
		t.Errorf("episode labels = %v, want show's", items[0].Labels)
	}
}

func TestItemsAreCachedUntilInvalidated(t *testing.T) {
	c, f := newFake(t)
	lib := Library{Section: Section{ID: "1", Title: "Movies", Type: "movie"}}
	a, _ := c.Items(ctx, lib)
	b, _ := c.Items(ctx, lib)
	if &a[0] != &b[0] {
		t.Error("second call did not return the cached slice")
	}
	before := f.hits("/library/sections/1/all?1")
	c.Invalidate(lib.ID())
	if _, err := c.Items(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if f.hits("/library/sections/1/all?1") <= before {
		t.Error("Invalidate did not force a reload")
	}
	c.Items(ctx, lib)
	n := f.hits("/library/sections/1/all?1")
	c.Invalidate() // everything
	c.Items(ctx, lib)
	if f.hits("/library/sections/1/all?1") <= n {
		t.Error("Invalidate() did not clear all items")
	}
}

func TestItemsErrorsAreNotCached(t *testing.T) {
	c, f := newFake(t)
	lib := Library{Section: Section{ID: "1", Title: "Movies", Type: "movie"}}
	f.mu.Lock()
	f.fail["/library/sections/1/all"] = true
	f.mu.Unlock()
	if _, err := c.Items(ctx, lib); err == nil {
		t.Fatal("want error")
	}
	f.mu.Lock()
	f.fail["/library/sections/1/all"] = false
	f.mu.Unlock()
	if items, err := c.Items(ctx, lib); err != nil || len(items) != 2 {
		t.Errorf("retry after failure: %v %v", items, err)
	}
}

func TestPlaylistMembersCarryPlaylistItemIDs(t *testing.T) {
	c, _ := newFake(t)
	src := Members{C: &model.Container{Kind: model.Playlist, ID: "50", Title: "Alpha"}}
	if src.ID() != "playlist:50" || src.Title() != "Alpha" || src.Container() != src.C {
		t.Errorf("source identity: %s %s", src.ID(), src.Title())
	}
	items, err := c.Items(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].MemberID != "900" || items[1].MemberID != "901" {
		t.Errorf("members = %+v", items)
	}
}

func TestCollectionMembersUseRatingKeyAndInheritSection(t *testing.T) {
	c, _ := newFake(t)
	src := Members{C: &model.Container{Kind: model.Collection, ID: "60", Title: "Sci-fi", SectionID: "1"}}
	items, err := c.Items(ctx, src)
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %v", items, err)
	}
	if items[0].MemberID != "11" || items[0].SectionID != "1" {
		t.Errorf("item = %+v", items[0])
	}
}

func TestLabelMembersAreFilteredLibraryItems(t *testing.T) {
	c, _ := newFake(t)
	src := Members{C: &model.Container{Kind: model.Label, ID: "keep", Title: "keep", SectionID: "1"}}
	items, err := c.Items(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if titles(items) != "Alien" || items[0].MemberID != "10" {
		t.Errorf("items = %+v", items)
	}
	// the cached library item must not be mutated by the member copy
	lib, _ := c.Items(ctx, Library{Section: Section{ID: "1", Title: "Movies", Type: "movie"}})
	if lib[0].MemberID != "" {
		t.Error("label listing leaked MemberID into the library cache")
	}
	bad := Members{C: &model.Container{Kind: model.Label, ID: "nolib", SectionID: "99"}}
	if _, err := c.Items(ctx, bad); err == nil {
		t.Error("unknown section should fail")
	}
	if _, err := c.Items(ctx, Members{C: &model.Container{Kind: model.ContainerKind(9), ID: "x"}}); err == nil {
		t.Error("unknown container kind should fail")
	}
}

func TestSavedUnionsSourcesDedupesAndFilters(t *testing.T) {
	c, _ := newFake(t)
	q, err := query.Parse("title:a") // matches Alien, Brazil
	if err != nil {
		t.Fatal(err)
	}
	saved := Saved{Name: "mix", Query: q, Sources: []Source{
		Library{Section: Section{ID: "1", Title: "Movies", Type: "movie"}},
		Members{C: &model.Container{Kind: model.Playlist, ID: "50", Title: "Alpha"}}, // Alien again
	}}
	if saved.ID() != "query:mix" || saved.Title() != "mix" || saved.Container() != nil {
		t.Error("saved identity")
	}
	items, err := c.Items(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	if titles(items) != "Alien,Brazil" {
		t.Errorf("items = %s, want union deduped by key", titles(items))
	}
	q2, _ := query.Parse("title:brazil")
	items, _ = c.Items(ctx, Saved{Name: "f", Query: q2, Sources: saved.Sources})
	if titles(items) != "Brazil" {
		t.Errorf("filtered = %s", titles(items))
	}
}

func TestSavedPropagatesSourceErrors(t *testing.T) {
	c, f := newFake(t)
	f.fail["/playlists/50/items"] = true
	q, _ := query.Parse("")
	saved := Saved{Name: "e", Query: q, Sources: []Source{Members{C: &model.Container{Kind: model.Playlist, ID: "50"}}}}
	if _, err := c.Items(ctx, saved); err == nil {
		t.Error("want error")
	}
}

func TestMembershipIndexesCollectionsAndPlaylists(t *testing.T) {
	c, f := newFake(t)
	movies, _ := c.Items(ctx, Library{Section: Section{ID: "1", Title: "Movies", Type: "movie"}})
	if !NeedsMembership(movies) {
		t.Fatal("fresh items should need membership")
	}
	if err := c.EnsureMembership(ctx, movies); err != nil {
		t.Fatal(err)
	}
	if NeedsMembership(movies) {
		t.Error("still needs membership after EnsureMembership")
	}
	alien, brazil := movies[0], movies[1]
	if !slices.Equal(alien.Playlists, []string{"Alpha"}) || len(alien.Collections) != 0 {
		t.Errorf("alien: playlists %v collections %v", alien.Playlists, alien.Collections)
	}
	if !slices.Equal(brazil.Collections, []string{"Sci-fi"}) || len(brazil.Playlists) != 0 {
		t.Errorf("brazil: playlists %v collections %v", brazil.Playlists, brazil.Collections)
	}
	// a playlist listing the same item twice still names it once
	// and indexes are built once
	n := f.hits("/playlists/50/items")
	other := &model.Item{Key: "11", SectionID: "1"}
	c.EnsureMembership(ctx, []*model.Item{other})
	if f.hits("/playlists/50/items") != n {
		t.Error("playlist index rebuilt")
	}
	// Invalidate drops the indexes
	c.Invalidate()
	c.EnsureMembership(ctx, []*model.Item{{Key: "11", SectionID: "1"}})
	if f.hits("/playlists/50/items") == n {
		t.Error("Invalidate did not drop the playlist index")
	}
}

func TestEpisodesInheritCollectionsOfShowAndSeason(t *testing.T) {
	c, _ := newFake(t)
	eps, _ := c.Items(ctx, Library{Section: Section{ID: "2", Title: "TV", Type: "show"}})
	if err := c.EnsureMembership(ctx, eps); err != nil {
		t.Fatal(err)
	}
	// collection 61 holds show 20, which is the episode's grandparent
	if !slices.Equal(eps[0].Collections, []string{"Shows"}) {
		t.Errorf("collections = %v", eps[0].Collections)
	}
}

func TestMembershipErrorsPropagate(t *testing.T) {
	c, f := newFake(t)
	f.fail["/playlists/50/items"] = true
	err := c.EnsureMembership(ctx, []*model.Item{{Key: "10", SectionID: "1"}})
	if err == nil {
		t.Fatal("want error from playlist listing")
	}
	// a failed build must not mark the index as built
	f.mu.Lock()
	f.fail["/playlists/50/items"] = false
	f.mu.Unlock()
	it := &model.Item{Key: "10", SectionID: "1"}
	if err := c.EnsureMembership(ctx, []*model.Item{it}); err != nil || !slices.Equal(it.Playlists, []string{"Alpha"}) {
		t.Errorf("retry: %v %v", err, it.Playlists)
	}
	f.mu.Lock()
	f.fail["/library/sections/1/collections"] = true
	f.mu.Unlock()
	c.Invalidate()
	if err := c.EnsureMembership(ctx, []*model.Item{{Key: "10", SectionID: "1"}}); err == nil {
		t.Error("want error from collection listing")
	}
}

func TestBuildMembershipSkipsKnownItems(t *testing.T) {
	c, f := newFake(t)
	known := &model.Item{Key: "10", SectionID: "1", MembershipKnown: true}
	if err := c.BuildMembership(ctx, []*model.Item{known}); err != nil {
		t.Fatal(err)
	}
	if n := f.hits("/library/sections/1/collections"); n != 0 {
		t.Errorf("collections listed for known items: %d", n)
	}
}

func TestGather(t *testing.T) {
	idx := map[string][]string{"a": {"z", "y"}, "b": {"y", "x"}}
	got := gather(idx, []string{"a", "", "b", "missing"})
	if !slices.Equal(got, []string{"x", "y", "z"}) {
		t.Errorf("gather = %v", got)
	}
	if gather(nil, []string{"a"}) != nil {
		t.Error("nil index should gather nothing")
	}
}

func TestSameLabelInTwoSectionsHasDistinctCacheKeys(t *testing.T) {
	c, _ := newFake(t)
	// "keep" exists on Alien in Movies (1); the TV section has no item with that label
	a := Members{C: &model.Container{Kind: model.Label, ID: "keep", Title: "keep", SectionID: "1"}}
	b := Members{C: &model.Container{Kind: model.Label, ID: "keep", Title: "keep", SectionID: "2"}}
	if a.ID() == b.ID() {
		t.Fatalf("both sources have ID %q", a.ID())
	}
	first, err := c.Items(ctx, a)
	if err != nil || titles(first) != "Alien" {
		t.Fatalf("movies: %s, %v", titles(first), err)
	}
	second, err := c.Items(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Errorf("TV 'keep' returned the Movies items: %s", titles(second))
	}
}

func TestSourceIDsAreStableAndDistinct(t *testing.T) {
	ids := map[string]string{
		Library{Section: Section{ID: "1"}}.ID():                                       "section:1",
		Members{C: &model.Container{Kind: model.Playlist, ID: "5"}}.ID():              "playlist:5",
		Members{C: &model.Container{Kind: model.Collection, ID: "5"}}.ID():            "collection:5",
		Members{C: &model.Container{Kind: model.Label, ID: "x", SectionID: "2"}}.ID(): "label:2:x",
		Saved{Name: "q"}.ID(): "query:q",
	}
	for got, want := range ids {
		if got != want {
			t.Errorf("ID = %q, want %q", got, want)
		}
	}
}
