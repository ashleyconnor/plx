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
