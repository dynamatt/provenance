package validate

import (
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/load/loadtest"
)

// A rule selects entities from any source, and reports each at its own
// place: a row of a file holding several entities is reported at that row.
func TestRulesOnASecondSource(t *testing.T) {
	load.UseTestSource(t, loadtest.Rows{})
	root := ruleRepo(t, map[string]string{
		"draft.yaml": "severity: error\nmessage: status is draft\nfrom: Part\nwhere: {field: status, operator: equals, value: draft}\n",
	})
	write(t, root, "data/parts.rows", "- id: PRT-0101\n  type: Part\n  title: Bolt\n  status: approved\n- id: PRT-0102\n  type: Part\n  title: Nut\n  status: draft\n")
	got := text(t, root)
	if want := "data/parts.rows:8: error [draft] status is draft (PRT-0102)\n"; !strings.Contains(got, want) {
		t.Errorf("missing %q in:\n%s", want, got)
	}
	if strings.Contains(got, "PRT-0101") {
		t.Errorf("PRT-0101 reported:\n%s", got)
	}
}
