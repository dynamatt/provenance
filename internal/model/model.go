// Package model maps parsed entities onto their schema types: every declared
// field becomes a typed Value, in schema order.
//
// Values that do not match their declared type are kept, marked Invalid with
// their source text, rather than failing: judging content is validate's job,
// and an export must still show what the file says.
package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/schema"
)

// Entity is a parsed entity with its type and typed field values.
type Entity struct {
	*entity.Entity
	Schema *schema.Type
	// Fields holds one value per declared field, in schema order.
	Fields []*Value
	// Body is the freeform body when the type has no body field; empty
	// otherwise (the body is then the body field's value).
	Body string
	// Incoming holds one entry per facet the schema gives this type, in
	// facet name order, each listing the linking entities by ID.
	Incoming []*Incoming
}

// Incoming is the entities linking to an entity through one facet.
type Incoming struct {
	Facet *schema.Facet
	From  []*Entity
}

// Facet returns the incoming entities for the named facet, or nil.
func (e *Entity) Facet(name string) *Incoming {
	for _, in := range e.Incoming {
		if in.Facet.Name == name {
			return in
		}
	}
	return nil
}

// Field returns the value of the named field, or nil.
func (e *Entity) Field(name string) *Value {
	for _, v := range e.Fields {
		if v.Field.Name == name {
			return v
		}
	}
	return nil
}

// Title is the entity's display title: the "title" field, else the first
// string field, else "".
func (e *Entity) Title() string {
	if v := e.Field("title"); v != nil && v.Present && !v.Invalid {
		return v.Str
	}
	for _, v := range e.Fields {
		if v.Field.Kind == schema.String && v.Present && !v.Invalid {
			return v.Str
		}
	}
	return ""
}

// Value is one field's value.
type Value struct {
	Field *schema.Field
	// Present is false when the entity does not set the field (or sets it to
	// null). A calculated field is never read from the file: it is present
	// once its formula has been evaluated to a value (internal/query), and
	// Result then says what kind of value it is.
	Present bool
	Result  schema.Kind // Number, Boolean or String, for a calculated value
	// Invalid marks a value that does not match its declared type; Raw holds
	// its source text and Problem says what is wrong.
	Invalid bool
	Raw     string
	Problem string

	Str  string   // string, text, enum, date (YYYY-MM-DD), calculated text
	Num  float64  // number
	Bool bool     // boolean
	IDs  []string // link targets; one entry for cardinality one
	// Targets parallels IDs: the linked entity, or nil when no entity has
	// that ID (unresolved; reporting it is validate's job).
	Targets []*Entity
	Rows    []Row    // a list's rows, when it has sub-fields
	Items   []*Value // a list's items, when it names an item type (Field.Elem)

	// Written is what the entity's record writes for the field, and
	// where; nil when it writes nothing, and for a calculated value.
	Written *entity.Written
}

// Row is one list row: a value per sub-field, in schema order.
type Row []*Value

// UnknownTypeError reports an entity whose type has no schema.
type UnknownTypeError struct {
	Path, Type string
	Line       int
}

func (e *UnknownTypeError) Error() string {
	return fmt.Sprintf("%s:%d: type %q is not declared in schema/", e.Path, e.Line, e.Type)
}

// Build types every entity. An entity whose type has no schema cannot be
// interpreted, so it fails the build rather than being silently left out.
func Build(s *schema.Schema, entities []*entity.Entity) ([]*Entity, error) {
	out := make([]*Entity, 0, len(entities))
	for _, e := range entities {
		t := s.Types[e.Type]
		if t == nil {
			return nil, &UnknownTypeError{Path: e.Path, Type: e.Type, Line: e.Record.TypeLine()}
		}
		m := &Entity{Entity: e, Schema: t}
		body, _ := e.Record.Body()
		for _, f := range t.Fields {
			var v *Value
			if f == t.BodyField {
				v = bodyValue(f, body)
			} else {
				v = value(f, e.Record.Value(f))
			}
			m.Fields = append(m.Fields, v)
		}
		if t.BodyField == nil {
			m.Body = bodyText(body)
		}
		out = append(out, m)
	}
	link(out)
	return out, nil
}

// link resolves every link value to its target entity and fills each
// entity's incoming facets. entities are sorted by ID, so every From list
// is too.
func link(entities []*Entity) {
	byID := make(map[string]*Entity, len(entities))
	for _, e := range entities {
		byID[e.ID] = e
		for _, f := range e.Schema.Facets {
			e.Incoming = append(e.Incoming, &Incoming{Facet: f})
		}
	}
	for _, src := range entities {
		for _, v := range src.Fields {
			switch v.Field.Kind {
			case schema.Link:
				resolve(byID, src, v)
			case schema.List:
				for _, row := range v.Rows {
					for _, cell := range row {
						if cell.Field.Kind == schema.Link {
							resolve(byID, src, cell)
						}
					}
				}
			}
		}
	}
}

// resolve fills v.Targets and records src on each target's reverse facet.
func resolve(byID map[string]*Entity, src *Entity, v *Value) {
	if v.Invalid {
		return
	}
	v.Targets = make([]*Entity, len(v.IDs))
	for i, id := range v.IDs {
		target := byID[id]
		v.Targets[i] = target
		if target == nil || v.Field.ReverseName == "" {
			continue
		}
		in := target.Facet(v.Field.ReverseName)
		if in == nil {
			continue // target's type has no such facet: a wrong target type, left to validate
		}
		if n := len(in.From); n == 0 || in.From[n-1] != src {
			in.From = append(in.From, src)
		}
	}
}

