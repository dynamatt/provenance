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
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/tmpl"
)

// Problem is something Check found: a problem that stops an export, or
// content the export would show as a gap (DES-0048).
type Problem struct {
	// Rule is the built-in check's ID, e.g. "content/query".
	Rule    string
	Warning bool
	// Path is slash-separated and relative to the repository root; Line is
	// 0 when not known.
	Path string
	Line int
	Msg  string
}

// Check renders the site as Export does, but records every problem and
// goes on instead of stopping at the first, so validate can report them
// all. Nothing is written. err is a failure to read the repository.
func (e *Exporter) Check(in *export.Input) ([]Problem, error) {
	c := &checker{seen: map[Problem]bool{}, chosen: map[string]bool{}}
	if _, err := e.run(in, c); err != nil {
		return nil, err
	}
	if err := c.templateFiles(in.Repo.Root, in.Schema); err != nil {
		return nil, err
	}
	return c.problems, nil
}

// checker collects Check's problems. A problem in an entity embedded on
// several pages is found on each; it is kept once.
type checker struct {
	problems []Problem
	seen     map[Problem]bool
	// chosen are the named templates query blocks choose.
	chosen map[string]bool
	// captions with an id, and [[#id]] references, on the page being
	// rendered.
	captions, xrefs []placed
}

type placed struct {
	id, path string
	line     int
}

func (c *checker) add(p Problem) {
	if !c.seen[p] {
		c.seen[p] = true
		c.problems = append(c.problems, p)
	}
}

// record keeps err, a problem Export would have stopped at, under the
// check its kind belongs to.
func (c *checker) record(err error) {
	var (
		qe    *query.FileError
		ce    *query.ContentError
		cycle *EmbedCycleError
		te    *tmpl.Error
	)
	switch {
	case errors.As(err, &qe):
		c.add(Problem{Rule: "content/query", Path: qe.Path, Line: qe.Line, Msg: "query block: " + qe.Msg})
	case errors.As(err, &ce):
		c.add(Problem{Rule: "content/caption", Path: ce.Path, Line: ce.Line, Msg: ce.Msg})
	case errors.As(err, &cycle):
		c.add(Problem{Rule: "content/embed-cycle", Path: cycle.Path, Msg: "embed cycle " + strings.Join(cycle.Chain, " → ")})
	case errors.As(err, &te):
		p := Problem{Rule: "content/template", Path: te.Path, Line: te.Line, Msg: te.Msg}
		if p.Path == "" {
			p.Path = templatesDir
		}
		c.add(p)
	default:
		c.add(Problem{Rule: "content/template", Path: templatesDir, Msg: err.Error()})
	}
}

// failed is how rendering reports err: returned while exporting, recorded
// (and rendering goes on) while checking.
func (s *site) failed(err error) error {
	if err == nil || s.check == nil {
		return err
	}
	s.check.record(err)
	return nil
}

// markdownProblem records a problem markdown.ConvertAll found in text
// rendered from the entity being rendered.
func (ctx *renderCtx) markdownProblem(err error) {
	var me *markdown.Error
	if !errors.As(err, &me) {
		ctx.site.check.record(err)
		return
	}
	rule := "content/caption"
	if me.Image {
		rule = "content/image"
	}
	ctx.site.check.add(Problem{Rule: rule, Path: ctx.path(), Line: ctx.fileLine(me.Line), Msg: me.Msg})
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
				c.add(Problem{Rule: "content/caption", Path: p.path, Line: p.line,
					Msg: fmt.Sprintf("caption id %q is used twice on one page (first at %s:%d)", p.id, f.path, f.line)})
			}
			continue
		}
		first[p.id] = p
	}
	for _, x := range c.xrefs {
		if _, ok := first[x.id]; !ok {
			c.add(Problem{Rule: "content/caption", Path: x.path, Line: x.line,
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
			p.Rule = "content/unknown-override"
			p.Msg = fmt.Sprintf("%s is not a site override (%s), so it is never used", name, strings.Join(siteOverrides, ", "))
		case s.Types[base] != nil:
			continue
		case query.IsTemplateName(base):
			if c.chosen[base] {
				continue
			}
			p.Rule = "content/unused-template"
			p.Msg = fmt.Sprintf("named template %s is not chosen by any query block", base)
		default:
			p.Rule = "content/template-name"
			p.Msg = fmt.Sprintf("%s names no declared type and is not a named template (lower-case kebab-case), so it is never used", name)
		}
		c.add(p)
	}
	return nil
}
