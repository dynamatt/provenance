// Package website is the static DHF website exporter (Requirements Spec §7).
// Every link in the output is relative, so the site works from any host path
// or straight from disk, with no network access.
//
// Each entity renders through its project template (templates/<Type>.tmpl)
// or the built-in fallback; embeds are spliced in with their headings shifted
// to their depth; the result is wrapped in the layout (templates/_layout.tmpl
// or built-in). The main page renders through templates/_index.tmpl or the
// built-in index.
package website

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

//go:embed assets/style.css
var styleCSS []byte

// Exporter renders the website format.
type Exporter struct{}

func New() *Exporter { return &Exporter{} }

// safeID matches IDs usable as a file name on every platform.
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (e *Exporter) Export(in *export.Input) (export.Files, error) {
	for _, ent := range in.Entities {
		if !safeID.MatchString(ent.ID) {
			return nil, fmt.Errorf("%s: ID %q cannot be used as a page name (letters, digits, '.', '_' and '-' only)", ent.Path, ent.ID)
		}
	}
	if err := checkTemplateNames(in.Schema); err != nil {
		return nil, err
	}
	ts, err := loadTemplates(in.Repo.Root, in.Schema)
	if err != nil {
		return nil, err
	}
	// The graph evaluates calculated fields, which the template data reads.
	graph := in.Graph
	if graph == nil {
		graph, err = query.NewGraph(in.Schema, in.Entities)
		if err != nil {
			return nil, err
		}
	}
	s := &site{
		component: in.Repo.Component,
		templates: ts,
		data:      buildData(in.Schema, in.Entities),
		graph:     graph,
		scope:     in.Scope,
		byID:      make(map[string]*model.Entity, len(in.Entities)),
		files:     export.Files{"style.css": ts.style},
	}
	for _, ent := range in.Entities {
		s.byID[ent.ID] = ent
	}

	var inScope []*model.Entity
	for _, ent := range in.Entities {
		if in.Scope.Has(ent.ID) {
			inScope = append(inScope, ent)
		}
	}
	// An entity scope's entity is the main page, rendered through its own
	// template (Detailed Design §2); otherwise the index lists the scope.
	if root := in.Scope.RootEntity(); root != nil {
		content, err := s.renderEntity(root, []*model.Entity{root}, "", "entities/")
		if err != nil {
			return nil, err
		}
		if err := s.page("index.html", "", pageTitle(root), content); err != nil {
			return nil, err
		}
	} else {
		index, err := s.renderIndex(inScope)
		if err != nil {
			return nil, err
		}
		if err := s.page("index.html", "", in.Repo.Component.Name, index); err != nil {
			return nil, err
		}
	}
	for _, ent := range inScope {
		content, err := s.renderEntity(ent, []*model.Entity{ent}, "", "")
		if err != nil {
			return nil, err
		}
		if err := s.page("entities/"+ent.ID+".html", "../", pageTitle(ent), content); err != nil {
			return nil, err
		}
	}
	return s.files, nil
}

func pageTitle(e *model.Entity) string {
	if t := e.Title(); t != "" {
		return e.ID + " " + t
	}
	return e.ID
}

type site struct {
	component repo.Component
	templates *templateSet
	data      *dataModel
	graph     *query.Graph
	scope     *export.Scope
	byID      map[string]*model.Entity
	files     export.Files
}

// layoutData is what the layout template receives.
type layoutData struct {
	// Title is the page title; Root the relative path from the page to the
	// site root ("" or "../"), for the stylesheet and site links.
	Title     string
	Root      string
	Component repo.Component
	// Content is the page's rendered content, also available to the layout
	// as {{template "content" .}}.
	Content template.HTML
}

// page wraps content in the layout and stores it at path.
func (s *site) page(path, root, title, content string) error {
	ctx := &renderCtx{site: s, linkBase: root + "entities/"}
	t, err := parse(s.templates.layout, ctx.funcs())
	if err == nil {
		_, err = t.New("content").Parse(`{{.Content}}`)
	}
	if err != nil {
		return templateError(err)
	}
	var b bytes.Buffer
	d := layoutData{Title: title, Root: root, Component: s.component, Content: template.HTML(content)}
	if err := t.Execute(&b, d); err != nil {
		return templateError(err)
	}
	s.files[path] = b.Bytes()
	return nil
}

// indexData is what the index template receives.
type indexData struct {
	Component repo.Component
	// Types groups every entity (as template data) by type, types in name
	// order and entities by ID.
	Types []typeGroup
}

type typeGroup struct {
	Type     string
	Entities []entityData
}

func (s *site) renderIndex(entities []*model.Entity) (string, error) {
	byType := map[string][]entityData{}
	for _, e := range entities {
		byType[e.Type] = append(byType[e.Type], s.data.byID[e.ID])
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)
	d := indexData{Component: s.component}
	for _, t := range types {
		d.Types = append(d.Types, typeGroup{Type: t, Entities: byType[t]})
	}

	ctx := &renderCtx{site: s, linkBase: "entities/"}
	t, err := parse(s.templates.index, ctx.funcs())
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return "", templateError(err)
	}
	return b.String(), nil
}

// renderCtx is the state of rendering one entity: the chain of entities
// being rendered, outermost first, for embed cycle detection; where links
// point from; the rendered embeds awaiting splicing; and the Markdown being
// converted, to place query errors in the file.
type renderCtx struct {
	site      *site
	chain     []*model.Entity
	linkBase  string
	fragments []string
	source    string
}

