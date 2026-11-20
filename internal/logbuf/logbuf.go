// Package logbuf is a small, concurrency-safe ring of log entries. The Plex
// client writes every request into it and the TUI's log pane reads it, so a
// failing request can be inspected in full without leaving the app.
package logbuf

import (
	"fmt"
	"sync"
	"time"
)

type Level int

const (
	Info Level = iota
	Error
)

// Entry is one logged event. Detail holds extra lines (request parameters,
// response body) shown indented under the summary.
type Entry struct {
	Time    time.Time
	Level   Level
	Summary string
	Detail  []string
}

// Buffer keeps the most recent entries, dropping the oldest past its cap.
type Buffer struct {
	mu      sync.Mutex
	entries []Entry
	cap     int
}

func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 500
	}
	return &Buffer{cap: capacity}
}

func (b *Buffer) Add(e Entry) {
	if b == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, e)
	if over := len(b.entries) - b.cap; over > 0 {
		b.entries = append([]Entry(nil), b.entries[over:]...)
	}
}

// Errorf records an error-level entry.
func (b *Buffer) Errorf(format string, args ...any) {
	b.Add(Entry{Level: Error, Summary: fmt.Sprintf(format, args...)})
}

// Entries returns a copy of the buffer, oldest first.
func (b *Buffer) Entries() []Entry {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Entry(nil), b.entries...)
}
