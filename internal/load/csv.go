package load

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/problem"
	"github.com/dynamatt/provenance/internal/schema"
)

// csvSource reads the CSV files the schema's types name as their source
// (DES-0053): each row an entity, typed by its field as an entity file's
// value is.
type csvSource struct{ s *schema.Schema }

func (c csvSource) table(t *schema.Type) *entity.Table {
	src := t.Source
	return &entity.Table{Type: t.Name, Path: src.CSV, Pages: src.Pages}
}

// Load reads every source's file, in type name order. A file that is
// missing is a content/csv-row problem at the schema's source:.
func (c csvSource) Load(root string, _ []string) (*Loaded, error) {
	out := &Loaded{}
	for _, t := range c.s.SourceTypes() {
		table := c.table(t)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(table.Path)))
		if errors.Is(err, fs.ErrNotExist) {
			msg := fmt.Sprintf("no file %s, the source of type %s", table.Path, t.Name)
			out.Problems = append(out.Problems, problem.Problem{Check: "content/csv-row", Path: t.File, Line: t.Source.Line, Msg: msg,
				Err: &entity.Error{Path: t.File, Line: t.Source.Line, Msg: msg}})
			continue
		}
		if err != nil {
			return nil, err
		}
		out.Files = append(out.Files, table.Path)
		out.Tables = append(out.Tables, table)
		parsed := entity.ParseCSV(table, t, data)
		out.Problems = append(out.Problems, parsed.Problems...)
		// A second row with an ID the file already holds is not an
		// entity; every row holding it is a problem, as for Markdown.
		byID := map[string]*entity.Entity{}
		copies := map[string][]*entity.DuplicateIDError{}
		var dupIDs []string
		for _, e := range parsed.Rows {
			prev := byID[e.ID]
			if prev == nil {
				byID[e.ID] = e
				out.Entities = append(out.Entities, e)
				continue
			}
			if copies[e.ID] == nil {
				dupIDs = append(dupIDs, e.ID)
			}
			copies[e.ID] = append(copies[e.ID], &entity.DuplicateIDError{ID: e.ID, First: prev.Path, Then: e.Path, FirstLine: prev.IDLine(), ThenLine: e.IDLine()})
		}
		for _, id := range dupIDs {
			out.Problems = append(out.Problems, duplicates(copies[id])...)
		}
	}
	return out, nil
}

// Entities are the rows of path when it is a source's file, each written
// as the cells its fields read (entity.RowText).
func (c csvSource) Entities(path string, data func() ([]byte, error)) ([]history.FileEntity, bool, error) {
	for _, t := range c.s.SourceTypes() {
		if path == t.Source.CSV {
			b, err := data()
			if err != nil {
				return nil, true, err
			}
			var out []history.FileEntity
			for _, e := range entity.ParseCSV(c.table(t), t, b).Rows {
				out = append(out, history.FileEntity{ID: e.ID, Type: e.Type, Text: entity.RowText(e)})
			}
			return out, true, nil
		}
	}
	return nil, false, nil
}
