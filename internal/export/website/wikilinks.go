package website

import (
	"fmt"
	"html/template"
	"maps"
	"strings"

	"github.com/yuin/goldmark/util"

	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// EmbedCycleError reports an entity that embeds itself, directly or through
// other embeds. Publishing it would need an infinitely long document.
type EmbedCycleError struct {
	// Path is the file of the entity where the cycle starts.
	Path string
	// Chain lists the IDs around the cycle; the first and last are the same.
	Chain []string
}

func (e *EmbedCycleError) Error() string {
	return fmt.Sprintf("%s: embed cycle %s", e.Path, strings.Join(e.Chain, " → "))
}

// resolver renders wikilinks for the website. Every entity page is in
// entities/, as is every page that can contain Markdown, so links to other
// entities are relative to that folder.
type resolver struct{ ctx *renderCtx }

var _ markdown.Resolver = (*resolver)(nil)

// Reference renders an inline wikilink: [[#id]], a caption on this page,
// or a citation (DES-0046), which the page collects and a project
// _cite.tmpl, if any, renders.
func (r *resolver) Reference(w util.BufWriter, l markdown.Link) error {
	if l.Local != "" {
		// A caption on this page; resolved once the page is numbered.
		text := "#" + l.Local
		if l.Label != "" {
			text = l.Label
		}
		_, _ = w.WriteString(xrefToken(l.Local, l.Label, unresolvedHTML(text, "no caption #"+l.Local)))
		return nil
	}
	if c := r.ctx.site.cites; c != nil {
		c.add(l.ID)
	}
	if l.Field == "" {
		if done, err := r.cite(w, l); done || err != nil {
			return err
		}
	}
	r.link(w, l)
	return nil
}

// link renders a link to l's entity showing its ID, its label or one of its
// fields: a citation's built-in rendering, and how query results list
// entities, which are not citations.
func (r *resolver) link(w util.BufWriter, l markdown.Link) {
	target := r.ctx.site.byID[l.ID]
	if target == nil {
		_, _ = w.WriteString(unresolvedHTML(l.ID, "unresolved"))
		return
	}
	text := l.ID
	marker := ""
	if l.Field != "" {
		value, ok := fieldText(target, l.Field)
		switch {
		case !ok:
			text, marker = l.ID+"#"+l.Field, fmt.Sprintf("no field %q", l.Field)
		case value == "":
			text, marker = l.ID+"#"+l.Field, "empty"
		default:
			text = value
		}
	}
	if l.Label != "" {
		text = l.Label
	}
	r.ctx.anchor(w, "ref", target, text)
	if marker != "" {
		_, _ = w.WriteString(unresolvedNote(marker))
	}
}

// cite renders an [[ID]] or [[ID|label]] citation through the project's
// _cite.tmpl (DES-0046), which receives the cited entity as in .Citations,
// with .CitationLabel the label or "". It reports false, leaving the
// citation to the built-in rendering, without a _cite.tmpl or before the
// page's citations are known: during a page's first render, whose output
// is discarded, and on the site index, which is about no entity.
func (r *resolver) cite(w util.BufWriter, l markdown.Link) (bool, error) {
	src, cited := r.ctx.site.templates.cite, r.ctx.site.cited[l.ID]
	if src == nil || cited == nil {
		return false, nil
	}
	if r.ctx.cite == nil {
		t, err := parse(*src, r.ctx.funcs())
		if err != nil {
			return true, err
		}
		r.ctx.cite = t
	}
	data := maps.Clone(cited)
	data["CitationLabel"] = l.Label
	var b strings.Builder
	if err := r.ctx.cite.Execute(&b, data); err != nil {
		return true, templateError(err)
	}
	_, _ = w.WriteString(b.String())
	return true, nil
}

func (r *resolver) Embed(w util.BufWriter, l markdown.Link) error {
	target := r.ctx.site.byID[l.ID]
	if target == nil {
		_, _ = w.WriteString(`<p class="embed-missing">` + unresolvedHTML(l.ID, "unresolved"))
		_, _ = w.WriteString("</p>\n")
		return nil
	}
	return r.embed(w, target, "")
}

// embed renders target in full, through the named template if one is
// given, wrapped in an embed section.
func (r *resolver) embed(w util.BufWriter, target *model.Entity, named string) error {
	for i, e := range r.ctx.chain {
		if e == target {
			chain := []string{}
			for _, c := range r.ctx.chain[i:] {
				chain = append(chain, c.ID)
			}
			return &EmbedCycleError{Path: target.Path, Chain: append(chain, target.ID)}
		}
	}
	chain := append(append([]*model.Entity{}, r.ctx.chain...), target)
	html, err := r.ctx.site.renderEntity(target, chain, named, r.ctx.linkBase)
	if err != nil {
		return err
	}
	// The fragment's headings can only be shifted once the host has
	// rendered and the heading this embed follows is known; leave a
	// placeholder for splice.
	r.ctx.fragments = append(r.ctx.fragments, html)
	fmt.Fprintf(w, "<section class=\"embed\" data-entity=\"%s\">%s</section>\n",
		template.HTMLEscapeString(target.ID), embedPlaceholder(len(r.ctx.fragments)-1))
	return nil
}

// MisplacedEmbed renders an embed written inside running text as a normal
// reference, marked, since a whole entity cannot go mid-sentence.
func (r *resolver) MisplacedEmbed(w util.BufWriter, l markdown.Link) error {
	if err := r.Reference(w, markdown.Link{ID: l.ID}); err != nil {
		return err
	}
	_, _ = w.WriteString(unresolvedNote("embed must be on its own line"))
	return nil
}

// unresolvedHTML shows text, an ID or reference the export could not
// resolve, marked with why.
func unresolvedHTML(text, marker string) string {
	return fmt.Sprintf(`<span class="id unresolved-id">%s</span>`, template.HTMLEscapeString(text)) + unresolvedNote(marker)
}

// unresolvedNote marks what precedes it as unresolved, saying why.
func unresolvedNote(marker string) string {
	return fmt.Sprintf(` <span class="unresolved">%s</span>`, template.HTMLEscapeString(marker))
}

// fieldText is a field's (or incoming facet's) value as plain text, for
// [[ID#field]]. ok is false when the entity has no such field or facet.
func fieldText(e *model.Entity, name string) (string, bool) {
	if v := e.Field(name); v != nil {
		return valueText(v), true
	}
	if in := e.Facet(name); in != nil {
		ids := make([]string, len(in.From))
		for i, f := range in.From {
			ids[i] = f.ID
		}
		return strings.Join(ids, ", "), true
	}
	return "", false
}

func valueText(v *model.Value) string {
	kind := v.Field.Kind
	switch {
	case !v.Present:
		return ""
	case v.Invalid && kind == schema.Calculated:
		return "" // the formula, not a value
	case v.Invalid:
		return v.Raw
	case kind == schema.Calculated:
		kind = v.Result
	}
	switch kind {
	case schema.Number:
		return formatNumber(v.Num)
	case schema.Boolean:
		if v.Bool {
			return "true"
		}
		return "false"
	case schema.Link:
		return strings.Join(v.IDs, ", ")
	case schema.List:
		if v.Field.Elem != nil {
			items := make([]string, len(v.Items))
			for i, item := range v.Items {
				items[i] = valueText(item)
			}
			return strings.Join(items, ", ")
		}
		if len(v.Rows) == 1 {
			return "1 row"
		}
		return fmt.Sprintf("%d rows", len(v.Rows))
	default:
		return v.Str
	}
}
