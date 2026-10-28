// Package loadtest is a second source of entities for tests (G6): files
// holding several entities each, whose values are typed by their field
// rather than by how they are written, so a test can show that a layer
// handles entities that are not one Markdown file each.
package loadtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/problem"
	"github.com/dynamatt/provenance/internal/schema"
)

// Rows reads every .rows file: a table of entities, one per line, its
// cells separated by |. The first line names the columns, id and type
// among them; an empty cell is not written, and a link or list cell holds
// its items separated by ;. An entity and each of its values are placed at
// its line. Blank lines are left out.
type Rows struct{}

var _ load.Source = Rows{}

const ext = ".rows"

func (Rows) Load(root string, skip []string) (*load.Loaded, error) {
	out := &load.Loaded{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(skip, p) || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ext) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out.Files = append(out.Files, rel)
		entities, fe := Parse(rel, data)
		if fe != nil {
			out.Problems = append(out.Problems, problem.Problem{Check: "content/frontmatter", Path: rel, Line: fe.Line, Msg: fe.Msg, Err: fe})
			return nil
		}
		out.Entities = append(out.Entities, entities...)
		return nil
	})
	return out, err
}

// Entities are the entities of a .rows file, each written as its line.
func (Rows) Entities(path string, data func() ([]byte, error)) ([]history.FileEntity, bool, error) {
	if !strings.HasSuffix(path, ext) {
		return nil, false, nil
	}
	b, err := data()
	if err != nil {
		return nil, true, err
	}
	entities, _ := Parse(path, b)
	lines := strings.Split(string(b), "\n")
	var out []history.FileEntity
	for _, e := range entities {
		out = append(out, history.FileEntity{ID: e.ID, Type: e.Type, Text: lines[e.IDLine()-1]})
	}
	return out, true, nil
}

// Parse reads the entities of a .rows file.
func Parse(path string, data []byte) ([]*entity.Entity, *entity.Error) {
	var header []string
	var out []*entity.Entity
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := strings.Split(line, "|")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		if header == nil {
			if !slices.Contains(cells, "id") || !slices.Contains(cells, "type") {
				return nil, &entity.Error{Path: path, Line: i + 1, Msg: "the first line names the columns, id and type among them"}
			}
			header = cells
			continue
		}
		if len(cells) != len(header) {
			return nil, &entity.Error{Path: path, Line: i + 1, Msg: "a row has one cell per column"}
		}
		r := &record{header: header, cells: cells, line: i + 1}
		out = append(out, &entity.Entity{ID: r.cell("id"), Type: r.cell("type"), Path: path, Record: r})
	}
	return out, nil
}

// record is one row.
type record struct {
	header, cells []string
	line          int
}

func (r *record) cell(name string) string {
	if i := slices.Index(r.header, name); i >= 0 {
		return r.cells[i]
	}
	return ""
}

func (r *record) Value(f *schema.Field) *entity.Written {
	text := r.cell(f.Name)
	switch {
	case text == "":
		return nil
	case f.Kind == schema.Link && f.Cardinality != "one", f.Kind == schema.List && f.Elem != nil:
		item := &schema.Field{Kind: schema.Link}
		if f.Elem != nil {
			item = f.Elem
		}
		w := &entity.Written{Kind: entity.List, Line: r.line, Source: func() string { return text }}
		for _, s := range strings.Split(text, ";") {
			w.Items = append(w.Items, r.single(item, strings.TrimSpace(s)))
		}
		return w
	}
	return r.single(f, text)
}

// single is text as a value of f's kind, or as text when it isn't one.
func (r *record) single(f *schema.Field, text string) *entity.Written {
	w := &entity.Written{Kind: entity.Text, Text: text, Line: r.line, TextLine: r.line}
	switch f.Kind {
	case schema.Number:
		if _, err := strconv.ParseFloat(text, 64); err == nil {
			w.Kind = entity.Number
		}
	case schema.Boolean:
		if text == "true" || text == "false" {
			w.Kind = entity.Boolean
		}
	}
	return w
}

// Keys are the columns the row fills.
func (r *record) Keys() []entity.Key {
	var out []entity.Key
	for i, name := range r.header {
		if r.cells[i] != "" {
			out = append(out, entity.Key{Name: name, Line: r.line, Set: true})
		}
	}
	return out
}

func (r *record) IDLine() int                       { return r.line }
func (r *record) TypeLine() int                     { return r.line }
func (r *record) Body() (string, int)               { return "", 0 }
func (r *record) Refs(string) (*entity.Refs, error) { return &entity.Refs{}, nil }
