package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/store"
)

type fakeAccount struct {
	mu          sync.Mutex
	pins        int // PINs issued
	expireFirst bool
	servers     []map[string]any
	plexURL     string // a reachable "Plex Media Server"
	deviceIDs   []string
}

// newAccount starts fake plex.tv and a fake Plex server, and returns a
// login environment wired to them.
func newAccount(t *testing.T, input string, nServers int) (*fakeAccount, loginEnv, *bytes.Buffer) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{"PLEX_URL", "PLEX_TOKEN", "PLEX_INSECURE"} {
		t.Setenv(k, "")
	}
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"M"}}`)
	}))
	t.Cleanup(pms.Close)
	a := &fakeAccount{plexURL: pms.URL}
	for i := 1; i <= nServers; i++ {
		a.servers = append(a.servers, map[string]any{
			"name": fmt.Sprintf("Server%d", i), "provides": "server", "owned": i != 2,
			"accessToken": fmt.Sprintf("srv-token-%d", i),
			"connections": []map[string]any{{"uri": pms.URL, "local": true}},
		})
	}
	tv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.deviceIDs = append(a.deviceIDs, r.Header.Get("X-Plex-Client-Identifier"))
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v2/pins":
			a.pins++
			json.NewEncoder(w).Encode(plex.Pin{ID: int64(a.pins), Code: fmt.Sprintf("CODE%d", a.pins), ExpiresAt: time.Now().Add(time.Hour)})
		case strings.HasPrefix(r.URL.Path, "/api/v2/pins/"):
			if a.expireFirst && r.URL.Path == "/api/v2/pins/1" {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(plex.Pin{AuthToken: "account-token"})
		case r.URL.Path == "/api/v2/resources":
			if r.Header.Get("X-Plex-Token") != "account-token" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(a.servers)
		}
	}))
	t.Cleanup(tv.Close)
	out := &bytes.Buffer{}
	env := loginEnv{
		in: strings.NewReader(input), out: out, poll: time.Millisecond, qr: false,
		device: func(id string) plex.Device {
			return plex.Device{ID: id, Product: "plx", Version: "test", BaseURL: tv.URL, HTTP: tv.Client()}
		},
	}
	return a, env, out
}

func TestLoginSingleServerIsPickedAutomatically(t *testing.T) {
	a, env, out := newAccount(t, "", 1)
	cfg, err := login(context.Background(), env)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if cfg.URL != a.plexURL || cfg.Token != "srv-token-1" {
		t.Errorf("cfg = %+v", cfg)
	}
	for _, want := range []string{"CODE1", "Waiting", "Connecting to Server1", "Signed in to Server1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), "Servers:") || strings.Contains(out.String(), "▀") {
		t.Errorf("no menu or QR expected:\n%s", out)
	}
	saved, err := store.ReadConfigFile()
	if err != nil || saved != cfg {
		t.Fatalf("saved %+v, %v; want %+v", saved, err, cfg)
	}
	if len(saved.ClientID) != 32 {
		t.Errorf("client id %q should be generated and saved", saved.ClientID)
	}
	if got := a.deviceIDs[0]; got != saved.ClientID {
		t.Errorf("plex.tv saw device %q, config has %q", got, saved.ClientID)
	}
}

func TestLoginKeepsExistingClientIDAndDropsInsecure(t *testing.T) {
	a, env, _ := newAccount(t, "", 1)
	if err := store.SaveConfig(store.Config{ClientID: "keep-me", Insecure: true, Icons: "ascii", URL: "old", Token: "old"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := login(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "keep-me" || cfg.Insecure || cfg.Icons != "ascii" || cfg.URL != a.plexURL {
		t.Errorf("cfg = %+v", cfg)
	}
	if a.deviceIDs[0] != "keep-me" {
		t.Errorf("device id = %q", a.deviceIDs[0])
	}
}

func TestLoginChoosesServerByNumber(t *testing.T) {
	_, env, out := newAccount(t, "3\n", 3)
	cfg, err := login(context.Background(), env)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// Server2 is shared, so it ranks after the owned servers: Server1, Server3, Server2
	if cfg.Token != "srv-token-2" {
		t.Errorf("token = %q, want the third listed server (Server2)", cfg.Token)
	}
	for _, want := range []string{"1. Server1", "2. Server3", "3. Server2 (shared with you)", "Use which? [1]"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("menu lacks %q:\n%s", want, out)
		}
	}
}

func TestLoginBlankChoiceTakesFirstServer(t *testing.T) {
	_, env, _ := newAccount(t, "\n", 2)
	cfg, err := login(context.Background(), env)
	if err != nil || cfg.Token != "srv-token-1" {
		t.Errorf("cfg %+v, err %v", cfg, err)
	}
}

func TestLoginInvalidChoicesAreReprompted(t *testing.T) {
	_, env, out := newAccount(t, "0\n9\nabc\n-1\n2\n", 2)
	cfg, err := login(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "srv-token-2" {
		t.Errorf("token = %q", cfg.Token)
	}
	if n := strings.Count(out.String(), "Use which?"); n != 5 {
		t.Errorf("prompted %d times, want 5", n)
	}
}

func TestLoginEndOfInputWithoutChoiceFailsAndSavesNothing(t *testing.T) {
	_, env, _ := newAccount(t, "nope\n", 2)
	_, err := login(context.Background(), env)
	if err == nil || !strings.Contains(err.Error(), "no server chosen") {
		t.Fatalf("err = %v", err)
	}
	if c, _ := store.ReadConfigFile(); c.Token != "" || c.URL != "" {
		t.Errorf("a failed login saved %+v", c)
	}
}

func TestLoginExpiredPinGetsANewCode(t *testing.T) {
	a, env, out := newAccount(t, "", 1)
	a.expireFirst = true
	if _, err := login(context.Background(), env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if a.pins != 2 || !strings.Contains(out.String(), "CODE1") || !strings.Contains(out.String(), "CODE2") ||
		!strings.Contains(out.String(), "The code expired") {
		t.Errorf("pins %d, output:\n%s", a.pins, out)
	}
}

func TestLoginAccountWithoutServers(t *testing.T) {
	_, env, _ := newAccount(t, "", 0)
	_, err := login(context.Background(), env)
	if err == nil || !strings.Contains(err.Error(), "no servers") {
		t.Errorf("err = %v", err)
	}
}

func TestLoginShowsQRWhenEnabled(t *testing.T) {
	_, env, out := newAccount(t, "", 1)
	env.qr = true
	if _, err := login(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "▀") {
		t.Error("QR code missing")
	}
}

func TestLoginWarnsAboutEnvironmentOverrides(t *testing.T) {
	_, env, out := newAccount(t, "", 1)
	t.Setenv("PLEX_URL", "http://other")
	t.Setenv("PLEX_TOKEN", "x")
	if _, err := login(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Note: PLEX_URL, PLEX_TOKEN set in your environment override config.toml") {
		t.Errorf("output:\n%s", out)
	}
}

func TestLoginUnreachableServer(t *testing.T) {
	a, env, _ := newAccount(t, "", 1)
	dead := httptest.NewServer(http.NotFoundHandler()) // answers, but isn't a Plex server
	defer dead.Close()
	a.servers[0]["connections"] = []map[string]any{{"uri": dead.URL, "local": true}}
	_, err := login(context.Background(), env)
	if err == nil || !strings.Contains(err.Error(), "couldn't reach Server1") {
		t.Errorf("err = %v", err)
	}
	if c, _ := store.ReadConfigFile(); c.Token != "" {
		t.Error("saved a server that can't be reached")
	}
}

func TestLoginPlexTVErrorIsReturned(t *testing.T) {
	_, env, _ := newAccount(t, "", 1)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer bad.Close()
	env.device = func(id string) plex.Device { return plex.Device{ID: id, BaseURL: bad.URL} }
	if _, err := login(context.Background(), env); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v", err)
	}
}

func TestDefaultLoginEnvIsWiredToRealPlexTV(t *testing.T) {
	e := defaultLoginEnv()
	d := e.device("abc")
	if d.ID != "abc" || d.Product != "plx" || d.BaseURL != "" || d.HTTP != nil || !e.qr || e.poll <= 0 || e.in == nil || e.out == nil {
		t.Errorf("env = %+v device = %+v", e, d)
	}
}

func TestLoginWithCancelledContextFails(t *testing.T) {
	// a cancelled context (ctrl+c) ends the login instead of hanging
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := defaultLoginEnv()
	env.device = func(id string) plex.Device { return plex.Device{ID: id, BaseURL: "http://127.0.0.1:1"} }
	if _, err := login(ctx, env); err == nil {
		t.Error("want error")
	}
}
