package query

import (
	"errors"
	"fmt"
	"slices"

	"github.com/yuin/goldmark/util"

	"github.com/dynamatt/provenance/internal/assets"
	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// Deps is what an entity's rendering pulls content from (Detailed Design §2
// scope resolution, §4 last-changed SHA): the entity itself, every entity it
// embeds with ![[ID]], every result of its query blocks, and the same for
// each entity rendered in full inside it. A wikilink reference ([[ID]]) is
// not a dependency: it shows only the target's ID or one field, and out of
// scope it falls back to the plain ID.
type Deps struct {
	// IDs are the entities, sorted, the root included.
	IDs []string
	// Full are those of IDs rendered in full (through a template), sorted.
	Full []string
	// Read are the other entities whose values the page shows through
	// calculated fields (FormulaInputs), sorted. They are inputs of the
	// page, not part of a scope.
	Read []string
	// Types are the types the query blocks select or read across links,
	// sorted. Every entity of these types can change a result set, whether
	// it is in it now or not.
	Types []string
	// Templates are the named templates the query blocks choose, sorted.
	Templates []string
	// Assets are the image files the content shows, as repository paths,
	// sorted.
	Assets []string
}

// Dependencies walks root's content.
func (g *Graph) Dependencies(root *model.Entity) (*Deps, error) {
	w := &walker{g: g, ids: map[string]bool{}, types: map[string]bool{}, templates: map[string]bool{}, assets: map[string]bool{}}
	w.add(root, true)
	for len(w.queue) > 0 {
		e := w.queue[0]
		w.queue = w.queue[1:]
		for _, text := range markdownSources(e) {
			r := &recorder{w: w}
			opts := markdown.Options{
				Fences: map[string]markdown.FenceFunc{"query": r.query},
				Image: func(dest string) (string, error) {
					rel, inline, err := assets.Resolve(e.Path, dest)
					if err == nil && !inline {
						w.assets[rel] = true
					}
					return dest, err
				},
			}
			if _, err := markdown.Convert(text, r, opts); err != nil {
				return nil, depError(e, text, err)
			}
		}
	}
	d := &Deps{IDs: keys(w.ids), Full: keys(w.full), Types: keys(w.types), Templates: keys(w.templates), Assets: keys(w.assets)}
	read := map[string]bool{}
	for _, id := range d.IDs {
		for _, r := range g.FormulaInputs(g.Entity(id)) {
			if !w.ids[r] {
				read[r] = true
			}
		}
	}
	d.Read = keys(read)
	return d, nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

type walker struct {
	g         *Graph
	ids       map[string]bool
	types     map[string]bool
	templates map[string]bool
	assets    map[string]bool
	// queue holds entities rendered in full whose content is not yet read.
	queue []*model.Entity
	full  map[string]bool
}

// add records e; rendered in full, its own content is walked too.
func (w *walker) add(e *model.Entity, full bool) {
	w.ids[e.ID] = true
	if !full {
		return
	}
	if w.full == nil {
		w.full = map[string]bool{}
	}
	if !w.full[e.ID] {
		w.full[e.ID] = true
		w.queue = append(w.queue, e)
	}
}

// markdownSources are the texts of e rendered as Markdown: its body and
// every text field, list rows' text sub-fields included.
func markdownSources(e *model.Entity) []string {
	var out []string
	if e.Body != "" {
		out = append(out, e.Body)
	}
	var visit func(v *model.Value)
	visit = func(v *model.Value) {
		if !v.Present || v.Invalid {
			return
		}
		switch v.Field.Kind {
		case schema.Text:
			out = append(out, v.Str)
		case schema.List:
			for _, row := range v.Rows {
				for _, cell := range row {
					visit(cell)
				}
			}
			for _, item := range v.Items {
				visit(item)
			}
		}
	}
	for _, v := range e.Fields {
		visit(v)
	}
	return out
}

// recorder is a Markdown resolver that only notes what a text pulls in.
type recorder struct{ w *walker }

func (r *recorder) Reference(util.BufWriter, markdown.Link) error       { return nil }
func (r *recorder) MisplacedEmbed(util.BufWriter, markdown.Link) error  { return nil }
func (r *recorder) CaptionStart(util.BufWriter, markdown.Caption) error { return nil }
func (r *recorder) CaptionEnd(util.BufWriter, markdown.Caption) error   { return nil }

func (r *recorder) Embed(_ util.BufWriter, l markdown.Link) error {
	if e := r.w.g.Entity(l.ID); e != nil {
		r.w.add(e, true)
	}
	return nil
}

// blockError is a query block that cannot be run, at Line of the Markdown
// text it is in.
type blockError struct {
	Line int
	Err  error
}

func (e *blockError) Error() string { return e.Err.Error() }

// FileError is a query block that cannot be run, at its file and line (0
// when the line is not known). A document must not be published with a
// section silently missing.
type FileError struct {
	Path string
	Line int
	Msg  string
}

func (e *FileError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: query block: %s", e.Path, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: query block: %s", e.Path, e.Msg)
}

// BlockFileError places err, from the query block starting at blockLine of
// text rendered from e, in e's file. err's own line, if any, counts from the
// block's first line.
func BlockFileError(e *model.Entity, text string, blockLine int, err error) *FileError {
	msg, line := err.Error(), blockLine
	var qe *Error
	if errors.As(err, &qe) {
		msg = qe.Msg
		if qe.Line > 0 {
			line += qe.Line - 1
		}
	}
	return &FileError{Path: e.Path, Line: e.TextFileLine(text, line), Msg: msg}
}

// ContentError is a problem in an entity's Markdown, such as a caption or
// an image, at its file and line (0 when unknown).
type ContentError struct {
	Path string
	Line int
	Msg  string
}

func (e *ContentError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Msg)
}

func depError(e *model.Entity, text string, err error) error {
	var be *blockError
	if !errors.As(err, &be) {
		var me *markdown.Error
		if errors.As(err, &me) {
			return &ContentError{Path: e.Path, Line: e.TextFileLine(text, me.Line), Msg: me.Msg}
		}
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	return BlockFileError(e, text, be.Line, be.Err)
}

func (r *recorder) query(_ util.BufWriter, f markdown.Fence) error {
	b, err := ParseBlock(f.Source, r.w.g.Schema)
	if err != nil {
		return &blockError{Line: f.Line, Err: err}
	}
	results, err := r.w.g.Run(b)
	if err != nil {
		return &blockError{Line: f.Line, Err: err}
	}
	for _, t := range b.From {
		r.w.types[t.Name] = true
	}
	for _, t := range viaTypes(b.From, b.Where) {
		r.w.types[t] = true
	}
	for _, c := range b.Templates {
		r.w.templates[c.Name] = true
	}
	for _, e := range results {
		r.w.add(e, b.Render.Mode == RenderFull)
	}
	return nil
}
