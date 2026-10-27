package plex

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// SearchType maps Plex item types to the numeric type used by listing and edit endpoints.
var SearchType = map[string]int{
	"movie": 1, "show": 2, "season": 3, "episode": 4,
	"artist": 8, "album": 9, "track": 10, "clip": 12, "photo": 13, "collection": 18,
}

// LeafType is the playable item type listed for each section type.
var LeafType = map[string]string{
	"movie": "movie", "show": "episode", "artist": "track", "photo": "photo",
}

// uriChunk caps how many ratingKeys go in one server:// URI to keep URLs a sane length.
const uriChunk = 100

func (c *Client) Identity(ctx context.Context) (string, error) {
	mc, err := c.get(ctx, "/identity", nil)
	if err != nil {
		return "", err
	}
	return mc.MachineIdentifier, nil
}

// AllowsMediaDeletion reports the server's "Allow media deletion" setting,
// without which DeleteItem is refused.
func (c *Client) AllowsMediaDeletion(ctx context.Context) (bool, error) {
	mc, err := c.get(ctx, "/:/prefs", nil)
	if err != nil {
		return false, err
	}
	for _, s := range mc.Setting {
		if s.ID == "allowMediaDeletion" {
			v, _ := s.Value.(bool)
			return v, nil
		}
	}
	return false, nil
}

// DeleteMedia deletes one version (Media entry) of an item and its files.
func (c *Client) DeleteMedia(ctx context.Context, key, mediaID string) error {
	return c.doIdem(ctx, http.MethodDelete, "/library/metadata/"+key+"/media/"+mediaID, nil, nil)
}

// DeleteItem deletes an item and all of its media files from disk.
func (c *Client) DeleteItem(ctx context.Context, key string) error {
	return c.doIdem(ctx, http.MethodDelete, "/library/metadata/"+key, nil, nil)
}

// Metadata fetches one item with every detail (all tags, streams, summary).
func (c *Client) Metadata(ctx context.Context, key string) (*Metadata, error) {
	mc, err := c.get(ctx, "/library/metadata/"+key, nil)
	if err != nil {
		return nil, err
	}
	if len(mc.Metadata) == 0 {
		return nil, fmt.Errorf("plex: item %s not found", key)
	}
	return &mc.Metadata[0], nil
}

// Image fetches an artwork path (e.g. a thumb) scaled by the server to fit
// within w×h pixels.
func (c *Client) Image(ctx context.Context, path string, w, h int) ([]byte, error) {
	p := Params{}.Add("width", fmt.Sprint(w)).Add("height", fmt.Sprint(h)).
		Add("minSize", "1").Add("upscale", "1").Add("url", path)
	var body []byte
	err := c.doIdem(ctx, http.MethodGet, "/photo/:/transcode", p, &body)
	return body, err
}

func (c *Client) Sections(ctx context.Context) ([]Directory, error) {
	mc, err := c.get(ctx, "/library/sections", nil)
	if err != nil {
		return nil, err
	}
	return mc.Directory, nil
}

// SectionItems lists every item of the given type in a section. Plex omits
// librarySectionID on each item here, so it is copied from the container.
func (c *Client) SectionItems(ctx context.Context, sectionID string, typ int) ([]Metadata, error) {
	mc, err := c.paged(ctx, "/library/sections/"+sectionID+"/all", Params{}.Add("type", fmt.Sprint(typ)))
	if err != nil {
		return nil, err
	}
	for i := range mc.Metadata {
		if mc.Metadata[i].LibrarySectionID == "" {
			mc.Metadata[i].LibrarySectionID = FlexString(sectionID)
			mc.Metadata[i].LibrarySectionTitle = mc.LibrarySectionTitle
		}
	}
	return mc.Metadata, nil
}

// SectionTags lists the values of a tag (e.g. "label", "genre") used in a section.
func (c *Client) SectionTags(ctx context.Context, sectionID, tag string) ([]Directory, error) {
	mc, err := c.get(ctx, "/library/sections/"+sectionID+"/"+tag, nil)
	if err != nil {
		return nil, err
	}
	return mc.Directory, nil
}

