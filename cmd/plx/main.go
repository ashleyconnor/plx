// Command plx is a terminal UI for managing a Plex library.
//
//	plx                         open the TUI
//	plx ls                      list sources
//	plx q <source> [query...]   print items matching a query
//	plx fields                  list query fields
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/ashleyconnor/plx/internal/field"
	"github.com/ashleyconnor/plx/internal/plex"
	"github.com/ashleyconnor/plx/internal/query"
	"github.com/ashleyconnor/plx/internal/source"
	"github.com/ashleyconnor/plx/internal/store"
	"github.com/ashleyconnor/plx/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "plx:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "fields" {
		printFields()
		return nil
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	cat := source.NewCatalog(plex.New(cfg.URL, cfg.Token, plex.Options{Insecure: cfg.Insecure}))
	ctx := context.Background()

	if len(args) == 0 {
		return tui.Run(cat, tui.Options{Icons: cfg.Icons})
	}
	switch args[0] {
	case "ls":
		return list(ctx, cat)
	case "q":
		if len(args) < 2 {
			return fmt.Errorf("usage: plx q <source> [query...]")
		}
		return printQuery(ctx, cat, args[1], strings.Join(args[2:], " "))
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func list(ctx context.Context, cat *source.Catalog) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	secs, err := cat.Sections(ctx)
	if err != nil {
		return err
	}
	for _, s := range secs {
		fmt.Fprintf(w, "section:%s\t%s\n", s.Title, s.Type)
	}
	pls, err := cat.Playlists(ctx)
	if err != nil {
		return err
	}
	for _, p := range pls {
		fmt.Fprintf(w, "playlist:%s\t%s\t%d items%s\n", p.Title, p.ListType, p.Count, smart(p.Smart))
	}
	cols, err := cat.AllCollections(ctx)
	if err != nil {
		return err
	}
	for _, c := range cols {
		fmt.Fprintf(w, "collection:%s\t%s\t%d items%s\n", c.Title, c.Subtype, c.Count, smart(c.Smart))
	}
	qs, err := store.LoadQueries()
	if err != nil {
		return err
	}
	for _, q := range qs {
		fmt.Fprintf(w, "query:%s\t%s\n", q.Name, q.Filter)
	}
	return nil
}

func smart(b bool) string {
	if b {
		return " (smart)"
	}
	return ""
}

func printQuery(ctx context.Context, cat *source.Catalog, ref, q string) error {
	src, err := store.ParseRef(ctx, cat, ref)
	if err != nil {
		return err
	}
	pq, err := query.Parse(q)
	if err != nil {
		return err
	}
	items, err := cat.Items(ctx, src)
	if err != nil {
		return err
	}
	if pq.UsesMembership() {
		if err := cat.EnsureMembership(ctx, items); err != nil {
			return err
		}
	}
	matched := pq.Filter(items)
	cols := []*field.Field{field.Size, field.Dur, field.Res, field.Plays, field.Label}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprint(w, "TITLE")
	for _, c := range cols {
		fmt.Fprintf(w, "\t%s", strings.ToUpper(c.Name))
	}
	fmt.Fprintln(w)
	var total int64
	for _, it := range matched {
		fmt.Fprint(w, it.DisplayTitle())
		for _, c := range cols {
			fmt.Fprintf(w, "\t%s", c.Format(it))
		}
		fmt.Fprintln(w)
		total += it.Size
	}
	w.Flush()
	fmt.Printf("\n%d of %d items · %s\n", len(matched), len(items), field.HumanBytes(total))
	return nil
}

func printFields() {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	for _, f := range field.All() {
		names := f.Name
		if len(f.Aliases) > 0 {
			names += " (" + strings.Join(f.Aliases, ", ") + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", names, f.Type, f.Doc)
	}
}
