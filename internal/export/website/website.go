// Package website is the static DHF website exporter (Requirements Spec §7).
// Every link in the output is relative, so the site works from any host path
// or straight from disk, with no network access.
package website

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/repo"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

//go:embed assets/style.css
var styleCSS []byte

// Exporter renders the website format.
type Exporter struct{}

func New() *Exporter { return &Exporter{} }

// page is the data every page template receives.
type page struct {
	// Root is the relative path from the page to the site root ("" or "../").
	Root      string
	Title     string
	Component repo.Component
	// Data is the page-specific content.
	Data any
}

// safeID matches IDs usable as a file name on every platform.
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (e *Exporter) Export(in *export.Input) (export.Files, error) {
	s := &site{
		component: in.Repo.Component,
		files:     export.Files{"style.css": styleCSS},
		byID:      make(map[string]*model.Entity, len(in.Entities)),
	}
	for _, ent := range in.Entities {
		if !safeID.MatchString(ent.ID) {
			return nil, fmt.Errorf("%s: ID %q cannot be used as a page name (letters, digits, '.', '_' and '-' only)", ent.Path, ent.ID)
		}
		s.byID[ent.ID] = ent
	}
	if err := s.render("index.html", "index.tmpl", "", in.Repo.Component.Name, nil, groupByType(in.Entities)); err != nil {
		return nil, err
	}
	for _, ent := range in.Entities {
		title := ent.ID
		if t := ent.Title(); t != "" {
			title += " " + t
		}
		ctx := &renderCtx{site: s, chain: []*model.Entity{ent}}
		if err := s.render("entities/"+ent.ID+".html", "entity.tmpl", "../", title, ctx, entityView(ent)); err != nil {
			return nil, err
		}
	}
	return s.files, nil
}

// typeGroup is one section of the index: every entity of one type.
type typeGroup struct {
	Type     string
	Entities []*model.Entity
}

// groupByType groups entities (already sorted by ID) by type, types in name
// order.
func groupByType(entities []*model.Entity) []typeGroup {
	byType := map[string][]*model.Entity{}
	for _, e := range entities {
		byType[e.Type] = append(byType[e.Type], e)
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	groups := make([]typeGroup, 0, len(types))
	for _, t := range types {
		groups = append(groups, typeGroup{Type: t, Entities: byType[t]})
	}
	return groups
}

// entityPage is the built-in fallback rendering of one entity: a table of its
// fields in schema order, then its body.
type entityPage struct {
	*model.Entity
	// Fields excludes the body field, which is rendered below the table.
	Fields    []*model.Value
	BodyField *model.Value
}

func entityView(e *model.Entity) entityPage {
	p := entityPage{Entity: e}
	for _, v := range e.Fields {
		if v.Field == e.Schema.BodyField {
			p.BodyField = v
			continue
		}
		p.Fields = append(p.Fields, v)
	}
	return p
}

var funcs = template.FuncMap{
	"kind":   func(v *model.Value) string { return string(v.Field.Kind) },
	"number": formatNumber,
	// humanize turns a snake_case field name into a label: verified_by ->
	// "Verified by".
	"humanize": func(name string) string {
		s := strings.ReplaceAll(name, "_", " ")
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	},
	// ref pairs a link target's ID with its entity (nil when unresolved)
	// for the "ref" template.
	"ref": func(id string, e *model.Entity) linkRef { return linkRef{ID: id, Entity: e} },
	// markdown is replaced per render with one that knows the embed chain.
	"markdown": func(string) (template.HTML, error) { return "", errors.New("markdown outside a render") },
}

func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

type linkRef struct {
	ID     string
	Entity *model.Entity
}

type site struct {
	component repo.Component
	files     export.Files
	byID      map[string]*model.Entity
}

// renderCtx is the state of rendering one page: the chain of entities being
// rendered, outermost first, so embeds can detect cycles.
type renderCtx struct {
	site  *site
	chain []*model.Entity
}

// templates parses the built-in templates with funcs bound to ctx.
func (s *site) templates(ctx *renderCtx, name string) (*template.Template, error) {
	fm := template.FuncMap{}
	if ctx != nil {
		fm["markdown"] = ctx.markdown
	}
	return template.New("").Funcs(funcs).Funcs(fm).ParseFS(templateFS,
		"templates/base.tmpl", "templates/values.tmpl", "templates/entity-body.tmpl", "templates/"+name)
}

// render executes templates/<name> inside the base layout and stores the
// result at path.
func (s *site) render(path, name, root, title string, ctx *renderCtx, data any) error {
	t, err := s.templates(ctx, name)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	p := page{Root: root, Title: title, Component: s.component, Data: data}
	if err := t.ExecuteTemplate(&b, "base", p); err != nil {
		return unwrapTemplateError(err)
	}
	s.files[path] = b.Bytes()
	return nil
}

// markdown converts entity Markdown, resolving wikilinks against the site.
func (ctx *renderCtx) markdown(src string) (template.HTML, error) {
	html, err := markdown.Convert(src, &resolver{ctx: ctx})
	return template.HTML(html), err
}

// fragment renders an entity's body (no page layout) for embedding.
func (ctx *renderCtx) fragment(e *model.Entity) (string, error) {
	t, err := ctx.site.templates(ctx, "entity.tmpl")
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.ExecuteTemplate(&b, "entity", entityView(e)); err != nil {
		return "", err
	}
	return b.String(), nil
}

// unwrapTemplateError returns the export error a template function raised
// (an embed cycle), rather than html/template's wrapping of it.
func unwrapTemplateError(err error) error {
	var cycle *EmbedCycleError
	if errors.As(err, &cycle) {
		return cycle
	}
	return err
}
