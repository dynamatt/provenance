package website

import (
	"bytes"
	"fmt"
	"html/template"
	"maps"
	"slices"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/tmpl"
)

// Tables: a CSV file's rows are entities (DES-0053). Each file gets a
// table page, entities/<Type>.html, listing its rows with the file's
// provenance and history. A row has its own page unless its source says
// pages: false; it is then shown, in full, on the table page, which every
// link to it points at.

// tableSite is what the site knows of its CSV files.
type tableSite struct {
	// tables are the files by the type of their rows; rows their rows,
	// sorted by ID.
	tables map[string]*entity.Table
	rows   map[string][]*model.Entity
	// noPage holds the rows without a page of their own, by ID, with
	// their type; tablePage the types whose table page is exported.
	noPage    map[string]string
	tablePage map[string]bool
}

// setupTables records the CSV files and their rows, and which rows have
// no pages of their own.
func (s *site) setupTables(tables []*entity.Table, entities []*model.Entity, inScope func(string) bool) {
	s.tableSite = tableSite{tables: map[string]*entity.Table{}, rows: map[string][]*model.Entity{},
		noPage: map[string]string{}, tablePage: map[string]bool{}}
	for _, t := range tables {
		s.tableSite.tables[t.Type] = t
	}
	for _, e := range entities {
		t := s.tableSite.tables[e.Type]
		if t == nil {
			continue
		}
		if _, row := e.Record.(*entity.Row); !row {
			continue
		}
		s.tableSite.rows[e.Type] = append(s.tableSite.rows[e.Type], e)
		if !t.Pages {
			s.tableSite.noPage[e.ID] = e.Type
		}
		if inScope(e.ID) {
			s.tableSite.tablePage[e.Type] = true
		}
	}
}

// hasPage reports whether entity id has a page of its own in this export.
func (s *site) hasPage(id string) bool {
	return s.scope.Has(id) && s.tableSite.noPage[id] == ""
}

// shownOn is where a row without a page of its own is shown: its table
// page, at the row, relative to the entity pages; "" for any other
// entity, and when the table page is not exported.
func (s *site) shownOn(id string) string {
	if typ := s.tableSite.noPage[id]; typ != "" && s.tableSite.tablePage[typ] {
		return typ + ".html#" + id
	}
	return ""
}

// tableHref is the table page of the rows of type typ, relative to the
// entity pages, or "" when it is not exported.
func (s *site) tableHref(typ string) string {
	if s.tableSite.tablePage[typ] {
		return typ + ".html"
	}
	return ""
}

// tableTypes are the types whose table page is exported, in name order.
func (s *site) tableTypes() []string {
	return slices.Sorted(maps.Keys(s.tableSite.tablePage))
}

// tableInputs are what a table page shows: the file, its sidecar and the
// schema of its type, and its rows; for rows with no pages of their own,
// which it shows in full, what each row's page would show.
func (s *site) tableInputs(typ string) (history.Inputs, error) {
	t := s.tableSite.tables[typ]
	paths := map[string]bool{".component": true, templatesDir + "/_layout.tmpl": true, t.Path: true}
	if t.Sidecar != "" {
		paths[t.Sidecar] = true
	}
	ids := map[string]bool{}
	types := map[string]bool{}
	for _, e := range s.tableSite.rows[typ] {
		ids[e.ID] = true
		s.schemaFiles(e.Schema, paths)
		if t.Pages {
			continue
		}
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

// tablePageData is what the built-in table page receives.
type tablePageData struct {
	Type string
	// File is the file as a whole: a row's .File without .Line.
	File tmpl.Data
	// Rows is the number of rows; RowsHTML the rows, rendered.
	Rows     int
	RowsHTML template.HTML
}

// renderTable renders the table page of typ's rows: a list of them, each
// linked to its page, or, when they have no pages, every row in full, one
// column per field and incoming facet, each at an anchor of its ID. Each
// row is rendered as the entity being rendered, so a problem in a text
// cell is placed at its line.
func (s *site) renderTable(typ string) (string, error) {
	defer s.check.pageEnd()
	s.check.pageStart()
	t := s.tableSite.tables[typ]
	rows := s.tableSite.rows[typ]
	ctx := &renderCtx{site: s, linkBase: ""}
	tp, err := template.New("").Funcs(fallbackFuncs).Funcs(ctx.funcs()).Funcs(template.FuncMap{"ref": ctx.ref}).ParseFS(templateFS,
		"templates/values.tmpl", "templates/table.tmpl")
	if err != nil {
		return "", err
	}
	var rowsHTML bytes.Buffer
	if t.Pages {
		if err := tp.ExecuteTemplate(&rowsHTML, "table-list", rows); err != nil {
			return "", templateError(err)
		}
	} else {
		var head bytes.Buffer
		var cols []string
		if len(rows) > 0 {
			for _, v := range entityView(rows[0]).Fields {
				cols = append(cols, v.Field.Name)
			}
			for _, in := range rows[0].Incoming {
				cols = append(cols, in.Facet.Name)
			}
		}
		if err := tp.ExecuteTemplate(&head, "table-head", cols); err != nil {
			return "", templateError(err)
		}
		rowsHTML.Write(head.Bytes())
		for _, e := range rows {
			ctx.chain = []*model.Entity{e}
			if err := tp.ExecuteTemplate(&rowsHTML, "table-row", entityView(e)); err != nil {
				return "", templateError(err)
			}
		}
		ctx.chain = nil
		rowsHTML.WriteString("</tbody>\n</table>\n")
	}
	var b bytes.Buffer
	d := tablePageData{Type: typ, File: tmpl.TableFile(t), Rows: len(rows), RowsHTML: template.HTML(rowsHTML.String())}
	if err := tp.ExecuteTemplate(&b, "table", d); err != nil {
		return "", templateError(err)
	}
	return splice(b.String(), ctx.fragments)
}

// tablePageTitle is the title of typ's table page.
func tablePageTitle(t *entity.Table) string {
	return fmt.Sprintf("%s (%s)", t.Type, t.Path)
}
