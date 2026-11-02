package entity

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/dynamatt/provenance/internal/problem"
	"github.com/dynamatt/provenance/internal/schema"
)

// Table is a CSV file a type's entities are read from, one per row, and
// what is known of where it came from (DES-0053).
type Table struct {
	// Type is the type of its rows; Path the file, slash-separated and
	// relative to the repository root.
	Type string
	Path string
	// System is the system it is a dump of, "" for a table kept by hand.
	System string
	// Sidecar is the file beside a dump saying when it was exported, ""
	// for a table kept by hand; Exported and SHA256 are what it says, ""
	// when it can't be read.
	Sidecar          string
	Exported, SHA256 string
	// Pages is false when the rows get no page of their own.
	Pages bool
}

// Row is the record of one CSV row (DES-0053): each cell typed by its
// field, as an entity file's value of that field would be read.
type Row struct {
	table *Table
	t     *schema.Type
	src   *schema.Source
	// cells are the row's cells by header; lines the file line each cell
	// starts on.
	cells map[string]string
	lines map[string]int
	line  int
}

// Table is the file the row is read from.
func (r *Row) Table() *Table { return r.table }

func (r *Row) IDLine() int         { return r.line }
func (r *Row) TypeLine() int       { return r.line }
func (r *Row) Body() (string, int) { return "", 0 }

// Refs parses a text cell as Markdown, as any text value is.
func (r *Row) Refs(text string) (*Refs, error) { return MarkdownRefs(r.table.Path, text) }

// Keys are the fields the row fills, in schema order. The columns no field
// reads are reported once for the file (content/csv-column), not per row.
func (r *Row) Keys() []Key {
	var out []Key
	for _, f := range r.t.Fields {
		if f.Kind == schema.Calculated || f.Body {
			continue
		}
		if text, ok := r.cells[f.ColumnName()]; ok && text != "" {
			out = append(out, Key{Name: f.Name, Line: r.lines[f.ColumnName()], Set: true})
		}
	}
	return out
}

// Value is the cell of f's column, nil when it is empty or there is no such
// column: an empty cell is a field not written.
func (r *Row) Value(f *schema.Field) *Written {
	if f.Kind == schema.Calculated {
		return nil
	}
	col := f.ColumnName()
	text := r.cells[col]
	if text == "" {
		return nil
	}
	line := r.lines[col]
	many := f.Kind == schema.Link && f.Cardinality == "many" || f.Kind == schema.List && f.Elem != nil
	if !many {
		return cell(f, text, line)
	}
	item := f.Elem
	if item == nil {
		item = &schema.Field{Kind: schema.Link}
	}
	w := &Written{Kind: List, Line: line, TextLine: line, Source: func() string { return text }}
	for _, s := range strings.Split(text, r.src.Separator) {
		// An empty item is left out, as YAML leaves out a flow list's
		// trailing comma.
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		w.Items = append(w.Items, cell(item, s, line))
	}
	return w
}

// number is a number as an entity file writes one: decimal, with an
// optional sign, fraction and exponent; not hex, octal, infinity, NaN or
// digit separators.
var number = regexp.MustCompile(`^[-+]?(\d+(\.\d*)?|\.\d+)([eE][-+]?\d+)?$`)

// cell is text as a single value of f's kind: what YAML would read it as
// in an entity file, so the typing rules every source shares (model) give
// it the same meaning. Text that is not of f's kind is text, which model
// reports as not of its type.
func cell(f *schema.Field, text string, line int) *Written {
	w := &Written{Kind: Text, Text: text, Line: line, TextLine: line}
	switch f.Kind {
	case schema.Number:
		if number.MatchString(text) {
			w.Kind = Number
		}
	case schema.Boolean:
		switch text {
		case "true", "True", "TRUE":
			w.Kind, w.Text = Boolean, "true"
		case "false", "False", "FALSE":
			w.Kind, w.Text = Boolean, "false"
		}
		if w.Kind == Boolean {
			w.Source = func() string { return text }
		}
	}
	return w
}

// CSV is a CSV file read (DES-0053): its rows, and the problems found.
type CSV struct {
	Rows     []*Entity
	Problems []problem.Problem
	// Header is the file's first row.
	Header []string
}

