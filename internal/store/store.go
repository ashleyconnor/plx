// Package store holds plx's local state: config, saved queries and the
// action log, under $XDG_CONFIG_HOME/plx (default ~/.config/plx).
package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ashleyconnor/plx/internal/model"
	"github.com/ashleyconnor/plx/internal/query"
	"github.com/ashleyconnor/plx/internal/source"
)

func Dir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "plx")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "plx")
}

type Config struct {
	URL      string `toml:"url"`
	Token    string `toml:"token"`
	Insecure bool   `toml:"insecure,omitempty"`
	Icons    string `toml:"icons,omitempty"`     // emoji (default), ascii or none
	ClientID string `toml:"client_id,omitempty"` // plx's device ID, set by plx login
}

func configPath() string { return filepath.Join(Dir(), "config.toml") }

// ErrNotConfigured means there's no server URL or token yet.
var ErrNotConfigured = errors.New("not signed in")

// ReadConfigFile reads config.toml alone, without environment overrides.
func ReadConfigFile() (Config, error) {
	var c Config
	if _, err := toml.DecodeFile(configPath(), &c); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return c, fmt.Errorf("config.toml: %w", err)
	}
	return c, nil
}

// SaveConfig writes config.toml, readable only by the user since it holds
// the token. Comments in a hand-written file are not kept.
func SaveConfig(c Config) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(configPath()+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(c); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(configPath()+".tmp", configPath())
}

