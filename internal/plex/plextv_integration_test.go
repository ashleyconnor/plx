//go:build integration

package plex

import (
	"context"
	"os"
	"testing"
	"time"
)

// Lists the account's servers and connects to the first, with PLEX_TOKEN
// as an account token. Prints names and addresses, never tokens.
func TestServersAndConnect(t *testing.T) {
	tok := os.Getenv("PLEX_TOKEN")
	if tok == "" {
		t.Skip("PLEX_TOKEN not set")
	}
	dev := Device{ID: "plx-integration-test", Product: "plx", Version: "test"}
	ctx := context.Background()
	start := time.Now()
	servers, err := dev.Servers(ctx, tok)
	t.Logf("resources took %s", time.Since(start))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) == 0 {
		t.Fatal("no servers")
	}
	for _, s := range servers {
		t.Logf("%s owned=%v connections=%d token=%v", s.Name, s.Owned, len(s.Connections), s.AccessToken != "")
	}
	start = time.Now()
	url, err := dev.Connect(ctx, servers[0])
	t.Logf("connect took %s", time.Since(start))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("connected to %s at %s", servers[0].Name, url)
}
