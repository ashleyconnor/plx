package store

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/source"
)

// home points the store at a temp config dir and clears env overrides.
func home(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, k := range []string{"PLEX_URL", "PLEX_TOKEN", "PLEX_INSECURE", "PLX_ICONS"} {
		t.Setenv(k, "")
	}
	return filepath.Join(dir, "plx")
}

func TestDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	if got := Dir(); got != filepath.Join("/x", "plx") {
		t.Errorf("Dir = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/h")
	if got := Dir(); got != filepath.Join("/h", ".config", "plx") {
		t.Errorf("Dir = %q", got)
	}
}

func TestConfigRoundTripIsPrivate(t *testing.T) {
	dir := home(t)
	want := Config{URL: "https://h:32400", Token: "secret", Insecure: true, Icons: "ascii", ClientID: "id"}
	if err := SaveConfig(want); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %v, want 0600 (holds the token)", st.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml.tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file left behind: %v", err)
	}
	got, err := ReadConfigFile()
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	home(t)
	if err := SaveConfig(Config{URL: "file-url", Token: "file-token", Icons: "none"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLEX_URL", "env-url")
	t.Setenv("PLEX_TOKEN", "env-token")
	t.Setenv("PLEX_INSECURE", "TRUE")
	t.Setenv("PLX_ICONS", "ascii")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "env-url" || c.Token != "env-token" || !c.Insecure || c.Icons != "ascii" {
		t.Errorf("config = %+v", c)
	}
	t.Setenv("PLEX_INSECURE", "0")
	if c, _ := LoadConfig(); c.Insecure {
		t.Error("PLEX_INSECURE=0 should disable insecure")
	}
	t.Setenv("PLEX_INSECURE", "1")
	if c, _ := LoadConfig(); !c.Insecure {
		t.Error("PLEX_INSECURE=1 should enable insecure")
	}
}

func TestLoadConfigNotConfigured(t *testing.T) {
	home(t)
	_, err := LoadConfig()
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	t.Setenv("PLEX_URL", "u") // token still missing
	if _, err := LoadConfig(); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("url without token: %v", err)
	}
}

func TestReadConfigFileInvalid(t *testing.T) {
	dir := home(t)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("url = ["), 0o600)
	if _, err := ReadConfigFile(); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("err = %v", err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Error("LoadConfig must surface a malformed file")
	}
}

func TestLoadQueriesMissingFileIsEmpty(t *testing.T) {
	home(t)
	qs, err := LoadQueries()
	if err != nil || len(qs) != 0 {
		t.Fatalf("got %v, %v", qs, err)
	}
}

func TestAppendQueryKeepsExistingFileAndRoundTrips(t *testing.T) {
	dir := home(t)
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "queries.toml")
	hand := "# my queries\n[[query]]\nname = \"old\"\nfilter = \"year > 2000\"\nsources = [{ section = \"Movies\" }]\n"
	os.WriteFile(path, []byte(hand), 0o644)

	q := SavedQuery{
		Name:    "Big unwatched",
		Sources: []SourceSpec{{Section: "Movies"}, {Playlist: "Watch"}, {Collection: "Sci-fi", Section: "1"}, {Label: "keep", Section: "1"}},
		Filter:  "size > 5G",
		Sort:    "size-",
		Columns: []string{"title", "size"},
	}
	if err := AppendQuery(q); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), hand) {
		t.Errorf("existing content was rewritten:\n%s", raw)
	}
	qs, err := LoadQueries()
	if err != nil || len(qs) != 2 {
		t.Fatalf("got %+v, %v", qs, err)
	}
	got := qs[1]
	if got.Name != q.Name || got.Filter != q.Filter || got.Sort != q.Sort || len(got.Columns) != 2 || len(got.Sources) != 4 {
		t.Errorf("round trip = %+v", got)
	}
	if got.Sources[1].Playlist != "Watch" || got.Sources[2].Collection != "Sci-fi" || got.Sources[3].Label != "keep" {
		t.Errorf("sources = %+v", got.Sources)
	}
}

