package query

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// graph builds a query graph over exampleGraph plus extra entities.
func graph(t *testing.T, extra map[string]string) *Graph {
	t.Helper()
	all := maps.Clone(exampleGraph)
	maps.Copy(all, extra)
	g, err := NewGraph(loadSchema(t, all))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func mustYAML(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Content[0]
}

func ids(t *testing.T, g *Graph, src string) string {
	t.Helper()
	b, err := ParseBlock(src, g.Schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	es, err := g.Run(b)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.ID)
	}
	return strings.Join(out, " ")
}

var evidence = map[string]string{
	"EVD-1": fm("EVD-1", "Evidence", `execution_date: 2026-03-01
passed: true
verifies: REQ-0001
equipment_used:
  - {serial: A, calibration_due_date: 2026-06-01}
  - {serial: B, calibration_due_date: 2026-01-01}
`),
	"EVD-2": fm("EVD-2", "Evidence", `execution_date: 2026-03-01
passed: false
equipment_used:
  - {serial: C, calibration_due_date: 2026-12-01}
`),
	"EVD-3": fm("EVD-3", "Evidence", "verifies: REQ-0002\n"),
}

func TestQueryBlocks(t *testing.T) {
	g := graph(t, evidence)
	for _, tc := range []struct{ name, src, want string }{
		{"document query", "from: Requirement\nwhere: {field: status, operator: equals, value: approved}\norder_by: order\n", "REQ-0001 REQ-0002"},
		{"no where", "from: UserNeed\n", "USR-0001 USR-0002"},
		{"no sort values: by ID", "from: Requirement\norder_by: title\n", "REQ-0001 REQ-0002 REQ-0003"},
		{"missing sort value last", "from: Evidence\norder_by: execution_date\n", "EVD-1 EVD-2 EVD-3"},
		{"not_equals holds when absent", "from: Evidence\nwhere: {field: passed, operator: not_equals, value: false}\n", "EVD-1 EVD-3"},
		{"implicit and", "from: Requirement\nwhere:\n  - {field: status, operator: equals, value: approved}\n  - {field: order, operator: greater_than, value: 1}\n", "REQ-0002"},
		{"any_of", "from: Requirement\nwhere:\n  any_of:\n    - {field: order, operator: less_than, value: 2}\n    - {field: status, operator: equals, value: in_review}\n", "REQ-0001 REQ-0003"},
		{"any_of of all-of groups", "from: Requirement\nwhere:\n  any_of:\n    - [{field: order, operator: less_or_equal, value: 2}, {field: order, operator: greater_or_equal, value: 2}]\n    - {field: id, operator: equals, value: REQ-0003}\n", "REQ-0002 REQ-0003"},
		{"via", "from: Requirement\nwhere: {field: {via: implements, field: status}, operator: equals, value: deprecated}\n", "REQ-0002 REQ-0003"},
		{"via a facet", "from: Requirement\nwhere: {field: {via: evidenced_by, field: passed}, operator: equals, value: true}\n", "REQ-0001"},
		{"exists on a facet", "from: Requirement\nwhere: {field: evidenced_by, operator: exists}\n", "REQ-0001 REQ-0002"},
		{"exists on a link", "from: Evidence\nwhere: {field: verifies, operator: exists}\n", "EVD-1 EVD-3"},
		{"link equals an ID", "from: Requirement\nwhere: {field: implements, operator: equals, value: USR-0001}\n", "REQ-0001"},
		{"rows exist", "from: Evidence\nwhere: {field: equipment_used, operator: exists}\n", "EVD-1 EVD-2"},
		{"row against entity field", "from: Evidence\nwhere: {field: {list: equipment_used, subfield: calibration_due_date}, operator: less_than, value: {field: execution_date}}\n", "EVD-1"},
		// Both tests read the same row: no single row is serial A and
		// out of calibration.
		{"tests share a row", "from: Evidence\nwhere:\n  - {field: {list: equipment_used, subfield: serial}, operator: equals, value: A}\n  - {field: {list: equipment_used, subfield: calibration_due_date}, operator: less_than, value: 2026-03-01}\n", ""},
		{"same row, negated", "from: Evidence\nwhere:\n  - {field: {list: equipment_used, subfield: serial}, operator: not_equals, value: A}\n  - {field: {list: equipment_used, subfield: calibration_due_date}, operator: less_than, value: 2026-03-01}\n", "EVD-1"},
		{"type", "from: Requirement\nwhere: {field: type, operator: equals, value: Requirement}\norder_by: [status, id]\n", "REQ-0001 REQ-0002 REQ-0003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, g, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestQueryBlockErrors(t *testing.T) {
	g := graph(t, evidence)
	for _, tc := range []struct{ name, src, want string }{
		{"symbol operator", "from: Requirement\nwhere:\n  field: status\n  operator: \"=\"\n  value: approved\n",
			`line 4: unknown operator "=" (valid operators: equals, not_equals, greater_or_equal, less_or_equal, greater_than, less_than, exists)`},
		{"no from", "where: {field: status, operator: exists}\n", "line 1: query block has no from: <type>"},
		{"unknown type", "from: Requirment\n", `line 1: from: unknown type "Requirment" (types: Evidence, OccurrenceLevel, Requirement, Risk, SeverityLevel, UserNeed)`},
		{"unknown key", "from: Requirement\nsort: order\n", `line 2: unknown key "sort" in a query block (expected from, where, order_by, render)`},
		{"unknown field", "from: Requirement\nwhere: {field: state, operator: exists}\n", `line 2: Requirement has no field "state" (fields: id, type, title, status, implements, order, evidenced_by)`},
		{"enum value", "from: Requirement\nwhere: {field: status, operator: equals, value: aproved}\n", `line 2: "aproved" is not a value of ApprovalStatus (values: draft, in_review, approved, deprecated)`},
		{"number as text", "from: Requirement\nwhere: {field: order, operator: equals, value: \"2\"}\n", `line 2: order is a number; "2" is not`},
		{"ordering an enum", "from: Requirement\nwhere: {field: status, operator: greater_than, value: draft}\n", "line 2: greater_than compares numbers, dates and text; status is an enum value"},
		{"bad date", "from: Evidence\nwhere: {field: execution_date, operator: less_than, value: soon}\n", `line 2: "soon" is not a date (YYYY-MM-DD)`},
		{"mismatched fields", "from: Evidence\nwhere: {field: passed, operator: equals, value: {field: execution_date}}\n", "line 2: passed is true or false but execution_date is a date"},
		{"exists with value", "from: Evidence\nwhere: {field: passed, operator: exists, value: true}\n", "line 2: exists takes no value"},
		{"missing value", "from: Evidence\nwhere: {field: passed, operator: equals}\n", "line 2: equals needs a value"},
		{"rows compared", "from: Evidence\nwhere: {field: equipment_used, operator: equals, value: A}\n", `line 2: "equipment_used" is a list of rows`},
		{"unknown sub-field", "from: Evidence\nwhere: {field: {list: equipment_used, subfield: serial_no}, operator: exists}\n", `line 2: list "equipment_used" has no sub-field "serial_no" (sub-fields: serial, calibration_due_date)`},
		{"unknown via", "from: Evidence\nwhere: {field: {via: tests, field: status}, operator: exists}\n", `line 2: Evidence has no link or incoming facet named "tests"`},
		{"nested any_of", "from: Requirement\nwhere:\n  any_of:\n    - any_of: [{field: status, operator: exists}]\n", "line 4: any_of cannot nest inside any_of here"},
		{"order_by many", "from: Requirement\norder_by: implements\n", `line 2: order_by: "implements" has several values`},
		{"render mode", "from: Requirement\nrender: summary\n", `line 2: render: unknown mode "summary" (expected full, id or field:<name>)`},
		{"render field", "from: Requirement\nrender: field:titel\n", `line 2: render: Requirement has no field "titel"`},
		{"yaml", "from: [Requirement\n", "line "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBlock(tc.src, g.Schema)
			var qe *Error
			if !errors.As(err, &qe) || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
		})
	}
}

func TestNestedAnyOfInAssertions(t *testing.T) {
	g := graph(t, evidence)
	n := mustYAML(t, "any_of:\n  - any_of: [{field: order, operator: equals, value: 1}, {field: order, operator: equals, value: 3}]\n  - {field: status, operator: equals, value: deprecated}\n")
	cond, err := ParseCondition(n, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.Match(g.Schema.Types["Requirement"], cond)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "REQ-0001 REQ-0003" {
		t.Errorf("got %v", got)
	}
}

func TestRender(t *testing.T) {
	g := graph(t, nil)
	for src, want := range map[string]Render{
		"from: Requirement\n":                            {Mode: RenderFull},
		"from: Requirement\nrender: id\n":                {Mode: RenderID},
		"from: Requirement\nrender: field:title\n":       {Mode: RenderField, Field: "title"},
		"from: UserNeed\nrender: field:implemented_by\n": {Mode: RenderField, Field: "implemented_by"},
	} {
		b, err := ParseBlock(src, g.Schema)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if b.Render != want {
			t.Errorf("%q: render %+v, want %+v", src, b.Render, want)
		}
	}
}
