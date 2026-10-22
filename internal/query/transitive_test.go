package query

import (
	"fmt"
	"strings"
	"testing"
)

// partsSchema has a decomposition link that may form cycles, reaching two
// types, and Markdown with fenced blocks in a body, a text field and a list
// row.
var partsSchema = map[string]string{
	"schema/Product.yaml": "type: Product\nfields:\n  - {name: title, type: string}\n",
	"schema/Part.yaml": `type: Part
fields:
  - {name: title, type: string}
  - {name: parent, type: link, target: [Part, Product], cardinality: one, reverse_name: children}
  - {name: notes, type: text}
  - name: steps
    type: list
    fields:
      - {name: action, type: text}
`,
}

var parts = map[string]string{
	// P1 → P2 → P3 → P1 is a cycle, P4 leads into it, P5 is its own parent.
	"P1": fm("P1", "Part", "parent: P2\n"),
	"P2": fm("P2", "Part", "parent: P3\n"),
	"P3": fm("P3", "Part", "parent: P1\n"),
	"P4": fm("P4", "Part", "parent: P1\n"),
	"P5": fm("P5", "Part", "parent: P5\n"),
	// P6 → P7 → PRD-1, a product with no parent.
	"P6":    fm("P6", "Part", "title: Bolt\nparent: P7\n") + "Body.\n\n```python\nprint(1)\n```\n\n```\nbare\n```\n",
	"P7":    fm("P7", "Part", "title: Bracket\nparent: PRD-1\nnotes: |\n  See:\n\n  ```mermaid\n  graph LR\n  ```\nsteps:\n  - action: \"```query\\nfrom: Part\\n```\"\n"),
	"PRD-1": fm("PRD-1", "Product", "title: Stimulator\n"),
}

