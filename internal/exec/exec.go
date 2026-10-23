// Package exec applies a Plan through the Plex client. It makes no
// decisions: everything it runs has already been validated by plan.
package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plan"
	"github.com/ashleyconnor/plx/internal/plex"
)

// editChunk caps how many items go into one multi-edit request.
const editChunk = 200

type Executor struct {
	C         *plex.Client
	MachineID string
	LogPath   string // JSONL action log; "" disables logging
}

// Progress is reported after each step.
type Progress struct {
	Done, Total int
	Step        string
}

// Steps counts the requests a plan will make, for progress reporting.
func Steps(p *plan.Plan) int {
	n := 0
	for _, op := range p.Ops {
		switch op.Kind {
		case plan.OpRemove:
			n += len(op.Items)
		default:
			n++
		}
	}
	return n
}

// Run applies every op in order and stops at the first error.
func (e *Executor) Run(ctx context.Context, p *plan.Plan, progress func(Progress)) error {
	total, done := Steps(p), 0
	step := func(desc string) {
		done++
		if progress != nil {
			progress(Progress{Done: done, Total: total, Step: desc})
		}
	}
	for _, op := range p.Ops {
		if err := ctx.Err(); err != nil {
			return err
		}
		created, err := e.apply(ctx, op, step)
		e.log(op, created, err)
		if err != nil {
			return fmt.Errorf("%s: %w", op.Describe(), err)
		}
	}
	return nil
}

func keys(items []*model.Item) []string {
	ks := make([]string, len(items))
	for i, it := range items {
		ks[i] = it.Key
	}
	return ks
}

func (e *Executor) apply(ctx context.Context, op plan.Op, step func(string)) (string, error) {
	desc := op.Describe()
	switch op.Kind {
	case plan.OpCreatePlaylist:
		id, err := e.C.CreatePlaylist(ctx, e.MachineID, op.Title, op.ListType, keys(op.Items))
		step(desc)
		return id, err
	case plan.OpCreateCollection:
		id, err := e.C.CreateCollection(ctx, e.MachineID, op.SectionID, op.Title, op.SearchType, keys(op.Items))
		step(desc)
		return id, err
	case plan.OpAdd:
		var err error
		switch op.Target.Kind {
		case model.Playlist:
			err = e.C.AddToPlaylist(ctx, e.MachineID, op.Target.ID, keys(op.Items))
		case model.Collection:
			err = e.C.AddToCollection(ctx, e.MachineID, op.Target.ID, keys(op.Items))
		default:
			err = fmt.Errorf("can't add to a %s", op.Target.Kind)
		}
		step(desc)
		return "", err
	case plan.OpRemove:
		for _, it := range op.Items {
			var err error
			switch op.Target.Kind {
			case model.Playlist:
				err = e.C.RemoveFromPlaylist(ctx, op.Target.ID, it.MemberID)
			case model.Collection:
				err = e.C.RemoveFromCollection(ctx, op.Target.ID, it.Key)
			default:
				err = fmt.Errorf("can't remove from a %s", op.Target.Kind)
			}
			step(fmt.Sprintf("remove %q from %s", it.DisplayTitle(), op.Target))
			if err != nil {
				return "", err
			}
		}
		return "", nil
	case plan.OpMove:
		var err error
		switch op.Target.Kind {
		case model.Playlist:
			err = e.C.MovePlaylistItem(ctx, op.Target.ID, op.Items[0].MemberID, op.AfterMember)
		case model.Collection:
			err = e.C.MoveCollectionItem(ctx, op.Target.ID, op.Items[0].MemberID, op.AfterMember)
		default:
			err = fmt.Errorf("can't reorder a %s", op.Target.Kind)
		}
		step(desc)
		return "", err
	case plan.OpSetCollectionSortCustom:
		err := e.C.SetCollectionSort(ctx, op.Target.ID, plex.CollectionSortCustom)
		step(desc)
		return "", err
	case plan.OpAddLabel, plan.OpRemoveLabel:
		ks := keys(op.Items)
		for len(ks) > 0 {
			n := min(editChunk, len(ks))
			if err := e.C.EditTags(ctx, op.SectionID, op.SearchType, ks[:n], "label",
				[]string{op.Label}, op.Kind == plan.OpRemoveLabel); err != nil {
				return "", err
			}
			ks = ks[n:]
		}
		step(desc)
		return "", nil
	}
	return "", fmt.Errorf("unknown op %v", op.Kind)
}

type logEntry struct {
	Time    time.Time `json:"time"`
	Op      string    `json:"op"`
	Target  string    `json:"target,omitempty"`
	Keys    []string  `json:"keys,omitempty"`
	Members []string  `json:"members,omitempty"`
	After   string    `json:"after,omitempty"`
	Created string    `json:"created,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// log appends one line per op. It records enough (keys, member IDs,
// positions) to reconstruct an inverse later.
func (e *Executor) log(op plan.Op, created string, err error) {
	if e.LogPath == "" {
		return
	}
	ent := logEntry{Time: time.Now(), Op: op.Describe(), Keys: keys(op.Items), After: op.AfterMember, Created: created}
	if op.Target != nil {
		ent.Target = op.Target.Kind.String() + ":" + op.Target.ID
	}
	for _, it := range op.Items {
		if it.MemberID != "" {
			ent.Members = append(ent.Members, it.MemberID)
		}
	}
	if err != nil {
		ent.Error = err.Error()
	}
	b, _ := json.Marshal(ent)
	_ = os.MkdirAll(filepath.Dir(e.LogPath), 0o755)
	f, ferr := os.OpenFile(e.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
