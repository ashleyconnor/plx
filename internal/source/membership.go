package source

import (
	"context"
	"sync"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/plex"
)

// Plex can't say which collections or playlists an item is in (section
// listings carry collection tags only sometimes), so membership is indexed
// by listing every collection and playlist.

// fetchParallel is how many container listings are fetched at once.
const fetchParallel = 8

// members lists each container's ratingKeys, in parallel.
func (c *Catalog) members(ctx context.Context, cs []*model.Container) (map[string][]string, error) {
	idx := map[string][]string{}
	var mu sync.Mutex
	var firstErr error
	sem := make(chan struct{}, fetchParallel)
	var wg sync.WaitGroup
	for _, ct := range cs {
		wg.Add(1)
		sem <- struct{}{}
		go func(ct *model.Container) {
			defer wg.Done()
			defer func() { <-sem }()
			var ms []plex.Metadata
			var err error
			if ct.Kind == model.Playlist {
				ms, err = c.C.PlaylistItems(ctx, ct.ID)
			} else {
				ms, err = c.C.CollectionItems(ctx, ct.ID)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, m := range ms {
				k := string(m.RatingKey)
				if !contains(idx[k], ct.Title) {
					idx[k] = append(idx[k], ct.Title)
				}
			}
		}(ct)
	}
	wg.Wait()
	return idx, firstErr
}

func (c *Catalog) ensureCollections(ctx context.Context, sectionID string) error {
	c.mu.Lock()
	_, ok := c.collIdx[sectionID]
	c.mu.Unlock()
	if ok {
		return nil
	}
	cs, err := c.Collections(ctx, sectionID)
	if err != nil {
		return err
	}
	idx, err := c.members(ctx, cs)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.collIdx[sectionID] = idx
	c.mu.Unlock()
	return nil
}

func (c *Catalog) ensurePlaylists(ctx context.Context) error {
	c.mu.Lock()
	built := c.plIdx != nil
	c.mu.Unlock()
	if built {
		return nil
	}
	pls, err := c.Playlists(ctx)
	if err != nil {
		return err
	}
	idx, err := c.members(ctx, pls)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.plIdx = idx
	c.mu.Unlock()
	return nil
}
