package cli

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/canon"
	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/repo"
)

// runFmt writes entity files in canonical form (DES-0038): the files
// changed since HEAD under the paths given, or every one with --all. It
// reads the repository without typing it (load.Read): formatting needs
// the schema and the files, not the graph. It writes nothing unless every
// file it would write passes canon's checks; with --check it writes
// nothing and lists the files not in canonical form. A file it can't read
// is skipped and reported, exit 1.
func runFmt(c *cobra.Command, args []string) error {
	name := commandName(c)
	all, _ := c.Flags().GetBool("all")
	check, _ := c.Flags().GetBool("check")
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	rp, err := repo.Open(cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	under, err := repoPaths(rp.Root, cwd, args)
	if err != nil {
		return exitcode.Usage(fmt.Errorf("%s: %w", name, err))
	}
	res, err := load.Read(rp)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if res.SchemaBroken {
		return fmt.Errorf("%s: the schema has errors, so no file can be put in schema order; run provenance validate", name)
	}
	selected := func(p string) bool { return within(p, under) }
	if !all {
		h, err := res.History()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		switch {
		case h == nil && !history.InRepository(rp.Root):
			return exitcode.Usage(fmt.Errorf("%s: not a git repository, so no file has changed since HEAD; use --all", name))
		case h != nil:
			// In a repository with no commits yet, every file is new.
			changed := map[string]bool{}
			for _, p := range h.Dirty.Paths {
				changed[p] = true
			}
			selected = func(p string) bool { return changed[p] && within(p, under) }
		}
	}

	// A file load could not read as one entity of a declared type is
	// skipped, with the problem validate reports for it.
	skipped := map[string]string{}
	for _, p := range res.Problems {
		switch p.Check {
		case "content/frontmatter", "content/duplicate-id", "schema/unknown-type":
			if _, seen := skipped[p.Path]; !seen && selected(p.Path) {
				skipped[p.Path] = (&entity.Error{Path: p.Path, Line: p.Line, Msg: p.Msg}).Error()
			}
		}
	}
	type rewrite struct {
		path string
		out  []byte
	}
	var rewrites []rewrite
	for _, e := range res.Parsed {
		if _, skip := skipped[e.Path]; skip || !selected(e.Path) {
			continue
		}
		out, line, ok, err := canon.Format(e, res.Schema)
		switch {
		case err != nil:
			return fmt.Errorf("%s: %w; nothing was written", name, err)
		case ok && line > 0:
			rewrites = append(rewrites, rewrite{e.Path, out})
		}
	}
	slices.SortFunc(rewrites, func(a, b rewrite) int { return strings.Compare(a.path, b.path) })
	for _, rw := range rewrites {
		if !check {
			local := filepath.Join(rp.Root, filepath.FromSlash(rw.path))
			info, err := os.Stat(local)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if err := os.WriteFile(local, rw.out, info.Mode().Perm()); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		fmt.Fprintln(c.OutOrStdout(), rw.path)
	}
	for _, p := range slices.Sorted(maps.Keys(skipped)) {
		fmt.Fprintf(c.ErrOrStderr(), "%s: %s; skipped\n", name, skipped[p])
	}
	switch {
	case check && len(rewrites) > 0:
		return exitcode.Failed(fmt.Errorf("%s: the files listed are not in canonical form", name))
	case len(skipped) > 0:
		return exitcode.Failed(fmt.Errorf("%s: files that could not be read were skipped", name))
	}
	return nil
}

// repoPaths are args, paths relative to cwd, as slash-separated paths
// relative to the repository root; nil for none, the whole repository.
func repoPaths(root, cwd string, args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		abs := a
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, a)
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%s is outside the repository", a)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}

// within reports whether p is one of under or in a folder of under; every
// path is when under is empty.
func within(p string, under []string) bool {
	if len(under) == 0 {
		return true
	}
	for _, u := range under {
		if u == "." || p == u || strings.HasPrefix(p, u+"/") {
			return true
		}
	}
	return false
}
