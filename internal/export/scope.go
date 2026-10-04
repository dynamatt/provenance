package export

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
)

// Scope limits an export to part of the repository (Detailed Design §2,
// scope resolution).
type Scope struct {
	// Path is the scope file, slash-separated and relative to the
	// repository root.
	Path string
	// Root is the entity an entity scope was given; the export renders it
	// as the site's main page. Nil for a query-file scope.
	Root *model.Entity
	// IDs are the entities in scope, sorted.
	IDs []string
	in  map[string]bool
}

// RootEntity is the entity an entity scope was given, or nil (no scope, or a
// query-file scope).
func (s *Scope) RootEntity() *model.Entity {
	if s == nil {
		return nil
	}
	return s.Root
}

// Has reports whether the entity with id is in scope; everything is when
// there is no scope.
func (s *Scope) Has(id string) bool {
	if s == nil {
		return true
	}
	if s.in == nil {
		s.in = make(map[string]bool, len(s.IDs))
		for _, id := range s.IDs {
			s.in[id] = true
		}
	}
	return s.in[id]
}

// ScopeError is a scope that cannot be resolved, at a file and line when
// the problem is inside a query file.
type ScopeError struct {
	Path string
	Line int
	Msg  string
}

func (e *ScopeError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("--scope %s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return fmt.Sprintf("--scope %s: %s", e.Path, e.Msg)
}

// ResolveScope resolves path (as given on the command line, relative to the
// working directory cwd) in the repository at root:
//
//   - an entity file scopes to the entity and everything its content pulls
//     in (query.Graph.Dependencies), and is rendered as the main page;
//   - any other file must be a query file, a YAML from/where document, and
//     scopes to its result set.
func ResolveScope(g *query.Graph, root, cwd, path string) (*Scope, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, &ScopeError{Path: path, Msg: "is outside the repository"}
	}
	rel = filepath.ToSlash(rel)
	sc := &Scope{Path: rel}
	for _, e := range g.Entities {
		if e.Path == rel {
			deps, err := g.Dependencies(e)
			if err != nil {
				return nil, err
			}
			sc.Root, sc.IDs = e, deps.IDs
			return sc, nil
		}
	}

	src, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &ScopeError{Path: rel, Msg: "no such file"}
	}
	if err != nil {
		return nil, err
	}
	b, err := query.ParseScope(string(src), g.Schema)
	if err != nil {
		var qe *query.Error
		if errors.As(err, &qe) {
			return nil, &ScopeError{Path: rel, Line: qe.Line, Msg: qe.Msg + " (a scope is an entity file, or a query file with from: and where:)"}
		}
		return nil, err
	}
	results, err := g.Run(b)
	if err != nil {
		return nil, err
	}
	for _, e := range results {
		sc.IDs = append(sc.IDs, e.ID)
	}
	slices.Sort(sc.IDs)
	return sc, nil
}
