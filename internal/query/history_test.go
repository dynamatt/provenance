package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
)

// An entity's changes from git, as conditions and formulas read them
// (DES-0058). The history is given here; internal/load reads it.

var historySchema = map[string]string{
	"schema/Protocol.yaml": `type: Protocol
id_prefix: VER
fields:
  - {name: title, type: string, title: true}
  - {name: status, type: string}
  - name: approvals
    type: list
    fields:
      - {name: date, type: date}
  - {name: approved_on, type: calculated, formula: "MAX(approvals[].date)"}
  - {name: changed_on, type: calculated, formula: "last_changed.date"}
  - {name: changed_in, type: calculated, formula: "last_changed.commit"}
  - {name: first_on, type: calculated, formula: "MIN(changes[].date)"}
  - {name: procedure, type: text, body: true}
`,
	"schema/Evidence.yaml": `type: Evidence
id_prefix: EVD
fields:
  - {name: title, type: string, title: true}
  - {name: executed, type: date}
  - {name: tests, type: link, target: [Protocol], cardinality: one, reverse_name: tested_by}
  - name: justifications
    type: list
    fields:
      - {name: date, type: date}
  - {name: current_as_of, type: calculated, formula: "MAX(executed, justifications[].date)"}
  - {name: mixed, type: calculated, formula: "MAX(executed, 3)"}
`,
}

var historyEntities = map[string]string{
	"VER-0001.md": "---\nid: VER-0001\ntype: Protocol\ntitle: Pump\nstatus: approved\napprovals:\n  - date: 2026-11-04\n---\nPrime it.\n",
	"VER-0002.md": "---\nid: VER-0002\ntype: Protocol\ntitle: Valve\nstatus: approved\napprovals:\n  - date: 2026-11-01\n---\nOpen it.\n",
	// Run before VER-0001's procedure changed on the 3rd; justified after.
	"EVD-0001.md": "---\nid: EVD-0001\ntype: Evidence\ntitle: Pump run\nexecuted: 2026-11-02\ntests: VER-0001\njustifications:\n  - date: 2026-11-05\n---\n",
	// Run before VER-0002's procedure changed on the 3rd; not justified.
	"EVD-0002.md": "---\nid: EVD-0002\ntype: Evidence\ntitle: Valve run\nexecuted: 2026-11-02\ntests: VER-0002\n---\n",
}

// historyOf: VER-0001 added on the 1st (c1), its procedure changed on the
// 3rd (c3), approved on the 4th (c4); VER-0002 added on the 1st and its
// procedure changed on the 3rd (c3) after its approval on the 1st. The
// evidence was added on the 2nd (c2).
func historyOf() *history.EntityHistory {
	return &history.EntityHistory{
		Order: []string{"c1", "c2", "c3", "c4"},
		Changes: map[string][]*history.Change{
			"VER-0001": {
				{Commit: "c4", Date: "2026-11-04", Author: "Bea", Kind: "changed", Fields: []string{"approvals"}},
				{Commit: "c3", Date: "2026-11-03", Author: "Ann", Kind: "changed", Fields: []string{"procedure"}},
				{Commit: "c1", Date: "2026-11-01", Author: "Ann", Kind: "added", Fields: []string{"title", "status", "procedure"}},
			},
			"VER-0002": {
				{Commit: "c3", Date: "2026-11-03", Author: "Ann", Kind: "changed", Fields: []string{"procedure"}},
				{Commit: "c1", Date: "2026-11-01", Author: "Ann", Kind: "added", Fields: []string{"title", "status", "procedure", "approvals"}},
			},
			"EVD-0001": {{Commit: "c2", Date: "2026-11-02", Author: "Cy", Kind: "added", Fields: []string{"title", "executed", "tests"}}},
			"EVD-0002": {{Commit: "c2", Date: "2026-11-02", Author: "Cy", Kind: "added", Fields: []string{"title", "executed", "tests"}}},
		},
	}
}

func historyGraph(t *testing.T, h History) (*Graph, *int) {
	t.Helper()
	reads := 0
	counted := History(nil)
	if h != nil {
		counted = func() (*history.EntityHistory, error) { reads++; return h() }
	}
	s, entities := loadWith(t, historySchema, historyEntities)
	g, err := NewGraphWith(s, entities, counted)
	if err != nil {
		t.Fatal(err)
	}
	return g, &reads
}