// LoadConfig reads config.toml; PLEX_URL, PLEX_TOKEN and PLEX_INSECURE override it.
func LoadConfig() (Config, error) {
	c, err := ReadConfigFile()
	if err != nil {
		return c, err
	}
	if v := os.Getenv("PLEX_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("PLEX_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("PLEX_INSECURE"); v != "" {
		c.Insecure = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("PLX_ICONS"); v != "" {
		c.Icons = v
	}
	if c.URL == "" || c.Token == "" {
		return c, fmt.Errorf("%w: run plx login, or set url and token in %s or PLEX_URL and PLEX_TOKEN",
			ErrNotConfigured, configPath())
	}
	return c, nil
}

// SourceSpec names a source in queries.toml. Exactly one of Section,
// Playlist or Collection is set; Label also needs Section. Playlist and
// Collection accept a ratingKey or a title.
type SourceSpec struct {
	Section    string `toml:"section,omitempty"`
	Playlist   any    `toml:"playlist,omitempty"`
	Collection any    `toml:"collection,omitempty"`
	Label      string `toml:"label,omitempty"`
}

type SavedQuery struct {
	Name    string       `toml:"name"`
	Sources []SourceSpec `toml:"sources"`
	Filter  string       `toml:"filter"`
	Sort    string       `toml:"sort,omitempty"`
	Columns []string     `toml:"columns,omitempty"`
}

type queriesFile struct {
	Query []SavedQuery `toml:"query"`
}

func queriesPath() string { return filepath.Join(Dir(), "queries.toml") }

func LoadQueries() ([]SavedQuery, error) {
	var f queriesFile
	if _, err := toml.DecodeFile(queriesPath(), &f); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("queries.toml: %w", err)
	}
	return f.Query, nil
}

// AppendQuery appends a query to queries.toml without rewriting the file, so
// hand-written comments and formatting survive.
func AppendQuery(q SavedQuery) error {
	existing, err := LoadQueries()
	if err != nil {
		return err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, q.Name) {
			return fmt.Errorf("a saved query named %q already exists", q.Name)
		}
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(queriesFile{Query: []SavedQuery{q}}); err != nil {
		return err
	}
	f, err := os.OpenFile(queriesPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n" + b.String())
	return err
}

// SpecFor describes a source so it can be saved. Saved queries are
// flattened: saving from a saved query saves its underlying sources.
func SpecFor(s source.Source) []SourceSpec {
	switch v := s.(type) {
	case source.Library:
		return []SourceSpec{{Section: v.Section.Title}}
	case source.Members:
		switch v.C.Kind {
		case model.Playlist:
			return []SourceSpec{{Playlist: v.C.Title}}
		case model.Collection:
			return []SourceSpec{{Collection: v.C.Title, Section: v.C.SectionID}}
		case model.Label:
			return []SourceSpec{{Label: v.C.ID, Section: v.C.SectionID}}
		}
	case source.Saved:
		var out []SourceSpec
		for _, s := range v.Sources {
			out = append(out, SpecFor(s)...)
		}
		return out
	}
	return nil
}

// Resolve turns a saved query into a source.
func Resolve(ctx context.Context, c *source.Catalog, q SavedQuery) (source.Saved, error) {
	text := q.Filter
	if q.Sort != "" {
		text += " sort:" + q.Sort
	}
	pq, err := query.Parse(text)
	if err != nil {
		return source.Saved{}, fmt.Errorf("query %q: %w", q.Name, err)
	}
	s := source.Saved{Name: q.Name, Query: pq}
	for _, spec := range q.Sources {
		src, err := resolveSpec(ctx, c, spec)
		if err != nil {
			return s, fmt.Errorf("query %q: %w", q.Name, err)
		}
		s.Sources = append(s.Sources, src)
	}
	if len(s.Sources) == 0 {
		return s, fmt.Errorf("query %q has no sources", q.Name)
	}
	return s, nil
}

func resolveSpec(ctx context.Context, c *source.Catalog, spec SourceSpec) (source.Source, error) {
	switch {
	case spec.Playlist != nil:
		pls, err := c.Playlists(ctx)
		if err != nil {
			return nil, err
		}
		if pl := findContainer(pls, fmt.Sprint(spec.Playlist)); pl != nil {
			return source.Members{C: pl}, nil
		}
		return nil, fmt.Errorf("no playlist %v", spec.Playlist)
	case spec.Collection != nil:
		var cs []*model.Container
		var err error
		if spec.Section != "" {
			sec, serr := c.Section(ctx, spec.Section)
			if serr != nil {
				return nil, serr
			}
			cs, err = c.Collections(ctx, sec.ID)
		} else {
			cs, err = c.AllCollections(ctx)
		}
		if err != nil {
			return nil, err
		}
		if col := findContainer(cs, fmt.Sprint(spec.Collection)); col != nil {
			return source.Members{C: col}, nil
		}
		return nil, fmt.Errorf("no collection %v", spec.Collection)
	case spec.Label != "":
		if spec.Section == "" {
			return nil, fmt.Errorf("label %q needs a section", spec.Label)
		}
		sec, err := c.Section(ctx, spec.Section)
		if err != nil {
			return nil, err
		}
		return source.Members{C: &model.Container{Kind: model.Label, ID: spec.Label, Title: spec.Label, SectionID: sec.ID}}, nil
	case spec.Section != "":
		sec, err := c.Section(ctx, spec.Section)
		if err != nil {
			return nil, err
		}
		return source.Library{Section: sec}, nil
	}
	return nil, fmt.Errorf("empty source")
}

// findContainer matches by ID first, then case-insensitive title.
func findContainer(cs []*model.Container, ref string) *model.Container {
	for _, c := range cs {
		if c.ID == ref {
			return c
		}
	}
	for _, c := range cs {
		if strings.EqualFold(c.Title, ref) {
			return c
		}
	}
	return nil
}

// ParseRef resolves a command-line source reference: section:<name>,
// playlist:<name|id>, collection:<name|id>, label:<section>/<label> or
// query:<saved query name>.
func ParseRef(ctx context.Context, c *source.Catalog, ref string) (source.Source, error) {
	kind, name, ok := strings.Cut(ref, ":")
	if !ok {
		return nil, fmt.Errorf("source %q: want kind:name (section, playlist, collection, label, query)", ref)
	}
	switch kind {
	case "section":
		return resolveSpec(ctx, c, SourceSpec{Section: name})
	case "playlist":
		return resolveSpec(ctx, c, SourceSpec{Playlist: name})
	case "collection":
		return resolveSpec(ctx, c, SourceSpec{Collection: name})
	case "label":
		sec, label, ok := strings.Cut(name, "/")
		if !ok {
			return nil, fmt.Errorf("label source: want label:<section>/<label>")
		}
		return resolveSpec(ctx, c, SourceSpec{Section: sec, Label: label})
	case "query":
		qs, err := LoadQueries()
		if err != nil {
			return nil, err
		}
		for _, q := range qs {
			if strings.EqualFold(q.Name, name) {
				return Resolve(ctx, c, q)
			}
		}
		return nil, fmt.Errorf("no saved query %q", name)
	}
	return nil, fmt.Errorf("unknown source kind %q", kind)
}
