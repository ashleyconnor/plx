package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ashleyconnor/plx/internal/logbuf"
)

func TestLogPane(t *testing.T) {
	h := newHarness(t)
	h.m.log.Add(logbuf.Entry{Level: logbuf.Error, Summary: "PUT /library/sections/1/all → 400 Bad Request", Detail: []string{"response:", "  " + strings.Repeat("y", 300)}})
	h.m.setStatus("failed: plex: PUT ...: 400", true)
	h.wantView("L: log")

	h.press("L")
	h.wantMode(modeLog)
	// The long body line wraps instead of being cut off.
	h.wantView("✗ PUT /library/sections/1/all → 400 Bad Request", "response:", strings.Repeat("y", 100), "✗ failed: plex: PUT")

	h.send(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	h.press("esc")
	h.wantMode(modeNormal)
}

func TestLogPaneFollowsAndScrolls(t *testing.T) {
	h := newHarness(t)
	h.m.h = 10
	for i := 0; i < 50; i++ {
		h.m.log.Add(logbuf.Entry{Summary: "req " + strings.Repeat("0", i%3) + string(rune('a'+i%26))})
	}
	h.m.log.Add(logbuf.Entry{Summary: "LAST"})
	h.press("L")
	h.wantView("LAST")
	h.press("g")
	if strings.Contains(h.view(), "LAST") {
		t.Error("g should scroll to the top")
	}
	h.press("G")
	h.m.log.Add(logbuf.Entry{Summary: "NEWER"})
	h.wantView("NEWER") // following the bottom picks up new entries
}

func TestLogPaneSave(t *testing.T) {
	h := newHarness(t)
	t.Setenv("TMPDIR", t.TempDir())
	h.m.log.Add(logbuf.Entry{Summary: "GET /x → 400", Detail: []string{"response:", "  boom"}})
	h.press("L", "w")
	h.wantMode(modeNormal)
	path := strings.TrimPrefix(h.m.status, "log saved to ")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("status %q: %v", h.m.status, err)
	}
	if !strings.Contains(string(b), "GET /x → 400") || !strings.Contains(string(b), "boom") {
		t.Errorf("saved log = %q", b)
	}
}
