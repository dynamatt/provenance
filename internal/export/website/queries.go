package website

import (
	"errors"
	"fmt"
	"html/template"
	"strings"

	"github.com/yuin/goldmark/util"
	"go.yaml.in/yaml/v3"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/query"
)

// QueryError is a query block that cannot be run, reported at its file and
// line. A document must not be published with a section silently missing.
type QueryError struct {
	Path string
	Line int
	Msg  string
}

func (e *QueryError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: query block: %s", e.Path, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: query block: %s", e.Path, e.Msg)
}

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
	results, err := g.Run(b)
	if err != nil {
		return r.queryError(q, err)
	}
	if len(results) == 0 {
		fmt.Fprintf(w, "<p class=\"query-empty\">No %s matches this query.</p>\n", template.HTMLEscapeString(b.From.Name))
		return nil
	}
	switch b.Render.Mode {
	case query.RenderFull:
		_, _ = w.WriteString("<div class=\"query\">\n")
		for _, e := range results {
			if err := r.embed(w, e); err != nil {
				return err
			}
		}
		_, _ = w.WriteString("</div>\n")
	default:
		_, _ = w.WriteString("<ul class=\"query\">\n")
		for _, e := range results {
			_, _ = w.WriteString("<li>")
			l := markdown.Link{ID: e.ID}
			if b.Render.Mode == query.RenderField {
				l.Field = b.Render.Field
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
	msg := err.Error()
	offset := 0
	var qe *query.Error
	if errors.As(err, &qe) {
		msg = qe.Msg
		if qe.Line > 0 {
			offset = qe.Line - 1
		}
	}
	if len(r.ctx.chain) == 0 {
		return &QueryError{Path: "templates", Msg: msg}
	}
	e := r.ctx.chain[len(r.ctx.chain)-1]
	line := r.ctx.sourceLine(q.Line + offset)
	return &QueryError{Path: e.Path, Line: line, Msg: msg}
}

// sourceLine maps a line of the Markdown being rendered to a line of the
// entity's file, or 0 when that is not known. The Markdown is the body
// (most query blocks) or a text field from the frontmatter.
func (ctx *renderCtx) sourceLine(line int) int {
	e := ctx.chain[len(ctx.chain)-1]
	src := ctx.source
	body := e.Body
	if f := e.Schema.BodyField; f != nil {
		body = e.Field(f.Name).Str
	}
	if src == body {
		// The body as rendered had leading blank lines trimmed.
		raw := strings.ReplaceAll(e.Entity.Body, "\r\n", "\n")
		lead := len(raw) - len(strings.TrimLeft(raw, "\n"))
		return e.BodyLine + lead + line - 1
	}
	for _, v := range e.Fields {
		if v.Present && !v.Invalid && v.Str == src {
			n := entity.Lookup(e.Front, v.Field.Name)
			if n == nil {
				return 0
			}
			if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
				return n.Line + line // content starts after the | or > line
			}
			return n.Line + line - 1
		}
	}
	return 0
}
