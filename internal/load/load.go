// Package load reads a repository the one way every command does: its
// entities, the schema, the entities typed against it, and the graph that
// evaluates their calculated fields and runs queries. It collects every
// problem on the way, each under its built-in check; export stops at the
// first, validate reports them all.
package load

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
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

	content     *content
	history     *History
	historyErr  error
	historyRead bool
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
	// The schema comes first, since a source may be declared in it; its
	// problems are reported after the entity files', as export meets them.
	s, problems, err := schema.Read(rp.Root)
	if err != nil && len(problems) == 0 {
		return nil, err
	}
	res.Schema = s
	if err := res.read(sources(s), skip); err != nil {
		return nil, err
	}
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
	for _, e := range res.Parsed {
		if s.Types[e.Type] != nil {
			known = append(known, e)
			continue
		}
		line := e.Record.TypeLine()
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

// Content is what history needs to know of the repository's content: the
// files the sources read, and the entities a file holds.
func (r *Result) Content() history.Content { return r.content }

// ContentOf reads only what history needs of rp, for a command that needs
// no entities, such as verify content. Folders under skip are not read.
func ContentOf(rp *repo.Repo, skip ...string) (history.Content, error) {
	res := &Result{Repo: rp}
	s, problems, err := schema.Read(rp.Root)
	if err != nil && len(problems) == 0 {
		return nil, err
	}
	if err := res.read(sources(s), skip); err != nil {
		return nil, err
	}
	return res.content, nil
}

// read runs every source, in order, keeping the first entity of each ID.
// An ID two sources both hold is a content/duplicate-id problem, as two
// files of one source holding it are.
func (r *Result) read(srcs []Source, skip []string) error {
	var files []string
	byID := map[string]*entity.Entity{}
	across := map[string][]*entity.DuplicateIDError{}
	var acrossIDs []string
	for _, src := range srcs {
		l, err := src.Load(r.Repo.Root, skip)
		if err != nil {
			return err
		}
		r.Problems = append(r.Problems, l.Problems...)
		files = append(files, l.Files...)
		for _, e := range l.Entities {
			prev := byID[e.ID]
			if prev == nil {
				byID[e.ID] = e
				r.Parsed = append(r.Parsed, e)
				continue
			}
			if across[e.ID] == nil {
				acrossIDs = append(acrossIDs, e.ID)
			}
			across[e.ID] = append(across[e.ID], &entity.DuplicateIDError{ID: e.ID, First: prev.Path, Then: e.Path, FirstLine: prev.IDLine(), ThenLine: e.IDLine()})
		}
	}
	for _, id := range acrossIDs {
		r.Problems = append(r.Problems, duplicates(across[id])...)
	}
	slices.SortFunc(r.Parsed, func(a, b *entity.Entity) int { return strings.Compare(a.ID, b.ID) })
	r.content = newContent(files, srcs)
	return nil
}

func (r *Result) add(p Problem) { r.Problems = append(r.Problems, p) }
