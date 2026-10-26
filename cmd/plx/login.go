package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

// loginEnv is everything login touches outside itself, so tests can drive it.
type loginEnv struct {
	in     io.Reader
	out    io.Writer
	device func(clientID string) plex.Device // the plex.tv client
	poll   time.Duration                     // how often the PIN is checked
	qr     bool                              // draw the QR code
}

func defaultLoginEnv() loginEnv {
	return loginEnv{
		in: os.Stdin, out: os.Stdout, poll: 2 * time.Second, qr: true,
		device: func(id string) plex.Device { return plex.Device{ID: id, Product: "plx", Version: version} },
	}
}

// login links plx to the user's Plex account with a PIN, picks a server and
// saves its address and access token to config.toml.
func login(ctx context.Context, env loginEnv) (store.Config, error) {
	out := env.out
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
	dev := env.device(cfg.ClientID)

	var token string
	for token == "" {
		pin, err := dev.NewPin(ctx)
		if err != nil {
			return cfg, err
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Sign in to Plex: scan the code, or go to plex.tv/link and enter")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "    %s\n\n", pin.Code)
		if env.qr {
			if q, err := qrBlocks(pin.LinkURL()); err == nil {
				fmt.Fprintln(out, q)
			}
		}
		fmt.Fprintln(out, "Waiting… (ctrl+c to cancel)")
		token, err = dev.WaitForToken(ctx, pin, env.poll)
		if errors.Is(err, plex.ErrPinExpired) {
			fmt.Fprintln(out, "The code expired; here's a new one.")
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
		if srv, err = chooseServer(env.in, out, servers); err != nil {
			return cfg, err
		}
	}
	fmt.Fprintf(out, "Connecting to %s…\n", srv.Name)
	url, err := dev.Connect(ctx, srv)
	if err != nil {
		return cfg, err
	}
	cfg.URL, cfg.Token, cfg.Insecure = url, srv.AccessToken, false
	if err := store.SaveConfig(cfg); err != nil {
		return cfg, err
	}
	fmt.Fprintf(out, "Signed in to %s at %s; saved to %s/config.toml\n", srv.Name, url, store.Dir())
	var set []string
	for _, k := range []string{"PLEX_URL", "PLEX_TOKEN", "PLEX_INSECURE"} {
		if os.Getenv(k) != "" {
			set = append(set, k)
		}
	}
	if len(set) > 0 {
		verb := "overrides"
		if len(set) > 1 {
			verb = "override"
		}
		fmt.Fprintf(out, "Note: %s set in your environment %s config.toml.\n", strings.Join(set, ", "), verb)
	}
	return cfg, nil
}

func chooseServer(r io.Reader, out io.Writer, servers []plex.Resource) (plex.Resource, error) {
	fmt.Fprintln(out, "\nServers:")
	for i, s := range servers {
		owner := ""
		if !s.Owned {
			owner = " (shared with you)"
		}
		fmt.Fprintf(out, "  %d. %s%s\n", i+1, s.Name, owner)
	}
	in := bufio.NewScanner(r)
	for {
		fmt.Fprint(out, "Use which? [1] ")
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
