package plan

import (
	"fmt"

	"github.com/ashleyconnor/plx/internal/model"
)

// Reorder rearranges an ordered container so its members appear in the
// order of desired. Plex only supports "move X after Y", one item per
// request, so this computes a minimal set of moves: members on the longest
// subsequence already in the right relative order stay put, and every other
// member is moved directly after its desired predecessor.
//
// current is the container's members in their present order; desired is
// the same members in the target order.
func Reorder(c *model.Container, current, desired []*model.Item) *Plan {
	p := &Plan{Title: "Reorder " + c.String()}
	caps := c.Caps()
	if !caps.Ordered {
		p.block("%ss have no order", c.Kind)
		return p
	}
	if caps.ReadOnly {
		p.block("%s is smart: Plex decides its order", c)
		return p
	}
	if len(current) != len(desired) {
		p.block("internal: reorder needs every member (%d current, %d desired)", len(current), len(desired))
		return p
	}
	pos := make(map[string]int, len(current))
	for i, it := range current {
		pos[it.MemberID] = i
	}
	seq := make([]int, len(desired))
	for i, it := range desired {
		j, ok := pos[it.MemberID]
		if !ok {
			p.block("internal: %q is not a member", it.DisplayTitle())
			return p
		}
		seq[i] = j
	}
	keep := lis(seq)

	var moves []Op
	for i, it := range desired {
		if keep[i] {
			continue
		}
		after := ""
		if i > 0 {
			after = desired[i-1].MemberID
		}
		moves = append(moves, Op{Kind: OpMove, Target: c, Items: []*model.Item{it}, AfterMember: after})
	}
	if len(moves) == 0 {
		p.block("already in that order")
		return p
	}
	if c.Kind == model.Collection {
		p.Ops = append(p.Ops, Op{Kind: OpSetCollectionSortCustom, Target: c})
		p.warn("the collection's sort will be switched to custom")
	}
	p.Ops = append(p.Ops, moves...)
	if len(moves) > 200 {
		p.warn("%d moves: Plex moves one item per request, this may take a while", len(moves))
	}
	return p
}

// lis marks the elements of a longest strictly increasing subsequence.
func lis(seq []int) []bool {
	n := len(seq)
	tails := []int{}       // tails[k] = index in seq of the smallest tail of an increasing run of length k+1
	prev := make([]int, n) // predecessor index in the run
	for i, v := range seq {
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if seq[tails[mid]] < v {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo > 0 {
			prev[i] = tails[lo-1]
		} else {
			prev[i] = -1
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}
	keep := make([]bool, n)
	if len(tails) == 0 {
		return keep
	}
	for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
		keep[i] = true
	}
	return keep
}

// Simulate applies the plan's moves to a list of member IDs, the way Plex
// would. Used by tests and to sanity-check plans.
func Simulate(order []string, ops []Op) ([]string, error) {
	out := append([]string(nil), order...)
	for _, op := range ops {
		if op.Kind != OpMove {
			continue
		}
		id := op.Items[0].MemberID
		from := index(out, id)
		if from < 0 {
			return nil, fmt.Errorf("move: %s not found", id)
		}
		out = append(out[:from], out[from+1:]...)
		to := 0
		if op.AfterMember != "" {
			a := index(out, op.AfterMember)
			if a < 0 {
				return nil, fmt.Errorf("move: after %s not found", op.AfterMember)
			}
			to = a + 1
		}
		out = append(out[:to], append([]string{id}, out[to:]...)...)
	}
	return out, nil
}

func index(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}
