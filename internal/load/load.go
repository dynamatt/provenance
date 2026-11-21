// Package load reads a repository the one way every command does: its
// entities, the schema, the entities typed against it, and the graph that
// evaluates their calculated fields and runs queries. It collects every
// problem on the way, each under its built-in check; export stops at the
// first, validate reports them all.
package load

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/problem"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
)

// Result is a loaded repository.
type Result struct {
	Repo *repo.Repo
	// Schema is what loaded, even with problems.
	Schema *schema.Schema
	// Parsed are every entity read, sorted by ID, whether or not the
	// schema declares its type.
	Parsed []*entity.Entity
	// Entities are the entities of declared types, typed, and Graph
	// evaluates them; both are nil when the schema has problems, and
	// after Read, which doesn't type them.
	Entities []*model.Entity
	Graph    *query.Graph
	// Problems are in the order export meets them: the entity files as
	// the walk finds them, then the schema, then unknown types by ID.
	Problems []problem.Problem
	// SchemaBroken is set when the schema has problems, so the entities
	// could not be typed.
	SchemaBroken bool
	// Tables are the CSV files entities were read from, in type name
	// order (DES-0053).
	Tables []*entity.Table
	// Commit is the commit At loaded, or "" for the working tree.
	Commit string

	content     *content
	history     *History
	historyErr  error
	historyRead bool
}

// Err is the first problem export stops at, or nil.
func (r *Result) Err() error { return problem.First(r.Problems) }

// Input is what an exporter receives.
func (r *Result) Input() *export.Input {
	return &export.Input{Repo: r.Repo, Schema: r.Schema, Entities: r.Entities, Graph: r.Graph, Tables: r.Tables}
}

// Load reads rp and types its entities: Read, then the typed model and
// its graph, which evaluates every calculated field. Folders under skip
// (absolute paths, e.g. the export output folder) are not read. err is a
// failure to read the repository or to evaluate the graph, not a problem
// with its content.
func Load(rp *repo.Repo, skip ...string) (*Result, error) {
	return load(rp, "", skip...)
}

// load is Load of commit's tree, read by At, or of the working tree when
// commit is "".
func load(rp *repo.Repo, commit string, skip ...string) (*Result, error) {
	res, err := Read(rp, skip...)
	if err != nil || res.SchemaBroken {
		return res, err
	}
	res.Commit = commit
	var known []*entity.Entity
	for _, e := range res.Parsed {
		if res.Schema.Types[e.Type] != nil {
			known = append(known, e)
		}
	}
	if res.Entities, err = model.Build(res.Schema, known); err != nil {
		return nil, err
	}
	if res.Graph, err = query.NewGraphWith(res.Schema, res.Entities, res.changes); err != nil {
		return nil, err
	}
	return res, nil
}

// Read reads rp's schema and every source's entities, with the problems
// found doing so, but neither types the entities nor builds their graph:
// for a command that needs only the files, such as fmt and verify
// content. Entities and Graph are nil.
func Read(rp *repo.Repo, skip ...string) (*Result, error) {
	res := &Result{Repo: rp}
	// The schema comes first, since a source may be declared in it; its
	// problems are reported after the entity files', as export meets them.
	fsys := rp.Files()
	s, problems, err := schema.ReadFS(fsys)
	if err != nil && len(problems) == 0 {
		return nil, err
	}
	res.Schema = s
	// A CSV file is read only against a schema that loaded: its rows are
	// typed by their fields.
	srcs := sources(s)
	if len(problems) > 0 {
		srcs = slices.DeleteFunc(srcs, func(src Source) bool { _, ok := src.(csvSource); return ok })
	}
	if err := res.read(fsys, srcs, relSkip(rp.Root, skip)); err != nil {
		return nil, err
	}
	for _, p := range problems {
		res.add(problem.Problem{Check: "schema/file", Path: p.Path, Line: p.Line, Msg: p.Msg, Err: p})
	}
	for _, p := range s.KeyProblems {
		res.add(problem.Problem{Check: "schema/unknown-key", Path: p.Path, Line: p.Line, Msg: p.Msg})
	}
	if len(problems) > 0 {
		res.SchemaBroken = true
		return res, nil
	}
	for _, e := range res.Parsed {
		if t := s.Types[e.Type]; t != nil {
			if t.Abstract {
				var subs []string
				for _, sub := range s.Concrete(t) {
					subs = append(subs, sub.Name)
				}
				msg := fmt.Sprintf("type %s is abstract: it has no entities of its own", t.Name)
				if len(subs) > 0 {
					msg += "; give the entity one of the types extending it (" + strings.Join(subs, ", ") + ")"
				}
				res.add(problem.Problem{Check: "schema/abstract-type", Path: e.Path, Line: e.Record.TypeLine(), Entity: e.ID, Msg: msg})
			}
			continue
		}
		line := e.Record.TypeLine()
		msg := fmt.Sprintf("type %q is not declared in schema/", e.Type)
		if names := s.TypeNames(); len(names) > 0 {
			msg += " (declared: " + strings.Join(names, ", ") + ")"
		}
		res.add(problem.Problem{Check: "schema/unknown-type", Path: e.Path, Line: line, Entity: e.ID, Msg: msg,
			Err: &model.UnknownTypeError{Path: e.Path, Type: e.Type, Line: line}})
	}
	return res, nil
}

