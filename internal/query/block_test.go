package query

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"
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
		{"through a link", "from: Requirement\nwhere: {field: implements, where: {field: status, operator: equals, value: deprecated}}\n", "REQ-0002 REQ-0003"},
		{"through a facet", "from: Requirement\nwhere: {field: evidenced_by, where: {field: passed, operator: equals, value: true}}\n", "REQ-0001"},
		{"through a link, then a facet", "from: Evidence\nwhere:\n  field: verifies\n  where: {field: implements, where: {field: status, operator: equals, value: approved}}\n", "EVD-1"},
		{"facet back to the entity", "from: UserNeed\nwhere:\n  field: implemented_by\n  where: {field: evidenced_by, where: {field: passed, operator: equals, value: false}}\n", ""},
		{"exists on a facet", "from: Requirement\nwhere: {field: evidenced_by, operator: exists}\n", "REQ-0001 REQ-0002"},
		{"exists on a link", "from: Evidence\nwhere: {field: verifies, operator: exists}\n", "EVD-1 EVD-3"},
		{"link equals an ID", "from: Requirement\nwhere: {field: implements, operator: equals, value: USR-0001}\n", "REQ-0001"},
		{"rows exist", "from: Evidence\nwhere: {field: equipment_used, operator: exists}\n", "EVD-1 EVD-2"},
		{"row against the entity's field", "from: Evidence\nwhere: {field: equipment_used, where: {field: calibration_due_date, operator: less_than, value_of: ^execution_date}}\n", "EVD-1"},
		// Conditions inside one sub-query are about one row: no single row
		// is serial A and out of calibration.
		{"one row", "from: Evidence\nwhere:\n  field: equipment_used\n  where:\n    - {field: serial, operator: equals, value: A}\n    - {field: calibration_due_date, operator: less_than, value: 2026-03-01}\n", ""},
		{"one row, negated", "from: Evidence\nwhere:\n  field: equipment_used\n  where:\n    - {field: serial, operator: not_equals, value: A}\n    - {field: calibration_due_date, operator: less_than, value: 2026-03-01}\n", "EVD-1"},
		// Two sub-queries on one list may match different rows.
		{"two rows", "from: Evidence\nwhere:\n  - {field: equipment_used, where: {field: serial, operator: equals, value: A}}\n  - {field: equipment_used, where: {field: calibration_due_date, operator: less_than, value: 2026-03-01}}\n", "EVD-1"},
		{"any_of inside a sub-query", "from: Evidence\nwhere:\n  field: equipment_used\n  where:\n    any_of:\n      - {field: serial, operator: equals, value: C}\n      - {field: serial, operator: equals, value: B}\n", "EVD-1 EVD-2"},
		{"nested any_of", "from: Requirement\nwhere:\n  any_of:\n    - any_of: [{field: order, operator: equals, value: 1}, {field: order, operator: equals, value: 3}]\n    - {field: status, operator: equals, value: deprecated}\n", "REQ-0001 REQ-0003"},
		{"value_of", "from: Evidence\nwhere: {field: title, operator: equals, value_of: execution_date}\n", ""},
		{"^ from a linked entity", "from: Evidence\nwhere: {field: verifies, where: {field: id, operator: equals, value_of: ^verifies}}\n", "EVD-1 EVD-3"},
		{"^^ two levels out", "from: Evidence\nwhere:\n  field: verifies\n  where:\n    field: evidenced_by\n    where: {field: id, operator: equals, value_of: ^^id}\n", "EVD-1 EVD-3"},
		{"missing", "from: Evidence\nwhere: {field: passed, operator: missing}\n", "EVD-3"},
		{"missing rows", "from: Evidence\nwhere: {field: equipment_used, operator: missing}\n", "EVD-3"},
		{"missing link", "from: Requirement\nwhere: {field: evidenced_by, operator: missing}\n", "REQ-0003"},
		{"not_in", "from: Requirement\nwhere: {field: status, operator: not_in, value: [approved, deprecated]}\n", "REQ-0003"},
		{"not_in holds when absent", "from: Evidence\nwhere: {field: execution_date, operator: not_in, value: [2026-03-01]}\n", "EVD-3"},
		{"from any", "from: any\nwhere: {field: status, operator: equals, value: deprecated}\n", "USR-0002"},
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
			`line 4: unknown operator "=" (valid operators: equals, not_equals, greater_or_equal, less_or_equal, greater_than, less_than, exists, missing, not_in)`},
		{"no from", "where: {field: status, operator: exists}\n", "line 1: query block has no from: <type>"},
		{"unknown type", "from: Requirment\n", `line 1: from: unknown type "Requirment" (types: Document, Evidence, OccurrenceLevel, Requirement, Risk, SeverityLevel, UserNeed)`},
		{"unknown key", "from: Requirement\nsort: order\n", `line 2: unknown key "sort" in a query block (expected from, where, order_by, render, template, templates)`},
		{"unknown field", "from: Requirement\nwhere: {field: state, operator: exists}\n", `line 2: Requirement has no field "state" (fields: id, type, title, status, implements, order, evidenced_by, code_blocks)`},
		{"enum value", "from: Requirement\nwhere: {field: status, operator: equals, value: aproved}\n", `line 2: "aproved" is not a value of ApprovalStatus (values: draft, in_review, approved, deprecated)`},
		{"number as text", "from: Requirement\nwhere: {field: order, operator: equals, value: \"2\"}\n", `line 2: order is a number; "2" is not`},
		{"ordering an enum", "from: Requirement\nwhere: {field: status, operator: greater_than, value: draft}\n", "line 2: greater_than compares numbers, dates and text; status is an enum value"},
		{"bad date", "from: Evidence\nwhere: {field: execution_date, operator: less_than, value: soon}\n", `line 2: "soon" is not a date (YYYY-MM-DD)`},
		{"mismatched fields", "from: Evidence\nwhere: {field: passed, operator: equals, value_of: execution_date}\n", "line 2: passed is true or false but execution_date is a date"},
		{"old value field", "from: Evidence\nwhere: {field: passed, operator: equals, value: {field: execution_date}}\n", "line 2: value: {field: execution_date} is now value_of: execution_date"},
		{"old list form", "from: Evidence\nwhere: {field: {list: equipment_used, subfield: serial}, operator: exists}\n", "line 2: {list: L, subfield: S} is now a sub-query: {field: L, where: {field: S, …}}"},
		{"old via form", "from: Requirement\nwhere: {field: {via: implements, field: status}, operator: exists}\n", "line 2: {via: L, field: F} is now a sub-query: {field: L, where: {field: F, …}}"},
		{"value and value_of", "from: Evidence\nwhere: {field: passed, operator: equals, value: true, value_of: passed}\n", "line 2: use value (a literal) or value_of (a field), not both"},
		{"missing with value", "from: Evidence\nwhere: {field: passed, operator: missing, value: true}\n", "line 2: missing takes no value"},
		{"not_in a scalar", "from: Requirement\nwhere: {field: status, operator: not_in, value: approved}\n", "line 2: not_in needs a list of values"},
		{"not_in enum value", "from: Requirement\nwhere: {field: status, operator: not_in, value: [approved, aproved]}\n", `line 2: "aproved" is not a value of ApprovalStatus`},
		{"list for equals", "from: Requirement\nwhere: {field: status, operator: equals, value: [approved]}\n", "line 2: value must be a single value (only not_in takes a list)"},
		{"sub-query with an operator", "from: Evidence\nwhere: {field: equipment_used, where: {field: serial, operator: exists}, operator: exists}\n", "line 2: a sub-query holds when some item matches its where, and takes no operator"},
		{"sub-query on a scalar", "from: Evidence\nwhere: {field: passed, where: {field: serial, operator: exists}}\n", `line 2: "passed" is true or false: a sub-query reads the rows of a list or the entities a link reaches`},
		{"sub-query on ^", "from: Evidence\nwhere: {field: equipment_used, where: {field: ^verifies, where: {field: id, operator: exists}}}\n", "line 2: a sub-query reads the item's own list or link, not ^verifies"},
		{"unknown sub-field", "from: Evidence\nwhere: {field: equipment_used, where: {field: serial_no, operator: exists}}\n", `line 2: list "equipment_used" has no sub-field "serial_no" (sub-fields: serial, calibration_due_date)`},
		{"unknown field on a linked entity", "from: Requirement\nwhere: {field: implements, where: {field: passed, operator: exists}}\n", `line 2: UserNeed has no field "passed"`},
		{"^ at the top", "from: Evidence\nwhere: {field: ^passed, operator: exists}\n", "line 2: ^passed: there is no enclosing entity 1 level(s) out"},
		{"^ names an unknown field", "from: Evidence\nwhere: {field: equipment_used, where: {field: serial, operator: equals, value_of: ^serial}}\n", `line 2: Evidence has no field "serial"`},
		{"id of a row", "from: Evidence\nwhere: {field: equipment_used, where: {field: id, operator: exists}}\n", `line 2: a row of "equipment_used" has no id; ^id is the entity's`},
		{"exists with value", "from: Evidence\nwhere: {field: passed, operator: exists, value: true}\n", "line 2: exists takes no value"},
		{"missing value", "from: Evidence\nwhere: {field: passed, operator: equals}\n", "line 2: equals needs a value or value_of"},
		{"rows compared", "from: Evidence\nwhere: {field: equipment_used, operator: equals, value: A}\n", `line 2: "equipment_used" is a list of rows`},
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

