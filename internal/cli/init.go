package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/version"
	"github.com/dynamatt/provenance/starters"
)

// runInit writes a starter project into a folder (DES-0052): nothing if
// any of its files is already there, or the folder is inside a Provenance
// repository. It runs git init when the folder isn't in a git repository,
// and never commits: the first commit is the team's.
func runInit(c *cobra.Command, args []string) error {
	name := commandName(c)
	// refuse is nothing written, exit 2: not a usage error, so no help.
	refuse := func(format string, a ...any) error {
		return fmt.Errorf("%s: "+format, append([]any{name}, a...)...)
	}
	shown := "."
	if len(args) == 1 {
		shown = args[0]
	}
	target, err := filepath.Abs(shown)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if root, err := repo.Find(target); err == nil {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, root); err == nil {
				root = filepath.ToSlash(rel)
			}
		}
		return refuse("%s is inside the Provenance repository whose root is %s (it holds a %s file, which is hidden: ls -a shows it); run init outside it", shown, root, repo.ComponentFile)
	} else if !errors.Is(err, repo.ErrNotFound) {
		return fmt.Errorf("%s: %w", name, err)
	}

	template, _ := c.Flags().GetString("template")
	if template == "" {
		template = starters.Default
	}
	code, _ := c.Flags().GetString("code")
	if code == "" {
		return refuse("--code is required: the component code, 1 to 8 upper-case letters and digits, such as --code NEURO")
	}
	if !validCode.MatchString(code) {
		return refuse("component code %q is not 1 to 8 upper-case letters and digits; give one with --code", code)
	}
	title, _ := c.Flags().GetString("name")
	if title == "" {
		title = filepath.Base(target)
	}
	files, err := starters.Files(template, starters.Values{Code: code, Name: title, Version: version.Version})
	if err != nil {
		return refuse("%v", err)
	}

	var inTheWay []string
	for _, f := range files {
		if _, err := os.Lstat(filepath.Join(target, filepath.FromSlash(f.Path))); err == nil {
			inTheWay = append(inTheWay, f.Path)
		}
	}
	if len(inTheWay) > 0 {
		return refuse("nothing was written: %s already has %s", shown, strings.Join(inTheWay, ", "))
	}
	for _, f := range files {
		p := filepath.Join(target, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	gitInit := !history.InRepository(target)
	if gitInit {
		opts := &git.PlainInitOptions{InitOptions: git.InitOptions{DefaultBranch: plumbing.Main}}
		if _, err := git.PlainInitWithOptions(target, opts); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	out := c.OutOrStdout()
	fmt.Fprintf(out, "Wrote the %s starter for %s (%s) to %s: %d files.\n", template, title, code, shown, len(files))
	if gitInit {
		fmt.Fprintln(out, "Created a git repository there, on branch main, with no commits.")
	}
	fmt.Fprintln(out, "\nNext:")
	if shown != "." {
		fmt.Fprintf(out, "  cd %s\n", shown)
	}
	fmt.Fprintln(out, "  provenance validate")
	fmt.Fprintln(out, "  git add -A && git commit -m \"Start the design history file\"")
	if starters.Release(version.Version) {
		fmt.Fprintln(out, "  set PROVENANCE_SHA256 in .github/workflows/validate.yml from the release's SHA256SUMS")
	}
	return nil
}

// validCode is a component code init writes: short, so IDs prefixed
// with it stay readable across repositories.
var validCode = regexp.MustCompile(`^[A-Z0-9]{1,8}$`)
