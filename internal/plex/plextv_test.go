package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// plexTVServer fakes plex.tv's PIN and resources endpoints.
type plexTVServer struct {
	mu      sync.Mutex
	polls   int              // GETs of the pin
	tokenAt int              // poll number that returns the token (0 = never)
	status  int              // if set, every pin poll answers this
	headers []http.Header    // headers of each request
	paths   []string         // "METHOD /path?query"
	servers []map[string]any // /resources body
	expires time.Time
}

func newPlexTV(t *testing.T) (*plexTVServer, Device) {
	t.Helper()
	p := &plexTVServer{expires: time.Now().Add(time.Hour)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.headers = append(p.headers, r.Header.Clone())
		p.paths = append(p.paths, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v2/pins":
			json.NewEncoder(w).Encode(Pin{ID: 42, Code: "AB12", ExpiresAt: p.expires})
		case r.Method == "GET" && r.URL.Path == "/api/v2/pins/42":
			p.polls++
			if p.status != 0 {
				w.WriteHeader(p.status)
				fmt.Fprint(w, "nope")
				return
			}
			pin := Pin{ID: 42, Code: "AB12", ExpiresAt: p.expires}
			if p.tokenAt != 0 && p.polls >= p.tokenAt {
				pin.AuthToken = "user-token"
			}
			json.NewEncoder(w).Encode(pin)
		case r.URL.Path == "/api/v2/resources":
			json.NewEncoder(w).Encode(p.servers)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return p, Device{ID: "dev-1", Product: "plx", Version: "9.9", BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestNewPinSendsIdentityHeaders(t *testing.T) {
	p, dev := newPlexTV(t)
	pin, err := dev.NewPin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pin.Code != "AB12" || pin.ID != 42 || pin.LinkURL() != "https://www.plex.tv/link/?pin=AB12" {
		t.Errorf("pin = %+v", pin)
	}
	h := p.headers[0]
	for k, want := range map[string]string{"X-Plex-Client-Identifier": "dev-1", "X-Plex-Product": "plx",
		"X-Plex-Version": "9.9", "Accept": "application/json"} {
		if h.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, h.Get(k), want)
		}
	}
	if h.Get("X-Plex-Token") != "" {
		t.Error("no token exists yet; none may be sent")
	}
	if p.paths[0] != "POST /api/v2/pins" {
		t.Errorf("request = %s", p.paths[0])
	}
}

func TestNewPinStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, " slow down ")
	}))
	defer srv.Close()
	_, err := Device{BaseURL: srv.URL}.NewPin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "slow down") {
		t.Errorf("err = %v", err)
	}
}

func TestBaseURLTrailingSlashAndDefaultClient(t *testing.T) {
	_, dev := newPlexTV(t)
	dev.BaseURL += "/"
	dev.HTTP = nil // http.DefaultClient
	if _, err := dev.NewPin(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForTokenPollsUntilLinked(t *testing.T) {
	p, dev := newPlexTV(t)
	p.tokenAt = 3
	tok, err := dev.WaitForToken(context.Background(), &Pin{ID: 42, ExpiresAt: p.expires}, time.Millisecond)
	if err != nil || tok != "user-token" {
		t.Fatalf("token %q, err %v", tok, err)
	}
	if p.polls != 3 {
		t.Errorf("polls = %d, want 3", p.polls)
	}
}

func TestWaitForTokenExpiry(t *testing.T) {
	p, dev := newPlexTV(t)
	// the server never links; the pin's own expiry time ends the wait
	_, err := dev.WaitForToken(context.Background(), &Pin{ID: 42, ExpiresAt: time.Now().Add(-time.Second)}, time.Millisecond)
	if !errors.Is(err, ErrPinExpired) {
		t.Errorf("err = %v, want ErrPinExpired", err)
	}
	if p.polls != 1 {
		t.Errorf("polls = %d: should check once, then give up", p.polls)
	}
}

func TestWaitForTokenNotFoundMeansExpired(t *testing.T) {
	p, dev := newPlexTV(t)
	p.status = 404
	_, err := dev.WaitForToken(context.Background(), &Pin{ID: 42}, time.Millisecond)
	if !errors.Is(err, ErrPinExpired) {
		t.Errorf("err = %v", err)
	}
}

func TestWaitForTokenOtherStatusIsAnError(t *testing.T) {
	p, dev := newPlexTV(t)
	p.status = 500
	_, err := dev.WaitForToken(context.Background(), &Pin{ID: 42}, time.Millisecond)
	if err == nil || errors.Is(err, ErrPinExpired) || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v", err)
	}
}

func TestWaitForTokenHonoursContext(t *testing.T) {
	_, dev := newPlexTV(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := dev.WaitForToken(ctx, &Pin{ID: 42}, time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestServersFiltersAndRanksOwnedFirst(t *testing.T) {
	p, dev := newPlexTV(t)
	p.servers = []map[string]any{
		{"name": "shared-1", "provides": "server", "owned": false, "clientIdentifier": "s1"},
		{"name": "phone", "provides": "client,player", "owned": true},
		{"name": "mine-1", "provides": "server", "owned": true, "accessToken": "tok-a",
			"connections": []map[string]any{{"uri": "http://10.0.0.2:32400", "local": true}}},
		{"name": "shared-2", "provides": "player,server", "owned": false},
		{"name": "mine-2", "provides": "server", "owned": true},
	}
	rs, err := dev.Servers(context.Background(), "user-token")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rs {
		names = append(names, r.Name)
	}
	if got := strings.Join(names, ","); got != "mine-1,mine-2,shared-1,shared-2" {
		t.Errorf("servers = %s (owned first, stable, servers only)", got)
	}
	if rs[0].AccessToken != "tok-a" || len(rs[0].Connections) != 1 || !rs[0].Connections[0].Local {
		t.Errorf("resource = %+v", rs[0])
	}
	if h := p.headers[0]; h.Get("X-Plex-Token") != "user-token" {
		t.Errorf("token header = %q", h.Get("X-Plex-Token"))
	}
	if !strings.Contains(p.paths[0], "includeHttps=1") || !strings.Contains(p.paths[0], "includeRelay=1") {
		t.Errorf("request = %s", p.paths[0])
	}
}

func TestServersErrorsAndEmpty(t *testing.T) {
	p, dev := newPlexTV(t)
	rs, err := dev.Servers(context.Background(), "t")
	if err != nil || len(rs) != 0 {
		t.Errorf("no resources: %v %v", rs, err)
	}
	dev.BaseURL += "/missing"
	if _, err := dev.Servers(context.Background(), "t"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
	_ = p
}
