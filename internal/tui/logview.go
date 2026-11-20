package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/ashleyconnor/plx/internal/logbuf"
)

// The log pane is a full-screen overlay (like help) listing every Plex
// request with its decoded parameters and, for failures, the full response
// body. logScroll < 0 means "follow the bottom".

func (m *Model) openLog() {
	m.mode, m.logScroll = modeLog, -1
}

// logText renders entries as plain text, one string per line, unwrapped.
func logText(entries []logbuf.Entry) []string {
	var out []string
	for _, e := range entries {
		mark := "·"
		if e.Level == logbuf.Error {
			mark = "✗"
		}
		out = append(out, e.Time.Format("15:04:05")+" "+mark+" "+e.Summary)
		for _, d := range e.Detail {
			out = append(out, "    "+d)
		}
	}
	return out
}

// logLines renders the buffer for the screen, wrapping long lines.
func (m *Model) logLines() []string {
	w := max(20, m.w)
	var out []string
	for _, e := range m.log.Entries() {
		head := e.Time.Format("15:04:05") + " "
		st := stBold
		if e.Level == logbuf.Error {
			head += "✗ "
			st = stErr
		} else {
			head += "· "
		}
		for _, l := range wrapLine(head+e.Summary, w) {
			out = append(out, st.Render(l))
		}
		for _, d := range e.Detail {
			for _, l := range wrapLine("    "+d, w) {
				out = append(out, stDim.Render(l))
			}
		}
	}
	if len(out) == 0 {
		out = []string{stDim.Render("No requests logged yet.")}
	}
	return out
}

// wrapLine hard-wraps s to width w by display cells.
func wrapLine(s string, w int) []string {
	s = strings.ReplaceAll(strings.TrimRight(s, "\r"), "\t", "    ")
	var out []string
	for runewidth.StringWidth(s) > w {
		head := runewidth.Truncate(s, w, "")
		if head == "" { // a single rune wider than w
			break
		}
		out = append(out, head)
		s = s[len(head):]
	}
	return append(out, s)
}

func (m *Model) logView() string {
	lines := m.logLines()
	h := max(1, m.h-2)
	maxScroll := max(0, len(lines)-h)
	if m.logScroll >= maxScroll {
		m.logScroll = -1 // at or past the bottom: follow new entries
	}
	top := m.logScroll
	if top < 0 {
		top = maxScroll
	}
	shown := lines[top:min(len(lines), top+h)]
	title := stBold.Render("Log") + stDim.Render(fmt.Sprintf("  %d lines", len(lines)))
	for len(shown) < h {
		shown = append(shown, "")
	}
	footer := renderHints([]hint{{"j/k", "scroll"}, {"g/G", "top / bottom"}, {"w", "save to file"}, {"esc", "close"}}, m.w)
	return title + "\n" + strings.Join(shown, "\n") + "\n" + footer
}

func (m *Model) logKey(msg tea.KeyMsg) tea.Cmd {
	page := max(1, m.h/2)
	if m.logScroll < 0 {
		m.logScroll = max(0, len(m.logLines())-max(1, m.h-2))
	}
	switch msg.String() {
	case "j", "down":
		m.logScroll++
	case "k", "up":
		m.logScroll = max(0, m.logScroll-1)
	case "ctrl+d", "pgdown", " ":
		m.logScroll += page
	case "ctrl+u", "pgup":
		m.logScroll = max(0, m.logScroll-page)
	case "g", "home":
		m.logScroll = 0
	case "G", "end":
		m.logScroll = -1
	case "w":
		path, err := m.saveLog()
		m.mode = modeNormal
		if err != nil {
			m.setStatus("saving log: "+err.Error(), true)
		} else {
			m.setStatus("log saved to "+path, false)
		}
	default:
		m.mode = modeNormal
	}
	return nil
}

func (m *Model) logWheel(msg tea.MouseMsg) {
	if m.logScroll < 0 {
		m.logScroll = max(0, len(m.logLines())-max(1, m.h-2))
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.logScroll = max(0, m.logScroll-wheelStep)
	case tea.MouseButtonWheelDown:
		m.logScroll += wheelStep
	}
}

// saveLog writes the whole log, unwrapped, to a file in the temp directory
// so it can be read or shared outside the TUI.
func (m *Model) saveLog() (string, error) {
	path := filepath.Join(os.TempDir(), "plx-"+time.Now().Format("20060102-150405")+".log")
	body := strings.Join(logText(m.log.Entries()), "\n") + "\n"
	return path, os.WriteFile(path, []byte(body), 0o600)
}
