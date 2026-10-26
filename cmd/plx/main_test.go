package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// capture runs f and returns what it wrote to stdout.
func capture(t *testing.T, f func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	runErr := f()
	w.Close()
	os.Stdout = old
	return <-done, runErr
}

// configure points plx at a fake server with one Movies section.
func configure(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`)
		case "/library/sections/1/all":
			if q.Get("label") != "" {
				fmt.Fprint(w, `{"MediaContainer":{"Metadata":[]}}`)
				return
			}
			fmt.Fprint(w, `{"MediaContainer":{"librarySectionTitle":"Movies","Metadata":[
				{"ratingKey":"1","type":"movie","title":"Alien","Media":[{"id":1,"duration":7200000,"videoResolution":"1080","Part":[{"file":"/m/a.mkv","size":2147483648}]}]},
				{"ratingKey":"2","type":"movie","title":"Brazil","Media":[{"id":2,"Part":[{"file":"/m/b.mkv","size":1024}]}]}]}}`)
		case "/library/sections/1/label":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
		case "/playlists":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"5","title":"Watch","playlistType":"video","smart":true,"leafCount":4}]}}`)
		case "/library/sections/1/collections":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"6","title":"Sci-fi","subtype":"movie","childCount":2}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PLEX_URL", srv.URL)
	t.Setenv("PLEX_TOKEN", "tok")
	t.Setenv("PLEX_INSECURE", "")
}

func TestFieldsListsEveryFieldWithAliasesAndType(t *testing.T) {
	out, err := capture(t, func() error { return run([]string{"fields"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"title ", "duration (dur, length", "size ", "bytes", "label (labels)"} {
		if !strings.Contains(out, want) {
			t.Errorf("fields output lacks %q:\n%s", want, out)
		}
	}
}

func TestListShowsSourcesYouCanQuery(t *testing.T) {
	configure(t)
	out, err := capture(t, func() error { return run([]string{"ls"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"section:Movies", "playlist:Watch", "4 items (smart)", "collection:Sci-fi", "2 items"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "collection:Sci-fi  movie  2 items (smart)") {
		t.Error("regular collection marked smart")
	}
}

func TestQueryPrintsMatchesAndTotals(t *testing.T) {
	configure(t)
	out, err := capture(t, func() error { return run([]string{"q", "section:Movies", "size>1G"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TITLE") || !strings.Contains(out, "Alien") || strings.Contains(out, "Brazil") {
		t.Errorf("output:\n%s", out)
	}
	if !strings.Contains(out, "1 of 2 items · 2.0G") {
		t.Errorf("summary missing:\n%s", out)
	}
	// no query words: everything
	out, err = capture(t, func() error { return run([]string{"q", "section:Movies"}) })
	if err != nil || !strings.Contains(out, "2 of 2 items") {
		t.Errorf("unfiltered: %v\n%s", err, out)
	}
}

func TestCommandErrors(t *testing.T) {
	configure(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"q"}, "usage: plx q"},
		{[]string{"bogus"}, `unknown command "bogus"`},
		{[]string{"q", "nokind"}, "want kind:name"},
		{[]string{"q", "section:Nope"}, "no library section"},
		{[]string{"q", "section:Movies", "nofield>1"}, "unknown field"},
	}
	for _, tc := range cases {
		_, err := capture(t, func() error { return run(tc.args) })
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestSubcommandsNeedConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{"PLEX_URL", "PLEX_TOKEN"} {
		t.Setenv(k, "")
	}
	// only a bare `plx` starts the interactive login; scripted commands fail instead of prompting
	_, err := capture(t, func() error { return run([]string{"ls"}) })
	if err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Errorf("err = %v", err)
	}
}

func TestSmart(t *testing.T) {
	if smart(true) != " (smart)" || smart(false) != "" {
		t.Error("smart suffix")
	}
}
