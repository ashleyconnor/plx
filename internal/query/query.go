// Package query implements plx's search-style filter language:
//
//	-is:watched size>4G label:keep,fav -label:trash path:/tv/anime/ added<30d sort:size-,title
//
// Whitespace is AND, commas within a value are OR, a leading - negates a
// term, bare words match the title. Fields come from the field registry.
package query

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
)

type Query struct {
	Text  string
	Terms []Term
	Sort  []SortKey
}

type Term struct {
	Neg    bool
	Field  *field.Field // nil for a bare word
	Op     string       // ":", "=", "!=", ">", ">=", "<", "<="
	Values []string     // OR'd
	Word   string       // bare word

	match func(*model.Item) bool
}

type SortKey struct {
	Field *field.Field
	Desc  bool
}

func (k SortKey) String() string {
	if k.Desc {
		return k.Field.Name + "-"
	}
	return k.Field.Name
}

// now is overridable in tests.
var now = time.Now

// Parse parses a query string. An empty string matches everything.
func Parse(s string) (*Query, error) {
	q := &Query{Text: s}
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	for _, tk := range toks {
		if err := q.addToken(tk); err != nil {
			return nil, err
		}
	}
	return q, nil
}

func (q *Query) addToken(tk token) error {
	text := tk.text
	neg := false
	if !tk.quotedAll && len(text) > 1 && text[0] == '-' {
		neg, text = true, text[1:]
	}
	if tk.quotedAll {
		q.Terms = append(q.Terms, wordTerm(neg, text))
		return nil
	}
	name, op, val, ok := splitOp(text)
	if !ok || val == "" {
		q.Terms = append(q.Terms, wordTerm(neg, text))
		return nil
	}
	switch strings.ToLower(name) {
	case "sort":
		if neg || op != ":" {
			return fmt.Errorf("use sort:field[-],field…")
		}
		keys, err := ParseSort(val)
		if err != nil {
			return err
		}
		q.Sort = keys
		return nil
	case "is":
		if op != ":" {
			return fmt.Errorf("use is:%s", val)
		}
		f, ok := field.Lookup(val)
		if !ok || f.Type != field.Bool {
			return fmt.Errorf("is:%s: not a yes/no field (try %s)", val, boolFieldNames())
		}
		name, val = f.Name, "yes"
	}
	f, ok := field.Lookup(name)
	if !ok {
		return fmt.Errorf("unknown field %q%s", name, suggest(name))
	}
	t := Term{Neg: neg, Field: f, Op: op, Values: splitValues(val)}
	m, err := compile(f, op, t.Values)
	if err != nil {
		return fmt.Errorf("%s: %w", text, err)
	}
	t.match = m
	q.Terms = append(q.Terms, t)
	return nil
}

func wordTerm(neg bool, w string) Term {
	w = strings.ReplaceAll(w, `"`, "")
	lw := strings.ToLower(w)
	return Term{Neg: neg, Word: w, match: func(it *model.Item) bool {
		return strings.Contains(strings.ToLower(it.DisplayTitle()), lw)
	}}
}

func (q *Query) Match(it *model.Item) bool {
	for i := range q.Terms {
		if q.Terms[i].match(it) == q.Terms[i].Neg {
			return false
		}
	}
	return true
}

// Filter returns the matching items, sorted by the query's sort keys if any.
func (q *Query) Filter(items []*model.Item) []*model.Item {
	out := make([]*model.Item, 0, len(items))
	for _, it := range items {
		if q.Match(it) {
			out = append(out, it)
		}
	}
	if len(q.Sort) > 0 {
		SortItems(out, q.Sort)
	}
	return out
}

// ParseSort parses "size-,title" style sort specs. A trailing - is
// descending, + (or nothing) ascending.
func ParseSort(s string) ([]SortKey, error) {
	var keys []SortKey
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		desc := false
		if strings.HasSuffix(part, "-") {
			desc, part = true, strings.TrimSuffix(part, "-")
		} else {
			part = strings.TrimSuffix(part, "+")
		}
		f, ok := field.Lookup(part)
		if !ok {
			return nil, fmt.Errorf("sort: unknown field %q%s", part, suggest(part))
		}
		keys = append(keys, SortKey{Field: f, Desc: desc})
	}
	return keys, nil
}

func FormatSort(keys []SortKey) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k.String()
	}
	return strings.Join(parts, ",")
}

