package plan

import (
	"fmt"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
)

// Delete removes items from the library and their files from disk. Plex
// refuses unless the server allows media deletion.
func Delete(items []*model.Item, allowed bool) *Plan {
	p := &Plan{Title: "Delete from disk"}
	if !allowed {
		p.block("the server doesn't allow media deletion (Plex settings → Library → Allow media deletion)")
		return p
	}
	items = dedupe(items)
	if len(items) == 0 {
		p.block("nothing to delete")
		return p
	}
	files := 0
	var size int64
	for _, it := range items {
		files += len(it.Files)
		size += it.Size
	}
	p.Title = fmt.Sprintf("Delete %d items (%d files, %s) from disk", len(items), files, field.HumanBytes(size))
	p.warn("files are permanently deleted; this can't be undone")
	p.warn("Plex deletes files, not folders: emptied folders are left behind")
	p.Ops = append(p.Ops, Op{Kind: OpDelete, Items: items})
	return p
}
