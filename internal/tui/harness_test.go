package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/source"
)

// server is a fake Plex that records every request. Movies (section 1) has
// Alien (two versions) and Brazil; Alpha is a regular playlist, Smart a smart one.
type server struct {
	mu         sync.Mutex
	reqs       []string // "METHOD /path"
	allowDel   bool
	denyDelete bool // answer DELETEs with 403
	srv        *httptest.Server
}

func (s *server) writes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.reqs {
		if !strings.HasPrefix(r, "GET ") {
			out = append(out, r)
		}
	}
	return out
}

func (s *server) count(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.reqs {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

func newServer(t *testing.T) *server {
	t.Helper()
	s := &server{allowDel: true}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Method+" "+r.URL.Path)
		allow, deny := s.allowDel, s.denyDelete
		s.mu.Unlock()
		if deny && r.Method == "DELETE" {
			w.WriteHeader(403)
			return
		}
		switch {
		case r.URL.Path == "/identity":
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"MID"}}`)
		case r.URL.Path == "/:/prefs":
			fmt.Fprintf(w, `{"MediaContainer":{"Setting":[{"id":"allowMediaDeletion","value":%v}]}}`, allow)
		case r.URL.Path == "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`)
		case r.URL.Path == "/library/sections/1/all":
			if r.URL.Query().Get("label") != "" {
				fmt.Fprint(w, `{"MediaContainer":{"Metadata":[]}}`)
				return
			}
			fmt.Fprint(w, `{"MediaContainer":{"librarySectionTitle":"Movies","Metadata":[
			{"ratingKey":"10","type":"movie","title":"Alien","year":1979,"Media":[
				{"id":100,"duration":7000000,"Part":[{"file":"/m/alien.mkv","size":4000}]},
				{"id":101,"duration":60000,"Part":[{"file":"/m/sample.mkv","size":10}]}]},
			{"ratingKey":"11","type":"movie","title":"Brazil","year":1985,"Media":[{"id":110,"duration":6000000,"Part":[{"file":"/m/brazil.mkv","size":9000}]}]}]}}`)
		case r.URL.Path == "/library/sections/1/label", r.URL.Path == "/library/sections/1/collections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[],"Metadata":[]}}`)
		case r.URL.Path == "/playlists" && r.Method == "GET":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[
				{"ratingKey":"50","title":"Alpha","playlistType":"video"},
				{"ratingKey":"51","title":"Smart","playlistType":"video","smart":1}]}}`)
		case r.URL.Path == "/playlists/50/items" && r.Method == "GET":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"11","type":"movie","title":"Brazil","playlistItemID":"900"}]}}`)
		case r.URL.Path == "/library/metadata/10":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"10","type":"movie","title":"Alien","summary":"In space no one can hear you scream."}]}}`)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// harness drives a Model the way Bubble Tea would, executing returned
// commands (in goroutines, with a deadline so timers are dropped) and
// feeding their messages back through Update.
type harness struct {
	t  *testing.T
	m  *Model
	sv *server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	sv := newServer(t)
	cat := source.NewCatalog(plex.New(sv.srv.URL, "tok", plex.Options{}))
	m := newModel(cat)
	m.w, m.h = 140, 40
	return &harness{t: t, m: m, sv: sv}
}

// open shows the Movies library in the list pane, loaded.
func (h *harness) open() {
	h.t.Helper()
	h.drive(h.m.sideInit())
	var lib *node
	for _, r := range h.m.side.roots {
		if r.name() == "Libraries" {
			for _, c := range r.children {
				if c.name() == "Movies" {
					lib = c
				}
			}
		}
	}
	if lib == nil {
		h.t.Fatal("Movies not in the sidebar")
	}
	h.m.focus = focusList
	h.drive(h.m.openSource(lib))
	if len(h.m.top().rows) != 2 {
		h.t.Fatalf("library rows = %d", len(h.m.top().rows))
	}
}

const cmdDeadline = 40 * time.Millisecond

func (h *harness) drive(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch msg := msg.(type) {
		case nil, tea.QuitMsg:
		case tea.BatchMsg:
			for _, c := range msg {
				h.drive(c)
			}
		default:
			h.send(msg)
		}
	case <-time.After(cmdDeadline): // a timer (spinner, blink, debounce): drop it
	}
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	h.drive(cmd)
}

var keyNames = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown,
	"left": tea.KeyLeft, "right": tea.KeyRight, "backspace": tea.KeyBackspace, "shift+tab": tea.KeyShiftTab, " ": tea.KeySpace,
}

// press sends keys: a name from keyNames, or the runes of any other string.
func (h *harness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		if kt, ok := keyNames[k]; ok {
			h.send(tea.KeyMsg{Type: kt})
		} else {
			h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		}
	}
}

func (h *harness) view() string { return stripANSI(h.m.View()) }

func (h *harness) wantView(sub ...string) {
	h.t.Helper()
	v := h.view()
	for _, s := range sub {
		if !strings.Contains(v, s) {
			h.t.Errorf("view lacks %q:\n%s", s, v)
		}
	}
}

func (h *harness) wantMode(m mode) {
	h.t.Helper()
	if h.m.mode != m {
		h.t.Fatalf("mode = %v, want %v\n%s", h.m.mode, m, h.view())
	}
}

func sourceMembers(h *harness, id string, smart bool) source.Source {
	return source.Members{C: &model.Container{Kind: model.Playlist, ID: id, Title: "Alpha", ListType: "video", Smart: smart}}
}