// BodyText is the body as rendered: the body field's text, or the freeform
// body.
func (e *Entity) BodyText() string {
	if f := e.Schema.BodyField; f != nil {
		return e.Field(f.Name).Str
	}
	return e.Body
}

// BodyFileLine maps a line of BodyText (from 1) to its line in the file.
// The rendered body has leading blank lines trimmed (bodyText).
func (e *Entity) BodyFileLine(line int) int {
	body, first := e.Record.Body()
	raw := strings.ReplaceAll(body, "\r\n", "\n")
	lead := len(raw) - len(strings.TrimLeft(raw, "\n"))
	return first + lead + line - 1
}

// TextFileLine maps a line (from 1) of Markdown text rendered from e, its
// body, a text field or a text sub-field or item of a list, to its line in
// the file; 0 when text is none of them.
func (e *Entity) TextFileLine(text string, line int) int {
	if text == e.BodyText() {
		return e.BodyFileLine(line)
	}
	isText := func(v *Value) bool {
		return v.Present && !v.Invalid && v.Field.Kind == schema.Text && v.Str == text
	}
	for _, v := range e.Fields {
		switch {
		case isText(v):
			return textLine(v.Written, line)
		case v.Field.Kind != schema.List || !v.Present || v.Invalid:
		case v.Field.Elem != nil:
			for _, item := range v.Items {
				if isText(item) {
					return textLine(item.Written, line)
				}
			}
		default:
			for _, row := range v.Rows {
				for _, cell := range row {
					if isText(cell) {
						return textLine(cell.Written, line)
					}
				}
			}
		}
	}
	return 0
}

// textLine maps line (from 1) of the text written as w to its file line.
func textLine(w *entity.Written, line int) int {
	if w == nil || w.TextLine == 0 {
		return 0
	}
	return w.TextLine + line - 1
}

func bodyValue(f *schema.Field, body string) *Value {
	text := bodyText(body)
	return &Value{Field: f, Present: strings.TrimSpace(text) != "", Str: text}
}

// bodyText normalizes line endings to LF and drops surrounding blank lines.
// A Windows checkout with core.autocrlf has CRLF bodies for the same commit;
// normalizing keeps rendered output identical on every platform.
func bodyText(body string) string {
	return strings.Trim(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
}

func value(f *schema.Field, w *entity.Written) *Value {
	v := &Value{Field: f}
	if f.Kind == schema.Calculated || w == nil {
		return v
	}
	v.Written = w
	if w.Kind == entity.Null {
		return v
	}
	v.Present = true
	invalid := func(problem string) *Value {
		v.Invalid, v.Problem = true, problem
		v.Raw = w.SourceText()
		return v
	}

	switch f.Kind {
	case schema.String, schema.Text, schema.Enum:
		if w.Kind != entity.Text {
			return invalid("not text")
		}
		v.Str = w.Text
	case schema.Number:
		if w.Kind != entity.Number {
			return invalid("not a number")
		}
		num, err := strconv.ParseFloat(w.Text, 64)
		if err != nil {
			return invalid("not a number")
		}
		v.Num = num
	case schema.Boolean:
		if w.Kind != entity.Boolean {
			return invalid("not true or false")
		}
		v.Bool = w.Text == "true"
	case schema.Date:
		if !single(w) {
			return invalid("not a date")
		}
		if _, err := time.Parse("2006-01-02", w.Text); err != nil {
			return invalid("not a date (YYYY-MM-DD)")
		}
		v.Str = w.Text
	case schema.Link:
		ids, ok := linkIDs(f, w)
		if !ok {
			if f.Cardinality == "one" {
				return invalid("not a single ID")
			}
			return invalid("not a list of IDs")
		}
		v.IDs = ids
	case schema.List:
		if f.Elem != nil {
			if w.Kind != entity.List {
				return invalid("not a list")
			}
			// An item that does not match the item type is marked on the
			// item, like a cell in a row.
			for _, item := range w.Items {
				v.Items = append(v.Items, value(f.Elem, item))
			}
			break
		}
		if w.Kind != entity.List {
			return invalid("not a list of rows")
		}
		for _, item := range w.Items {
			if item.Kind != entity.Mapping {
				return invalid("not a list of rows")
			}
			row := make(Row, 0, len(f.Fields))
			for _, sub := range f.Fields {
				row = append(row, value(sub, item.Row.Value(sub)))
			}
			v.Rows = append(v.Rows, row)
		}
	}
	return v
}

// single reports a value written as one value, of any kind.
func single(w *entity.Written) bool {
	switch w.Kind {
	case entity.Text, entity.Number, entity.Boolean, entity.Scalar, entity.Null:
		return true
	}
	return false
}

func linkIDs(f *schema.Field, w *entity.Written) ([]string, bool) {
	isID := func(w *entity.Written) bool { return w.Kind == entity.Text && w.Text != "" }
	if f.Cardinality == "one" {
		if !isID(w) {
			return nil, false
		}
		return []string{w.Text}, true
	}
	if w.Kind != entity.List {
		return nil, false
	}
	ids := make([]string, 0, len(w.Items))
	for _, c := range w.Items {
		if !isID(c) {
			return nil, false
		}
		ids = append(ids, c.Text)
	}
	return ids, true
}
