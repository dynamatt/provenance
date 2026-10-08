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
	"errors"
	"fmt"
	"html/template"
	"io"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/history"
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
		root:      in.Repo.Root,
		templates: ts,
		data:      buildData(in.Schema, in.Entities),
		graph:     graph,
		scope:     in.Scope,
		git:       in.Git,
		inputs:    map[string]history.Inputs{},
		byID:      make(map[string]*model.Entity, len(in.Entities)),
		files:     export.Files{"style.css": ts.style},
	}
	for _, ent := range in.Entities {
		s.byID[ent.ID] = ent
	}
	// Every entity's data carries its git stamps, embedded or not: a
	// template naming .LastChangedSHA must render wherever it is used.
	for _, ent := range in.Entities {
		in, err := s.pageInputs(ent)
		if err != nil {
			return nil, err
		}
		last, revs := s.git.Page(in)
		m := s.data.byID[ent.ID]
		m["LastChangedSHA"], m["Revisions"] = last, revisionData(revs)
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
		content, data, err := s.renderPage(root, "entities/")
		if err != nil {
			return nil, err
		}
		if err := s.page("index.html", "", pageTitle(root), s.finalize(content), data); err != nil {
			return nil, err
		}
	} else {
		index, err := s.renderIndex(inScope)
		if err != nil {
			return nil, err
		}
		last, revs := s.git.Page(s.indexInputs(inScope))
		stamps := entityData{"LastChangedSHA": last, "Revisions": revisionData(revs)}
		if err := s.page("index.html", "", in.Repo.Component.Name, s.finalize(index), stamps); err != nil {
			return nil, err
		}
	}
	for _, ent := range inScope {
		content, data, err := s.renderPage(ent, "")
		if err != nil {
			return nil, err
		}
		if err := s.page("entities/"+ent.ID+".html", "../", pageTitle(ent), s.finalize(content), data); err != nil {
			return nil, err
		}
	}
	return s.files, nil
}

// pageInputs are the files e's page shows (Detailed Design §4): the files
// of the entities its dependency walk pulls in and of those their calculated
// fields read, the images it shows, every entity of the types its queries
// select, the schema of the types it renders, and the templates it renders
// through: the layout, the type template of each entity shown in full (a
// path that does not exist yet still counts, so adding one is a change) and
// the named templates its queries choose, and _cite.tmpl when it cites
// anything. When the page lists its citations, or a _cite.tmpl renders
// them, the entities it cites are inputs too. The stylesheet is a
// separate file, so it is not an input of any page.
func (s *site) pageInputs(e *model.Entity) (history.Inputs, error) {
	if in, ok := s.inputs[e.ID]; ok {
		return in, nil
	}
	deps, err := s.graph.Dependencies(e)
	if err != nil {
		return history.Inputs{}, err
	}
	paths := map[string]bool{
		".component":                   true,
		templatesDir + "/_layout.tmpl": true,
	}
	for _, id := range append(slices.Clone(deps.IDs), deps.Read...) {
		ent := s.byID[id]
		paths[ent.Path] = true
		s.schemaFiles(ent.Schema, paths)
	}
	for _, id := range deps.Full {
		paths[templatesDir+"/"+s.byID[id].Type+".tmpl"] = true
	}
	for _, name := range deps.Templates {
		paths[templatesDir+"/"+name+".tmpl"] = true
	}
	for _, a := range deps.Assets {
		paths[a] = true
	}
	if len(deps.Cited) > 0 {
		paths[templatesDir+"/_cite.tmpl"] = true
	}
	if s.listsCitations(e) || s.templates.cite != nil {
		for _, id := range deps.Cited {
			if ent := s.byID[id]; ent != nil {
				paths[ent.Path] = true
				s.schemaFiles(ent.Schema, paths)
			}
		}
	}
	in := history.Inputs{Paths: slices.Sorted(maps.Keys(paths)), Types: deps.Types}
	s.inputs[e.ID] = in
	return in, nil
}

// schemaFiles adds the files declaring t: its own, and the records its lists
// use.
func (s *site) schemaFiles(t *schema.Type, paths map[string]bool) {
	paths[t.File] = true
	for _, f := range t.Fields {
		if f.Record != "" {
			if r := s.graph.Schema.Records[f.Record]; r != nil {
				paths[r.File] = true
			}
		}
	}
}

// indexInputs are what the index page shows: every entity in scope, with
// the schema of their types, the index template and the layout.
func (s *site) indexInputs(entities []*model.Entity) history.Inputs {
	paths := map[string]bool{
		".component":                   true,
		templatesDir + "/_layout.tmpl": true,
		templatesDir + "/_index.tmpl":  true,
	}
	for _, e := range entities {
		paths[e.Path] = true
		s.schemaFiles(e.Schema, paths)
	}
	return history.Inputs{Paths: slices.Sorted(maps.Keys(paths))}
}

