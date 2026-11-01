package canon

import (
	"errors"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

var s = &schema.Schema{Types: map[string]*schema.Type{"Widget": {Name: "Widget", Fields: []*schema.Field{
	{Name: "title", Kind: schema.String},
	{Name: "notes", Kind: schema.Text},
	{Name: "uses", Kind: schema.Link, Cardinality: "many"},
}}}}

func parse(t *testing.T, in string) *entity.Entity {
	t.Helper()
	e, err := entity.Parse("W-1.md", []byte(in))
	if err != nil || e == nil {
		t.Fatalf("parse: %v", err)
	}
	return e
}

func TestFormat(t *testing.T) {
	in := "---\nuses: [W-2]\nid: W-1\ntype: Widget\n# kept\nnotes: >-\n  one\n  two\ntitle:\n---\nBody\n"
	want := "---\nid: W-1\ntype: Widget\n# kept\nnotes: |-\n  one\n  two\nuses:\n  - W-2\n---\nBody\n"
	got, line, ok, err := Format(parse(t, in), s)
	if err != nil || !ok || line != 2 || string(got) != want {
		t.Errorf("got line %d ok %v err %v:\n%s\nwant:\n%s", line, ok, err, got, want)
	}
	if _, line, _, err := Format(parse(t, want), s); line != 0 || err != nil {
		t.Errorf("canonical file: line %d, err %v", line, err)
	}
}

// Only a Markdown record has a canonical form, and only against a
// declared type.
func TestFormatNot(t *testing.T) {
	if _, _, ok, _ := Format(&entity.Entity{ID: "W-1", Type: "Widget"}, s); ok {
		t.Error("a record that isn't Markdown has a canonical form")
	}
	_, _, _, err := Format(parse(t, "---\nid: W-1\ntype: Gadget\n---\n"), s)
	var u *model.UnknownTypeError
	if !errors.As(err, &u) {
		t.Errorf("an undeclared type: got %v", err)
	}
}

// differ allows only what DES-0038 allows: a removed empty field, and a
// folded text value's line breaks.
func TestDiffer(t *testing.T) {
	text, str := &schema.Field{Name: "notes", Kind: schema.Text}, &schema.Field{Name: "title", Kind: schema.String}
	links := &schema.Field{Name: "uses", Kind: schema.Link, Cardinality: "many"}
	for _, c := range []struct {
		name string
		a, b *model.Value
		same bool
	}{
		{"folded text", &model.Value{Field: text, Present: true, Str: "one two"}, &model.Value{Field: text, Present: true, Str: "one\ntwo"}, true},
		{"string breaks", &model.Value{Field: str, Present: true, Str: "one two"}, &model.Value{Field: str, Present: true, Str: "one\ntwo"}, false},
		{"empty removed", &model.Value{Field: str, Present: true, Str: " "}, &model.Value{Field: str}, true},
		{"value removed", &model.Value{Field: str, Present: true, Str: "x"}, &model.Value{Field: str}, false},
		{"links reordered", &model.Value{Field: links, Present: true, IDs: []string{"A", "B"}}, &model.Value{Field: links, Present: true, IDs: []string{"B", "A"}}, false},
		{"invalid rewritten", &model.Value{Field: str, Present: true, Invalid: true, Raw: "1"}, &model.Value{Field: str, Present: true, Invalid: true, Raw: "'1'"}, false},
	} {
		if got := differ(c.a, c.b) == ""; got != c.same {
			t.Errorf("%s: same = %v, want %v (%s)", c.name, got, c.same, differ(c.a, c.b))
		}
	}
}