// Content is what history needs to know of the repository's content: the
// files the sources read, and the entities a file holds.
func (r *Result) Content() history.Content { return r.content }

// ContentOf reads only what history needs of rp, for a command that needs
// no entities, such as verify content. Folders under skip are not read.
func ContentOf(rp *repo.Repo, skip ...string) (history.Content, error) {
	res, err := Read(rp, skip...)
	if err != nil {
		return nil, err
	}
	return res.content, nil
}

// read runs every source, in order, keeping the first entity of each ID.
// An ID two sources both hold is a content/duplicate-id problem, as two
// files of one source holding it are.
func (r *Result) read(fsys fs.FS, srcs []Source, skip []string) error {
	var files []string
	byID := map[string]*entity.Entity{}
	across := map[string][]*entity.DuplicateIDError{}
	var acrossIDs []string
	for _, src := range srcs {
		l, err := src.Load(fsys, skip)
		if err != nil {
			return err
		}
		r.Problems = append(r.Problems, l.Problems...)
		files = append(files, l.Files...)
		r.Tables = append(r.Tables, l.Tables...)
		for _, e := range l.Entities {
			if r.fromOtherSource(e) {
				continue
			}
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

// relSkip is skip's folders (absolute, or relative to the working
// directory) inside root, slash-separated and relative to root; a folder
// outside root is never read anyway.
func relSkip(root string, skip []string) []string {
	var rel []string
	for _, s := range skip {
		abs, err := filepath.Abs(s)
		if err != nil {
			continue
		}
		r, err := filepath.Rel(root, abs)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			continue
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	return rel
}

func (r *Result) add(p problem.Problem) { r.Problems = append(r.Problems, p) }

// fromOtherSource reports, as a content/frontmatter problem, an entity of
// a type the schema reads from a CSV file that is not one of its rows: a
// type's entities come from its source alone (DES-0053), so it is not
// read.
func (r *Result) fromOtherSource(e *entity.Entity) bool {
	t := r.Schema.Types[e.Type]
	if t == nil || t.Source == nil {
		return false
	}
	if _, row := e.Record.(*entity.Row); row {
		return false
	}
	msg := fmt.Sprintf("type %s is read from %s, one entity per row, so this file is not read as an entity", t.Name, t.Source.CSV)
	line := e.Record.TypeLine()
	r.add(problem.Problem{Check: "content/frontmatter", Path: e.Path, Line: line, Entity: e.ID, Msg: msg,
		Err: &entity.Error{Path: e.Path, Line: line, Msg: msg}})
	return true
}

// At loads the repository rp as it was at commit, a full hash h resolved,
// from git's objects rather than the working tree (DES-0055): the same
// loader over the commit's tree, with the commit's own .component, schema
// and templates. Its Repo reads the tree; Commit is set; it has no History,
// which describes HEAD and the working tree.
func At(rp *repo.Repo, h *history.Repo, commit string) (*Result, error) {
	tree, err := h.Tree(commit)
	if err != nil {
		return nil, err
	}
	at, err := repo.FromFS(rp.Root, tree)
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", history.Short(commit), err)
	}
	res, err := load(at, commit)
	if err != nil {
		return nil, err
	}
	res.Commit = commit
	res.historyRead = true
	return res, nil
}
