// Package field is the single registry of item attributes. The query
// language, sorting, list columns and detail pane all read from it, so adding
// an attribute means adding one entry here.
package field

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ashleyconnor/plx/internal/model"
)

type Type int

const (
	String Type = iota
	List        // multi-valued string, e.g. labels
	Int
	Bytes
	Duration
	Time
	Bool
)

func (t Type) String() string {
	return [...]string{"string", "list", "int", "bytes", "duration", "time", "bool"}[t]
}

// Numeric reports whether the field is compared as a number (Time is
// compared as a number of seconds when sorting).
func (t Type) Numeric() bool { return t == Int || t == Bytes || t == Duration || t == Time }

type Field struct {
	Name    string
	Aliases []string
	Type    Type
	Doc     string

	Str  func(*model.Item) string   // String
	List func(*model.Item) []string // List
	Num  func(*model.Item) int64    // Int, Bytes, Duration (ns), Time (unix s; 0 = never)
	Bool func(*model.Item) bool     // Bool

	// Per-version fields. For numbers, Num is the largest value across an
	// item's versions and Min the smallest (a "<name>.min" field is
	// registered for filtering on it). For text, Strs has every version's
	// value, shortest version first, and a filter matches any of them.
	Min  func(*model.Item) int64
	Strs func(*model.Item) []string

	Width int // column width; 0 = not shown as a column by default

	// Enum marks fields with a small set of values worth suggesting in
	// filter autocomplete (labels, resolutions…), unlike titles or paths.
	Enum bool
}

// Format renders the field's value for display. When versions differ it
// shows them all, shortest first: "2m|2h30m".
func (f *Field) Format(it *model.Item) string {
	if f.Min != nil && len(it.Versions) > 1 {
		lo, hi := f.formatNum(f.Min(it)), f.formatNum(f.Num(it))
		if lo != hi { // compare as shown: 1h21m and 1h21m+4s are the same here
			return lo + "|" + hi
		}
	}
	if f.Strs != nil {
		var out []string
		for _, s := range f.Strs(it) {
			if s != "" && (len(out) == 0 || out[len(out)-1] != s) {
				out = append(out, s)
			}
		}
		if len(out) > 1 {
			return strings.Join(out, "|")
		}
	}
	switch f.Type {
	case Int, Bytes, Duration:
		return f.formatNum(f.Num(it))
	}
	switch f.Type {
	case String:
		return f.Str(it)
	case List:
		return strings.Join(f.List(it), ",")
	case Int:
		n := f.Num(it)
		if n == 0 {
			return ""
		}
		return fmt.Sprint(n)
	case Bytes:
		return HumanBytes(f.Num(it))
	case Duration:
		return HumanDuration(time.Duration(f.Num(it)))
	case Time:
		n := f.Num(it)
		if n == 0 {
			return ""
		}
		return time.Unix(n, 0).Format("2006-01-02")
	case Bool:
		if f.Bool(it) {
			return "✓"
		}
		return ""
	}
	return ""
}

func (f *Field) formatNum(n int64) string {
	switch f.Type {
	case Bytes:
		return HumanBytes(n)
	case Duration:
		return HumanDuration(time.Duration(n))
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

// SortKey returns a comparable value: int64 for numeric/bool fields, a
// lower-cased string otherwise.
func (f *Field) SortKey(it *model.Item) any {
	switch f.Type {
	case Int, Bytes, Duration, Time:
		return f.Num(it)
	case Bool:
		if f.Bool(it) {
			return int64(1)
		}
		return int64(0)
	case List:
		return strings.ToLower(strings.Join(f.List(it), ","))
	}
	return strings.ToLower(f.Str(it))
}

var (
	byName = map[string]*Field{}
	all    []*Field
)

func register(f *Field) *Field {
	if f.Min != nil {
		// "duration.max" is the plain field; "duration.min" filters on the
		// smallest version.
		min := &Field{Name: f.Name + ".min", Type: f.Type, Num: f.Min, Doc: "smallest across versions: " + f.Doc}
		base := f.Aliases
		for _, a := range base {
			min.Aliases = append(min.Aliases, a+".min")
			f.Aliases = append(f.Aliases, a+".max")
		}
		f.Aliases = append(f.Aliases, f.Name+".max")
		defer register(min)
	}
	for _, n := range append([]string{f.Name}, f.Aliases...) {
		if _, dup := byName[n]; dup {
			panic("field: duplicate name " + n)
		}
		byName[n] = f
	}
	all = append(all, f)
	return f
}

func Lookup(name string) (*Field, bool) {
	f, ok := byName[strings.ToLower(name)]
	return f, ok
}

func MustLookup(name string) *Field {
	f, ok := Lookup(name)
	if !ok {
		panic("field: unknown " + name)
	}
	return f
}

// All returns every field, sorted by name.
func All() []*Field {
	out := append([]*Field(nil), all...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func HumanBytes(n int64) string {
	if n <= 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

func HumanDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", max(m, 1))
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%02dm", h, m)
}