func TestHistoryInConditions(t *testing.T) {
	g, reads := historyGraph(t, func() (*history.EntityHistory, error) { return historyOf(), nil })
	for _, tc := range []struct{ name, src, want string }{
		{"stale evidence", `from: Evidence
where:
  field: tests
  where:
    field: changes
    where:
      - {field: kind, operator: equals, value: changed}
      - {field: fields, operator: equals, value: procedure}
      - {field: date, operator: greater_than, value_of: ^^current_as_of}
`, "EVD-0002"},
		{"changed after approval", `from: Protocol
where:
  field: changes
  where:
    - {field: kind, operator: equals, value: changed}
    - {field: fields, operator: equals, value: procedure}
    - {field: date, operator: greater_than, value_of: ^approved_on}
`, "VER-0002"},
		{"last changed by", `from: Protocol
where: {field: last_changed, where: {field: author, operator: equals, value: Bea}}
`, "VER-0001"},
		{"how often", `from: Protocol
where: {field: changes, aggregation: {type: count}, operator: greater_than, value: 2}
`, "VER-0001"},
		{"latest date by aggregation", `from: Protocol
where: {field: changes, aggregation: {type: max, field: date}, operator: equals, value: 2026-11-04}
`, "VER-0001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(t, g, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if *reads != 1 {
		t.Errorf("history read %d times, want once", *reads)
	}
}

func TestHistoryInFormulas(t *testing.T) {
	g, _ := historyGraph(t, func() (*history.EntityHistory, error) { return historyOf(), nil })
	for _, tc := range []struct{ id, field, want string }{
		{"VER-0001", "changed_on", "2026-11-04"},
		{"VER-0001", "changed_in", "c4"},
		{"VER-0001", "first_on", "2026-11-01"},
		{"VER-0001", "approved_on", "2026-11-04"},
		{"EVD-0001", "current_as_of", "2026-11-05"},
		{"EVD-0002", "current_as_of", "2026-11-02"},
		{"EVD-0001", "mixed", ""}, // dates and numbers have no maximum
	} {
		if got := shownValue(g.Entity(tc.id).Field(tc.field)); got != tc.want {
			t.Errorf("%s.%s = %q, want %q", tc.id, tc.field, got, tc.want)
		}
	}
}

func shownValue(v *model.Value) string {
	if v == nil || !v.Present || v.Invalid {
		return ""
	}
	return v.Str
}

// Commits compare by first-parent ancestry: c4 descends from c3.
func TestCommitOrder(t *testing.T) {
	g, _ := historyGraph(t, func() (*history.EntityHistory, error) { return historyOf(), nil })
	got := ids(t, g, `from: Protocol
where:
  field: changes
  where:
    - {field: kind, operator: equals, value: changed}
    - {field: fields, operator: equals, value: procedure}
    - {field: commit, operator: less_than, value_of: ^changed_in}
`)
	// VER-0001's procedure changed at c3, before its last change, c4;
	// VER-0002's last change is its procedure's own, at c3.
	if got != "VER-0001" {
		t.Errorf("got %q, want VER-0001", got)
	}
}

// A graph never reads its history unless asked, and without one, asking
// is an error rather than an empty answer.
func TestHistoryReadOnlyWhenNamed(t *testing.T) {
	s, entities := loadWith(t, map[string]string{"schema/Evidence.yaml": strings.Replace(historySchema["schema/Evidence.yaml"], "  - {name: mixed, type: calculated, formula: \"MAX(executed, 3)\"}\n", "", 1)},
		map[string]string{"EVD-0002.md": "---\nid: EVD-0002\ntype: Evidence\ntitle: Valve run\nexecuted: 2026-11-02\n---\n"})
	plain, err := NewGraphWith(s, entities, func() (*history.EntityHistory, error) { return nil, errors.New("must not be read") })
	if err != nil {
		t.Fatalf("a graph whose formulas don't name history read it: %v", err)
	}
	if got := ids(t, plain, "from: Evidence\n"); got != "EVD-0002" {
		t.Errorf("got %q", got)
	}
	b, err := ParseBlock("from: Evidence\nwhere: {field: last_changed, where: {field: author, operator: exists}}\n", plain.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Run(b); err == nil || !strings.Contains(err.Error(), "must not be read") {
		t.Errorf("err = %v, want the history's error", err)
	}
	none, err := NewGraph(s, entities)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := none.Run(b); !errors.Is(err, ErrNoHistory) {
		t.Errorf("err = %v, want ErrNoHistory", err)
	}
}
