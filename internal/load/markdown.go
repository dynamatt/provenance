package load

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/problem"
)

// markdown is the Markdown source (DES-0006): any .md file whose
// frontmatter has an id and a type is an entity. It walks the repository,
// skipping the configuration folders at its root and .git anywhere.
type markdown struct{}

// Entities is the entity a Markdown file holds, written as the whole file.
func (markdown) Entities(p string, data func() ([]byte, error)) ([]history.FileEntity, bool, error) {
	if !strings.EqualFold(path.Ext(p), ".md") {
		return nil, false, nil
	}
	b, err := data()
	if err != nil {
		return nil, true, err
	}
	if e, _ := entity.Parse(p, b); e != nil {
		return []history.FileEntity{{ID: e.ID, Type: e.Type, Text: string(b)}}, true, nil
	}
	return nil, true, nil
}

// Parse is the entity a Markdown file holds; none when it doesn't parse.
func (markdown) Parse(p string, data func() ([]byte, error)) ([]*entity.Entity, bool, error) {
	if !strings.EqualFold(path.Ext(p), ".md") {
		return nil, false, nil
	}
	b, err := data()
	if err != nil {
		return nil, true, err
	}
	if e, _ := entity.Parse(p, b); e != nil {
		return []*entity.Entity{e}, true, nil
	}
	return nil, true, nil
}

// Load reads every entity file. A file whose frontmatter does not parse,
// and one declaring only one of id and type, is a content/frontmatter
// problem. Of several files declaring one ID only the first found is an
// entity; every one of them is a content/duplicate-id problem, naming the
// others, where the walk first found the duplicate.
func (markdown) Load(fsys fs.FS, skip []string) (*Loaded, error) {
	skipped := map[string]bool{}
	for _, s := range skip {
		skipped[s] = true
	}
	for _, d := range entity.ConfigDirs {
		skipped[d] = true
	}

	out := &Loaded{}
	byID := map[string]*entity.Entity{}
	// found are the walk's findings in order: a frontmatter problem, or
	// the first duplicate of an ID, reported once every copy is known.
	type finding struct {
		problem *problem.Problem
		dup     *entity.DuplicateIDError
	}
	var found []finding
	var partial []problem.Problem
	copies := map[string][]*entity.DuplicateIDError{}
	err := fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && (d.Name() == ".git" || skipped[rel]) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(path.Ext(rel), ".md") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return err
		}
		e, half, err := entity.ParseFile(rel, raw)
		var fe *entity.Error
		switch {
		case errors.As(err, &fe):
			out.Files = append(out.Files, rel)
			found = append(found, finding{problem: &problem.Problem{Check: "content/frontmatter", Path: fe.Path, Line: fe.Line, Msg: fe.Msg, Err: fe}})
		case err != nil:
			return err
		case half != nil:
			out.Files = append(out.Files, rel)
			partial = append(partial, problem.Problem{Check: "content/frontmatter", Path: half.Path, Line: half.Line, Msg: half.Msg})
		case e == nil:
		case byID[e.ID] != nil:
			out.Files = append(out.Files, rel)
			prev := byID[e.ID]
			de := &entity.DuplicateIDError{ID: e.ID, First: prev.Path, Then: e.Path, FirstLine: prev.IDLine(), ThenLine: e.IDLine()}
			if copies[e.ID] == nil {
				found = append(found, finding{dup: de})
			}
			copies[e.ID] = append(copies[e.ID], de)
		default:
			out.Files = append(out.Files, rel)
			byID[e.ID] = e
			out.Entities = append(out.Entities, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, f := range found {
		if f.problem != nil {
			out.Problems = append(out.Problems, *f.problem)
		} else {
			out.Problems = append(out.Problems, duplicates(copies[f.dup.ID])...)
		}
	}
	out.Problems = append(out.Problems, partial...)
	return out, nil
}

// duplicates are the problems of one duplicated ID, from the walk's
// duplicate errors for it: every file declaring the ID, naming the others.
// Export stops at the first.
func duplicates(dups []*entity.DuplicateIDError) []problem.Problem {
	type place struct {
		path string
		line int
	}
	all := []place{{dups[0].First, dups[0].FirstLine}}
	for _, d := range dups {
		all = append(all, place{d.Then, d.ThenLine})
	}
	var out []problem.Problem
	for i, c := range all {
		var others []string
		for j, o := range all {
			switch {
			case j == i:
			case o.path == c.path: // two rows of one CSV file
				others = append(others, fmt.Sprintf("%s:%d", o.path, o.line))
			default:
				others = append(others, o.path)
			}
		}
		p := problem.Problem{Check: "content/duplicate-id", Path: c.path, Line: c.line, Entity: dups[0].ID,
			Msg: fmt.Sprintf("duplicate entity ID %s, also in %s", dups[0].ID, strings.Join(others, ", "))}
		if i == 0 {
			p.Err = dups[0]
		}
		out = append(out, p)
	}
	return out
}
