package website

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/problem"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/tmpl"
)

// Problem is something rendering found: a problem that stops an export,
// or content the export would show as a gap (DES-0048).
type Problem = problem.Problem

// Check renders the site as Export does, but records every problem and
// goes on instead of stopping at the first, so validate can report them
// all. Nothing is written, so nothing is stamped. err is a failure to read
// the repository.
func (e *Exporter) Check(in *export.Input) ([]Problem, error) {
	c := newChecker(false)
	unstamped := *in
	unstamped.Git = nil
	if _, err := e.run(&unstamped, c); err != nil {
		return nil, err
	}
	if err := c.templateFiles(in.Repo.Root, in.Schema); err != nil {
		return nil, err
	}
	return c.problems, nil
}

// checker is where rendering reports what it finds (G8). Rendering goes
// the same way for an export and a check: an export's checker stops it at
// the first problem that stops an export, by returning that problem's
// Err; a check's records every problem and lets rendering go on. A
// problem in an entity embedded on several pages is found on each; it is
// kept once.
type checker struct {
	stop     bool
	problems []Problem
	seen     map[Problem]bool
	// chosen are the named templates query blocks choose.
	chosen map[string]bool
	// captions with an id, and [[#id]] references, on the page being
	// rendered.
	captions, xrefs []placed
}

type placed struct {
	id, path, entity string
	line             int
}

func newChecker(stop bool) *checker {
	return &checker{stop: stop, seen: map[Problem]bool{}, chosen: map[string]bool{}}
}

// report records p, or, while exporting, returns p.Err when p stops an
// export.
func (c *checker) report(p Problem) error {
	if c.stop && p.Err != nil {
		return p.Err
	}
	key := p
	key.Err = nil
	if !c.seen[key] {
		c.seen[key] = true
		c.problems = append(c.problems, p)
	}
	return nil
}

// add records p, a problem an export carries on past.
func (c *checker) add(p Problem) { _ = c.report(p) }

// record reports err, which stops an export, under the check its kind
// belongs to.
func (c *checker) record(err error) error {
	var (
		qe    *query.FileError
		ce    *query.ContentError
		cycle *EmbedCycleError
		te    *tmpl.Error
	)
	var p Problem
	switch {
	case errors.As(err, &qe):
		p = Problem{Check: "content/query", Path: qe.Path, Line: qe.Line, Entity: qe.Entity, Msg: "query block: " + qe.Msg}
	case errors.As(err, &ce):
		p = Problem{Check: "content/caption", Path: ce.Path, Line: ce.Line, Entity: ce.Entity, Msg: ce.Msg}
	case errors.As(err, &cycle):
		p = Problem{Check: "content/embed-cycle", Path: cycle.Path, Entity: cycle.Chain[len(cycle.Chain)-1], Msg: "embed cycle " + strings.Join(cycle.Chain, " → ")}
	case errors.As(err, &te):
		p = Problem{Check: "content/template", Path: te.Path, Line: te.Line, Msg: te.Msg}
		if p.Path == "" {
			p.Path = templatesDir
		}
	default:
		p = Problem{Check: "content/template", Path: templatesDir, Msg: err.Error()}
	}
	p.Err = err
	return c.report(p)
}

// failed reports err, if any, from rendering: returned while exporting,
// recorded while checking, when rendering goes on.
func (s *site) failed(err error) error {
	if err == nil {
		return nil
	}
	return s.check.record(err)
}

// markdownProblem reports err, a problem markdown.ConvertAll found in text
// rendered from the entity being rendered; exportErr is how an export
// stops at it.
func (ctx *renderCtx) markdownProblem(err, exportErr error) error {
	var me *markdown.Error
	if !errors.As(err, &me) {
		return ctx.site.check.record(exportErr)
	}
	rule := "content/caption"
	if me.Image {
		rule = "content/image"
	}
	return ctx.site.check.report(Problem{Check: rule, Path: ctx.path(), Line: ctx.fileLine(me.Line), Entity: ctx.entityID(), Msg: me.Msg, Err: exportErr})
}

// entityID is the ID of the entity being rendered, or "" for the index
// and the layout.
func (ctx *renderCtx) entityID() string {
	if e := ctx.entity(); e != nil {
		return e.ID
	}
	return ""
}

// path is the file of the entity being rendered, or the templates folder
// for the index and the layout.
func (ctx *renderCtx) path() string {
	if e := ctx.entity(); e != nil {
		return e.Path
	}
	return templatesDir
}

// fileLine maps a line of the Markdown being converted to its line in the
// entity's file; inside a caption's text, the caption's line.
func (ctx *renderCtx) fileLine(line int) int {
	if ctx.lineAt > 0 {
		return ctx.lineAt
	}
	if e := ctx.entity(); e != nil && line > 0 {
		return e.TextFileLine(ctx.source, line)
	}
	return 0
}

// pageStart forgets the previous page's captions.
func (c *checker) pageStart() { c.captions, c.xrefs = nil, nil }

// pageEnd checks a page's captions: an id used twice, and a [[#id]] with
// no caption of that id on the page.
func (c *checker) pageEnd() {
	first := map[string]placed{}
	for _, p := range c.captions {
		if f, dup := first[p.id]; dup {
			if f != p {
				c.add(Problem{Check: "content/caption", Path: p.path, Line: p.line, Entity: p.entity,
					Msg: fmt.Sprintf("caption id %q is used twice on one page (first at %s:%d)", p.id, f.path, f.line)})
			}
			continue
		}
		first[p.id] = p
	}
	for _, x := range c.xrefs {
		if _, ok := first[x.id]; !ok {
			c.add(Problem{Check: "content/caption", Path: x.path, Line: x.line, Entity: x.entity,
				Msg: fmt.Sprintf("[[#%s]]: no caption with id %q on this page", x.id, x.id)})
		}
	}
	c.pageStart()
}

// siteOverrides are the _-prefixed templates a project may provide.
var siteOverrides = []string{"_layout.tmpl", "_index.tmpl", "_cite.tmpl"}

// templateFiles warns of templates/ files the export never uses: a
// _-prefixed one that is no site override, a named template no query
// block chooses, and one that is neither a type's nor a named template.
func (c *checker) templateFiles(root string, s *schema.Schema) error {
	entries, err := os.ReadDir(filepath.Join(root, templatesDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		base, ok := strings.CutSuffix(name, ".tmpl")
		if e.IsDir() || !ok {
			continue
		}
		p := Problem{Warning: true, Path: templatesDir + "/" + name}
		switch {
		case strings.HasPrefix(name, "_"):
			if slices.Contains(siteOverrides, name) {
				continue
			}
			p.Check = "content/unknown-override"
			p.Msg = fmt.Sprintf("%s is not a site override (%s), so it is never used", name, strings.Join(siteOverrides, ", "))
		case s.Types[base] != nil:
			continue
		case query.IsTemplateName(base):
			if c.chosen[base] {
				continue
			}
			p.Check = "content/unused-template"
			p.Msg = fmt.Sprintf("named template %s is not chosen by any query block", base)
		default:
			p.Check = "content/template-name"
			p.Msg = fmt.Sprintf("%s names no declared type and is not a named template (lower-case kebab-case), so it is never used", name)
		}
		c.add(p)
	}
	return nil
}