// renderEntity renders e through the named template (a query's choice,
// already checked to exist), else its project type template, else the
// built-in fallback, with embeds spliced in. Headings are relative to e:
// <h1> is its top level. chain ends with e.
//
// linkBase is the relative path from the page to the entity pages: "" on an
// entity page, "entities/" when rendered on the main page.
func (s *site) renderEntity(e *model.Entity, chain []*model.Entity, named, linkBase string) (string, error) {
	ctx := &renderCtx{site: s, chain: chain, linkBase: linkBase}
	var b bytes.Buffer
	src, ok := s.templates.types[e.Type]
	if named != "" {
		src, ok = s.templates.named[named]
	}
	if ok {
		t, err := parse(src, ctx.funcs())
		if err != nil {
			return "", err
		}
		if err := t.Execute(&b, s.data.byID[e.ID]); err != nil {
			return "", templateError(err)
		}
	} else {
		t, err := template.New("").Funcs(fallbackFuncs).Funcs(ctx.funcs()).Funcs(template.FuncMap{"ref": ctx.ref}).ParseFS(templateFS,
			"templates/values.tmpl", "templates/entity-body.tmpl")
		if err != nil {
			return "", err
		}
		if err := t.ExecuteTemplate(&b, "entity", entityView(e)); err != nil {
			return "", templateError(err)
		}
	}
	return splice(b.String(), ctx.fragments)
}

// funcs are the template functions bound to this render.
func (ctx *renderCtx) funcs() template.FuncMap {
	return template.FuncMap{
		"markdown": ctx.markdown,
		"link":     ctx.link,
		"href":     ctx.href,
	}
}

// markdown converts entity Markdown, resolving wikilinks against the site.
// An absent field (nil) renders as nothing.
func (ctx *renderCtx) markdown(v any) (template.HTML, error) {
	src, ok := v.(string)
	if v == nil || (ok && src == "") {
		return "", nil
	}
	if !ok {
		return "", fmt.Errorf("markdown: want text, got %T", v)
	}
	prev := ctx.source
	ctx.source = src
	r := &resolver{ctx: ctx}
	out, err := markdown.Convert(src, r, r.fences())
	ctx.source = prev
	return template.HTML(out), err
}

// link renders a link to an entity (template data), with optional text: the
// ID by default. nil renders nothing; an unresolved target is marked; one
// outside the export's scope is its text, unlinked.
func (ctx *renderCtx) link(v any, text ...any) (template.HTML, error) {
	ref, err := asRef(v, "link")
	if ref == nil || err != nil {
		return "", err
	}
	id, _ := ref["ID"].(string)
	label := id
	if len(text) > 0 {
		label = fmt.Sprint(text[0])
	}
	if resolved, _ := ref["Resolved"].(bool); !resolved {
		return template.HTML(fmt.Sprintf(`<span class="id unresolved-id">%s</span> <span class="unresolved">unresolved</span>`, template.HTMLEscapeString(label))), nil
	}
	var b strings.Builder
	ctx.anchor(&b, "ref", ctx.site.byID[id], label)
	return template.HTML(b.String()), nil
}

// anchor writes a link with the given class to target's page showing text,
// or, when target is outside the export's scope, the text alone (Requirements
// Spec §7: the plain citable ID for an ID).
func (ctx *renderCtx) anchor(w io.StringWriter, class string, target *model.Entity, text string) {
	if !ctx.site.scope.Has(target.ID) {
		_, _ = w.WriteString(fmt.Sprintf(`<span class="%s out-of-scope">%s</span>`, class, template.HTMLEscapeString(text)))
		return
	}
	title := ""
	if t := target.Title(); t != "" {
		title = fmt.Sprintf(` title="%s"`, template.HTMLEscapeString(t))
	}
	_, _ = w.WriteString(fmt.Sprintf(`<a class="%s" href="%s"%s>%s</a>`, class,
		template.HTMLEscapeString(ctx.linkBase+target.ID+".html"), title, template.HTMLEscapeString(text)))
}

// href is the relative URL of an entity's page, for custom links; "" for an
// entity outside the export's scope, which has no page.
func (ctx *renderCtx) href(v any) (string, error) {
	ref, err := asRef(v, "href")
	if ref == nil || err != nil {
		return "", err
	}
	id, _ := ref["ID"].(string)
	if !ctx.site.scope.Has(id) {
		return "", nil
	}
	return ctx.linkBase + id + ".html", nil
}

// ref renders one link target for the built-in templates: a link, the bare
// ID marked unresolved, or the plain ID outside the scope.
func (ctx *renderCtx) ref(id string, e *model.Entity) template.HTML {
	if e == nil {
		return template.HTML(fmt.Sprintf(`<span class="id unresolved-id">%s</span> <span class="unresolved">unresolved</span>`, template.HTMLEscapeString(id)))
	}
	var b strings.Builder
	ctx.anchor(&b, "id", e, id)
	return template.HTML(b.String())
}

func asRef(v any, fn string) (entityData, error) {
	if v == nil {
		return nil, nil
	}
	ref, ok := v.(entityData)
	if !ok {
		return nil, fmt.Errorf("%s: want an entity (a link field value), got %T", fn, v)
	}
	return ref, nil
}

// Built-in fallback rendering.

// entityPage is the fallback rendering of one entity: a table of its fields
// in schema order, then its body.
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

var fallbackFuncs = template.FuncMap{
	// kind is the field's kind, or for a calculated value the kind of
	// value its formula gave.
	"kind": func(v *model.Value) string {
		if v.Field.Kind == schema.Calculated && v.Result != "" {
			return string(v.Result)
		}
		return string(v.Field.Kind)
	},
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
}

func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