func TestMultiTypeQueryBlocks(t *testing.T) {
	g := graph(t, evidence)
	for _, tc := range []struct{ name, src, want string }{
		{"one list, ordered together", "from: [Evidence, Requirement]\norder_by: [execution_date, id]\n", "EVD-1 EVD-2 EVD-3 REQ-0001 REQ-0002 REQ-0003"},
		{"order field on one type", "from: [Requirement, Evidence]\norder_by: order\n", "REQ-0001 REQ-0002 REQ-0003 EVD-1 EVD-2 EVD-3"},
		{"field on one type: empty on the other", "from: [Requirement, Evidence]\nwhere: {field: passed, operator: exists}\n", "EVD-1 EVD-2"},
		{"not_equals holds where the field is missing", "from: [Requirement, Evidence]\nwhere: {field: status, operator: not_equals, value: approved}\n", "EVD-1 EVD-2 EVD-3 REQ-0003"},
		{"shared field", "from: [Requirement, UserNeed]\nwhere: {field: status, operator: equals, value: deprecated}\n", "USR-0002"},
		{"any_of across types", "from: [Requirement, Evidence]\nwhere:\n  any_of:\n    - {field: order, operator: equals, value: 1}\n    - {field: passed, operator: equals, value: false}\n", "EVD-2 REQ-0001"},
		{"rows on one type", "from: [Requirement, Evidence]\nwhere: {field: equipment_used, where: {field: serial, operator: equals, value: C}}\n", "EVD-2"},
		{"missing on the type without the field", "from: [Requirement, Evidence]\nwhere: {field: passed, operator: missing}\n", "EVD-3 REQ-0001 REQ-0002 REQ-0003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, g, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMultiTypeQueryBlockErrors(t *testing.T) {
	g := graph(t, evidence)
	for _, tc := range []struct{ name, src, want string }{
		{"on neither type", "from: [Requirement, Evidence]\nwhere: {field: hazrd, operator: exists}\n", `line 2: neither Requirement nor Evidence has a field "hazrd"`},
		{"on none of three", "from: [Requirement, Evidence, UserNeed]\norder_by: rank\n", `line 2: order_by: none of Requirement, Evidence, UserNeed has a field "rank"`},
		{"render field on neither", "from: [Requirement, Evidence]\nrender: field:label\n", `line 2: render: neither Requirement nor Evidence has a field "label"`},
		{"link on one type only", "from: [Requirement, Evidence]\nwhere: {field: verifies, operator: exists}\n", ""},
		{"different value types", "from: [Requirement, Evidence]\nwhere: {field: title, operator: exists}\n", "line 2: title is text on Requirement but a date on Evidence"},
		{"listed twice", "from: [Requirement, Requirement]\n", "line 1: from: Requirement is listed twice"},
		{"empty list", "from: []\n", "line 1: from: an empty list of types"},
		{"unknown type in list", "from: [Requirement, Rsk]\n", `line 1: from: unknown type "Rsk"`},
		{"wrong literal on the type that has it", "from: [Requirement, Evidence]\nwhere: {field: passed, operator: equals, value: yes please}\n", "line 2: passed is true or false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBlock(tc.src, g.Schema)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
		})
	}
}

func TestQueryBlockTemplates(t *testing.T) {
	g := graph(t, evidence)
	b, err := ParseBlock("from: [Requirement, Evidence]\ntemplates:\n  Evidence: evidence-row\n", g.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.TemplateFor("Evidence"); got != (TemplateChoice{Type: "Evidence", Name: "evidence-row", Line: 3}) {
		t.Errorf("Evidence: %+v", got)
	}
	if got := b.TemplateFor("Requirement"); got.Name != "" {
		t.Errorf("Requirement should keep its default, got %+v", got)
	}
	b, err = ParseBlock("from: [Requirement, Evidence]\ntemplate: compact\n", g.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"Requirement", "Evidence"} {
		if got := b.TemplateFor(typ).Name; got != "compact" {
			t.Errorf("%s: template %q, want compact", typ, got)
		}
	}

	for _, tc := range []struct{ name, src, want string }{
		{"both keys", "from: Requirement\ntemplate: a\ntemplates: {Requirement: b}\n", "line 3: use template: (every result) or templates: (per type), not both"},
		{"with render id", "from: Requirement\nrender: id\ntemplate: a\n", "line 2: template: applies only to render: full"},
		{"type not selected", "from: Requirement\ntemplates: {Evidence: a}\n", "line 2: templates: Evidence is not selected by from (Requirement)"},
		{"type template name", "from: Requirement\ntemplate: Requirement\n", `line 2: template: "Requirement" is not a template name: named templates are lower-case kebab-case`},
		{"path", "from: Requirement\ntemplates: {Requirement: templates/a.tmpl}\n", `line 2: templates: "templates/a.tmpl" is not a template name`},
		{"not a mapping", "from: Requirement\ntemplates: [a]\n", "line 2: templates: expected a mapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBlock(tc.src, g.Schema)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
		})
	}
}

// Each match carries the rows and links its top-level sub-queries matched,
// placed where they are written.
func TestMatchItems(t *testing.T) {
	all := maps.Clone(evidence)
	maps.Copy(all, aggregates)
	g := graph(t, all)
	items := func(src string) string {
		t.Helper()
		b, err := ParseBlock(src, g.Schema)
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
		return strings.Join(out, "; ")
	}
	for _, tc := range []struct{ name, src, want string }{
		{"rows", "from: Evidence\nwhere: {field: equipment_used, where: {field: calibration_due_date, operator: less_than, value: 2026-12-31}}\n",
			"EVD-1: equipment_used[0]@EVD-1:8 equipment_used[1]@EVD-1:9; EVD-2: equipment_used[0]@EVD-2:7"},
		{"link", "from: Requirement\nwhere: {field: implements, where: {field: status, operator: equals, value: deprecated}}\n",
			"REQ-0002: implements[-1]USR-0002@REQ-0002:6; REQ-0003: implements[-1]USR-0002@REQ-0003:6"},
		{"facet: where the linking entity writes it", "from: Requirement\nwhere: {field: evidenced_by, where: {field: id, operator: exists}}\n",
			"REQ-0001: evidenced_by[-1]EVD-1@EVD-1:6; REQ-0002: evidenced_by[-1]EVD-3@EVD-3:4"},
		{"two sub-queries, in order", "from: Evidence\nwhere:\n  - {field: verifies, where: {field: id, operator: exists}}\n  - {field: equipment_used, where: {field: serial, operator: equals, value: B}}\n",
			"EVD-1: verifies[-1]REQ-0001@EVD-1:6 equipment_used[1]@EVD-1:9"},
		{"none inside any_of", "from: Evidence\nwhere:\n  any_of:\n    - {field: equipment_used, where: {field: serial, operator: equals, value: C}}\n",
			"EVD-2:"},
		{"other entities, at their id", "from: Requirement\nwhere: {from: Requirement, where: [{field: title, operator: equals, value_of: ^title}, {field: id, operator: not_equals, value_of: ^id}]}\n",
			"REQ-0004: [-1]REQ-0005@REQ-0005:2; REQ-0005: [-1]REQ-0004@REQ-0004:2"},
		{"none without a sub-query", "from: Evidence\nwhere: {field: passed, operator: equals, value: false}\n", "EVD-2:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := items(tc.src); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestViaTypes(t *testing.T) {
	g := graph(t, evidence)
	for _, tc := range []struct{ name, src, want string }{
		{"none", "from: Requirement\nwhere: {field: status, operator: exists}\n", ""},
		{"link", "from: Requirement\nwhere: {field: implements, where: {field: status, operator: exists}}\n", "UserNeed"},
		{"facet", "from: Requirement\nwhere: {field: evidenced_by, where: {field: passed, operator: exists}}\n", "Evidence"},
		{"nested, inside any_of", "from: Evidence\nwhere:\n  any_of:\n    - field: verifies\n      where: {field: implements, where: {field: status, operator: exists}}\n", "Requirement UserNeed"},
		{"aggregation without where", "from: Requirement\nwhere: {field: evidenced_by, aggregation: {type: count}, operator: equals, value: 0}\n", "Evidence"},
		{"from", "from: Requirement\nwhere: {from: [UserNeed, Risk], where: {field: title, operator: exists}}\n", "Risk UserNeed"},
		{"rows' links", "from: Risk\nwhere: {field: failure_modes, where: {field: severity, where: {field: score, operator: exists}}}\n", "SeverityLevel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := ParseBlock(tc.src, g.Schema)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(viaTypes(g.Schema, b.From, b.Where), " "); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// aggregates holds entities for aggregation and sub-queries on from: a risk
// with no failure modes, and titles shared within and across types.
var aggregates = map[string]string{
	"RSK-0002": fm("RSK-0002", "Risk", "title: No failure modes\n"),
	"REQ-0004": fm("REQ-0004", "Requirement", "title: Dup\nstatus: approved\n"),
	"REQ-0005": fm("REQ-0005", "Requirement", "title: Dup\nstatus: draft\n"),
	"REQ-0006": fm("REQ-0006", "Requirement", "title: Solo\nstatus: approved\n"),
	"USR-0003": fm("USR-0003", "UserNeed", "title: Dup\n"),
}

func TestAggregationAndOtherEntities(t *testing.T) {
	all := maps.Clone(evidence)
	maps.Copy(all, aggregates)
	g := graph(t, all)
	// RSK-0001's row ratings are 5, 15 and 25 (calc_test.go).
	for _, tc := range []struct{ name, src, want string }{
		{"count of a facet", "from: Requirement\nwhere: {field: evidenced_by, aggregation: {type: count}, operator: less_than, value: 1}\n", "REQ-0003 REQ-0004 REQ-0005 REQ-0006"},
		{"count of matching items", "from: Requirement\nwhere:\n  field: evidenced_by\n  where: {field: passed, operator: equals, value: true}\n  aggregation: {type: count}\n  operator: greater_or_equal\n  value: 1\n", "REQ-0001"},
		{"count of rows", "from: Evidence\nwhere: {field: equipment_used, aggregation: {type: count}, operator: equals, value: 2}\n", "EVD-1"},
		{"count of no rows is 0", "from: Evidence\nwhere: {field: equipment_used, aggregation: {type: count}, operator: equals, value: 0}\n", "EVD-3"},
		{"sum", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: sum, field: row_rating}, operator: equals, value: 45}\n", "RSK-0001"},
		{"sum of nothing is 0", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: sum, field: row_rating}, operator: equals, value: 0}\n", "RSK-0002"},
		{"min", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: min, field: row_rating}, operator: equals, value: 5}\n", "RSK-0001"},
		{"max", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: max, field: row_rating}, operator: equals, value: 25}\n", "RSK-0001"},
		{"avg", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: avg, field: row_rating}, operator: equals, value: 15}\n", "RSK-0001"},
		{"max of nothing is blank", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: max, field: row_rating}, operator: missing}\n", "RSK-0002"},
		{"not_equals holds on a blank aggregate", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: max, field: row_rating}, operator: not_equals, value: 25}\n", "RSK-0002"},
		{"sum over linked entities, through rows", "from: Risk\nwhere:\n  field: failure_modes\n  where:\n    field: occurrence\n    aggregation: {type: sum, field: score}\n    operator: equals\n    value: 3\n", "RSK-0001"},
		{"aggregate against value_of", "from: Requirement\nwhere: {field: implements, aggregation: {type: count}, operator: equals, value_of: order}\n", "REQ-0001"},
		{"aggregation in any_of", "from: Evidence\nwhere:\n  any_of:\n    - {field: equipment_used, aggregation: {type: count}, operator: greater_than, value: 1}\n    - {field: passed, operator: equals, value: false}\n", "EVD-1 EVD-2"},
		// Another entity sharing a value: a sub-query on from, comparing
		// with ^ and excluding the entity itself.
		{"another with the same title", "from: Requirement\nwhere:\n  from: Requirement\n  where: [{field: title, operator: equals, value_of: ^title}, {field: id, operator: not_equals, value_of: ^id}]\n", "REQ-0004 REQ-0005"},
		{"same title and status", "from: Requirement\nwhere:\n  from: Requirement\n  where: [{field: title, operator: equals, value_of: ^title}, {field: id, operator: not_equals, value_of: ^id}, {field: status, operator: equals, value_of: ^status}]\n", ""},
		{"across types", "from: [Requirement, UserNeed]\nwhere:\n  from: [Requirement, UserNeed]\n  where: [{field: title, operator: equals, value_of: ^title}, {field: id, operator: not_equals, value_of: ^id}]\n", "REQ-0004 REQ-0005 USR-0003"},
		{"among approved ones only", "from: Requirement\nwhere:\n  from: Requirement\n  where: [{field: title, operator: equals, value_of: ^title}, {field: id, operator: not_equals, value_of: ^id}, {field: status, operator: equals, value: approved}]\n", "REQ-0005"},
		{"filtered outside", "from: Requirement\nwhere:\n  - {field: status, operator: equals, value: approved}\n  - from: Requirement\n    where: [{field: title, operator: equals, value_of: ^title}, {field: id, operator: not_equals, value_of: ^id}]\n", "REQ-0004"},
		{"counting the others", "from: Requirement\nwhere:\n  from: Requirement\n  where: {field: title, operator: equals, value_of: ^title}\n  aggregation: {type: count}\n  operator: greater_than\n  value: 1\n", "REQ-0004 REQ-0005"},
		{"any entity exists", "from: UserNeed\nwhere: {from: Risk, where: {field: title, operator: exists}}\n", "USR-0001 USR-0002 USR-0003"},
		{"none exists", "from: UserNeed\nwhere:\n  from: Risk\n  where: {field: title, operator: equals, value: Nothing}\n  aggregation: {type: count}\n  operator: equals\n  value: 0\n", "USR-0001 USR-0002 USR-0003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, g, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAggregationAndOtherEntitiesErrors(t *testing.T) {
	g := graph(t, evidence)
	for _, tc := range []struct{ name, src, want string }{
		{"no operator", "from: Evidence\nwhere: {field: equipment_used, aggregation: {type: count}}\n", "line 2: an aggregation needs an operator to test it"},
		{"count with a field", "from: Evidence\nwhere: {field: equipment_used, aggregation: {type: count, field: serial}, operator: equals, value: 1}\n", "line 2: count counts the items and reads no field"},
		{"sum without a field", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: sum}, operator: equals, value: 1}\n", "line 2: sum needs the field of each item it reads"},
		{"unknown type", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: median, field: row_rating}, operator: equals, value: 1}\n", "line 2: aggregation type must be one of count, sum, min, max, avg"},
		{"sum of text", "from: Evidence\nwhere: {field: equipment_used, aggregation: {type: sum, field: serial}, operator: equals, value: 1}\n", "line 2: sum reads numbers; serial is text"},
		{"unknown item field", "from: Risk\nwhere: {field: failure_modes, aggregation: {type: max, field: rating}, operator: equals, value: 1}\n", `line 2: list "failure_modes" has no sub-field "rating"`},
		{"aggregate against text", "from: Requirement\nwhere: {field: evidenced_by, aggregation: {type: count}, operator: equals, value: none}\n", `line 2: count of evidenced_by is a number; "none" is not`},
		{"aggregate on a scalar", "from: Evidence\nwhere: {field: passed, aggregation: {type: count}, operator: equals, value: 1}\n", `line 2: "passed" is true or false: a sub-query reads the rows`},
		{"field and from", "from: Requirement\nwhere: {field: title, from: Requirement, where: {field: id, operator: exists}}\n", "line 2: a condition tests a field, or with from looks among other entities, not both"},
		{"from unknown type", "from: Requirement\nwhere: {from: Requirment, where: {field: id, operator: exists}}\n", `line 2: from: unknown type "Requirment"`},
		{"from with an operator", "from: Requirement\nwhere: {from: Requirement, operator: exists}\n", "line 2: a sub-query holds when some item matches its where"},
		{"^ names an unknown field", "from: Requirement\nwhere: {from: UserNeed, where: {field: title, operator: equals, value_of: ^titel}}\n", `line 2: Requirement has no field "titel"`},
		{"from any, a field of two kinds", "from: Requirement\nwhere: {from: any, where: {field: title, operator: equals, value_of: ^title}}\n", "line 2: title is text on Document but a date on Evidence"},
		{"duplicated is gone", "from: Requirement\nwhere: {field: title, operator: duplicated}\n", `line 2: unknown operator "duplicated"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBlock(tc.src, g.Schema)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
		})
	}
}
