package query

import (
	"errors"
	"fmt"
	"slices"

	"github.com/yuin/goldmark/util"

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
	// Types are the types the query blocks select, sorted. Every entity of
	// these types can change a result set, whether it is in it now or not.
	Types []string
}

// Dependencies walks root's content.
func (g *Graph) Dependencies(root *model.Entity) (*Deps, error) {
	w := &walker{g: g, ids: map[string]bool{}, types: map[string]bool{}}
	w.add(root, true)
	for len(w.queue) > 0 {
		e := w.queue[0]
		w.queue = w.queue[1:]
		for _, text := range markdownSources(e) {
			r := &recorder{w: w}
			if _, err := markdown.Convert(text, r, map[string]markdown.FenceFunc{"query": r.query}); err != nil {
				return nil, depError(e, text, err)
			}
		}
	}
	d := &Deps{}
	for id := range w.ids {
		d.IDs = append(d.IDs, id)
	}
	for t := range w.types {
		d.Types = append(d.Types, t)
	}
	slices.Sort(d.IDs)
	slices.Sort(d.Types)
	return d, nil
}

type walker struct {
	g     *Graph
	ids   map[string]bool
	types map[string]bool
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

func (r *recorder) Reference(util.BufWriter, markdown.Link) error      { return nil }
func (r *recorder) MisplacedEmbed(util.BufWriter, markdown.Link) error { return nil }

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

// depError reports a query block problem at its file line when it is in
// the body, as export would report it.
func depError(e *model.Entity, text string, err error) error {
	var be *blockError
	if !errors.As(err, &be) {
		return fmt.Errorf("%s: %w", e.Path, err)
	}
	msg := be.Err.Error()
	line := be.Line
	var qe *Error
	if errors.As(be.Err, &qe) {
		msg = qe.Msg
		if qe.Line > 0 {
			line += qe.Line - 1
		}
	}
	if text == e.BodyText() {
		return fmt.Errorf("%s:%d: query block: %s", e.Path, e.BodyFileLine(line), msg)
	}
	return fmt.Errorf("%s: query block in a text field: %s", e.Path, msg)
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
	for _, e := range results {
		r.w.add(e, b.Render.Mode == RenderFull)
	}
	return nil
}
