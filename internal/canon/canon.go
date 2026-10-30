// Package canon writes entity files in their canonical form (DES-0038)
// and checks that doing so changed only how a file is written: every
// value is unchanged, read back through the source, and formatting again
// changes nothing.
package canon

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// UnreadableError is a file fmt can't put in canonical form: its
// frontmatter doesn't parse, or its type isn't declared. Validate reports
// it; fmt skips it.
type UnreadableError struct {
	Path string
	Line int
	Msg  string
}

func (e *UnreadableError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return e.Path + ": " + e.Msg
}

// UnsafeError is a file whose canonical form would change a value, or
// would change again when formatted: a fault in fmt, which writes nothing.
type UnsafeError struct {
	Path string
	// Field is the field whose value would change, "" for the body or a
	// second formatting.
	Field string
	Msg   string
}

func (e *UnsafeError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: formatting would change %s: %s", e.Path, e.Field, e.Msg)
	}
	return fmt.Sprintf("%s: formatting %s", e.Path, e.Msg)
}

// File is raw, the Markdown entity file at path, in canonical form against
// s. It is an *UnreadableError when the file can't be formatted, and an
// *UnsafeError when its canonical form fails DES-0038's checks.
func File(path string, raw []byte, s *schema.Schema) ([]byte, error) {
	before, t, err := parse(path, raw, s)
	if err != nil {
		return nil, err
	}
	out, err := before.Record.(*entity.Markdown).Format(t)
	if err != nil {
		return nil, &UnreadableError{Path: path, Msg: err.Error()}
	}
	after, _, err := parse(path, out, s)
	if err != nil {
		return nil, &UnsafeError{Path: path, Msg: "makes the file unreadable: " + err.Error()}
	}
	if before.ID != after.ID || before.Type != after.Type {
		return nil, &UnsafeError{Path: path, Field: "id or type", Msg: "not read back"}
	}
	if err := same(path, s, before, after); err != nil {
		return nil, err
	}
	if !slices.Equal(comments(before), comments(after)) {
		return nil, &UnsafeError{Path: path, Msg: "would lose a comment"}
	}
	again, err := entity.Format(out, t)
	if err != nil || !bytes.Equal(again, out) {
		return nil, &UnsafeError{Path: path, Msg: "again would change it"}
	}
	return out, nil
}

// parse reads raw as the Markdown source does, with its declared type.
func parse(path string, raw []byte, s *schema.Schema) (*entity.Entity, *schema.Type, error) {
	e, partial, err := entity.ParseFile(path, raw)
	switch {
	case err != nil:
		var fe *entity.Error
		if errors.As(err, &fe) {
			return nil, nil, &UnreadableError{Path: path, Line: fe.Line, Msg: fe.Msg}
		}
		return nil, nil, &UnreadableError{Path: path, Msg: err.Error()}
	case partial != nil:
		return nil, nil, &UnreadableError{Path: path, Line: partial.Line, Msg: partial.Msg}
	case e == nil:
		return nil, nil, &UnreadableError{Path: path, Msg: "not an entity file"}
	}
	t := s.Types[e.Type]
	if t == nil {
		return nil, nil, &UnreadableError{Path: path, Line: e.Record.TypeLine(), Msg: fmt.Sprintf("type %q is not declared in schema/", e.Type)}
	}
	return e, t, nil
}

// same checks that after holds every value before does, typed against s,
// a removed empty field read as blank (DES-0010) and a folded text value
// read as it renders; and every key s doesn't declare, as written.
func same(path string, s *schema.Schema, before, after *entity.Entity) error {
	built, err := model.Build(s, []*entity.Entity{before, after})
	if err != nil {
		return &UnreadableError{Path: path, Msg: err.Error()}
	}
	a, b := built[0], built[1]
	for i, v := range a.Fields {
		if msg := differ(v, b.Fields[i]); msg != "" {
			return &UnsafeError{Path: path, Field: v.Field.Name, Msg: msg}
		}
	}
	if a.Body != b.Body {
		return &UnsafeError{Path: path, Field: "the body", Msg: "its text differs"}
	}
	declared := map[string]bool{"id": true, "type": true}
	for _, f := range a.Schema.Fields {
		declared[f.Name] = true
	}
	for _, k := range before.Record.Keys() {
		if declared[k.Name] {
			continue
		}
		f := &schema.Field{Name: k.Name}
		if w, w2 := before.Record.Value(f), after.Record.Value(f); w2 == nil || w.SourceText() != w2.SourceText() {
			return &UnsafeError{Path: path, Field: k.Name, Msg: "an undeclared key not kept as written"}
		}
	}
	return nil
}

// differ says how b differs from a, "" when it doesn't.
func differ(a, b *model.Value) string {
	switch {
	case blank(a) && blank(b):
		return ""
	case a.Present != b.Present:
		return "set in one and not the other"
	case a.Invalid || b.Invalid:
		if a.Invalid != b.Invalid || a.Raw != b.Raw {
			return "a value not of its type, rewritten"
		}
		return ""
	case a.Str != b.Str && !(a.Field.Kind == schema.Text && entity.Fold(b.Str) == a.Str):
		return fmt.Sprintf("%q becomes %q", a.Str, b.Str)
	case a.Num != b.Num || a.Bool != b.Bool:
		return "a different value"
	case !slices.Equal(a.IDs, b.IDs):
		return fmt.Sprintf("links %s become %s", strings.Join(a.IDs, ", "), strings.Join(b.IDs, ", "))
	case len(a.Items) != len(b.Items) || len(a.Rows) != len(b.Rows):
		return "a different number of items"
	}
	for i := range a.Items {
		if msg := differ(a.Items[i], b.Items[i]); msg != "" {
			return fmt.Sprintf("item %d: %s", i+1, msg)
		}
	}
	for i := range a.Rows {
		for j := range a.Rows[i] {
			if msg := differ(a.Rows[i][j], b.Rows[i][j]); msg != "" {
				return fmt.Sprintf("row %d, %s: %s", i+1, a.Rows[i][j].Field.Name, msg)
			}
		}
	}
	return ""
}

// blank is a value validate reads as not set.
func blank(v *model.Value) bool {
	if !v.Present {
		return true
	}
	if v.Invalid {
		return false
	}
	switch v.Field.Kind {
	case schema.String, schema.Text, schema.Enum:
		return strings.TrimSpace(v.Str) == ""
	case schema.Link:
		return len(v.IDs) == 0
	case schema.List:
		return len(v.Rows) == 0 && len(v.Items) == 0
	}
	return false
}

// comments are the comments of e's frontmatter, sorted: fmt moves a
// comment with its key and drops none.
func comments(e *entity.Entity) []string {
	var out []string
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			for _, l := range strings.Split(c, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					out = append(out, l)
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(e.Record.(*entity.Markdown).Front)
	slices.Sort(out)
	return out
}
