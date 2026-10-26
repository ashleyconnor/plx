package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/store"
)

const version = "0.1"

// login links plx to the user's Plex account with a PIN, picks a server and
// saves its address and access token to config.toml.
func login(ctx context.Context) (store.Config, error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	cfg, err := store.ReadConfigFile()
	if err != nil {
		return cfg, err
	}
	if cfg.ClientID == "" {
		b := make([]byte, 16)
		rand.Read(b)
		cfg.ClientID = hex.EncodeToString(b)
	}
	dev := plex.Device{ID: cfg.ClientID, Product: "plx", Version: version}

	var token string
	for token == "" {
		pin, err := dev.NewPin(ctx)
		if err != nil {
			return cfg, err
		}
		fmt.Println()
		fmt.Println("Sign in to Plex: scan the code, or go to plex.tv/link and enter")
		fmt.Println()
		fmt.Printf("    %s\n\n", strings.Join(strings.Split(pin.Code, ""), " "))
		if q, err := qrBlocks(pin.LinkURL()); err == nil {
			fmt.Println(q)
		}
		fmt.Println("Waiting… (ctrl+c to cancel)")
		token, err = dev.WaitForToken(ctx, pin, 2*time.Second)
		if errors.Is(err, plex.ErrPinExpired) {
			fmt.Println("The code expired; here's a new one.")
			continue
		}
		if err != nil {
			return cfg, err
		}
	}

	servers, err := dev.Servers(ctx, token)
	if err != nil {
		return cfg, err
	}
	if len(servers) == 0 {
		return cfg, fmt.Errorf("signed in, but this Plex account has no servers")
	}
	srv := servers[0]
	if len(servers) > 1 {
		if srv, err = chooseServer(servers); err != nil {
			return cfg, err
		}
	}
	fmt.Printf("Connecting to %s…\n", srv.Name)
	url, err := dev.Connect(ctx, srv)
	if err != nil {
		return cfg, err
	}
	cfg.URL, cfg.Token, cfg.Insecure = url, srv.AccessToken, false
	if err := store.SaveConfig(cfg); err != nil {
		return cfg, err
	}
	fmt.Printf("Signed in to %s at %s; saved to %s/config.toml\n", srv.Name, url, store.Dir())
	return cfg, nil
}

func chooseServer(servers []plex.Resource) (plex.Resource, error) {
	fmt.Println("\nServers:")
	for i, s := range servers {
		owner := ""
		if !s.Owned {
			owner = " (shared with you)"
		}
		fmt.Printf("  %d. %s%s\n", i+1, s.Name, owner)
	}
	in := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Use which? [1] ")
		if !in.Scan() {
			return plex.Resource{}, fmt.Errorf("no server chosen")
		}
		text := strings.TrimSpace(in.Text())
		if text == "" {
			return servers[0], nil
		}
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(servers) {
			return servers[n-1], nil
		}
	}
}

// qrBlocks draws a QR code with half blocks, two modules per character
// row, in explicit black and white so it scans on dark and light themes.
func qrBlocks(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 2 // modules of white border
	n := code.Size + 2*quiet
	black := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	color := func(b bool) int { // ANSI 30 black / 97 bright white
		if b {
			return 30
		}
		return 97
	}
	var sb strings.Builder
	for y := 0; y < n; y += 2 {
		sb.WriteString("    ")
		for x := 0; x < n; x++ {
			// ▀: foreground paints the top module, background the bottom.
			fmt.Fprintf(&sb, "\x1b[%d;%dm▀", color(black(x, y)), color(black(x, y+1))+10)
		}
		sb.WriteString("\x1b[0m\n")
	}
	return sb.String(), nil
}
