package validate

import (
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/load/loadtest"
)

// A rule selects entities from any source, and reports each at its own
// place: a row of a file holding several entities is reported at that row.
func TestSourceRules(t *testing.T) {
	load.UseTestSource(t, loadtest.Rows{})
	root := ruleRepo(t, map[string]string{
		"draft.yaml": "severity: error\nmessage: status is draft\nfrom: Part\nwhere: {field: status, operator: equals, value: draft}\n",
	})
	write(t, root, "data/parts.rows", "id | type | title | status\nPRT-0101 | Part | Bolt | approved\nPRT-0102 | Part | Nut | draft\n")
	got := text(t, root)
	if want := "data/parts.rows:3: error [draft] status is draft (PRT-0102)\n"; !strings.Contains(got, want) {
		t.Errorf("missing %q in:\n%s", want, got)
	}
	if strings.Contains(got, "PRT-0101") {
		t.Errorf("PRT-0101 reported:\n%s", got)
	}
}

// An entity of another source is checked against its type through its
// record, which types a value by its field: each problem is placed at the
// record's own position, its row.
func TestSourceConformance(t *testing.T) {
	load.UseTestSource(t, loadtest.Rows{})
	root := ruleRepo(t, nil)
	write(t, root, "schema/Bin.yaml", "type: Bin\nid_prefix: BIN\nfields:\n  - {name: title, type: string, required: true}\n  - {name: status, type: string}\n  - {name: weight, type: number}\n  - {name: uses, type: link, target: [Bin], cardinality: many}\n")
	write(t, root, "data/parts.rows", "id | type | title | weight | uses | colour\nBIN-0101 | Bin | Bolt | 3 | | \nBIN-0102 | Bin | | heavy | BIN-0101; BIN-0999 | red\n")
	got := text(t, root)
	for _, want := range []string{
		"data/parts.rows:3: error [links/unresolved] uses: no entity BIN-0999 (BIN-0102)\n",
		"data/parts.rows:3: error [schema/invalid-value] weight: not a number, got heavy (BIN-0102)\n",
		"data/parts.rows:3: error [schema/required] required field title is not set (BIN-0102)\n",
		`data/parts.rows:3: error [schema/unknown-field] Bin has no field "colour" (fields: title, status, weight, uses) (BIN-0102)` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(BIN-0101)") {
		t.Errorf("BIN-0101 reported:\n%s", got)
	}
}