// SortItems sorts stably by the keys, in order of precedence.
func SortItems(items []*model.Item, keys []SortKey) {
	sort.SliceStable(items, func(i, j int) bool {
		for _, k := range keys {
			c := compareKeys(k.Field.SortKey(items[i]), k.Field.SortKey(items[j]))
			if c == 0 {
				continue
			}
			if k.Desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
}

func compareKeys(a, b any) int {
	switch av := a.(type) {
	case int64:
		bv := b.(int64)
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
		return 0
	case string:
		return strings.Compare(av, b.(string))
	}
	return 0
}

// --- compilation -------------------------------------------------------------

func compile(f *field.Field, op string, vals []string) (func(*model.Item) bool, error) {
	switch f.Type {
	case field.String:
		return compileString(f, op, vals)
	case field.List:
		return compileList(f, op, vals)
	case field.Bool:
		return compileBool(f, op, vals)
	case field.Time:
		return compileTime(f, op, vals)
	default:
		return compileNum(f, op, vals)
	}
}

func anyOf(preds []func(*model.Item) bool) func(*model.Item) bool {
	if len(preds) == 1 {
		return preds[0]
	}
	return func(it *model.Item) bool {
		for _, p := range preds {
			if p(it) {
				return true
			}
		}
		return false
	}
}

// strMatcher returns a case-insensitive string predicate. contains selects
// substring matching for ":"; a * in the value makes it an anchored glob.
func strMatcher(op, v string, contains bool) (func(string) bool, error) {
	lv := strings.ToLower(v)
	if (op == ":" || op == "=" || op == "!=") && strings.Contains(v, "*") {
		re, err := regexp.Compile("(?i)^" + strings.ReplaceAll(regexp.QuoteMeta(v), `\*`, ".*") + "$")
		if err != nil {
			return nil, err
		}
		if op == "!=" {
			return func(s string) bool { return !re.MatchString(s) }, nil
		}
		return re.MatchString, nil
	}
	switch op {
	case ":":
		if contains {
			return func(s string) bool { return strings.Contains(strings.ToLower(s), lv) }, nil
		}
		return func(s string) bool { return strings.ToLower(s) == lv }, nil
	case "=":
		return func(s string) bool { return strings.ToLower(s) == lv }, nil
	case "!=":
		return func(s string) bool { return strings.ToLower(s) != lv }, nil
	case ">":
		return func(s string) bool { return strings.ToLower(s) > lv }, nil
	case ">=":
		return func(s string) bool { return strings.ToLower(s) >= lv }, nil
	case "<":
		return func(s string) bool { return strings.ToLower(s) < lv }, nil
	case "<=":
		return func(s string) bool { return strings.ToLower(s) <= lv }, nil
	}
	return nil, fmt.Errorf("bad operator %q", op)
}

func compileString(f *field.Field, op string, vals []string) (func(*model.Item) bool, error) {
	var preds []func(*model.Item) bool
	for _, v := range vals {
		m, err := strMatcher(op, v, true)
		if err != nil {
			return nil, err
		}
		preds = append(preds, func(it *model.Item) bool { return m(f.Str(it)) })
	}
	return anyOf(preds), nil
}

// List fields match if any element matches. Labels and genres match whole
// values; paths match substrings.
func compileList(f *field.Field, op string, vals []string) (func(*model.Item) bool, error) {
	contains := f != field.Label && f != field.Genre
	var preds []func(*model.Item) bool
	for _, v := range vals {
		m, err := strMatcher(op, v, contains)
		if err != nil {
			return nil, err
		}
		preds = append(preds, func(it *model.Item) bool {
			for _, s := range f.List(it) {
				if m(s) {
					return true
				}
			}
			return false
		})
	}
	return anyOf(preds), nil
}

func compileBool(f *field.Field, op string, vals []string) (func(*model.Item) bool, error) {
	if op != ":" && op != "=" && op != "!=" {
		return nil, fmt.Errorf("use %s:yes or %s:no", f.Name, f.Name)
	}
	if len(vals) != 1 {
		return nil, fmt.Errorf("one value expected")
	}
	var want bool
	switch strings.ToLower(vals[0]) {
	case "yes", "y", "true", "1":
		want = true
	case "no", "n", "false", "0":
		want = false
	default:
		return nil, fmt.Errorf("expected yes or no, got %q", vals[0])
	}
	if op == "!=" {
		want = !want
	}
	return func(it *model.Item) bool { return f.Bool(it) == want }, nil
}

func cmpInt(op string, a, b int64) (bool, error) {
	switch op {
	case ":", "=":
		return a == b, nil
	case "!=":
		return a != b, nil
	case ">":
		return a > b, nil
	case ">=":
		return a >= b, nil
	case "<":
		return a < b, nil
	case "<=":
		return a <= b, nil
	}
	return false, fmt.Errorf("bad operator %q", op)
}

func compileNum(f *field.Field, op string, vals []string) (func(*model.Item) bool, error) {
	if _, err := cmpInt(op, 0, 0); err != nil {
		return nil, err
	}
	var preds []func(*model.Item) bool
	for _, v := range vals {
		var n int64
		var err error
		switch f.Type {
		case field.Bytes:
			n, err = ParseBytes(v)
		case field.Duration:
			var d time.Duration
			d, err = ParseDuration(v, time.Minute)
			n = int64(d)
		default:
			n, err = strconv.ParseInt(v, 10, 64)
		}
		if err != nil {
			return nil, err
		}
		preds = append(preds, func(it *model.Item) bool {
			ok, _ := cmpInt(op, f.Num(it), n)
			return ok
		})
	}
	return anyOf(preds), nil
}

// Time values are either dates (added>2024-01-01: after that date) or ages
// (added<30d: less than 30 days ago). ":" with an age means "within".
// Items with no date (never viewed) are infinitely old.
func compileTime(f *field.Field, op string, vals []string) (func(*model.Item) bool, error) {
	if _, err := cmpInt(op, 0, 0); err != nil {
		return nil, err
	}
	var preds []func(*model.Item) bool
	for _, v := range vals {
		if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			ts := t.Unix()
			if op == ":" || op == "=" {
				lo, hi := ts, t.AddDate(0, 0, 1).Unix()
				preds = append(preds, func(it *model.Item) bool { n := f.Num(it); return n >= lo && n < hi })
				continue
			}
			preds = append(preds, func(it *model.Item) bool {
				n := f.Num(it)
				if n == 0 {
					return false
				}
				ok, _ := cmpInt(op, n, ts)
				return ok
			})
			continue
		}
		age, err := ParseDuration(v, 24*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("expected a date (2024-01-31) or an age (30d), got %q", v)
		}
		ageOp := op
		if op == ":" || op == "=" {
			ageOp = "<="
		}
		maxAge := int64(age / time.Second)
		preds = append(preds, func(it *model.Item) bool {
			n := f.Num(it)
			a := int64(1<<62 - 1)
			if n != 0 {
				a = now().Unix() - n
			}
			ok, _ := cmpInt(ageOp, a, maxAge)
			return ok
		})
	}
	return anyOf(preds), nil
}

// ParseBytes parses 4G, 4GB, 500m, 1.5T or a plain byte count (1024-based).
func ParseBytes(s string) (int64, error) {
	u := strings.ToUpper(strings.TrimSpace(s))
	u = strings.TrimSuffix(strings.TrimSuffix(u, "IB"), "B")
	mult := int64(1)
	if u != "" {
		if i := strings.IndexByte("KMGTP", u[len(u)-1]); i >= 0 {
			mult = int64(1) << (10 * (i + 1))
			u = u[:len(u)-1]
		}
	}
	f, err := strconv.ParseFloat(u, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("expected a size like 4G or 500M, got %q", s)
	}
	return int64(f * float64(mult)), nil
}

// ParseDuration accepts Go durations plus d, w and y units; a bare number
// is interpreted in the given unit.
func ParseDuration(s string, bare time.Duration) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(bare)), nil
	}
	units := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour, 'y': 365 * 24 * time.Hour}
	if len(s) > 1 {
		if u, ok := units[s[len(s)-1]]; ok {
			if n, err := strconv.ParseFloat(s[:len(s)-1], 64); err == nil {
				return time.Duration(n * float64(u)), nil
			}
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("expected a duration like 90m, 2h or 30d, got %q", s)
	}
	return d, nil
}

