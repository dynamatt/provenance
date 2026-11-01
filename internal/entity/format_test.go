package entity

import (
	"testing"

	"github.com/dynamatt/provenance/internal/schema"
)

// widget declares a field of each kind, in the order fmt writes them.
var widget = &schema.Type{Name: "Widget", Fields: []*schema.Field{
	{Name: "title", Kind: schema.String},
	{Name: "status", Kind: schema.Enum, EnumName: "Status"},
	{Name: "due", Kind: schema.Date},
	{Name: "weight", Kind: schema.Number},
	{Name: "done", Kind: schema.Boolean},
	{Name: "notes", Kind: schema.Text},
	{Name: "parent", Kind: schema.Link, Cardinality: "one"},
	{Name: "uses", Kind: schema.Link, Cardinality: "many"},
	{Name: "tags", Kind: schema.List, Elem: &schema.Field{Name: "tags", Kind: schema.String}},
	{Name: "steps", Kind: schema.List, Fields: []*schema.Field{
		{Name: "n", Kind: schema.Number},
		{Name: "description", Kind: schema.Text},
	}},
}}

func TestFormat(t *testing.T) {
	cases := []struct{ name, in, want string }{
		// DES-0038 scope: the body is kept, with LF endings and one final newline.
		{"body kept", "---\nid: W-1\ntype: Widget\n---\n\nText  with *Markdown*\n\n\n", "---\nid: W-1\ntype: Widget\n---\n\nText  with *Markdown*\n"},
		{"CRLF", "---\r\nid: W-1\r\ntype: Widget\r\n---\r\nLine one\r\nLine two\r\n", "---\nid: W-1\ntype: Widget\n---\nLine one\nLine two\n"},
		{"no body", "---\nid: W-1\ntype: Widget\n---\n\n", "---\nid: W-1\ntype: Widget\n---\n"},
		// Key order: id, type, schema order, then undeclared keys as written.
		{"key order", "---\ntitle: T\ntype: Widget\nzeta: 1\nid: W-1\nalpha: 2\ndue: 2026-10-28\n---\n", "---\nid: W-1\ntype: Widget\ntitle: T\ndue: 2026-10-28\nzeta: 1\nalpha: 2\n---\n"},
		// Layout: lists in block style, rows' sub-fields in schema order.
		{"link list", "---\nid: W-1\ntype: Widget\nuses: [W-2, W-3]\n---\n", "---\nid: W-1\ntype: Widget\nuses:\n  - W-2\n  - W-3\n---\n"},
		{"item list", "---\nid: W-1\ntype: Widget\ntags: [a, b]\n---\n", "---\nid: W-1\ntype: Widget\ntags:\n  - a\n  - b\n---\n"},
		{"rows", "---\nid: W-1\ntype: Widget\nsteps:\n- {description: Go, n: 1}\n---\n", "---\nid: W-1\ntype: Widget\nsteps:\n  - n: 1\n    description: Go\n---\n"},
		// Empty fields are left out; an undeclared one is kept.
		{"empty fields", "---\nid: W-1\ntype: Widget\ntitle:\nnotes: ''\nuses: []\nsteps:\n  - n: 1\n    description:\nextra:\n---\n", "---\nid: W-1\ntype: Widget\nsteps:\n  - n: 1\nextra:\n---\n"},
		// Text with line breaks: literal blocks, a folded text keeps its breaks.
		{"literal", "---\nid: W-1\ntype: Widget\nnotes: \"one\\ntwo\\n\"\n---\n", "---\nid: W-1\ntype: Widget\nnotes: |\n  one\n  two\n---\n"},
		{"folded text", "---\nid: W-1\ntype: Widget\nnotes: >-\n  one\n  two\n\n  three\n---\n", "---\nid: W-1\ntype: Widget\nnotes: |-\n  one\n  two\n\n  three\n---\n"},
		{"folded clip", "---\nid: W-1\ntype: Widget\nnotes: >\n  one\n  two\n---\n", "---\nid: W-1\ntype: Widget\nnotes: |\n  one\n  two\n---\n"},
		{"folded string", "---\nid: W-1\ntype: Widget\ntitle: >-\n  one\n  two\n---\n", "---\nid: W-1\ntype: Widget\ntitle: one two\n---\n"},
		{"folded row text", "---\nid: W-1\ntype: Widget\nsteps:\n  - description: >-\n      one\n      two\n---\n", "---\nid: W-1\ntype: Widget\nsteps:\n  - description: |-\n      one\n      two\n---\n"},
		{"no reflow", "---\nid: W-1\ntype: Widget\ntitle: " + long + "\n---\n", "---\nid: W-1\ntype: Widget\ntitle: " + long + "\n---\n"},
		// Scalars: plain unless YAML would read another value.
		{"plain", "---\nid: 'W-1'\ntype: \"Widget\"\ntitle: 'T'\nstatus: \"open\"\n---\n", "---\nid: W-1\ntype: Widget\ntitle: T\nstatus: open\n---\n"},
		{"quoted", "---\nid: W-1\ntype: Widget\ntitle: '#3'\nnotes: 'yes'\ntags: ['a: b', '12']\n---\n", "---\nid: W-1\ntype: Widget\ntitle: \"#3\"\nnotes: \"yes\"\ntags:\n  - \"a: b\"\n  - \"12\"\n---\n"},
		{"date", "---\nid: W-1\ntype: Widget\ndue: '2026-10-28'\n---\n", "---\nid: W-1\ntype: Widget\ndue: 2026-10-28\n---\n"},
		{"numbers", "---\nid: W-1\ntype: Widget\nweight: 1_000\nsteps:\n  - n: 2.50\n---\n", "---\nid: W-1\ntype: Widget\nweight: 1000\nsteps:\n  - n: 2.5\n---\n"},
		{"boolean", "---\nid: W-1\ntype: Widget\ndone: True\n---\n", "---\nid: W-1\ntype: Widget\ndone: true\n---\n"},
		// A value not of its field's type is kept as written.
		{"invalid kept", "---\nid: W-1\ntype: Widget\nweight: 'heavy'\nuses: W-2\ndue: tomorrow\n---\n", "---\nid: W-1\ntype: Widget\ndue: tomorrow\nweight: 'heavy'\nuses: W-2\n---\n"},
		// Comments move with their key; an empty field with a comment stays.
		{"comments", "---\nid: W-1\ntype: Widget\n# why\nuses: [W-2] # see\ntitle: T\nnotes: # fill in\n---\n", "---\nid: W-1\ntype: Widget\ntitle: T\nnotes: # fill in\n# why\nuses: # see\n  - W-2\n---\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Format([]byte(c.in), widget)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
			again, err := Format(got, widget)
			if err != nil || string(again) != string(got) {
				t.Errorf("formatting again: %v\n%s", err, again)
			}
		})
	}
}

const long = "a title longer than the eighty characters a YAML encoder folds a plain scalar at, kept on one line"

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"one":                  "one",
		"one\ntwo":             "one two",
		"one\ntwo\n":           "one two\n",
		"one\ntwo\n\nthree":    "one two\nthree",
		"one\n  indented\ntwo": "one\n  indented\ntwo",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}
