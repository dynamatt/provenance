package query

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
	dl "github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
)

// The three fixed cases of spike E1.7, written by hand as the programs the
// condition and formula compilers produce from E1.8 and E1.9.

var exampleSchema = map[string]string{
	"schema/enums/ApprovalStatus.yaml": "enum: ApprovalStatus\nvalues: [draft, in_review, approved, deprecated]\n",
	"schema/UserNeed.yaml": `type: UserNeed
fields:
  - {name: title, type: string}
  - {name: status, type: ApprovalStatus}
`,
	"schema/Requirement.yaml": `type: Requirement
fields:
  - {name: title, type: string}
  - {name: status, type: ApprovalStatus}
  - {name: implements, type: link, target: [UserNeed], cardinality: many, reverse_name: implemented_by}
  - {name: order, type: number}
`,
	"schema/Evidence.yaml": `type: Evidence
fields:
  - {name: execution_date, type: date}
  - {name: passed, type: boolean}
  - {name: verifies, type: link, target: [Requirement], cardinality: one, reverse_name: evidenced_by}
  - name: equipment_used
    type: list
    fields:
      - {name: serial, type: string}
      - {name: calibration_due_date, type: date}
`,
	"schema/SeverityLevel.yaml":   "type: SeverityLevel\nfields:\n  - {name: score, type: number}\n",
	"schema/OccurrenceLevel.yaml": "type: OccurrenceLevel\nfields:\n  - {name: score, type: number}\n",
	"schema/Risk.yaml": `type: Risk
fields:
  - {name: title, type: string}
  - name: failure_modes
    type: list
    fields:
      - {name: severity, type: link, target: [SeverityLevel], cardinality: one}
      - {name: occurrence, type: link, target: [OccurrenceLevel], cardinality: one}
      - name: row_rating
        type: calculated
        formula: "IF(ISBLANK(occurrence.score), severity.score * 5, severity.score * occurrence.score)"
  - name: overall_risk_rating
    type: calculated
    formula: "MAX(failure_modes[].row_rating)"
`,
}

// load builds the typed model from schema files and entity files.
func load(t testing.TB, entities map[string]string) []*model.Entity {
	_, built := loadSchema(t, entities)
	return built
}

func loadSchema(t testing.TB, entities map[string]string) (*schema.Schema, []*model.Entity) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range exampleSchema {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := schema.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var parsed []*entity.Entity
	for path, src := range entities {
		e, err := entity.Parse(path, []byte(src))
		if err != nil || e == nil {
			t.Fatalf("%s: %v", path, err)
		}
		parsed = append(parsed, e)
	}
	// model.Build expects ID order, as entity.Discover returns.
	sortByID(parsed)
	built, err := model.Build(s, parsed)
	if err != nil {
		t.Fatal(err)
	}
	return s, built
}

func sortByID(es []*entity.Entity) {
	sort.Slice(es, func(i, j int) bool { return es[i].ID < es[j].ID })
}

func fm(id, typ, front string) string {
	return "---\nid: " + id + "\ntype: " + typ + "\n" + front + "---\n"
}

// exampleGraph mirrors the provenance-example entities the three cases read.
var exampleGraph = map[string]string{
	"USR-0001": fm("USR-0001", "UserNeed", "status: approved\n"),
	"USR-0002": fm("USR-0002", "UserNeed", "status: deprecated\n"),
	"REQ-0001": fm("REQ-0001", "Requirement", "status: approved\norder: 1\nimplements: [USR-0001]\n"),
	"REQ-0002": fm("REQ-0002", "Requirement", "status: approved\norder: 2\nimplements: [USR-0002]\n"),
	"REQ-0003": fm("REQ-0003", "Requirement", "status: in_review\norder: 3\nimplements: [USR-0002]\n"),
	"SEV-0003": fm("SEV-0003", "SeverityLevel", "score: 5\n"),
	"OCC-0001": fm("OCC-0001", "OccurrenceLevel", "score: 1\n"),
	"OCC-0002": fm("OCC-0002", "OccurrenceLevel", "score: 3\n"),
	"RSK-0001": fm("RSK-0001", "Risk", `failure_modes:
  - {severity: SEV-0003, occurrence: OCC-0001}
  - {severity: SEV-0003, occurrence: OCC-0002}
  - {severity: SEV-0003}
`),
}

var (
	E, T, R, V, O, S, X = dl.Var("E"), dl.Var("T"), dl.Var("R"), dl.Var("V"), dl.Var("O"), dl.Var("S"), dl.Var("X")
	W                   = dl.Var("_")
)

func str(s string) dl.Term { return dl.Const(dl.String(s)) }

// Case 1, DOC-0001's query block:
//
//	from: Requirement
//	where: {field: status, operator: equals, value: approved}
//	order_by: order
func docQuery() *dl.Program {
	var p dl.Program
	p.Add(dl.Rule{Head: dl.NewAtom("match", E), Body: []dl.Literal{
		dl.P(RelEntity, E, str("Requirement")),
		dl.P(RelField, E, str("status"), V),
		dl.Cmp(V, dl.Eq, str("approved")),
	}})
	// order_by reads the sort key alongside each match; entities without
	// one sort last.
	p.Add(dl.Rule{Head: dl.NewAtom("sort_key", E, V), Body: []dl.Literal{dl.P("match", E), dl.P(RelField, E, str("order"), V)}})
	return &p
}

// Case 2, rules/no-implements-deprecated-need.yaml:
//
//	from: Requirement
//	where: {field: {via: implements, field: status}, operator: equals, value: deprecated}
func viaAssertion() *dl.Program {
	var p dl.Program
	p.Add(dl.Rule{Head: dl.NewAtom("violation", E), Body: []dl.Literal{
		dl.P(RelEntity, E, str("Requirement")),
		dl.P(RelLink, E, str("implements"), T),
		dl.P(RelField, T, str("status"), V),
		dl.Cmp(V, dl.Eq, str("deprecated")),
	}})
	return &p
}

