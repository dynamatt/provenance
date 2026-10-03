package website

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/yuin/goldmark/util"

	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/query"
)

// QueryError is a query block that cannot be run, at its file and line.
type QueryError = query.FileError

// fences are the website's renderers for fenced blocks, by language. Every
// language in markdown.RenderedLanguages must have one (TestEveryRenderedLanguageHasAFence).
func (r *resolver) fences() map[string]markdown.FenceFunc {
	return map[string]markdown.FenceFunc{
		"query":   r.query,
		"mermaid": unrendered,
		"drawio":  unrendered,
	}
}

// unrendered shows a block in a language the product renders but this
// version does not yet: its source, marked, rather than a plain code listing
// that would read as the content itself. validate's BlockLanguage rule
// reports these blocks.
func unrendered(w util.BufWriter, f markdown.Fence) error {
	lang := template.HTMLEscapeString(f.Lang)
	fmt.Fprintf(w, "<div class=\"unrendered\"><p class=\"unrendered-note\">%s blocks are not rendered by this version of Provenance</p>\n<pre><code class=\"language-%s\">%s</code></pre></div>\n",
		lang, lang, template.HTMLEscapeString(f.Source))
	return nil
}

// query renders a query block's results live (High-Level Design §4.3a):
// each matching entity embedded through its own template, or its linked ID,
// or one field's value.
func (r *resolver) query(w util.BufWriter, q markdown.Fence) error {
	g := r.ctx.site.graph
	b, err := query.ParseBlock(q.Source, g.Schema)
	if err != nil {
		return r.queryError(q, err)
	}
	// Every chosen template must exist, whether or not anything matches:
	// a typo must not wait for data to surface.
	for _, c := range b.Templates {
		if _, ok := r.ctx.site.templates.named[c.Name]; !ok {
			return r.queryError(q, &query.Error{Line: c.Line, Msg: r.ctx.site.templates.unknownNamed(c.Name)})
		}
	}
	results, err := g.Run(b)
	if err != nil {
		return r.queryError(q, err)
	}
	if len(results) == 0 {
		names := make([]string, len(b.From))
		for i, t := range b.From {
			names[i] = t.Name
		}
		fmt.Fprintf(w, "<p class=\"query-empty\">No %s matches this query.</p>\n", template.HTMLEscapeString(strings.Join(names, " or ")))
		return nil
	}
	switch b.Render.Mode {
	case query.RenderFull:
		_, _ = w.WriteString("<div class=\"query\">\n")
		for _, e := range results {
			if err := r.embed(w, e, b.TemplateFor(e.Type).Name); err != nil {
				return err
			}
		}
		_, _ = w.WriteString("</div>\n")
	default:
		_, _ = w.WriteString("<ul class=\"query\">\n")
		for _, e := range results {
			_, _ = w.WriteString("<li>")
			l := markdown.Link{ID: e.ID}
			if f := b.Render.Field; b.Render.Mode == query.RenderField {
				if e.Field(f) == nil && e.Facet(f) == nil {
					// Another selected type has the field; this one's
					// value is empty, as for an unset field.
					r.ctx.anchor(w, "ref", e, e.ID+"#"+f)
					_, _ = w.WriteString(" <span class=\"unresolved\">empty</span></li>\n")
					continue
				}
				l.Field = f
			}
			if err := r.Reference(w, l); err != nil {
				return err
			}
			_, _ = w.WriteString("</li>\n")
		}
		_, _ = w.WriteString("</ul>\n")
	}
	return nil
}

// queryError places err, whose line (if any) counts from the block's first
// line, in the file being rendered.
func (r *resolver) queryError(q markdown.Fence, err error) error {
	if len(r.ctx.chain) == 0 {
		return &QueryError{Path: "templates", Msg: err.Error()}
	}
	e := r.ctx.chain[len(r.ctx.chain)-1]
	return query.BlockFileError(e, r.ctx.source, q.Line, err)
}