func TestAppendQueryCreatesFileAndRejectsDuplicateNames(t *testing.T) {
	home(t)
	q := SavedQuery{Name: "Dup", Sources: []SourceSpec{{Section: "Movies"}}, Filter: "x"}
	if err := AppendQuery(q); err != nil {
		t.Fatal(err)
	}
	q.Name = "dUP"
	err := AppendQuery(q)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if qs, _ := LoadQueries(); len(qs) != 1 {
		t.Errorf("duplicate was written: %+v", qs)
	}
}

func TestAppendQueryRefusesToTouchBrokenFile(t *testing.T) {
	dir := home(t)
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "queries.toml")
	os.WriteFile(path, []byte("[[query\n"), 0o644)
	if err := AppendQuery(SavedQuery{Name: "n", Filter: "x"}); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(path); string(b) != "[[query\n" {
		t.Errorf("broken file was modified: %q", b)
	}
}

func TestSpecFor(t *testing.T) {
	sec := source.Section{ID: "1", Title: "Movies", Type: "movie"}
	pl := &model.Container{Kind: model.Playlist, ID: "9", Title: "Watch"}
	co := &model.Container{Kind: model.Collection, ID: "8", Title: "Sci-fi", SectionID: "1"}
	lb := &model.Container{Kind: model.Label, ID: "keep", Title: "keep", SectionID: "1"}
	tests := []struct {
		name string
		src  source.Source
		want []SourceSpec
	}{
		{"library saves title", source.Library{Section: sec}, []SourceSpec{{Section: "Movies"}}},
		{"playlist saves title", source.Members{C: pl}, []SourceSpec{{Playlist: "Watch"}}},
		{"collection keeps section", source.Members{C: co}, []SourceSpec{{Collection: "Sci-fi", Section: "1"}}},
		{"label keeps section", source.Members{C: lb}, []SourceSpec{{Label: "keep", Section: "1"}}},
		{"saved is flattened", source.Saved{Name: "q", Sources: []source.Source{source.Library{Section: sec}, source.Members{C: pl}}},
			[]SourceSpec{{Section: "Movies"}, {Playlist: "Watch"}}},
		{"unknown container kind", source.Members{C: &model.Container{Kind: model.ContainerKind(9)}}, nil},
	}
	for _, tc := range tests {
		got := SpecFor(tc.src)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFindContainerPrefersIDOverTitle(t *testing.T) {
	cs := []*model.Container{{ID: "1", Title: "2"}, {ID: "2", Title: "other"}, {ID: "3", Title: "Mixed Case"}}
	if c := findContainer(cs, "2"); c.ID != "2" {
		t.Errorf("matched by title before ID: %+v", c)
	}
	if c := findContainer(cs, "mixed case"); c == nil || c.ID != "3" {
		t.Errorf("case-insensitive title: %+v", c)
	}
	if findContainer(cs, "nope") != nil {
		t.Error("matched a missing container")
	}
}

// fakePlex serves a small library: Movies (1) and TV (2) sections, one
// playlist, and one collection in Movies.
func fakePlex(t *testing.T) *source.Catalog {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[
				{"key":"2","title":"TV","type":"show"},{"key":"1","title":"Movies","type":"movie"},{"key":"3","title":"Other","type":"weird"}]}}`)
		case "/playlists":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"50","title":"Watch","playlistType":"video"}]}}`)
		case "/library/sections/1/collections":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"60","title":"Sci-fi","subtype":"movie"}]}}`)
		case "/library/sections/2/collections":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return source.NewCatalog(plex.New(srv.URL, "tok", plex.Options{}))
}

func TestResolveSpecs(t *testing.T) {
	cat := fakePlex(t)
	ctx := context.Background()
	tests := []struct {
		name   string
		spec   SourceSpec
		wantID string
		err    string
	}{
		{"section by title", SourceSpec{Section: "movies"}, "section:1", ""},
		{"section by id", SourceSpec{Section: "2"}, "section:2", ""},
		{"unknown section", SourceSpec{Section: "Nope"}, "", "no library section"},
		{"playlist by title", SourceSpec{Playlist: "watch"}, "playlist:50", ""},
		{"playlist by numeric id", SourceSpec{Playlist: int64(50)}, "playlist:50", ""},
		{"missing playlist", SourceSpec{Playlist: "x"}, "", "no playlist"},
		{"collection in section", SourceSpec{Collection: "Sci-fi", Section: "Movies"}, "collection:60", ""},
		{"collection across sections", SourceSpec{Collection: "60"}, "collection:60", ""},
		{"missing collection", SourceSpec{Collection: "x"}, "", "no collection"},
		{"collection in unknown section", SourceSpec{Collection: "x", Section: "Nope"}, "", "no library section"},
		{"label", SourceSpec{Label: "keep", Section: "Movies"}, "label:1:keep", ""},
		{"label without section", SourceSpec{Label: "keep"}, "", "needs a section"},
		{"label in unknown section", SourceSpec{Label: "keep", Section: "Nope"}, "", "no library section"},
		{"empty", SourceSpec{}, "", "empty source"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, err := resolveSpec(ctx, cat, tc.spec)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if src.ID() != tc.wantID {
				t.Errorf("ID = %q, want %q", src.ID(), tc.wantID)
			}
		})
	}
}

func TestLabelSpecResolvesSectionToID(t *testing.T) {
	src, err := resolveSpec(context.Background(), fakePlex(t), SourceSpec{Label: "keep", Section: "Movies"})
	if err != nil {
		t.Fatal(err)
	}
	if c := src.Container(); c.SectionID != "1" || c.Kind != model.Label {
		t.Errorf("container = %+v", c)
	}
}

func TestResolveSavedQuery(t *testing.T) {
	cat := fakePlex(t)
	ctx := context.Background()
	s, err := Resolve(ctx, cat, SavedQuery{Name: "q", Filter: "year > 2000", Sort: "year-",
		Sources: []SourceSpec{{Section: "Movies"}, {Playlist: "Watch"}}})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID() != "query:q" || len(s.Sources) != 2 || s.Query == nil {
		t.Errorf("saved = %+v", s)
	}
	for name, q := range map[string]SavedQuery{
		"bad filter": {Name: "a", Filter: "bogusfield>1", Sources: []SourceSpec{{Section: "Movies"}}},
		"no sources": {Name: "b", Filter: "year > 1"},
		"bad source": {Name: "c", Filter: "year > 1", Sources: []SourceSpec{{Section: "Nope"}}},
	} {
		_, err := Resolve(ctx, cat, q)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("query %q", q.Name)) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestParseRef(t *testing.T) {
	home(t)
	cat := fakePlex(t)
	ctx := context.Background()
	if err := AppendQuery(SavedQuery{Name: "Mine", Filter: "year > 1", Sources: []SourceSpec{{Section: "Movies"}}}); err != nil {
		t.Fatal(err)
	}
	ok := map[string]string{
		"section:Movies":    "section:1",
		"playlist:Watch":    "playlist:50",
		"playlist:50":       "playlist:50",
		"collection:Sci-fi": "collection:60",
		"label:Movies/keep": "label:1:keep",
		"query:mine":        "query:Mine",
	}
	for ref, id := range ok {
		src, err := ParseRef(ctx, cat, ref)
		if err != nil || src.ID() != id {
			t.Errorf("%s: %v, %v", ref, src, err)
		}
	}
	bad := map[string]string{
		"Movies":        "want kind:name",
		"label:Movies":  "label:<section>/<label>",
		"query:missing": "no saved query",
		"folder:Movies": "unknown source kind",
		"section:Nope":  "no library section",
		"playlist:Nope": "no playlist",
	}
	for ref, msg := range bad {
		if _, err := ParseRef(ctx, cat, ref); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: err = %v, want %q", ref, err, msg)
		}
	}
}

func TestParseRefQueryWithBrokenFile(t *testing.T) {
	dir := home(t)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "queries.toml"), []byte("[[query\n"), 0o644)
	if _, err := ParseRef(context.Background(), fakePlex(t), "query:x"); err == nil {
		t.Error("want error from malformed queries.toml")
	}
}