// SectionKeysWithTag returns the ratingKeys of items of a type carrying a tag value (by tag ID).
func (c *Client) SectionKeysWithTag(ctx context.Context, sectionID string, typ int, tag, tagID string) ([]string, error) {
	mc, err := c.paged(ctx, "/library/sections/"+sectionID+"/all",
		Params{}.Add("type", fmt.Sprint(typ)).Add(tag, tagID))
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(mc.Metadata))
	for i, m := range mc.Metadata {
		keys[i] = string(m.RatingKey)
	}
	return keys, nil
}

func (c *Client) Playlists(ctx context.Context) ([]Metadata, error) {
	mc, err := c.get(ctx, "/playlists", nil)
	if err != nil {
		return nil, err
	}
	return mc.Metadata, nil
}

func (c *Client) PlaylistItems(ctx context.Context, id string) ([]Metadata, error) {
	mc, err := c.paged(ctx, "/playlists/"+id+"/items", nil)
	if err != nil {
		return nil, err
	}
	return mc.Metadata, nil
}

func (c *Client) Collections(ctx context.Context, sectionID string) ([]Metadata, error) {
	mc, err := c.get(ctx, "/library/sections/"+sectionID+"/collections", nil)
	if err != nil {
		return nil, err
	}
	for i := range mc.Metadata {
		if mc.Metadata[i].LibrarySectionID == "" {
			mc.Metadata[i].LibrarySectionID = FlexString(sectionID)
		}
	}
	return mc.Metadata, nil
}

func (c *Client) CollectionItems(ctx context.Context, id string) ([]Metadata, error) {
	mc, err := c.paged(ctx, "/library/collections/"+id+"/children", nil)
	if err != nil {
		return nil, err
	}
	return mc.Metadata, nil
}

func libraryURI(machineID string, keys []string) string {
	return fmt.Sprintf("server://%s/com.plexapp.plugins.library/library/metadata/%s",
		machineID, strings.Join(keys, ","))
}

func chunks(keys []string) [][]string {
	var out [][]string
	for len(keys) > uriChunk {
		out = append(out, keys[:uriChunk])
		keys = keys[uriChunk:]
	}
	if len(keys) > 0 {
		out = append(out, keys)
	}
	return out
}

// CreatePlaylist creates a regular playlist containing keys, in order.
// listType is "video", "audio" or "photo".
func (c *Client) CreatePlaylist(ctx context.Context, machineID, title, listType string, keys []string) (string, error) {
	cs := chunks(keys)
	if len(cs) == 0 {
		return "", fmt.Errorf("plex: cannot create an empty playlist")
	}
	var env envelope
	p := Params{}.Add("uri", libraryURI(machineID, cs[0])).Add("type", listType).Add("title", title).Add("smart", "0")
	if err := c.do(ctx, http.MethodPost, "/playlists", p, &env); err != nil {
		return "", err
	}
	if len(env.MediaContainer.Metadata) == 0 {
		return "", fmt.Errorf("plex: create playlist returned no playlist")
	}
	id := string(env.MediaContainer.Metadata[0].RatingKey)
	for _, ch := range cs[1:] {
		if err := c.AddToPlaylist(ctx, machineID, id, ch); err != nil {
			return id, err
		}
	}
	return id, nil
}

