package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/canon"
	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/repo"
)

// runFmt writes entity files in canonical form (DES-0038): the files
// changed since HEAD under the paths given, or every one with --all. It
// writes nothing unless every file it would write passes canon's checks;
// with --check it writes nothing and lists the files not in canonical
// form. A file it can't read is skipped and reported, exit 1.
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
	res, err := load.Load(rp)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if res.SchemaBroken {
		return fmt.Errorf("%s: the schema has errors, so no file can be put in schema order; run provenance validate", name)
	}
	files, err := res.Content().Files()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	changed := map[string]bool{}
	if !all {
		h, err := res.History()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if h == nil {
			return exitcode.Usage(fmt.Errorf("%s: not a git repository, so no file has changed since HEAD; use --all", name))
		}
		for _, p := range h.Dirty.Paths {
			changed[p] = true
		}
	}

	type rewrite struct {
		path string
		out  []byte
	}
	var rewrites []rewrite
	var skipped []string
	for _, p := range files {
		if !strings.EqualFold(path.Ext(p), ".md") || !all && !changed[p] || !within(p, under) {
			continue
		}
		local := filepath.Join(rp.Root, filepath.FromSlash(p))
		raw, err := os.ReadFile(local)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		out, err := canon.File(p, raw, res.Schema)
		var unreadable *canon.UnreadableError
		switch {
		case errors.As(err, &unreadable):
			skipped = append(skipped, unreadable.Error())
		case err != nil:
			return fmt.Errorf("%s: %w; nothing was written", name, err)
		case !bytes.Equal(out, raw):
			rewrites = append(rewrites, rewrite{p, out})
		}
	}
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
	for _, s := range skipped {
		fmt.Fprintf(c.ErrOrStderr(), "%s: %s; skipped\n", name, s)
	}
	switch {
	case check && len(rewrites) > 0:
		return exitcode.Failed(fmt.Errorf("%s: %s not in canonical form", name, plural(len(rewrites), "file")))
	case len(skipped) > 0:
		return exitcode.Failed(fmt.Errorf("%s: %s could not be read", name, plural(len(skipped), "file")))
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

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
