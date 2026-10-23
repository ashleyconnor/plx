package plan

import (
	"fmt"
	"time"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/model"
)

// VersionPick chooses which versions of an item to delete.
type VersionPick struct {
	Name string
	Pick func(it *model.Item) []model.Version
}

func pickBy(name string, less func(a, b *model.Version) bool) VersionPick {
	return VersionPick{Name: name, Pick: func(it *model.Item) []model.Version {
		best := 0
		for i := range it.Versions {
			if less(&it.Versions[i], &it.Versions[best]) {
				best = i
			}
		}
		return []model.Version{it.Versions[best]}
	}}
}

// Picks are the bulk rules offered when deleting versions of many items.
var Picks = []VersionPick{
	{Name: "samples (versions under half the longest's length)", Pick: samples},
	pickBy("smallest file", func(a, b *model.Version) bool { return a.Size < b.Size }),
	pickBy("largest file", func(a, b *model.Version) bool { return a.Size > b.Size }),
	pickBy("lowest bitrate", func(a, b *model.Version) bool { return a.Bitrate < b.Bitrate }),
}

// samples picks versions under half as long as the longest one: samples,
// trailers, extras. Versions of similar length (two encodes of the same
// film, or part 1 and part 2 misfiled as versions) are never picked.
func samples(it *model.Item) []model.Version {
	var longest time.Duration
	for _, v := range it.Versions {
		longest = max(longest, v.Duration)
	}
	var out []model.Version
	for _, v := range it.Versions {
		if v.Duration < longest/2 {
			out = append(out, v)
		}
	}
	return out
}

// PickMedia deletes one specific version, by media ID.
func PickMedia(mediaID string) VersionPick {
	return VersionPick{Name: "version " + mediaID, Pick: func(it *model.Item) []model.Version {
		for _, v := range it.Versions {
			if v.MediaID == mediaID {
				return []model.Version{v}
			}
		}
		return nil
	}}
}

// DeleteVersions deletes the chosen version(s) of each item and their files,
// keeping the rest. It never deletes an item's last version (that's Delete),
// and never a version whose file another kept version also uses: Plex can
// list one file under two versions, and deleting it would break the other.
func DeleteVersions(items []*model.Item, pick VersionPick, allowed bool) *Plan {
	p := &Plan{Title: "Delete versions: " + pick.Name}
	if !allowed {
		p.block("the server doesn't allow media deletion (Plex settings → Library → Allow media deletion)")
		return p
	}
	op := Op{Kind: OpDeleteVersion}
	var size int64
	files := 0
	for _, it := range dedupe(items) {
		if len(it.Versions) < 2 {
			p.skip(it, "only one version")
			continue
		}
		del := pick.Pick(it)
		if len(del) == 0 {
			p.skip(it, "no %s", pick.Name)
			continue
		}
		if len(del) >= len(it.Versions) {
			p.skip(it, "would delete every version")
			continue
		}
		gone := map[string]bool{}
		for _, v := range del {
			gone[v.MediaID] = true
		}
		kept := map[string]bool{}
		for _, v := range it.Versions {
			if !gone[v.MediaID] {
				for _, f := range v.Files {
					kept[f] = true
				}
			}
		}
		for _, v := range del {
			shared := false
			for _, f := range v.Files {
				shared = shared || kept[f]
			}
			if shared {
				p.skip(it, "version's file is also used by a version being kept")
				continue
			}
			op.Items = append(op.Items, it)
			op.Versions = append(op.Versions, v)
			size += v.Size
			files += len(v.Files)
		}
	}
	if len(op.Items) == 0 {
		p.block("nothing to delete")
		return p
	}
	p.Title = fmt.Sprintf("Delete %d versions (%d files, %s): %s", len(op.Items), files, field.HumanBytes(size), pick.Name)
	p.warn("files are permanently deleted; this can't be undone")
	p.Ops = append(p.Ops, op)
	return p
}