func (c *Client) AddToPlaylist(ctx context.Context, machineID, id string, keys []string) error {
	for _, ch := range chunks(keys) {
		if err := c.do(ctx, http.MethodPut, "/playlists/"+id+"/items",
			Params{}.Add("uri", libraryURI(machineID, ch)), nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) RemoveFromPlaylist(ctx context.Context, id, playlistItemID string) error {
	return c.doIdem(ctx, http.MethodDelete, "/playlists/"+id+"/items/"+playlistItemID, nil, nil)
}

// MovePlaylistItem moves an item after another; after == "" moves it to the top.
func (c *Client) MovePlaylistItem(ctx context.Context, id, playlistItemID, after string) error {
	var p Params
	if after != "" {
		p = p.Add("after", after)
	}
	return c.doIdem(ctx, http.MethodPut, "/playlists/"+id+"/items/"+playlistItemID+"/move", p, nil)
}

func (c *Client) DeletePlaylist(ctx context.Context, id string) error {
	return c.doIdem(ctx, http.MethodDelete, "/playlists/"+id, nil, nil)
}

func (c *Client) RenamePlaylist(ctx context.Context, id, title string) error {
	return c.doIdem(ctx, http.MethodPut, "/playlists/"+id, Params{}.Add("title", title), nil)
}

// DeleteCollection deletes a collection; its items are not touched.
func (c *Client) DeleteCollection(ctx context.Context, id string) error {
	return c.doIdem(ctx, http.MethodDelete, "/library/collections/"+id, nil, nil)
}

// RenameCollection sets and locks a collection's title. Collections are
// edited like any other item in their section.
func (c *Client) RenameCollection(ctx context.Context, sectionID, id, title string) error {
	p := Params{}.Add("type", fmt.Sprint(SearchType["collection"])).Add("id", id).
		Add("title.value", title).Add("title.locked", "1")
	return c.doIdem(ctx, http.MethodPut, "/library/sections/"+sectionID+"/all", p, nil)
}

// CreateCollection creates a regular collection in a section from keys of one item type.
func (c *Client) CreateCollection(ctx context.Context, machineID, sectionID, title string, typ int, keys []string) (string, error) {
	cs := chunks(keys)
	if len(cs) == 0 {
		return "", fmt.Errorf("plex: cannot create an empty collection")
	}
	var env envelope
	p := Params{}.Add("uri", libraryURI(machineID, cs[0])).Add("type", fmt.Sprint(typ)).
		Add("title", title).Add("smart", "0").Add("sectionId", sectionID)
	if err := c.do(ctx, http.MethodPost, "/library/collections", p, &env); err != nil {
		return "", err
	}
	if len(env.MediaContainer.Metadata) == 0 {
		return "", fmt.Errorf("plex: create collection returned no collection")
	}
	id := string(env.MediaContainer.Metadata[0].RatingKey)
	for _, ch := range cs[1:] {
		if err := c.AddToCollection(ctx, machineID, id, ch); err != nil {
			return id, err
		}
	}
	return id, nil
}

func (c *Client) AddToCollection(ctx context.Context, machineID, id string, keys []string) error {
	for _, ch := range chunks(keys) {
		if err := c.do(ctx, http.MethodPut, "/library/collections/"+id+"/items",
			Params{}.Add("uri", libraryURI(machineID, ch)), nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) RemoveFromCollection(ctx context.Context, id, key string) error {
	return c.doIdem(ctx, http.MethodDelete, "/library/collections/"+id+"/items/"+key, nil, nil)
}

// MoveCollectionItem moves an item after another; after == "" moves it to the top.
// Only meaningful when the collection's sort is custom.
func (c *Client) MoveCollectionItem(ctx context.Context, id, key, after string) error {
	var p Params
	if after != "" {
		p = p.Add("after", after)
	}
	return c.doIdem(ctx, http.MethodPut, "/library/collections/"+id+"/items/"+key+"/move", p, nil)
}

// Collection sort modes for SetCollectionSort.
const (
	CollectionSortRelease = 0
	CollectionSortAlpha   = 1
	CollectionSortCustom  = 2
)

func (c *Client) SetCollectionSort(ctx context.Context, id string, mode int) error {
	return c.doIdem(ctx, http.MethodPut, "/library/metadata/"+id+"/prefs",
		Params{}.Add("collectionSort", fmt.Sprint(mode)), nil)
}

// EditTags adds or removes tag values (e.g. labels) on many items of one type in
// one section with a single multi-edit request.
func (c *Client) EditTags(ctx context.Context, sectionID string, typ int, keys []string, tag string, values []string, remove bool) error {
	p := Params{}.Add("type", fmt.Sprint(typ)).Add("id", strings.Join(keys, ",")).Add(tag+".locked", "1")
	if remove {
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = Quote(v) // plexapi double-encodes removal values; match it
		}
		p = p.Add(tag+"[].tag.tag-", strings.Join(quoted, ","))
	} else {
		for i, v := range values {
			p = p.Add(fmt.Sprintf("%s[%d].tag.tag", tag, i), v)
		}
	}
	return c.doIdem(ctx, http.MethodPut, "/library/sections/"+sectionID+"/all", p, nil)
}
