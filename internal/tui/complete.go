package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
)

// suggestion replaces the text from completion.start up to the cursor.
type suggestion struct {
	insert, label, detail string
}

type completion struct {
	start int
	sugs  []suggestion
	hint  string // what the field under the cursor means
}

var compOpRe = regexp.MustCompile(`^([A-Za-z_]+)(:|!=|>=|<=|=|>|<)(.*)$`)

const maxSuggestions = 50

// complete suggests completions for the filter token ending at the end of
// text (the text before the cursor). Field names come from the registry;
// values of enumerable fields (labels, resolutions…) come from items.
func complete(text string, items []*model.Item) completion {
	start := tokenStart(text)
	tok := text[start:]
	neg := ""
	if strings.HasPrefix(tok, "-") {
		neg, tok = "-", tok[1:]
	}
	c := completion{start: start}

	m := compOpRe.FindStringSubmatch(tok)
	if m == nil {
		// A field name, keyword, or a bare title word (no suggestions).
		needle := strings.ToLower(tok)
		if needle == "" && strings.TrimSpace(text) != "" {
			return c
		}
		for _, kw := range []struct{ name, doc string }{
			{"is", "yes/no fields: is:watched"},
			{"has", "has any value: has:label"},
			{"sort", "sort:size-,title"},
		} {
			if strings.HasPrefix(kw.name, needle) {
				c.sugs = append(c.sugs, suggestion{neg + kw.name + ":", kw.name + ":", kw.doc})
			}
		}
		for _, f := range field.All() {
			if strings.HasPrefix(f.Name, needle) {
				c.sugs = append(c.sugs, suggestion{neg + f.Name + ":", f.Name + ":", f.Doc})
			}
		}
		return c
	}

	name, op, val := strings.ToLower(m[1]), m[2], m[3]
	switch name {
	case "is", "has":
		for _, f := range field.All() {
			ok := f.Type == field.Bool
			if name == "has" {
				ok = f.Type == field.List || f.Type == field.String
			}
			if ok && strings.HasPrefix(f.Name, strings.ToLower(val)) {
				c.sugs = append(c.sugs, suggestion{neg + name + ":" + f.Name + " ", f.Name, f.Doc})
			}
		}
		return c
	case "sort":
		c.hint = "comma-separated fields; add - for descending, e.g. sort:size-,title"
		parts := strings.Split(val, ",")
		last := strings.TrimRight(strings.ToLower(parts[len(parts)-1]), "-+")
		head := strings.Join(parts[:len(parts)-1], ",")
		if head != "" {
			head += ","
		}
		for _, f := range field.All() {
			if strings.HasPrefix(f.Name, last) {
				c.sugs = append(c.sugs, suggestion{"sort:" + head + f.Name, f.Name, f.Doc})
			}
		}
		return c
	}

	f, ok := field.Lookup(name)
	if !ok {
		c.hint = fmt.Sprintf("unknown field %q", name)
		return c
	}
	c.hint = f.Name + ": " + f.Doc
	if !f.Enum {
		return c
	}
	// Complete the value after the last comma; earlier values are kept.
	head, cur := "", val
	if i := strings.LastIndex(val, ","); i >= 0 {
		head, cur = val[:i+1], val[i+1:]
	}
	needle := strings.ToLower(strings.Trim(cur, `"`))
	for _, v := range values(f, items) {
		if needle == "" || strings.Contains(strings.ToLower(v.value), needle) {
			c.sugs = append(c.sugs, suggestion{
				insert: neg + m[1] + op + head + quoteValue(v.value),
				label:  v.value,
				detail: plural(v.count, "item"),
			})
			if len(c.sugs) == maxSuggestions {
				break
			}
		}
	}
	return c
}

type valueCount struct {
	value string
	count int
}

// values lists a field's distinct values in items, most common first.
func values(f *field.Field, items []*model.Item) []valueCount {
	counts := map[string]int{}
	for _, it := range items {
		switch f.Type {
		case field.List:
			for _, v := range f.List(it) {
				counts[v]++
			}
		case field.String:
			if v := f.Str(it); v != "" {
				counts[v]++
			}
		default:
			if n := f.Num(it); n != 0 {
				counts[fmt.Sprint(n)]++
			}
		}
	}
	out := make([]valueCount, 0, len(counts))
	for v, n := range counts {
		out = append(out, valueCount{v, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].value < out[j].value
	})
	return out
}

func quoteValue(v string) string {
	if strings.ContainsAny(v, " ,\t") {
		return `"` + v + `"`
	}
	return v
}

// tokenStart finds where the token ending at len(text) begins: just after
// the last whitespace outside quotes.
func tokenStart(text string) int {
	start, in := 0, false
	for i, r := range text {
		switch {
		case r == '"':
			in = !in
		case !in && (r == ' ' || r == '\t'):
			start = i + 1
		}
	}
	return start
}
