package exec_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/exec"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
	"github.com/ashleyconnor/plx/internal/plex"
)

type recorder struct {
	mu   sync.Mutex
	reqs []string // "METHOD /path?rawquery"
	// status overrides the response by "METHOD /path"; default 200.
	status map[string]int
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reqs...)
}

func newServer(t *testing.T) (*exec.Executor, *recorder) {
	t.Helper()
	rec := &recorder{status: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		line := r.Method + " " + r.URL.Path
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, line+"?"+r.URL.RawQuery)
		st := rec.status[line]
		rec.mu.Unlock()
		if st != 0 {
			w.WriteHeader(st)
			return
		}
		switch line {
		case "POST /playlists":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"777"}]}}`)
		case "POST /library/collections":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"888"}]}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return &exec.Executor{C: plex.New(srv.URL, "tok", plex.Options{}), MachineID: "MID"}, rec
}

func item(key, member string) *model.Item {
	return &model.Item{Key: key, MemberID: member, Kind: "movie", Title: "T" + key}
}

const lib = "server%3A%2F%2FMID%2Fcom.plexapp.plugins.library%2Flibrary%2Fmetadata%2F"

var (
	pl   = &model.Container{Kind: model.Playlist, ID: "50", Title: "P"}
	coll = &model.Container{Kind: model.Collection, ID: "60", Title: "C", SectionID: "1"}
)

func run(t *testing.T, e *exec.Executor, ops ...plan.Op) error {
	t.Helper()
	return e.Run(context.Background(), &plan.Plan{Ops: ops}, nil)
}

func TestRequestsPerOp(t *testing.T) {
	tests := []struct {
		name string
		op   plan.Op
		want []string
	}{
		{"create playlist", plan.Op{Kind: plan.OpCreatePlaylist, Title: "My list", ListType: "video", Items: []*model.Item{item("1", ""), item("2", "")}},
			[]string{"POST /playlists?uri=" + lib + "1%2C2&type=video&title=My%20list&smart=0"}},
		{"create collection", plan.Op{Kind: plan.OpCreateCollection, Title: "C", SectionID: "1", SearchType: 1, Items: []*model.Item{item("1", "")}},
			[]string{"POST /library/collections?uri=" + lib + "1&type=1&title=C&smart=0&sectionId=1"}},
		{"add to playlist", plan.Op{Kind: plan.OpAdd, Target: pl, Items: []*model.Item{item("1", ""), item("2", "")}},
			[]string{"PUT /playlists/50/items?uri=" + lib + "1%2C2"}},
		{"add to collection", plan.Op{Kind: plan.OpAdd, Target: coll, Items: []*model.Item{item("3", "")}},
			[]string{"PUT /library/collections/60/items?uri=" + lib + "3"}},
		{"remove from playlist uses member id", plan.Op{Kind: plan.OpRemove, Target: pl, Items: []*model.Item{item("1", "m1"), item("1", "m2")}},
			[]string{"DELETE /playlists/50/items/m1?", "DELETE /playlists/50/items/m2?"}},
		{"remove from collection uses rating key", plan.Op{Kind: plan.OpRemove, Target: coll, Items: []*model.Item{item("1", "x")}},
			[]string{"DELETE /library/collections/60/items/1?"}},
		{"move playlist item after", plan.Op{Kind: plan.OpMove, Target: pl, Items: []*model.Item{item("1", "m1")}, AfterMember: "m9"},
			[]string{"PUT /playlists/50/items/m1/move?after=m9"}},
		{"move playlist item to top", plan.Op{Kind: plan.OpMove, Target: pl, Items: []*model.Item{item("1", "m1")}},
			[]string{"PUT /playlists/50/items/m1/move?"}},
		{"move collection item", plan.Op{Kind: plan.OpMove, Target: coll, Items: []*model.Item{item("1", "1")}, AfterMember: "2"},
			[]string{"PUT /library/collections/60/items/1/move?after=2"}},
		{"collection sort custom", plan.Op{Kind: plan.OpSetCollectionSortCustom, Target: coll},
			[]string{"PUT /library/metadata/60/prefs?collectionSort=2"}},
		{"add label", plan.Op{Kind: plan.OpAddLabel, Label: "keep", SectionID: "1", SearchType: 1, Items: []*model.Item{item("1", ""), item("2", "")}},
			[]string{"PUT /library/sections/1/all?type=1&id=1%2C2&label.locked=1&label[0].tag.tag=keep"}},
		{"remove label", plan.Op{Kind: plan.OpRemoveLabel, Label: "keep", SectionID: "1", SearchType: 1, Items: []*model.Item{item("1", "")}},
			[]string{"PUT /library/sections/1/all?type=1&id=1&label.locked=1&label[].tag.tag-=keep"}},
		{"delete items", plan.Op{Kind: plan.OpDelete, Items: []*model.Item{item("1", ""), item("2", "")}},
			[]string{"DELETE /library/metadata/1?", "DELETE /library/metadata/2?"}},
		{"delete versions", plan.Op{Kind: plan.OpDeleteVersion, Items: []*model.Item{item("1", ""), item("2", "")},
			Versions: []model.Version{{MediaID: "11"}, {MediaID: "22"}}},
			[]string{"DELETE /library/metadata/1/media/11?", "DELETE /library/metadata/2/media/22?"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, rec := newServer(t)
			if err := run(t, e, tc.op); err != nil {
				t.Fatal(err)
			}
			got := rec.all()
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestLargeAddIsChunkedAndLabelEditsBatched(t *testing.T) {
	e, rec := newServer(t)
	var items []*model.Item
	for i := 0; i < 250; i++ {
		items = append(items, item(fmt.Sprint(i), ""))
	}
	if err := run(t, e, plan.Op{Kind: plan.OpAddLabel, Label: "x", SectionID: "1", SearchType: 1, Items: items}); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.all()); n != 2 { // 200 + 50
		t.Errorf("label edits = %d requests, want 2", n)
	}
	e, rec = newServer(t)
	if err := run(t, e, plan.Op{Kind: plan.OpAdd, Target: pl, Items: items}); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.all()); n != 3 { // 100 + 100 + 50
		t.Errorf("playlist adds = %d requests, want 3", n)
	}
}

func TestCreatePlaylistLargeAddsRemainderToNewPlaylist(t *testing.T) {
	e, rec := newServer(t)
	var items []*model.Item
	for i := 0; i < 150; i++ {
		items = append(items, item(fmt.Sprint(i), ""))
	}
	if err := run(t, e, plan.Op{Kind: plan.OpCreatePlaylist, Title: "t", ListType: "video", Items: items}); err != nil {
		t.Fatal(err)
	}
	got := rec.all()
	if len(got) != 2 || !strings.HasPrefix(got[0], "POST /playlists?") || !strings.HasPrefix(got[1], "PUT /playlists/777/items?") {
		t.Errorf("requests = %v", got)
	}
}

func TestRunStopsAtFirstFailureAndWrapsDescription(t *testing.T) {
	e, rec := newServer(t)
	rec.status["DELETE /library/metadata/2"] = 403
	err := run(t, e,
		plan.Op{Kind: plan.OpDelete, Items: []*model.Item{item("1", ""), item("2", ""), item("3", "")}},
		plan.Op{Kind: plan.OpAdd, Target: pl, Items: []*model.Item{item("9", "")}},
	)
	if err == nil || !strings.Contains(err.Error(), "delete 3 items") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
	got := rec.all()
	if len(got) != 2 { // item 3 and the later op must never be sent
		t.Errorf("requests after failure: %v", got)
	}
}

func TestDeleteVersionStopsAtFirstFailure(t *testing.T) {
	e, rec := newServer(t)
	rec.status["DELETE /library/metadata/1/media/11"] = 400
	err := run(t, e, plan.Op{Kind: plan.OpDeleteVersion, Items: []*model.Item{item("1", ""), item("2", "")},
		Versions: []model.Version{{MediaID: "11"}, {MediaID: "22"}}})
	if err == nil || len(rec.all()) != 1 {
		t.Errorf("err = %v, requests = %v", err, rec.all())
	}
}

func TestRunRejectsOpsForWrongContainer(t *testing.T) {
	label := &model.Container{Kind: model.Label, ID: "x"}
	for name, op := range map[string]plan.Op{
		"add":     {Kind: plan.OpAdd, Target: label, Items: []*model.Item{item("1", "")}},
		"remove":  {Kind: plan.OpRemove, Target: label, Items: []*model.Item{item("1", "")}},
		"move":    {Kind: plan.OpMove, Target: label, Items: []*model.Item{item("1", "")}},
		"unknown": {Kind: plan.OpKind(99)},
	} {
		e, rec := newServer(t)
		if err := run(t, e, op); err == nil {
			t.Errorf("%s: want error", name)
		}
		if n := len(rec.all()); n != 0 {
			t.Errorf("%s: sent %d requests", name, n)
		}
	}
}

func TestRunHonoursCancelledContext(t *testing.T) {
	e, rec := newServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := e.Run(ctx, &plan.Plan{Ops: []plan.Op{{Kind: plan.OpDelete, Items: []*model.Item{item("1", "")}}}}, nil)
	if err == nil || len(rec.all()) != 0 {
		t.Errorf("err = %v, requests = %v", err, rec.all())
	}
}

func TestStepsAndProgress(t *testing.T) {
	p := &plan.Plan{Ops: []plan.Op{
		{Kind: plan.OpRemove, Target: pl, Items: []*model.Item{item("1", "a"), item("2", "b")}},
		{Kind: plan.OpDelete, Items: []*model.Item{item("3", "")}},
		{Kind: plan.OpAdd, Target: pl, Items: []*model.Item{item("4", ""), item("5", "")}},
		{Kind: plan.OpDeleteVersion, Items: []*model.Item{item("6", ""), item("7", "")}, Versions: []model.Version{{MediaID: "1"}, {MediaID: "2"}}},
	}}
	if n := exec.Steps(p); n != 2+1+1+2 {
		t.Fatalf("Steps = %d", n)
	}
	e, _ := newServer(t)
	var seen []exec.Progress
	if err := e.Run(context.Background(), p, func(pr exec.Progress) { seen = append(seen, pr) }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 6 {
		t.Fatalf("progress calls = %d", len(seen))
	}
	for i, pr := range seen {
		if pr.Done != i+1 || pr.Total != 6 || pr.Step == "" {
			t.Errorf("progress[%d] = %+v", i, pr)
		}
	}
	if !strings.Contains(seen[0].Step, `remove "T1"`) {
		t.Errorf("first step = %q", seen[0].Step)
	}
}

func TestActionLog(t *testing.T) {
	e, _ := newServer(t)
	e.LogPath = filepath.Join(t.TempDir(), "sub", "actions.jsonl")
	ok := plan.Op{Kind: plan.OpMove, Target: pl, Items: []*model.Item{item("1", "m1")}, AfterMember: "m0"}
	create := plan.Op{Kind: plan.OpCreatePlaylist, Title: "n", ListType: "video", Items: []*model.Item{item("5", "")}}
	if err := run(t, e, create, ok); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(e.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, m)
	}
	if len(entries) != 2 {
		t.Fatalf("log lines = %d", len(entries))
	}
	if entries[0]["created"] != "777" || entries[0]["error"] != nil {
		t.Errorf("create entry = %v", entries[0])
	}
	mv := entries[1]
	if mv["target"] != "playlist:50" || mv["after"] != "m0" || fmt.Sprint(mv["members"]) != "[m1]" || fmt.Sprint(mv["keys"]) != "[1]" {
		t.Errorf("move entry = %v", mv)
	}
	if _, err := time.Parse(time.RFC3339Nano, mv["time"].(string)); err != nil {
		t.Errorf("time = %v", mv["time"])
	}
}

func TestActionLogRecordsFailureAndNoLogWhenDisabled(t *testing.T) {
	e, rec := newServer(t)
	rec.status["POST /playlists"] = 400
	e.LogPath = filepath.Join(t.TempDir(), "a.jsonl")
	op := plan.Op{Kind: plan.OpCreatePlaylist, Title: "n", ListType: "video", Items: []*model.Item{item("5", "")}}
	if err := run(t, e, op); err == nil {
		t.Fatal("want error")
	}
	b, _ := os.ReadFile(e.LogPath)
	if !strings.Contains(string(b), `"error":"plex: POST /playlists: 400`) {
		t.Errorf("log = %s", b)
	}

	e2, _ := newServer(t)
	dir := t.TempDir()
	if err := run(t, e2, plan.Op{Kind: plan.OpDelete, Items: []*model.Item{item("1", "")}}); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Error("log written although LogPath is empty")
	}
}
