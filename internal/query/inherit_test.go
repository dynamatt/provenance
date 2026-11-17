package query

import (
	"strings"
	"testing"
)

// docSchema has an abstract Item, two types extending it, and a Doc type
// extended by Report, which numbers in Doc's series.
var docSchema = map[string]string{
	"schema/Item.yaml":   "type: Item\nabstract: true\nfields:\n  - {name: title, type: string, title: true}\n  - {name: about, type: link, target: [Doc], cardinality: one, reverse_name: items}\n",
	"schema/Bolt.yaml":   "type: Bolt\nextends: Item\nid_prefix: BLT\nfields:\n  - {name: torque, type: number}\n",
	"schema/Nut.yaml":    "type: Nut\nextends: Item\nid_prefix: NUT\n",
	"schema/Doc.yaml":    "type: Doc\nid_prefix: DOC\nfields:\n  - {name: title, type: string, title: true}\n",
	"schema/Report.yaml": "type: Report\nextends: Doc\nfields:\n  - {name: scope, type: link, target: [Bolt], cardinality: many}\n",
}

var docEntities = map[string]string{
	"BLT-0001": fm("BLT-0001", "Bolt", "torque: 5\nabout: DOC-0002\n"),
	"BLT-0002": fm("BLT-0002", "Bolt", "torque: 9\n"),
	"NUT-0001": fm("NUT-0001", "Nut", "about: DOC-0001\n"),
	"DOC-0001": fm("DOC-0001", "Doc", "title: Plain\n"),
	"DOC-0002": fm("DOC-0002", "Report", "title: Report\nscope: [BLT-0001]\n"),
}

func TestInheritedTypesInQueries(t *testing.T) {
	g, err := NewGraph(loadWith(t, docSchema, docEntities))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, src, want string }{
		{"an abstract type stands for its subtypes", "from: Item\n", "BLT-0001 BLT-0002 NUT-0001"},
		{"a type stands for itself and its subtypes", "from: Doc\n", "DOC-0001 DOC-0002"},
		{"a field only a subtype has", "from: Item\nwhere: {field: torque, operator: greater_than, value: 6}\n", "BLT-0002"},
		{"an inherited link's facet", "from: Doc\nwhere: {field: items, operator: exists}\n", "DOC-0001 DOC-0002"},
		{"type compares the exact type", "from: Doc\nwhere: {field: type, operator: equals, value: Doc}\n", "DOC-0001"},
		{"is_a takes subtypes", "from: any\nwhere: {field: type, operator: is_a, value: Doc}\n", "DOC-0001 DOC-0002"},
		{"is_a an abstract type", "from: any\nwhere: {field: type, operator: is_a, value: Item}\n", "BLT-0001 BLT-0002 NUT-0001"},
		{"a sub-query from a parent", "from: Bolt\nwhere: {field: about, where: {field: type, operator: is_a, value: Doc}}\n", "BLT-0001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, g, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	for _, tc := range []struct{ name, src, want string }{
		{"is_a on a field", "from: Doc\nwhere: {field: title, operator: is_a, value: Doc}\n", "line 2: is_a tests an entity's type"},
		{"is_a an unknown type", "from: Doc\nwhere: {field: type, operator: is_a, value: Dco}\n", `line 2: is_a: unknown type "Dco"`},
		{"is_a with value_of", "from: Doc\nwhere: {field: type, operator: is_a, value_of: title}\n", "line 2: is_a takes a type's name as value, not value_of"},
		{"templates: for a parent", "from: Item\ntemplates: {Item: row}\n", ""},
		{"templates: for an unrelated type", "from: Item\ntemplates: {Doc: row}\n", "line 2: templates: Doc is not selected by from (Bolt, Nut)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBlock(tc.src, g.Schema)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected %v", err)
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s…", err, tc.want)
			}
		})
	}
	b, err := ParseBlock("from: Item\ntemplates: {Item: row, Nut: nut-row}\n", g.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.TemplateFor(g.Schema.Types["Bolt"]).Name; got != "row" {
		t.Errorf("Bolt: %q, want the template chosen for Item", got)
	}
	if got := b.TemplateFor(g.Schema.Types["Nut"]).Name; got != "nut-row" {
		t.Errorf("Nut: %q, want its own", got)
	}
}