func partsGraph(t *testing.T) *Graph {
	t.Helper()
	g, err := NewGraph(loadWith(t, partsSchema, parts))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestTransitive(t *testing.T) {
	g := partsGraph(t)
	for _, tc := range []struct{ name, src, want string }{
		{"a cycle reaches itself", "from: Part\nwhere: {field: parent, transitive: true, where: {field: id, operator: equals, value_of: ^id}}\n", "P1 P2 P3 P5"},
		{"not transitive: only the parent", "from: Part\nwhere: {field: parent, where: {field: id, operator: equals, value_of: ^id}}\n", "P5"},
		{"across types", "from: Part\nwhere: {field: parent, transitive: true, where: {field: type, operator: equals, value: Product}}\n", "P6 P7"},
		{"a field only the far type has", "from: Part\nwhere: {field: parent, transitive: true, where: {field: title, operator: equals, value: Stimulator}}\n", "P6 P7"},
		{"without where: any ancestor", "from: Part\nwhere: {field: parent, transitive: true}\n", "P1 P2 P3 P4 P5 P6 P7"},
		{"an incoming facet, downwards", "from: Product\nwhere: {field: children, transitive: true, where: {field: title, operator: equals, value: Bolt}}\n", "PRD-1"},
		{"counting ancestors", "from: Part\nwhere: {field: parent, transitive: true, aggregation: {type: count}, operator: equals, value: 2}\n", "P6"},
		{"P4 reaches the cycle, once each", "from: Part\nwhere: {field: parent, transitive: true, aggregation: {type: count}, operator: equals, value: 3}\n", "P1 P2 P3 P4"},
		{"transitive: false is a plain sub-query", "from: Part\nwhere: {field: parent, transitive: false, where: {field: type, operator: equals, value: Product}}\n", "P7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, g, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodeBlocks(t *testing.T) {
	g := partsGraph(t)
	for _, tc := range []struct{ name, src, want string }{
		{"by language", "from: any\nwhere: {field: code_blocks, where: {field: language, operator: equals, value: python}}\n", "P6"},
		{"a bare fence is none", "from: any\nwhere: {field: code_blocks, where: {field: language, operator: equals, value: none}}\n", "P6"},
		{"in a text field and a list row", "from: any\nwhere: {field: code_blocks, where: {field: language, operator: not_in, value: [python, none]}}\n", "P7"},
		{"counted", "from: any\nwhere: {field: code_blocks, aggregation: {type: count}, operator: equals, value: 0}\n", "P1 P2 P3 P4 P5 PRD-1"},
		{"line and language of one block", "from: Part\nwhere: {field: code_blocks, where: [{field: line, operator: equals, value: 13}, {field: language, operator: equals, value: none}]}\n", "P6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, g, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTransitiveAndCodeBlockItems(t *testing.T) {
	g := partsGraph(t)
	for _, tc := range []struct{ name, src, want string }{
		// Each member of a cycle at its own link (D8).
		{"cycle", "from: Part\nwhere: {field: parent, transitive: true, where: {field: id, operator: equals, value_of: ^id}}\n",
			"P1: parent[-1]P1@P1:4; P2: parent[-1]P2@P2:4; P3: parent[-1]P3@P3:4; P5: parent[-1]P5@P5:4"},
		{"facet: a direct child where it links, a further one at the id", "from: Product\nwhere: {field: children, transitive: true, where: {field: title, operator: exists}}\n",
			"PRD-1: children[-1]P7@P7:5 children[-1]P6@PRD-1:2"},
		{"code blocks at their fences, in the body, a text field and a row", "from: Part\nwhere: {field: code_blocks, where: {field: language, operator: exists}}\n",
			"P6: code_blocks[0]@P6:9 code_blocks[1]@P6:13; P7: code_blocks[0]@P7:9 code_blocks[1]@P7:13"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := ParseBlock(tc.src, g.Schema)
			if err != nil {
				t.Fatal(err)
			}
			ms, err := g.Matches(b.From, b.Where)
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, m := range ms {
				s := m.Entity.ID + ":"
				for _, it := range m.Items {
					s += fmt.Sprintf(" %s[%d]%s@%s:%d", it.Field, it.Row, it.ID, it.Path, it.Line)
				}
				out = append(out, s)
			}
			if got := strings.Join(out, "; "); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestTransitiveViaTypes(t *testing.T) {
	g := partsGraph(t)
	b, err := ParseBlock("from: Part\nwhere: {field: parent, transitive: true, where: {field: title, operator: exists}}\n", g.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(viaTypes(g.Schema, b.From, b.Where), " "); got != "Part Product" {
		t.Errorf("got %q", got)
	}
}

func TestTransitiveAndCodeBlockErrors(t *testing.T) {
	g := partsGraph(t)
	for _, tc := range []struct{ name, src, want string }{
		{"on a list", "from: Part\nwhere: {field: steps, transitive: true, where: {field: action, operator: exists}}\n", `line 2: "steps" is a list of rows: transitive follows a link field or incoming facet`},
		{"on a scalar", "from: Part\nwhere: {field: title, transitive: true}\n", `line 2: "title" is text: a sub-query reads the rows`},
		{"with from", "from: Part\nwhere: {from: Part, transitive: true, where: {field: id, operator: exists}}\n", "line 2: transitive follows a link field or incoming facet; from looks among other entities"},
		{"not a boolean", "from: Part\nwhere: {field: parent, transitive: always}\n", "line 2: transitive is true or false"},
		{"with an operator", "from: Part\nwhere: {field: parent, transitive: true, operator: exists}\n", "line 2: a sub-query holds when some item matches its where"},
		{"code_blocks' sub-fields", "from: Part\nwhere: {field: code_blocks, where: {field: lang, operator: equals, value: go}}\n", `line 2: list "code_blocks" has no sub-field "lang" (sub-fields: language, line)`},
		{"code_blocks is a list", "from: Part\nwhere: {field: code_blocks, operator: equals, value: go}\n", `line 2: "code_blocks" is a list of rows`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBlock(tc.src, g.Schema)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
		})
	}
}
