package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/export/website"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/repo"
)

// exporters is the registry of export formats. PDF and Word are part of the
// design but deferred past v1 (DES-0029).
func exporters() *export.Registry {
	r := export.NewRegistry()
	r.Register("website", website.New())
	r.Defer("pdf")
	r.Defer("docx")
	return r
}

// runExport renders the whole output in memory first and only then replaces
// the output folder, so a failed export never leaves a half-written site.
// Every failure exits 2: export never returns 1 (DES-0018).
func runExport(c *cobra.Command, args []string) error {
	if err := exportFormat(c, args[0]); err != nil {
		return fmt.Errorf("%s: %w", commandName(c), err)
	}
	return nil
}

func exportFormat(c *cobra.Command, format string) error {
	exp, err := exporters().Lookup(format)
	if err != nil {
		var unknown *export.UnknownFormatError
		if errors.As(err, &unknown) {
			return exitcode.Usage(err)
		}
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	out, _ := c.Flags().GetString("out")
	outDir := out
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(cwd, outDir)
	}
	// The output folder may sit inside the repository (the default ./_site
	// usually does); never read it back as source.
	in, content, err := loadInput(cwd, outDir)
	if err != nil {
		return err
	}
	if path, _ := c.Flags().GetString("scope"); path != "" {
		if in.Scope, err = export.ResolveScope(in.Graph, in.Repo.Root, cwd, path); err != nil {
			return err
		}
	}
	if in.Git, err = gitContext(in.Repo.Root, content); err != nil {
		return err
	}
	files, err := exp.Export(in)
	if err != nil {
		return err
	}
	if err := export.WriteOutput(outDir, out, files); err != nil {
		return err
	}
	fmt.Fprintf(c.OutOrStdout(), "exported %s to %s\n", format, out)
	return nil
}

// loadInput reads the repository containing cwd, stopping at its first
// problem, and what history needs to know of its content. Folders under
// skip are not read.
func loadInput(cwd string, skip ...string) (*export.Input, history.Content, error) {
	r, err := repo.Open(cwd)
	if err != nil {
		return nil, nil, err
	}
	res, err := load.Load(r, skip...)
	if err != nil {
		return nil, nil, err
	}
	if err := res.Err(); err != nil {
		return nil, nil, err
	}
	return res.Input(), res.Content(), nil
}

// gitContext reads what an export stamps into its output (DES-0021,
// DES-0023). Outside a git repository there is nothing to stamp: export
// still works, without commit or content hash.
func gitContext(root string, content history.Content) (*export.Git, error) {
	h, err := history.Open(root)
	if errors.Is(err, history.ErrNotRepository) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer h.Close()
	g := &export.Git{SHA: h.HEAD()}
	if g.ContentHash, g.Dirty, err = h.WorkingContentHash(content); err != nil {
		return nil, err
	}
	if g.Log, err = h.Log(content); err != nil {
		return nil, err
	}
	return g, nil
}

// runVerifyContent prints the DHF content hash of HEAD (suffixed -dirty when
// the working tree differs) or of --commit, and with --expected compares it:
// exit 1 on a mismatch.
func runVerifyContent(c *cobra.Command, _ []string) error {
	name := commandName(c)
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	r, err := repo.Open(cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	h, err := history.Open(r.Root)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer h.Close()

	commit, _ := c.Flags().GetString("commit")
	var hash string
	if commit != "" {
		sha, err := h.Resolve(commit)
		if err != nil {
			return exitcode.Usage(fmt.Errorf("%s: %w", name, err))
		}
		// A historical commit has no working tree to differ from.
		if hash, err = h.ContentHash(sha); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	} else {
		content, err := load.ContentOf(r)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if hash, _, err = h.WorkingContentHash(content); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	fmt.Fprintln(c.OutOrStdout(), hash)
	if expected, _ := c.Flags().GetString("expected"); expected != "" && expected != hash {
		return exitcode.Failed(fmt.Errorf("%s: content hash %s does not match the expected %s", name, hash, expected))
	}
	return nil
}
