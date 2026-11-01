// Package canon writes entity files in their canonical form (DES-0038)
// and checks that doing so changed only how a file is written: every
// value is unchanged, read back through the source, and formatting again
// changes nothing.
package canon

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

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

// Format is e's file in canonical form against its type in s, and line,
// the first line of the file as read that it changes: 0 when the file is
// already in canonical form. ok is false when e's record has no
// canonical form: only a Markdown file's has (DES-0038), not a CSV row's.
// A file already in canonical form is returned as it is; any other is
// an *UnsafeError unless it passes DES-0038's checks. e's type must be
// declared in s.
func Format(e *entity.Entity, s *schema.Schema) (out []byte, line int, ok bool, err error) {
	m, ok := e.Record.(*entity.Markdown)
	if !ok {
		return nil, 0, false, nil
	}
	t := s.Types[e.Type]
	if t == nil {
		return nil, 0, true, &model.UnknownTypeError{Path: e.Path, Type: e.Type, Line: e.Record.TypeLine()}
	}
	if out, err = m.Format(t); err != nil {
		return nil, 0, true, &UnsafeError{Path: e.Path, Msg: err.Error()}
	}
	if line = firstDiff(m.Raw(), out); line == 0 {
		return out, 0, true, nil
	}
	return out, line, true, check(e, t, s, out)
}

// firstDiff is the first line, from 1, on which a and b differ; 0 when
// they don't.
func firstDiff(a, b []byte) int {
	if bytes.Equal(a, b) {
		return 0
	}
	al, bl := bytes.SplitAfter(a, []byte("\n")), bytes.SplitAfter(b, []byte("\n"))
	for i := range min(len(al), len(bl)) {
		if !bytes.Equal(al[i], bl[i]) {
			return i + 1
		}
	}
	return min(len(al), len(bl)) + 1
}

// check is DES-0038's safety check of out, e's file formatted: read back
// through the Markdown source, every value is unchanged, no comment is
// lost, and formatting again changes nothing.
func check(e *entity.Entity, t *schema.Type, s *schema.Schema, out []byte) error {
	after, _, err := entity.ParseFile(e.Path, out)
	if err != nil || after == nil {
		return &UnsafeError{Path: e.Path, Msg: fmt.Sprintf("makes the file unreadable: %v", err)}
	}
	if e.ID != after.ID || e.Type != after.Type {
		return &UnsafeError{Path: e.Path, Field: "id or type", Msg: "not read back"}
	}
	if err := same(e.Path, s, e, after); err != nil {
		return err
	}
	if !slices.Equal(comments(e), comments(after)) {
		return &UnsafeError{Path: e.Path, Msg: "would lose a comment"}
	}
	if again, err := entity.Format(out, t); err != nil || !bytes.Equal(again, out) {
		return &UnsafeError{Path: e.Path, Msg: "again would change it"}
	}
	return nil
}

// same checks that after holds every value before does, typed against s,
// a removed empty field read as blank (DES-0010) and a folded text value
// read as it renders; and every key s doesn't declare, as written.
func same(path string, s *schema.Schema, before, after *entity.Entity) error {
	built, err := model.Build(s, []*entity.Entity{before, after})
	if err != nil {
		return err
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
	case a.Blank() && b.Blank():
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
