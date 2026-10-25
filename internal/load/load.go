// Package load reads a repository the one way every command does: its
// entities, the schema, the entities typed against it, and the graph that
// evaluates their calculated fields and runs queries. It collects every
// problem on the way, each under its built-in check; export stops at the
// first, validate reports them all.
package load

import (
	"fmt"
	"strings"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Problem is one problem found while loading.
type Problem struct {
	// Check is the built-in check it is reported under (DES-0048), such as
	// "content/frontmatter". The set is open: a new problem names its own
	// check, and validate's catalogue (validate.Checks) lists them all.
	Check string
	// Path is slash-separated and relative to the repository root; Line is
	// 0 when not known; Entity is the entity's ID, when there is one.
	Path   string
	Line   int
	Entity string
	// Msg is the problem as validate reports it.
	Msg string
	// Err is what export stops at, worded as export has always worded it,
	// or nil for a problem export carries on past, such as a file that is
	// not an entity or an ignored schema key.
	Err error
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
	// SchemaBroken is set when the schema has problems, so the entities
	// could not be typed.
	SchemaBroken bool
}

// Err is the first problem export stops at, or nil.
func (r *Result) Err() error {
	for _, p := range r.Problems {
		if p.Err != nil {
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
	res.entityProblems(sc)

	s, problems, err := schema.Read(rp.Root)
	if err != nil && len(problems) == 0 {
		return nil, err
	}
	res.Schema = s
	for _, p := range problems {
		res.add(Problem{Check: "schema/file", Path: p.Path, Line: p.Line, Msg: p.Msg, Err: p})
	}
	for _, p := range s.KeyProblems {
		res.add(Problem{Check: "schema/unknown-key", Path: p.Path, Line: p.Line, Msg: p.Msg})
	}
	if len(problems) > 0 {
		res.SchemaBroken = true
		return res, nil
	}

	var known []*entity.Entity
	for _, e := range sc.Entities {
		if s.Types[e.Type] != nil {
			known = append(known, e)
			continue
		}
		line := yamlnode.Lookup(e.Front, "type").Line
		msg := fmt.Sprintf("type %q is not declared in schema/", e.Type)
		if names := s.TypeNames(); len(names) > 0 {
			msg += " (declared: " + strings.Join(names, ", ") + ")"
		}
		res.add(Problem{Check: "schema/unknown-type", Path: e.Path, Line: line, Entity: e.ID, Msg: msg,
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

// entityProblems adds the files that could not be read as entities. Every
// file declaring a duplicated ID is reported, naming the others, where the
// walk first found the duplicate.
func (r *Result) entityProblems(sc *entity.Scanned) {
	type place struct {
		path string
		line int
	}
	copies := map[string][]place{}
	for _, p := range sc.Problems {
		if de, ok := p.(*entity.DuplicateIDError); ok {
			if copies[de.ID] == nil {
				copies[de.ID] = []place{{de.First, de.FirstLine}}
			}
			copies[de.ID] = append(copies[de.ID], place{de.Then, de.ThenLine})
		}
	}
	for _, p := range sc.Problems {
		switch p := p.(type) {
		case *entity.Error:
			r.add(Problem{Check: "content/frontmatter", Path: p.Path, Line: p.Line, Msg: p.Msg, Err: p})
		case *entity.DuplicateIDError:
			all := copies[p.ID]
			if all == nil {
				continue // already reported
			}
			delete(copies, p.ID)
			for i, c := range all {
				var others []string
				for j, o := range all {
					if j != i {
						others = append(others, o.path)
					}
				}
				prob := Problem{Check: "content/duplicate-id", Path: c.path, Line: c.line, Entity: p.ID,
					Msg: fmt.Sprintf("duplicate entity ID %s, also in %s", p.ID, strings.Join(others, ", "))}
				if i == 0 {
					prob.Err = p
				}
				r.add(prob)
			}
		}
	}
	for _, p := range sc.Partial {
		r.add(Problem{Check: "content/frontmatter", Path: p.Path, Line: p.Line, Msg: p.Msg})
	}
}

func (r *Result) add(p Problem) { r.Problems = append(r.Problems, p) }