// Case 3, Risk's calculated fields:
//
//	row_rating = IF(ISBLANK(occurrence.score), severity.score * 5, severity.score * occurrence.score)
//	overall_risk_rating = MAX(failure_modes[].row_rating)
func riskRating() *dl.Program {
	var p dl.Program
	failureMode := dl.P(RelRow, W, str("failure_modes"), R)
	sevScore := []dl.Literal{dl.P(RelLink, R, str("severity"), T), dl.P(RelField, T, str("score"), S)}
	p.Add(
		dl.Rule{Head: dl.NewAtom("occ_score", R, O), Body: []dl.Literal{failureMode, dl.P(RelLink, R, str("occurrence"), T), dl.P(RelField, T, str("score"), O)}},
		dl.Rule{Head: dl.NewAtom("has_occ_score", R), Body: []dl.Literal{dl.P("occ_score", R, W)}},
		dl.Rule{Head: dl.NewAtom("row_rating", R, X), Body: append([]dl.Literal{failureMode, dl.P("occ_score", R, O), dl.Calc("X", S, dl.Mul, O)}, sevScore...)},
		dl.Rule{Head: dl.NewAtom("row_rating", R, X), Body: append([]dl.Literal{failureMode, dl.N("has_occ_score", R), dl.Calc("X", S, dl.Mul, dl.Const(dl.Number(5)))}, sevScore...)},
		dl.Rule{
			Head: dl.NewAtom("overall_risk_rating", E, dl.Var("M")),
			Body: []dl.Literal{dl.P(RelRow, E, str("failure_modes"), R), dl.P("row_rating", R, X)},
			Agg:  &dl.Aggregate{Func: dl.Max, Pos: 1, Of: "X"},
		},
	)
	return &p
}

func run(t testing.TB, p *dl.Program, entities []*model.Entity) *dl.Database {
	t.Helper()
	out, err := dl.Eval(p, Facts(entities))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func rows(db *dl.Database, rel string) string {
	var lines []string
	for _, t := range db.Relation(rel).Tuples() {
		parts := make([]string, len(t))
		for i, v := range t {
			parts[i] = v.String()
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	return strings.Join(lines, "\n")
}

func TestSpikeDocumentQuery(t *testing.T) {
	out := run(t, docQuery(), load(t, exampleGraph))
	if got, want := rows(out, "sort_key"), `"REQ-0001" 1`+"\n"+`"REQ-0002" 2`; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSpikeViaAssertion(t *testing.T) {
	out := run(t, viaAssertion(), load(t, exampleGraph))
	if got, want := rows(out, "violation"), `"REQ-0002"`+"\n"+`"REQ-0003"`; got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSpikeRiskRating(t *testing.T) {
	out := run(t, riskRating(), load(t, exampleGraph))
	want := `#RSK-0001/failure_modes/0 5` + "\n" + `#RSK-0001/failure_modes/1 15` + "\n" + `#RSK-0001/failure_modes/2 25`
	if got := rows(out, "row_rating"); got != want {
		t.Errorf("row_rating:\n%s\nwant\n%s", got, want)
	}
	if got, want := rows(out, "overall_risk_rating"), `"RSK-0001" 25`; got != want {
		t.Errorf("overall_risk_rating: %s, want %s", got, want)
	}
}

// syntheticGraph has n entities shaped like a design history file: a fifth
// user needs, two fifths requirements, a fifth risks with three failure
// modes each, a fifth severity and occurrence levels.
func syntheticGraph(n int) map[string]string {
	g := map[string]string{}
	per := n / 5
	statuses := []string{"draft", "in_review", "approved", "deprecated"}
	for i := range per {
		id := fmt.Sprintf("USR-%05d", i)
		g[id] = fm(id, "UserNeed", "status: "+statuses[i%4]+"\n")
		id = fmt.Sprintf("SEV-%05d", i)
		g[id] = fm(id, "SeverityLevel", fmt.Sprintf("score: %d\n", i%5+1))
		id = fmt.Sprintf("OCC-%05d", i)
		g[id] = fm(id, "OccurrenceLevel", fmt.Sprintf("score: %d\n", i%5+1))
	}
	for i := range 2 * per {
		id := fmt.Sprintf("REQ-%05d", i)
		g[id] = fm(id, "Requirement", fmt.Sprintf("status: %s\norder: %d\nimplements: [USR-%05d, USR-%05d]\n",
			statuses[i%4], 2*per-i, i%per, (i*7)%per))
	}
	for i := range per {
		id := fmt.Sprintf("RSK-%05d", i)
		var b strings.Builder
		b.WriteString("failure_modes:\n")
		for j := range 3 {
			fmt.Fprintf(&b, "  - {severity: SEV-%05d", (i+j)%per)
			if j != 2 || i%2 == 0 {
				fmt.Fprintf(&b, ", occurrence: OCC-%05d", (i*3+j)%per)
			}
			b.WriteString("}\n")
		}
		g[id] = fm(id, "Risk", b.String())
	}
	return g
}

func BenchmarkSpike(b *testing.B) {
	entities := load(b, syntheticGraph(10000))
	facts := Facts(entities)
	b.Logf("%d entities", len(entities))
	for name, p := range map[string]*dl.Program{"document-query": docQuery(), "via-assertion": viaAssertion(), "risk-rating": riskRating()} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if _, err := dl.Eval(p, facts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	b.Run("facts", func(b *testing.B) {
		for b.Loop() {
			Facts(entities)
		}
	})
}
