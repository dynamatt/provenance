package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/diff"
	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/version"
)

// runDiff compares the design history file at two refs (DES-0055) and
// writes each --output (DES-0057). A ref that doesn't resolve is exit 2,
// always; otherwise 0, or 1 with --exit-code when anything differs.
func runDiff(c *cobra.Command, args []string) error {
	name := commandName(c)
	outs, err := outputs(c)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	rp, err := repo.Open(cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	h, err := history.Open(rp.Root)
	switch {
	case errors.Is(err, history.ErrNotRepository):
		return fmt.Errorf("%s: %s is not in a git repository: diff compares git refs", name, rp.Root)
	case err != nil:
		return fmt.Errorf("%s: %w", name, err)
	}
	defer h.Close()

	oldRef, newRef, mergeBase, err := refs(args)
	if err != nil {
		return exitcode.Usage(fmt.Errorf("%s: %w", name, err))
	}
	resolve := func(ref string) (string, error) {
		if ref == "" {
			return "", nil
		}
		commit, err := h.Resolve(ref)
		if err != nil {
			return "", exitcode.Usage(fmt.Errorf("%s: %w", name, err))
		}
		return commit, nil
	}
	oldCommit, err := resolve(oldRef)
	if err != nil {
		return err
	}
	newCommit, err := resolve(newRef)
	if err != nil {
		return err
	}
	if mergeBase {
		if oldCommit, err = h.MergeBase(oldCommit, newCommit); err != nil {
			return exitcode.Usage(fmt.Errorf("%s: %w", name, err))
		}
		oldRef = "merge base of " + oldRef + " and " + newRef
	}
	old, err := side(rp, h, oldRef, oldCommit)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	new, err := side(rp, h, newRef, newCommit)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	var scope map[string]bool
	if p, _ := c.Flags().GetString("scope"); p != "" {
		if scope, err = diffScope(rp.Root, cwd, p, old, new); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	d, err := diff.Compare(old, new, scope)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	d.Version = version.Version
	bufs, err := renderDiff(d, outs)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for i, o := range outs {
		if o.file == "" {
			_, err = c.OutOrStdout().Write(bufs[i])
		} else {
			err = writeReport(o.file, bufs[i])
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if exit, _ := c.Flags().GetBool("exit-code"); exit && d.Changed() {
		return exitcode.Failed(fmt.Errorf("%s: %s", name, diff.Summary(d)))
	}
	return nil
}

// refs reads the arguments as git's diff does: none compares HEAD with
// the working tree, one ref with the working tree, two refs with each
// other; old..new names both, and old...new compares new with its merge
// base with old. "" is the working tree.
func refs(args []string) (old, new string, mergeBase bool, err error) {
	switch len(args) {
	case 0:
		return "HEAD", "", false, nil
	case 2:
		return args[0], args[1], false, nil
	}
	a := args[0]
	if before, after, ok := strings.Cut(a, "..."); ok {
		return or(before, "HEAD"), or(after, "HEAD"), true, nil
	}
	if before, after, ok := strings.Cut(a, ".."); ok {
		return or(before, "HEAD"), or(after, "HEAD"), false, nil
	}
	if a == "" {
		return "", "", false, errors.New("empty ref")
	}
	return a, "", false, nil
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// side loads a side: a commit from git's objects, or the working tree.
func side(rp *repo.Repo, h *history.Repo, ref, commit string) (*diff.Side, error) {
	if commit != "" {
		res, err := load.At(rp, h, commit)
		if err != nil {
			return nil, err
		}
		hash, err := h.ContentHash(commit)
		if err != nil {
			return nil, err
		}
		return diff.NewSide(ref, commit, hash, res), nil
	}
	res, err := load.Load(rp)
	if err != nil {
		return nil, err
	}
	hash := ""
	if hist, err := res.History(); err != nil {
		return nil, err
	} else if hist != nil {
		if hash, err = hist.ContentHash(); err != nil {
			return nil, err
		}
	}
	return diff.NewSide("", "", hash, res), nil
}

// diffScope resolves --scope at each side, as export does (DES-0033), and
// joins them: an entity is compared when either side's scope has it. A
// side without the file, or whose schema didn't load, adds nothing; the
// scope is an error only when no side resolves it.
func diffScope(root, cwd, p string, sides ...*diff.Side) (map[string]bool, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, &export.ScopeError{Path: p, Msg: "is outside the repository"}
	}
	rel = filepath.ToSlash(rel)
	ids := map[string]bool{}
	var first error
	resolved := false
	for _, s := range sides {
		g := s.Graph()
		if g == nil {
			continue
		}
		sc, err := export.ResolveScopeIn(g, s.Files(), rel)
		if err != nil {
			first = cmpErr(first, err)
			continue
		}
		resolved = true
		for _, id := range sc.IDs {
			ids[id] = true
		}
	}
	if !resolved {
		if first == nil {
			first = &export.ScopeError{Path: rel, Msg: "neither side's schema loaded"}
		}
		return nil, first
	}
	return ids, nil
}

func cmpErr(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

// renderDiff renders every output into memory, so nothing is written
// unless all of them render. The pages are compared first when an output
// shows them (DES-0056). An output that is neither built in nor a project
// template is a usage error; a template that fails is exit 2 too, at its
// file and line.
func renderDiff(d *diff.Diff, outs []output) ([][]byte, error) {
	for _, o := range outs {
		if diff.NeedsPages(o.name) {
			if err := d.Render(); err != nil {
				return nil, err
			}
			break
		}
	}
	bufs := make([][]byte, len(outs))
	for i, o := range outs {
		b, err := d.Output(o.name)
		var unknown *diff.UnknownOutputError
		if errors.As(err, &unknown) {
			return nil, exitcode.Usage(err)
		}
		if err != nil {
			return nil, err
		}
		bufs[i] = b
	}
	return bufs, nil
}