// revisionData is revision history as template data, newest first.
func revisionData(revs []*history.Commit) []entityData {
	out := []entityData{}
	for _, c := range revs {
		out = append(out, entityData{
			"SHA": c.SHA, "Short": history.Short(c.SHA), "Date": c.Date,
			"Author": c.Author, "Subject": c.Subject, "Tags": c.Tags,
		})
	}
	return out
}

func pageTitle(e *model.Entity) string {
	if t := e.Title(); t != "" {
		return e.ID + " " + t
	}
	return e.ID
}

type site struct {
	component repo.Component
	root      string // the repository root, for images
	templates *templateSet
	data      *dataModel
	graph     *query.Graph
	scope     *export.Scope
	git       *export.Git
	inputs    map[string]history.Inputs // page inputs by entity ID
	serial    int                       // numbers caption placeholders
	byID      map[string]*model.Entity
	files     export.Files
	// cites collects the citations of the page being rendered, during its
	// first render; pageData is the page's own entity data, with its
	// citations, and cited those citations by ID, during its second.
	cites    *citations
	pageData entityData
	cited    map[string]entityData
}

// citations are the IDs a page cites, in order of first citation.
type citations struct {
	ids  []string
	seen map[string]bool
}

func (c *citations) add(id string) {
	if !c.seen[id] {
		c.seen[id] = true
		c.ids = append(c.ids, id)
	}
}

// citesField matches a template that may read .Citations; a false match
// (a comment) only adds page inputs.
var citesField = regexp.MustCompile(`\.Citations\b`)

// listsCitations reports whether e's page may list its citations: its type
// template or the layout reads .Citations. The built-in templates do not.
func (s *site) listsCitations(e *model.Entity) bool {
	if citesField.MatchString(s.templates.layout.text) {
		return true
	}
	t, ok := s.templates.types[e.Type]
	return ok && citesField.MatchString(t.text)
}

// renderPage renders e as the entity a page is about (DES-0046). A page
// that cites anything is rendered twice: the first render collects its
// citations, in order of first citation; the second renders with them
// known, as e's .Citations and to _cite.tmpl. The data returned is e's, with .Citations, for
// the layout.
func (s *site) renderPage(e *model.Entity, linkBase string) (string, entityData, error) {
	chain := []*model.Entity{e}
	s.cites = &citations{seen: map[string]bool{}}
	content, err := s.renderEntity(e, chain, "", linkBase)
	cites := s.cites
	s.cites = nil
	data := s.data.byID[e.ID]
	if err != nil || len(cites.ids) == 0 {
		return content, data, err
	}
	data = maps.Clone(data)
	list := s.data.citations(cites.ids)
	data["Citations"] = list
	s.pageData, s.cited = data, map[string]entityData{}
	for _, c := range list {
		s.cited[c["ID"].(string)] = c
	}
	content, err = s.renderEntity(e, chain, "", linkBase)
	s.pageData, s.cited = nil, nil
	return content, data, err
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
	// Git stamps (Detailed Design §4), all empty outside a git repository:
	// HEAD's commit, the DHF content hash, and for this page the last
	// commit that changed what it shows and the commits that did.
	GitSHA         string
	ContentHash    string
	LastChangedSHA string
	Revisions      []entityData
	// Citations are what the page cites (DES-0046), as on its entity; none
	// on the site index.
	Citations []entityData
}

// page wraps content in the layout and stores it at path. stamps holds the
// page's LastChangedSHA, Revisions and Citations.
func (s *site) page(path, root, title, content string, stamps entityData) error {
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
	if s.git != nil {
		d.GitSHA, d.ContentHash = s.git.SHA, s.git.ContentHash
	}
	d.LastChangedSHA, _ = stamps["LastChangedSHA"].(string)
	d.Revisions, _ = stamps["Revisions"].([]entityData)
	d.Citations, _ = stamps["Citations"].([]entityData)
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
	cite      *template.Template // _cite.tmpl, parsed on first use
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
		data := s.data.byID[e.ID]
		if len(chain) == 1 && s.pageData != nil {
			data = s.pageData // the page's own entity, with its citations
		}
		if err := t.Execute(&b, data); err != nil {
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
		"short":    history.Short,
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
	out, err := markdown.Convert(src, r, r.options())
	ctx.source = prev
	var me *markdown.Error
	if errors.As(err, &me) && len(ctx.chain) > 0 {
		e := ctx.chain[len(ctx.chain)-1]
		err = &ContentError{Path: e.Path, Line: e.TextFileLine(src, me.Line), Msg: me.Msg}
	}
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
