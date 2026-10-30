package canon

import (
	"errors"
	"testing"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

var s = &schema.Schema{Types: map[string]*schema.Type{"Widget": {Name: "Widget", Fields: []*schema.Field{
	{Name: "title", Kind: schema.String},
	{Name: "notes", Kind: schema.Text},
	{Name: "uses", Kind: schema.Link, Cardinality: "many"},
}}}}

func TestFile(t *testing.T) {
	in := "---\nuses: [W-2]\nid: W-1\ntype: Widget\n# kept\nnotes: >-\n  one\n  two\ntitle:\n---\nBody\n"
	want := "---\nid: W-1\ntype: Widget\n# kept\nnotes: |-\n  one\n  two\nuses:\n  - W-2\n---\nBody\n"
	got, err := File("W-1.md", []byte(in), s)
	if err != nil || string(got) != want {
		t.Errorf("got %v:\n%s\nwant:\n%s", err, got, want)
	}
}

// A file fmt can't read is an UnreadableError, at its line.
func TestFileUnreadable(t *testing.T) {
	for name, in := range map[string]string{
		"yaml":         "---\nid: W-1\ntype: Widget\nuses: [W-2\n---\n",
		"no type":      "---\nid: W-1\n---\n",
		"unknown type": "---\nid: W-1\ntype: Gadget\n---\n",
		"not entity":   "# Notes\n",
	} {
		_, err := File("a.md", []byte(in), s)
		var u *UnreadableError
		if !errors.As(err, &u) {
			t.Errorf("%s: got %v, want an UnreadableError", name, err)
		}
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
