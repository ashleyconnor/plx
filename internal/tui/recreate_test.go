package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/ashleyconnor/plx/internal/exec"
	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
	"github.com/ashleyconnor/plx/internal/plex"
)

// failedAdd is the execDoneMsg for a plan whose add to collection id failed
// with status.
func failedAdd(id string, status int) execDoneMsg {
	c := &model.Container{Kind: model.Collection, ID: id, Title: "delete", SectionID: "1", Subtype: "movie"}
	items := []*model.Item{{Key: "11", Kind: "movie", Title: "Brazil", SectionID: "1"}}
	p := &plan.Plan{Title: "Add to delete", Ops: []plan.Op{{Kind: plan.OpAdd, Target: c, Items: items}}}
	err := &exec.OpError{Op: p.Ops[0], Index: 0, Err: &plex.APIError{Method: "PUT", Path: "/library/collections/" + id + "/items", Status: status}}
	return execDoneMsg{plan: p, err: err}
}

func TestRejectedAddToEmptyCollectionOffersRecreate(t *testing.T) {
	h := newHarness(t)
	h.send(failedAdd("70", 400))
	h.wantMode(modePreview)
	if h.m.confirmYes {
		t.Error("the recreate preview deletes a collection, so it should default to No")
	}
	h.wantView("Recreate collection", "Items can't be added to an empty collection, so it must be recreated.", "create collection \"delete\" with 1 items")
	if got := h.m.preview.Ops[0]; got.Kind != plan.OpDeleteContainer || got.Target.ID != "70" {
		t.Errorf("op 0 = %s", got.Describe())
	}
	if n := len(h.sv.writes()); n != 0 {
		t.Errorf("nothing should be written before confirming: %v", h.sv.writes())
	}
}

func TestRejectedAddToNonEmptyCollectionJustFails(t *testing.T) {
	h := newHarness(t)
	h.send(failedAdd("71", 400))
	h.wantMode(modeNormal)
	if !h.m.statusErr || !strings.Contains(h.m.status, "failed:") {
		t.Errorf("status = %q", h.m.status)
	}
}

func TestOtherAddFailuresDontOfferRecreate(t *testing.T) {
	h := newHarness(t)
	h.send(failedAdd("70", 500))
	h.wantMode(modeNormal)
	h.send(execDoneMsg{plan: failedAdd("70", 400).plan, err: errors.New("network down")})
	h.wantMode(modeNormal)
}
