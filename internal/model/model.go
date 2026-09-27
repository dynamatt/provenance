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

	"go.yaml.in/yaml/v3"

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
	// null). Calculated fields are never present: they are never stored.
	Present bool
	// Invalid marks a value that does not match its declared type; Raw holds
	// its source text and Problem says what is wrong.
	Invalid bool
	Raw     string
	Problem string

	Str  string   // string, text, enum, date (YYYY-MM-DD)
	Num  float64  // number
	Bool bool     // boolean
	IDs  []string // link targets; one entry for cardinality one
	Rows []Row    // list rows
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
			return nil, &UnknownTypeError{Path: e.Path, Type: e.Type, Line: entity.Lookup(e.Front, "type").Line}
		}
		m := &Entity{Entity: e, Schema: t}
		for _, f := range t.Fields {
			var v *Value
			if f == t.BodyField {
				v = bodyValue(f, e.Body)
			} else {
				v = value(f, entity.Lookup(e.Front, f.Name))
			}
			m.Fields = append(m.Fields, v)
		}
		if t.BodyField == nil {
			m.Body = bodyText(e.Body)
		}
		out = append(out, m)
	}
	return out, nil
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

func value(f *schema.Field, n *yaml.Node) *Value {
	v := &Value{Field: f}
	if f.Kind == schema.Calculated || n == nil || n.Tag == "!!null" {
		return v
	}
	v.Present = true
	invalid := func(problem string) *Value {
		v.Invalid, v.Problem = true, problem
		v.Raw = source(n)
		return v
	}

	switch f.Kind {
	case schema.String, schema.Text, schema.Enum:
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
			return invalid("not text")
		}
		v.Str = n.Value
	case schema.Number:
		if n.Kind != yaml.ScalarNode || (n.Tag != "!!int" && n.Tag != "!!float") {
			return invalid("not a number")
		}
		num, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64)
		if err != nil {
			return invalid("not a number")
		}
		v.Num = num
	case schema.Boolean:
		if n.Kind != yaml.ScalarNode || n.Tag != "!!bool" {
			return invalid("not true or false")
		}
		v.Bool = n.Value == "true" || n.Value == "True" || n.Value == "TRUE"
	case schema.Date:
		if n.Kind != yaml.ScalarNode {
			return invalid("not a date")
		}
		if _, err := time.Parse("2006-01-02", n.Value); err != nil {
			return invalid("not a date (YYYY-MM-DD)")
		}
		v.Str = n.Value
	case schema.Link:
		ids, ok := linkIDs(f, n)
		if !ok {
			if f.Cardinality == "one" {
				return invalid("not a single ID")
			}
			return invalid("not a list of IDs")
		}
		v.IDs = ids
	case schema.List:
		if n.Kind != yaml.SequenceNode {
			return invalid("not a list of rows")
		}
		for _, rowNode := range n.Content {
			if rowNode.Kind != yaml.MappingNode {
				return invalid("not a list of rows")
			}
			row := make(Row, 0, len(f.Fields))
			for _, sub := range f.Fields {
				row = append(row, value(sub, entity.Lookup(rowNode, sub.Name)))
			}
			v.Rows = append(v.Rows, row)
		}
	}
	return v
}

func linkIDs(f *schema.Field, n *yaml.Node) ([]string, bool) {
	isID := func(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!str" && n.Value != "" }
	if f.Cardinality == "one" {
		if !isID(n) {
			return nil, false
		}
		return []string{n.Value}, true
	}
	if n.Kind != yaml.SequenceNode {
		return nil, false
	}
	ids := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		if !isID(c) {
			return nil, false
		}
		ids = append(ids, c.Value)
	}
	return ids, true
}

// source renders a node back to YAML text for display.
func source(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	b, err := yaml.Marshal(n)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
