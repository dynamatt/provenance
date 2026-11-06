package website

import (
	"bytes"
	"html/template"
	"maps"
	"slices"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/tmpl"
)

// Type pages (DES-0054): a type whose schema says pages: false gets no page
// per entity. Its entities are shown together on the type's page,
// entities/<Type>.html, through templates/_<Type>.tmpl or the built-in
// rendering, and every link to one of them points at its place there.
// Other types have no type page: a listing is a Document the team writes.

// typeSite is what the site knows of its type pages and CSV files.
type typeSite struct {
	// tables are the CSV files by the type of their rows (DES-0053).
	tables map[string]*entity.Table
	// shown are the entities of each type with pages: false, by ID; noPage
	// holds their IDs, with their type; typePage the types whose page is
	// exported, those with an entity in scope.
	shown    map[string][]*model.Entity
	noPage   map[string]string
	typePage map[string]bool
}

// setupTypePages records which entities have no page of their own.
func (s *site) setupTypePages(tables []*entity.Table, entities []*model.Entity, inScope func(string) bool) {
	s.typeSite = typeSite{tables: map[string]*entity.Table{}, shown: map[string][]*model.Entity{},
		noPage: map[string]string{}, typePage: map[string]bool{}}
	for _, t := range tables {
		s.typeSite.tables[t.Type] = t
	}
	for _, e := range entities {
		if e.Schema.Pages || !inScope(e.ID) {
			continue
		}
		s.typeSite.shown[e.Type] = append(s.typeSite.shown[e.Type], e)
		s.typeSite.noPage[e.ID] = e.Type
		s.typeSite.typePage[e.Type] = true
	}
}

// hasPage reports whether entity id has a page of its own in this export.
func (s *site) hasPage(id string) bool {
	return s.scope.Has(id) && s.typeSite.noPage[id] == ""
}

// shownOn is where an entity without a page of its own is shown: its
// type's page, at the entity, relative to the entity pages; "" for any
// other entity.
func (s *site) shownOn(id string) string {
	if typ := s.typeSite.noPage[id]; typ != "" {
		return typ + ".html#" + id
	}
	return ""
}

// typePageHref is the page of type typ, relative to the entity pages, or
// "" when it has none in this export.
func (s *site) typePageHref(typ string) string {
	if s.typeSite.typePage[typ] {
		return typ + ".html"
	}
	return ""
}

// typePages are the types whose page is exported, in name order.
func (s *site) typePages() []string {
	return slices.Sorted(maps.Keys(s.typeSite.typePage))
}

// typePageTemplate is the project template of typ's page.
func typePageTemplate(typ string) string { return templatesDir + "/_" + typ + ".tmpl" }

// typePageInputs are what a type page shows: every entity it shows in
// full, with what each one's page would show, the page's template (a path
// that does not exist yet still counts) and, for a CSV-backed type, the
// file, whose commits are the page's history.
func (s *site) typePageInputs(typ string) (history.Inputs, error) {
	paths := map[string]bool{".component": true, templatesDir + "/_layout.tmpl": true, typePageTemplate(typ): true}
	if t := s.typeSite.tables[typ]; t != nil {
		paths[t.Path] = true
	}
	ids, types := map[string]bool{}, map[string]bool{}
	for _, e := range s.typeSite.shown[typ] {
		in, err := s.pageInputs(e)
		if err != nil {
			return history.Inputs{}, err
		}
		for _, id := range in.Entities {
			ids[id] = true
		}
		for _, p := range in.Paths {
			paths[p] = true
		}
		for _, ty := range in.Types {
			types[ty] = true
		}
	}
	return history.Inputs{Entities: slices.Sorted(maps.Keys(ids)), Paths: slices.Sorted(maps.Keys(paths)), Types: slices.Sorted(maps.Keys(types))}, nil
}

// typePageData is what a type page's template receives: the type, its
// entities in ID order, and, for a CSV-backed type, the file they are read
// from, as a row's .File without .Line.
func (s *site) typePageData(typ string) tmpl.Data {
	entities := []tmpl.Data{}
	for _, e := range s.typeSite.shown[typ] {
		entities = append(entities, s.data.ByID[e.ID])
	}
	d := tmpl.Data{"Type": typ, "Entities": entities, "File": nil}
	if t := s.typeSite.tables[typ]; t != nil {
		d["File"] = tmpl.TableFile(t)
	}
	return d
}

// builtinTypePage is what the built-in type page receives.
type builtinTypePage struct {
	Type string
	File tmpl.Data
	// Count is the number of entities; Entities the entities, rendered.
	Count    int
	Entities template.HTML
}

// renderTypePage renders typ's page through templates/_<Type>.tmpl, or the
// built-in rendering: one table row per entity, a column per field and
// incoming facet, when no entity of the type has a body; otherwise one
// section per entity, rendered as its type template renders it (an
// embed's headings, one level down). Each entity is rendered as the
// entity being rendered, so a problem in its text is placed at its line.
func (s *site) renderTypePage(typ string) (string, error) {
	defer s.check.pageEnd()
	s.check.pageStart()
	entities := s.typeSite.shown[typ]
	ctx := &renderCtx{site: s, linkBase: ""}
	if src, ok := s.templates.typePages[typ]; ok {
		t, err := parse(src, ctx.funcs())
		if err != nil {
			return "", err
		}
		var b bytes.Buffer
		if err := t.Execute(&b, s.typePageData(typ)); err != nil {
			return "", templateError(err)
		}
		return splice(b.String(), ctx.fragments)
	}
	tp, err := template.New("").Funcs(fallbackFuncs).Funcs(ctx.funcs()).Funcs(template.FuncMap{"ref": ctx.ref}).ParseFS(templateFS,
		"templates/values.tmpl", "templates/type.tmpl")
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	if hasBodies(entities) {
		for _, e := range entities {
			html, err := s.renderEntity(e, []*model.Entity{e}, "", "")
			if err := s.failed(err); err != nil {
				return "", err
			}
			ctx.fragments = append(ctx.fragments, html)
			if err := tp.ExecuteTemplate(&body, "type-section", template.HTML(embedPlaceholder(len(ctx.fragments)-1))); err != nil {
				return "", templateError(err)
			}
		}
	} else {
		var cols []string
		if len(entities) > 0 {
			for _, v := range entityView(entities[0]).Fields {
				cols = append(cols, v.Field.Label())
			}
			for _, in := range entities[0].Incoming {
				cols = append(cols, in.Facet.Name)
			}
		}
		if err := tp.ExecuteTemplate(&body, "type-table-head", cols); err != nil {
			return "", templateError(err)
		}
		for _, e := range entities {
			ctx.chain = []*model.Entity{e}
			if err := tp.ExecuteTemplate(&body, "type-table-row", entityView(e)); err != nil {
				return "", templateError(err)
			}
		}
		ctx.chain = nil
		body.WriteString("</tbody>\n</table>\n")
	}
	d := builtinTypePage{Type: typ, Count: len(entities), Entities: template.HTML(body.String())}
	if t := s.typeSite.tables[typ]; t != nil {
		d.File = tmpl.TableFile(t)
	}
	var b bytes.Buffer
	if err := tp.ExecuteTemplate(&b, "type", d); err != nil {
		return "", templateError(err)
	}
	return splice(b.String(), ctx.fragments)
}

// hasBodies reports whether any of entities has a body, which a table
// cell can't hold: its type's body field, or freeform text below its
// frontmatter.
func hasBodies(entities []*model.Entity) bool {
	for _, e := range entities {
		if e.Schema.BodyField != nil || e.Body != "" {
			return true
		}
	}
	return false
}