// ParseCSV reads data, the CSV file of table, whose rows are entities of
// type t: RFC 4180 with a header row, UTF-8 (a BOM is ignored), strictly
// (C11): a row with the wrong number of cells is a content/csv-row problem
// at its line, and a quote out of place stops the file there. A row
// without an ID is left out. Columns the type doesn't read are
// content/csv-column warnings, at the header.
func ParseCSV(table *Table, t *schema.Type, data []byte) *CSV {
	out := &CSV{}
	src := t.Source
	path := table.Path
	fail := func(check string, line int, warning bool, msg string, args ...any) {
		p := problem.Problem{Check: check, Warning: warning, Path: path, Line: line, Msg: fmt.Sprintf(msg, args...)}
		if !warning {
			p.Err = &Error{Path: path, Line: line, Msg: p.Msg}
		}
		out.Problems = append(out.Problems, p)
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(data) {
		line := bytes.Count(data[:invalidUTF8(data)], []byte("\n")) + 1
		fail("content/csv-row", line, false, "the file is not UTF-8")
		return out
	}
	rd := csv.NewReader(bytes.NewReader(data))
	rd.Comma = src.Delimiter
	header, err := rd.Read()
	if errors.Is(err, io.EOF) {
		fail("content/csv-row", 1, false, "the file is empty: it has no header row")
		return out
	}
	if err != nil {
		fail("content/csv-row", csvLine(err), false, "%s", csvMsg(err))
		return out
	}
	out.Header = header
	seen := map[string]bool{}
	for _, h := range header {
		if seen[h] {
			fail("content/csv-row", 1, false, "column %q is in the header twice", h)
			return out
		}
		seen[h] = true
	}
	if !seen[src.ID] {
		fail("content/csv-row", 1, false, "no column %q, the ID column of a %s (source id:)", src.ID, t.Name)
		return out
	}
	read := map[string]bool{src.ID: true}
	for _, f := range t.Fields {
		if f.Kind == schema.Calculated || f.Body || f.Kind == schema.List && f.Elem == nil {
			continue
		}
		read[f.ColumnName()] = true
		if !seen[f.ColumnName()] {
			fail("content/csv-column", 1, true, "no column %q for the field %s of a %s, so it is empty on every row", f.ColumnName(), f.Name, t.Name)
		}
	}
	for _, h := range header {
		if !read[h] {
			fail("content/csv-column", 1, true, "column %q is not read: no field of a %s names it", h, t.Name)
		}
	}

	for {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		var pe *csv.ParseError
		if errors.As(err, &pe) && errors.Is(pe.Err, csv.ErrFieldCount) {
			fail("content/csv-row", pe.StartLine, false, "the row has %d cells, but the header has %d", len(rec), len(header))
			continue
		}
		if err != nil {
			fail("content/csv-row", csvLine(err), false, "%s; the rows after it are not read", csvMsg(err))
			break
		}
		line, _ := rd.FieldPos(0)
		r := &Row{table: table, t: t, src: src, cells: map[string]string{}, lines: map[string]int{}, line: line}
		for i, h := range header {
			r.cells[h] = rec[i]
			r.lines[h], _ = rd.FieldPos(i)
		}
		id := strings.TrimSpace(r.cells[src.ID])
		if id == "" {
			fail("content/csv-row", line, false, "the row has no ID in column %q", src.ID)
			continue
		}
		out.Rows = append(out.Rows, &Entity{ID: id, Type: t.Name, Path: path, Record: r})
	}
	return out
}

// RowText is what the file writes for a row, for history to compare two
// versions of it (DES-0023): the ID and the cells its fields read, by
// field, so a dump that reorders or adds columns changes no row.
func RowText(e *Entity) string {
	r, ok := e.Record.(*Row)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(e.ID)
	b.WriteByte('\n')
	for _, f := range r.t.Fields {
		if text, ok := r.cells[f.ColumnName()]; ok && f.Kind != schema.Calculated {
			fmt.Fprintf(&b, "%s\x00%s\n", f.Name, text)
		}
	}
	return b.String()
}

func csvLine(err error) int {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return pe.Line
	}
	return 0
}

func csvMsg(err error) string {
	var pe *csv.ParseError
	if !errors.As(err, &pe) {
		return err.Error()
	}
	switch {
	case errors.Is(pe.Err, csv.ErrBareQuote):
		return "a quote inside a cell that is not quoted (quote the cell, and double the quote)"
	case errors.Is(pe.Err, csv.ErrQuote):
		return "a quoted cell is not closed, or a quote follows its closing quote"
	}
	return pe.Err.Error()
}

// invalidUTF8 is the offset of the first byte of data that is not UTF-8.
func invalidUTF8(data []byte) int {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return len(data)
}

var _ Record = (*Row)(nil)
