// Package export defines the exporter abstraction (Requirements Spec §7): an
// exporter takes the loaded repository and returns the complete content of an
// output folder. Formats are registered by name, so no format is
// special-cased by the CLI.
package export

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
)

// Input is everything an exporter reads.
type Input struct {
	Repo   *repo.Repo
	Schema *schema.Schema
	// Entities are all entities in the repository, typed against Schema and
	// sorted by ID.
	Entities []*model.Entity
	// Graph is the entities prepared for queries, calculated fields
	// evaluated; nil makes the exporter build it.
	Graph *query.Graph
	// Scope limits the output; nil exports everything.
	Scope *Scope
	// Git is the repository's git context; nil outside a git repository,
	// when outputs carry no commit or content hash.
	Git *Git
}

// Git is what an export stamps into its output (Detailed Design §4).
type Git struct {
	// SHA is HEAD's full commit hash.
	SHA string
	// ContentHash is the DHF content hash of HEAD, suffixed -dirty when the
	// working tree differs from HEAD.
	ContentHash string
	Log         *history.Log
	Dirty       *history.Dirty
}

// Page is the git context of a page with the given inputs: the last commit
// that changed any of them (suffixed -dirty when one differs in the
// working tree; "uncommitted" when none is committed yet) and the commits
// that changed them, newest first.
func (g *Git) Page(in history.Inputs) (lastChanged string, revisions []*history.Commit) {
	if g == nil {
		return "", nil
	}
	revisions = g.Log.Revisions(in)
	if len(revisions) > 0 {
		lastChanged = revisions[0].SHA
	}
	if g.Dirty.Touches(in) {
		if lastChanged == "" {
			return "uncommitted", revisions
		}
		lastChanged += "-dirty"
	}
	return lastChanged, revisions
}

// Files is the content of an output folder: slash-separated relative path to
// file bytes.
type Files map[string][]byte

// Exporter renders one output format. It must not touch the filesystem
// outside what Input provides: writing the output folder is done separately
// (WriteOutput), only after rendering has fully succeeded.
type Exporter interface {
	Export(in *Input) (Files, error)
}

// Registry maps format names to exporters.
type Registry struct {
	exporters map[string]Exporter
	deferred  map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{exporters: map[string]Exporter{}, deferred: map[string]bool{}}
}

// Register makes format available.
func (r *Registry) Register(format string, e Exporter) { r.exporters[format] = e }

// Defer records a format that is recognised but not shipped in this release.
func (r *Registry) Defer(format string) { r.deferred[format] = true }

// Formats lists the available formats in name order.
func (r *Registry) Formats() []string {
	var names []string
	for name := range r.exporters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// UnknownFormatError reports a format that is neither available nor deferred.
type UnknownFormatError struct {
	Format    string
	Available []string
}

func (e *UnknownFormatError) Error() string {
	return fmt.Sprintf("unknown format %q (available: %s)", e.Format, strings.Join(e.Available, ", "))
}

// Lookup returns the exporter for format.
func (r *Registry) Lookup(format string) (Exporter, error) {
	if e, ok := r.exporters[format]; ok {
		return e, nil
	}
	if r.deferred[format] {
		return nil, fmt.Errorf("%s is not available in this release", format)
	}
	return nil, &UnknownFormatError{Format: format, Available: r.Formats()}
}
