package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// Linking a device the way TV apps do: plx asks plex.tv for a 4-character
// PIN, the user enters it at plex.tv/link (or scans a QR code of that page),
// and polling the PIN then returns a token. The token lists the user's
// servers, each with its own access token and connection addresses.

const plexTV = "https://plex.tv"

// Device identifies plx to plex.tv and servers. ID should stay the same
// across runs so the device appears once in Plex's Authorized Devices.
type Device struct {
	ID      string
	Product string
	Version string
}

func (d Device) setHeaders(h http.Header) {
	host, _ := os.Hostname()
	h.Set("Accept", "application/json")
	h.Set("X-Plex-Client-Identifier", d.ID)
	h.Set("X-Plex-Product", d.Product)
	h.Set("X-Plex-Version", d.Version)
	h.Set("X-Plex-Device-Name", strings.TrimSuffix(host, ".local"))
	h.Set("X-Plex-Platform", runtime.GOOS)
	h.Set("X-Plex-Device", runtime.GOOS)
}

type Pin struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
	AuthToken string    `json:"authToken"`
}

// LinkURL is the page where the user enters the PIN, with it filled in.
func (p *Pin) LinkURL() string { return "https://www.plex.tv/link/?pin=" + p.Code }

type Resource struct {
	Name             string       `json:"name"`
	ClientIdentifier string       `json:"clientIdentifier"`
	Provides         string       `json:"provides"`
	Owned            bool         `json:"owned"`
	AccessToken      string       `json:"accessToken"`
	Connections      []Connection `json:"connections"`
}

type Connection struct {
	URI      string `json:"uri"`
	Protocol string `json:"protocol"`
	Local    bool   `json:"local"`
	Relay    bool   `json:"relay"`
}

func (r Resource) IsServer() bool {
	for _, p := range strings.Split(r.Provides, ",") {
		if p == "server" {
			return true
		}
	}
	return false
}

func (d Device) call(ctx context.Context, method, path, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, plexTV+path, nil)
	if err != nil {
		return err
	}
	d.setHeaders(req.Header)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("plex.tv %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// NewPin starts linking. The PIN expires after about 15 minutes.
func (d Device) NewPin(ctx context.Context) (*Pin, error) {
	var p Pin
	if err := d.call(ctx, http.MethodPost, "/api/v2/pins", "", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

var ErrPinExpired = errors.New("the code expired")

// WaitForToken polls the PIN until the user links it, returning their token.
func (d Device) WaitForToken(ctx context.Context, p *Pin, every time.Duration) (string, error) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		var cur Pin
		if err := d.call(ctx, http.MethodGet, fmt.Sprintf("/api/v2/pins/%d", p.ID), "", &cur); err != nil {
			if strings.Contains(err.Error(), ": 404 ") { // plex.tv drops expired PINs
				return "", ErrPinExpired
			}
			return "", err
		}
		if cur.AuthToken != "" {
			return cur.AuthToken, nil
		}
		if !p.ExpiresAt.IsZero() && time.Now().After(p.ExpiresAt) {
			return "", ErrPinExpired
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-t.C:
		}
	}
}

// Servers lists the Plex Media Servers the user can reach, owned first.
func (d Device) Servers(ctx context.Context, token string) ([]Resource, error) {
	var rs []Resource
	if err := d.call(ctx, http.MethodGet, "/api/v2/resources?includeHttps=1&includeRelay=1", token, &rs); err != nil {
		return nil, err
	}
	var out []Resource
	for _, r := range rs {
		if r.IsServer() {
			out = append(out, r)
		}
	}
	for i := 1; i < len(out); i++ { // stable: owned first
		for j := i; j > 0 && out[j].Owned && !out[j-1].Owned; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// Connect tries a server's addresses at once and returns the best one that
// answers: local before remote before relay. It returns as soon as a local
// address answers rather than waiting for slower ones.
func (d Device) Connect(ctx context.Context, r Resource) (string, error) {
	rank := func(c Connection) int {
		switch {
		case c.Relay:
			return 2
		case !c.Local:
			return 1
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	type result struct {
		c  Connection
		ok bool
	}
	results := make(chan result, len(r.Connections))
	for _, c := range r.Connections {
		go func() {
			cl := New(c.URI, r.AccessToken, Options{Timeout: 5 * time.Second, ClientID: d.ID})
			_, err := cl.Identity(ctx)
			results <- result{c, err == nil}
		}()
	}
	var best *Connection
	for range r.Connections {
		res := <-results
		if res.ok && (best == nil || rank(res.c) < rank(*best)) {
			best = &res.c
		}
		if best != nil && rank(*best) == 0 {
			break
		}
	}
	if best == nil {
		return "", fmt.Errorf("couldn't reach %s at any of its %d addresses", r.Name, len(r.Connections))
	}
	return best.URI, nil
}
