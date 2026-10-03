package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/export/website"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
)

// exporters is the registry of export formats. PDF and Word are part of the
// design but deferred past v1 (Requirements Spec §7).
func exporters() *export.Registry {
	r := export.NewRegistry()
	r.Register("website", website.New())
	r.Defer("pdf")
	r.Defer("docx")
	return r
}

// runExport renders the whole output in memory first and only then replaces
// the output folder, so a failed export never leaves a half-written site.
// Every failure exits 2: export never returns 1 (Detailed Design §2).
func runExport(c *cobra.Command, args []string) error {
	name := commandName(c)

	exp, err := exporters().Lookup(args[0])
	if err != nil {
		var unknown *export.UnknownFormatError
		if errors.As(err, &unknown) {
			return exitcode.Usage(fmt.Errorf("%s: %w", name, err))
		}
		return fmt.Errorf("%s: %w", name, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	r, err := repo.Open(cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	out, _ := c.Flags().GetString("out")
	outDir := out
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(cwd, outDir)
	}

	// The output folder may sit inside the repository (the default ./_site
	// usually does); never read it back as source.
	parsed, err := entity.Discover(r.Root, outDir)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	s, err := schema.Load(r.Root)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	entities, err := model.Build(s, parsed)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	// The graph evaluates calculated fields; scope resolution runs queries.
	graph, err := query.NewGraph(s, entities)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var scope *export.Scope
	if path, _ := c.Flags().GetString("scope"); path != "" {
		if scope, err = export.ResolveScope(graph, r.Root, cwd, path); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	files, err := exp.Export(&export.Input{Repo: r, Schema: s, Entities: entities, Graph: graph, Scope: scope})
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := export.WriteOutput(outDir, out, files); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Fprintf(c.OutOrStdout(), "exported %s to %s\n", args[0], out)
	return nil
}
