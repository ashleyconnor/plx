package tui

import (
	"strings"
	"testing"

	"github.com/ashleyconnor/plx/internal/model"
)

func labels(ls ...string) *model.Item {
	return &model.Item{Kind: "movie", Labels: ls, Resolution: "1080"}
}

func inserts(c completion) string {
	var out []string
	for _, s := range c.sugs {
		out = append(out, s.insert)
	}
	return strings.Join(out, "|")
}

func TestComplete(t *testing.T) {
	items := []*model.Item{labels("keep", "to watch"), labels("keep"), labels("trash")}
	cases := []struct{ text, want string }{
		{"lab", "label:|labelcount:"},
		{"-lab", "-label:|-labelcount:"},
		{"label:", `label:keep|label:"to watch"|label:trash`}, // most common first, then by name
		{"label:tr", "label:trash"},
		{"label:keep,t", `label:keep,"to watch"|label:keep,trash`},
		{"size>4G -label:k", "-label:keep"},
		{"is:wa", "is:watched "},
		{"has:lab", "has:label "},
		{"sort:size-,ti", "sort:size-,title"},
		{"res:", "res:1080"},
		{"alien", ""}, // bare title words get no suggestions
		{"size>", ""}, // not enumerable: hint only
	}
	for _, c := range cases {
		comp := complete(c.text, items)
		if got := inserts(comp); got != c.want {
			t.Errorf("complete(%q) = %q, want %q", c.text, got, c.want)
		}
	}
	if h := complete("size>", items).hint; !strings.Contains(h, "size>4G") {
		t.Errorf("size hint = %q", h)
	}
	if s := complete("size>4G lab", items).start; s != len("size>4G ") {
		t.Errorf("start = %d", s)
	}
}

func TestCompleteNumericValuesAndTabSeparatedTokens(t *testing.T) {
	items := []*model.Item{{Year: 1999}, {Year: 1999}, {Year: 1980}, {}}
	// numeric enum fields suggest the values present, most common first; zero means unknown and is skipped
	if got := inserts(complete("year:", items)); got != "year:1999|year:1980" {
		t.Errorf("year: = %q", got)
	}
	if got := inserts(complete("year:19", items)); got != "year:1999|year:1980" {
		t.Errorf("year:19 = %q", got)
	}
	// a tab separates tokens like a space does
	if s := complete("size>4G\tlab", items).start; s != len("size>4G\t") {
		t.Errorf("start = %d", s)
	}
	// spaces inside quotes belong to the token
	if s := complete(`label:"to wa`, items).start; s != 0 {
		t.Errorf("start inside quote = %d", s)
	}
	if got := inserts(complete("", items)); got == "" {
		t.Error("an empty filter suggests the field names")
	}
	if got := inserts(complete("alien ", items)); got != "" {
		t.Errorf("after a space with no token: %q", got)
	}
}
