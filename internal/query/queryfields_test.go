package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/model"
)

var qfSchema = map[string]string{
	"schema/Req.yaml": `type: Req
id_prefix: REQ
fields:
  - {name: title, type: string, title: true}
  - {name: status, type: string}
  - {name: order, type: number}
  - {name: parent, type: link, target: [Req], cardinality: one, reverse_name: children}
  - {name: reports, type: calculated, formula: "COUNT(reported_in)"}
  - {name: rank, type: calculated, formula: "order * 10"}
`,
	"schema/Report.yaml": `type: Report
id_prefix: DOC
fields:
  - {name: title, type: string, title: true}
  - {name: covers, type: link, target: [Req], cardinality: one}
  - {name: scope, type: query, of: [Req], reverse_name: reported_in}
  - {name: size, type: calculated, formula: "COUNT(scope)"}
`,
}

func qfGraph(t *testing.T, entities map[string]string) *Graph {
	t.Helper()
	g, err := NewGraph(loadWith(t, qfSchema, entities))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

var qfReqs = map[string]string{
	"REQ-0001": fm("REQ-0001", "Req", "status: approved\norder: 3\n"),
	"REQ-0002": fm("REQ-0002", "Req", "status: approved\norder: 1\n"),
	"REQ-0003": fm("REQ-0003", "Req", "status: draft\norder: 2\nparent: REQ-0002\n"),
}

func with(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func idsOf(v *model.Value) string { return strings.Join(v.IDs, " ") }

func TestQueryFields(t *testing.T) {
	g := qfGraph(t, with(qfReqs, map[string]string{
		"DOC-0001": fm("DOC-0001", "Report", "scope:\n  from: Req\n  where: {field: status, operator: equals, value: approved}\n  order_by: order\n"),
		// ^covers reads the report: the requirement it covers and its children.
		"DOC-0002": fm("DOC-0002", "Report", `covers: REQ-0002
scope:
  from: Req
  where:
    any_of:
      - {field: id, operator: equals, value_of: ^covers}
      - {field: parent, operator: equals, value_of: ^covers}
`),
		// Selecting by a calculated field.
		"DOC-0003": fm("DOC-0003", "Report", "scope:\n  from: Req\n  where: {field: rank, operator: greater_or_equal, value: 20}\n"),
	}))
	for id, want := range map[string]string{"DOC-0001": "REQ-0002 REQ-0001", "DOC-0002": "REQ-0002 REQ-0003", "DOC-0003": "REQ-0001 REQ-0003"} {
		if got := idsOf(g.Entity(id).Field("scope")); got != want {
			t.Errorf("%s scope = %q, want %q", id, got, want)
		}
	}
	if v := g.Entity("DOC-0001").Field("size"); !v.Present || v.Num != 2 {
		t.Errorf("DOC-0001 size = %+v, want 2", v)
	}
	in := g.Entity("REQ-0002").Facet("reported_in")
	var from []string
	for _, e := range in.From {
		from = append(from, e.ID)
	}
	if strings.Join(from, " ") != "DOC-0001 DOC-0002" {
		t.Errorf("REQ-0002 reported_in = %v", from)
	}
	// Query blocks and rules read the results like stored links.
	for _, tc := range []struct{ src, want string }{
		{"from: Req\nwhere: {field: reported_in, operator: missing}\n", ""},
		{"from: Report\nwhere: {field: scope, where: {field: status, operator: equals, value: draft}}\n", "DOC-0002 DOC-0003"},
		{"from: Req\nwhere: {field: reports, operator: equals, value: 2}\n", "REQ-0001 REQ-0002 REQ-0003"},
	} {
		if got := ids(t, g, tc.src); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestQueryFieldLoops(t *testing.T) {
	// Each report selects the requirements the other doesn't report: no
	// answer. A positive loop has one, and is allowed.
	g := qfGraph(t, with(qfReqs, map[string]string{
		"DOC-0001": fm("DOC-0001", "Report", "scope:\n  from: Req\n  where: {field: reported_in, operator: missing}\n"),
		"DOC-0002": fm("DOC-0002", "Report", "scope:\n  from: Req\n  where: {field: status, operator: equals, value: approved}\n"),
	}))
	v := g.Entity("DOC-0001").Field("scope")
	if !v.Invalid || !strings.Contains(v.Problem, "the query depends on its own results") {
		t.Errorf("DOC-0001 scope = %+v", v)
	}
	if got := idsOf(g.Entity("DOC-0002").Field("scope")); got != "REQ-0001 REQ-0002" {
		t.Errorf("DOC-0002 should still be evaluated, got %q", got)
	}
	// A query selecting by a count of the results it feeds has none either.
	g = qfGraph(t, with(qfReqs, map[string]string{
		"DOC-0001": fm("DOC-0001", "Report", "scope:\n  from: Req\n  where: {field: reports, operator: greater_or_equal, value: 1}\n"),
	}))
	if v := g.Entity("DOC-0001").Field("scope"); !v.Invalid {
		t.Errorf("a loop through COUNT should have no answer: %+v", v)
	}
	g = qfGraph(t, with(qfReqs, map[string]string{
		"DOC-0001": fm("DOC-0001", "Report", "scope:\n  from: Req\n  where:\n    any_of:\n      - {field: status, operator: equals, value: draft}\n      - {field: reported_in, operator: exists}\n"),
	}))
	if got := idsOf(g.Entity("DOC-0001").Field("scope")); got != "REQ-0003" {
		t.Errorf("positive loop: got %q, want its least answer REQ-0003", got)
	}
}

func TestQueryFieldErrors(t *testing.T) {
	for _, tc := range []struct{ name, scope, want string }{
		{"not a mapping", "scope: REQ-0001\n", "not a query"},
		{"from outside of", "scope:\n  from: Report\n", "line 5: from: Report is not a type scope may select (of: [Req] in schema/Report.yaml)"},
		{"from any", "scope:\n  from: any\n", "line 5: from: any: scope selects only Req"},
		{"render", "scope:\n  from: Req\n  render: id\n", "line 6: render: a query field gives its results to its entity's template"},
		{"unknown key", "scope:\n  from: Req\n  colour: red\n", `line 6: unknown key "colour" in a query field (expected from, where, order_by)`},
		{"unknown field", "scope:\n  from: Req\n  where: {field: colour, operator: exists}\n", `line 6: Req has no field "colour"`},
		{"unknown holder field", "scope:\n  from: Req\n  where: {field: id, operator: equals, value_of: ^colour}\n", `line 6: Report has no field "colour"`},
		{"order_by", "scope:\n  from: Req\n  order_by: colour\n", "line 6: order_by:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := qfGraph(t, with(qfReqs, map[string]string{"DOC-0001": fm("DOC-0001", "Report", tc.scope)}))
			v := g.Entity("DOC-0001").Field("scope")
			if got := fmt.Sprintf("line %d: %s", v.ProblemLine, v.Problem); !v.Invalid || !strings.HasPrefix(got, tc.want) && !strings.HasPrefix(v.Problem, tc.want) {
				t.Errorf("got %+v, want problem %s…", v, tc.want)
			}
		})
	}
}

func TestQueryText(t *testing.T) {
	g := qfGraph(t, with(qfReqs, map[string]string{
		"DOC-0001": fm("DOC-0001", "Report", "scope:\n  from: Req # requirements\n  where: {field: status, operator: equals, value: approved}\n"),
	}))
	want := "from: Req\nwhere:\n  field: status\n  operator: equals\n  value: approved"
	if got := QueryText(g.Entity("DOC-0001").Field("scope").Query); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestQueryFieldDependencies(t *testing.T) {
	g := qfGraph(t, with(qfReqs, map[string]string{
		"DOC-0001": fm("DOC-0001", "Report", "scope:\n  from: Req\n  where: {field: parent, where: {field: status, operator: equals, value: approved}}\n"),
	}))
	d, err := g.Dependencies(g.Entity("DOC-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.IDs, " "); got != "DOC-0001 REQ-0003" {
		t.Errorf("IDs = %s", got)
	}
	if got := strings.Join(d.Full, " "); got != "DOC-0001" {
		t.Errorf("Full = %s: results are shown by the template, not rendered in full", got)
	}
	if got := strings.Join(d.Types, " "); got != "Req" {
		t.Errorf("Types = %s", got)
	}
}
