// Package load reads a repository the one way every command does: its
// entities, the schema, the entities typed against it, and the graph that
// evaluates their calculated fields and runs queries. It collects every
// problem on the way; export stops at the first, validate reports them all
// as its built-in checks.
package load

import (
	"errors"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Kind is what a problem is. validate reports each kind as a built-in
// check; export stops at the first problem that is Fatal.
type Kind int

const (
	// Frontmatter is a file whose frontmatter cannot be read: an
	// *entity.Error.
	Frontmatter Kind = iota
	// Partial is frontmatter declaring only one of id and type, so the
	// file is not an entity: an *entity.Error. Export leaves it out.
	Partial
	// DuplicateID is a second file declaring an ID: an
	// *entity.DuplicateIDError.
	DuplicateID
	// SchemaFile is a schema declaration that cannot be loaded: a
	// *schema.Error. The entities are then not typed.
	SchemaFile
	// SchemaKey is a key the schema language doesn't have, which is
	// ignored: a *schema.Error. Export carries on.
	SchemaKey
	// UnknownType is an entity of a type the schema doesn't declare: a
	// *model.UnknownTypeError. The entity is left out of the model.
	UnknownType
)

// Fatal reports whether export stops at a problem of kind k.
func (k Kind) Fatal() bool { return k != Partial && k != SchemaKey }

// Problem is one problem found while loading. Err is the error export
// reports, worded as it always has been.
type Problem struct {
	Kind Kind
	// Path is slash-separated and relative to the repository root; Line is
	// 0 when not known; Entity is the entity's ID, when there is one.
	Path   string
	Line   int
	Entity string
	Err    error
}

// Result is a loaded repository.
type Result struct {
	Repo *repo.Repo
	// Schema is what loaded, even with problems.
	Schema *schema.Schema
	// Parsed are every entity read, sorted by ID, whether or not the
	// schema declares its type.
	Parsed []*entity.Entity
	// Entities are the entities of declared types, typed, and Graph
	// evaluates them; both are nil when the schema has problems.
	Entities []*model.Entity
	Graph    *query.Graph
	// Problems are in the order export meets them: the entity files as
	// the walk finds them, then the schema, then unknown types by ID.
	Problems []Problem
}

// SchemaBroken reports whether the schema has problems, so the entities
// could not be typed.
func (r *Result) SchemaBroken() bool {
	for _, p := range r.Problems {
		if p.Kind == SchemaFile {
			return true
		}
	}
	return false
}

// Err is the first problem export stops at, or nil.
func (r *Result) Err() error {
	for _, p := range r.Problems {
		if p.Kind.Fatal() {
			return p.Err
		}
	}
	return nil
}

// Input is what an exporter receives.
func (r *Result) Input() *export.Input {
	return &export.Input{Repo: r.Repo, Schema: r.Schema, Entities: r.Entities, Graph: r.Graph}
}

// Load reads rp. Folders under skip (absolute paths, e.g. the export output
// folder) are not read. err is a failure to read the repository or to
// evaluate the graph, not a problem with its content.
func Load(rp *repo.Repo, skip ...string) (*Result, error) {
	res := &Result{Repo: rp}
	sc, err := entity.Scan(rp.Root, skip...)
	if err != nil {
		return nil, err
	}
	res.Parsed = sc.Entities
	for _, p := range sc.Problems {
		var fe *entity.Error
		var de *entity.DuplicateIDError
		switch {
		case errors.As(p, &fe):
			res.add(Problem{Kind: Frontmatter, Path: fe.Path, Line: fe.Line, Err: p})
		case errors.As(p, &de):
			res.add(Problem{Kind: DuplicateID, Path: de.Then, Line: de.ThenLine, Entity: de.ID, Err: p})
		default:
			return nil, p
		}
	}
	for _, p := range sc.Partial {
		res.add(Problem{Kind: Partial, Path: p.Path, Line: p.Line, Err: p})
	}

	s, problems, err := schema.Read(rp.Root)
	for _, p := range problems {
		res.add(Problem{Kind: SchemaFile, Path: p.Path, Line: p.Line, Err: p})
	}
	if err != nil && len(problems) == 0 {
		return nil, err
	}
	res.Schema = s
	for _, p := range s.KeyProblems {
		res.add(Problem{Kind: SchemaKey, Path: p.Path, Line: p.Line, Err: p})
	}
	if len(problems) > 0 {
		return res, nil
	}

	var known []*entity.Entity
	for _, e := range sc.Entities {
		if s.Types[e.Type] != nil {
			known = append(known, e)
			continue
		}
		line := yamlnode.Lookup(e.Front, "type").Line
		res.add(Problem{Kind: UnknownType, Path: e.Path, Line: line, Entity: e.ID,
			Err: &model.UnknownTypeError{Path: e.Path, Type: e.Type, Line: line}})
	}
	if res.Entities, err = model.Build(s, known); err != nil {
		return nil, err
	}
	if res.Graph, err = query.NewGraph(s, res.Entities); err != nil {
		return nil, err
	}
	return res, nil
}

func (r *Result) add(p Problem) { r.Problems = append(r.Problems, p) }
