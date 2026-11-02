package load

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/problem"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// csvSource reads the CSV files the schema's types name as their source
// (DES-0053): each row an entity, typed by its field as an entity file's
// value is.
type csvSource struct{ s *schema.Schema }

func (c csvSource) table(t *schema.Type) *entity.Table {
	src := t.Source
	table := &entity.Table{Type: t.Name, Path: src.CSV, System: src.System, Pages: src.Pages}
	if src.Dump() {
		table.Sidecar = src.Sidecar()
	}
	return table
}

// Load reads every source's file, in type name order. A file that is
// missing is a content/csv-row problem at the schema's source:; a dump's
// sidecar is checked against its file (content/csv-sidecar).
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
		if table.Sidecar != "" {
			problems, exists, err := sidecar(root, table, data)
			if err != nil {
				return nil, err
			}
			if exists {
				out.Files = append(out.Files, table.Sidecar)
			}
			out.Problems = append(out.Problems, problems...)
		}
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
// as the cells its fields read (entity.RowText); a sidecar holds none.
func (c csvSource) Entities(path string, data func() ([]byte, error)) ([]history.FileEntity, bool, error) {
	for _, t := range c.s.SourceTypes() {
		switch path {
		case t.Source.CSV:
			b, err := data()
			if err != nil {
				return nil, true, err
			}
			var out []history.FileEntity
			for _, e := range entity.ParseCSV(c.table(t), t, b).Rows {
				out = append(out, history.FileEntity{ID: e.ID, Type: e.Type, Text: entity.RowText(e)})
			}
			return out, true, nil
		case t.Source.Sidecar():
			if t.Source.Dump() {
				return nil, true, nil
			}
		}
	}
	return nil, false, nil
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// sidecar checks a dump's sidecar (C9): it exists, says when the dump was
// exported, and holds the hash of the file beside it, so a new dump can't
// be committed without saying when it was exported. A file checked out
// with CRLF line endings matches the hash of the file as committed with
// LF. exists reports whether there is a sidecar file.
func sidecar(root string, table *entity.Table, csv []byte) (problems []problem.Problem, exists bool, err error) {
	at := func(path string, line int, msg string, args ...any) {
		m := fmt.Sprintf(msg, args...)
		problems = append(problems, problem.Problem{Check: "content/csv-sidecar", Path: path, Line: line, Msg: m,
			Err: &entity.Error{Path: path, Line: line, Msg: m}})
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(table.Sidecar)))
	if errors.Is(err, fs.ErrNotExist) {
		at(table.Path, 0, "no sidecar %s saying when this dump of %s was exported: write one with exported: (YYYY-MM-DD) and sha256: (the file's hash, as sha256sum prints it)", table.Sidecar, table.System)
		return problems, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var doc yaml.Node
	if err := yamlnode.Unmarshal(raw, 1, &doc); err != nil {
		line, msg := yamlnode.ErrorLine(err)
		at(table.Sidecar, line, "%s", msg)
		return problems, true, nil
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		at(table.Sidecar, 1, "a sidecar is a mapping with exported: and sha256:")
		return problems, true, nil
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Value != "exported" && k.Value != "sha256" {
			at(table.Sidecar, k.Line, "unknown key %q (expected exported, sha256)", k.Value)
		}
	}
	if n := yamlnode.Lookup(m, "exported"); n == nil {
		at(table.Sidecar, m.Line, "no exported: the date the dump was exported, YYYY-MM-DD")
	} else if _, err := time.Parse(schema.DateLayout, n.Value); err != nil || n.Kind != yaml.ScalarNode {
		at(table.Sidecar, n.Line, "exported: %q is not a date (YYYY-MM-DD)", n.Value)
	} else {
		table.Exported = n.Value
	}
	n := yamlnode.Lookup(m, "sha256")
	switch {
	case n == nil:
		at(table.Sidecar, m.Line, "no sha256: the hash of %s, as sha256sum prints it", table.Path)
	case n.Kind != yaml.ScalarNode || !sha256Hex.MatchString(n.Value):
		at(table.Sidecar, n.Line, "sha256: %q is not a SHA-256 hash (64 lower-case hex digits)", n.Value)
	case !slices.Contains(hashes(csv), n.Value):
		at(table.Sidecar, n.Line, "sha256: %s is not the hash of %s (%s): the sidecar describes another dump", n.Value, table.Path, hashes(csv)[0])
	default:
		table.SHA256 = n.Value
	}
	return problems, true, nil
}

// hashes are the SHA-256 of data as read, and, when it has CRLF line
// endings, of data with LF ones, as git commits a file a core.autocrlf
// checkout writes with CRLF.
func hashes(data []byte) []string {
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	out := []string{sum(data)}
	if bytes.Contains(data, []byte("\r\n")) {
		out = append(out, sum(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))))
	}
	return out
}