// --- lexing ------------------------------------------------------------------

type token struct {
	text      string
	quotedAll bool // the whole token was quoted: always a bare word
}

func tokenize(s string) ([]token, error) {
	var toks []token
	var b strings.Builder
	inQuote, started, quotedAll := false, false, false
	flush := func() {
		if started {
			toks = append(toks, token{text: b.String(), quotedAll: quotedAll})
		}
		b.Reset()
		started, quotedAll = false, false
	}
	for _, r := range s {
		switch {
		case r == '"':
			if !started {
				quotedAll = true
			}
			started = true
			inQuote = !inQuote
			// keep quotes so splitValues can protect commas inside them
			b.WriteRune(r)
		case !inQuote && (r == ' ' || r == '\t'):
			flush()
		default:
			if !inQuote && quotedAll {
				quotedAll = false // text after a closing quote
			}
			started = true
			b.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	for i := range toks {
		if toks[i].quotedAll {
			toks[i].text = strings.ReplaceAll(toks[i].text, `"`, "")
		}
	}
	return toks, nil
}

var opRe = regexp.MustCompile(`^([A-Za-z_]+)(:|!=|>=|<=|=|>|<)(.*)$`)

func splitOp(s string) (name, op, val string, ok bool) {
	m := opRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// splitValues splits on commas outside quotes and strips the quotes.
func splitValues(v string) []string {
	var out []string
	var b strings.Builder
	in := false
	for _, r := range v {
		switch {
		case r == '"':
			in = !in
		case r == ',' && !in:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(out, b.String())
}

func boolFieldNames() string {
	var names []string
	for _, f := range field.All() {
		if f.Type == field.Bool {
			names = append(names, "is:"+f.Name)
		}
	}
	return strings.Join(names, ", ")
}

func suggest(name string) string {
	best, bestD := "", 3
	for _, f := range field.All() {
		for _, n := range append([]string{f.Name}, f.Aliases...) {
			if d := levenshtein(strings.ToLower(name), n); d < bestD {
				best, bestD = n, d
			}
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
