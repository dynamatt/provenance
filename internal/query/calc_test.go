package query

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

var calcFormulas = []struct{ name, formula string }{
	{"arith", "a + b * 2"},
	{"paren", "=(a + b) / 2"},
	{"neg", "-a"},
	{"divzero", "a / 0"},
	{"mixed", "a + s"},
	{"gt", "a > b"},
	{"eqs", `s = "x"`},
	{"quote", `"say ""hi"""`},
	{"ifv", `if(flag, "yes", "no")`},
	{"if2", "IF(flag, 1)"},
	{"andv", "AND(flag, a > 2)"},
	{"orv", "OR(flag, a > 5)"},
	{"notv", "NOT(flag)"},
	{"blank", "ISBLANK(b)"},
	{"sum", "SUM(nums)"},
	{"count", "COUNT(nums)"},
	{"avg", "AVG(nums)"},
	{"maxmix", "MAX(rows[].x, a)"},
	{"sumrows", "SUM(rows[].x)"},
	{"rowsum", "SUM(rows[].y)"},
	{"partnera", "partner.a"},
	{"sumpeers", "SUM(peers.a)"},
	{"facet", "COUNT(partnered_by)"},
	{"chain", "arith * 2"},
	{"trueish", "TRUE"},
	// Formulas that cannot be evaluated.
	{"p1", "a +"},
	{"p2", "FOO(1)"},
	{"p3", "rows"},
	{"p4", "a.b"},
	{"outside", "peers.a"},
	{"nofield", "nope + 1"},
	{"cyc1", "cyc2 + 1"},
	{"cyc2", "cyc1 + 1"},
	{"self", "self + 1"},
	{"dep", "p1 + 1"},
}

func calcSchema() map[string]string {
	var b strings.Builder
	b.WriteString(`type: Calc
fields:
  - {name: a, type: number}
  - {name: b, type: number}
  - {name: s, type: string}
  - {name: flag, type: boolean}
  - {name: nums, type: list, of: number}
  - {name: partner, type: link, target: [Calc], cardinality: one, reverse_name: partnered_by}
  - {name: peers, type: link, target: [Calc], cardinality: many}
  - name: rows
    type: list
    fields:
      - {name: x, type: number}
      - {name: peer, type: link, target: [Calc], cardinality: one}
      - {name: y, type: calculated, formula: "x * 10"}
      - {name: z, type: calculated, formula: "peer.a"}
`)
	for _, f := range calcFormulas {
		fmt.Fprintf(&b, "  - {name: %s, type: calculated, formula: %s}\n", f.name, strconv.Quote(f.formula))
	}
	return map[string]string{"schema/Calc.yaml": b.String()}
}

var calcEntities = map[string]string{
	"C-1": fm("C-1", "Calc", "a: 3\nb: 1\ns: x\nflag: true\nnums: [2, 2, 3]\npartner: C-2\npeers: [C-2, C-3]\nrows:\n  - {x: 5, peer: C-2}\n  - {x: 5}\n"),
	"C-2": fm("C-2", "Calc", "a: 10\nflag: false\n"),
	"C-3": fm("C-3", "Calc", "a: 10\n"),
}

// show renders a value: "" for blank, "!problem" for a formula that cannot
// be evaluated.
func show(v *model.Value) string {
	switch {
	case !v.Present:
		return ""
	case v.Invalid:
		return "!" + v.Problem
	}
	switch v.Result {
	case schema.Number:
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	case schema.Boolean:
		return strconv.FormatBool(v.Bool)
	}
	return strconv.Quote(v.Str)
}

func calcGraph(t *testing.T) *Graph {
	t.Helper()
	g, err := NewGraph(loadWith(t, calcSchema(), calcEntities))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCalculatedFields(t *testing.T) {
	g := calcGraph(t)
	want := map[string][3]string{ // C-1, C-2, C-3
		"arith":    {"5", "", ""},
		"paren":    {"2", "", ""},
		"neg":      {"-3", "-10", "-10"},
		"divzero":  {"", "", ""},
		"mixed":    {"", "", ""},
		"gt":       {"true", "", ""},
		"eqs":      {"true", "", ""},
		"quote":    {`"say \"hi\""`, `"say \"hi\""`, `"say \"hi\""`},
		"ifv":      {`"yes"`, `"no"`, ""},
		"if2":      {"1", "false", ""},
		"andv":     {"true", "false", ""},
		"orv":      {"true", "true", "true"},
		"notv":     {"false", "true", ""},
		"blank":    {"false", "true", "true"},
		"sum":      {"7", "0", "0"},
		"count":    {"3", "0", "0"},
		"avg":      {"2.3333333333333335", "", ""},
		"maxmix":   {"5", "10", "10"},
		"sumrows":  {"10", "0", "0"},
		"rowsum":   {"100", "0", "0"},
		"partnera": {"10", "", ""},
		"sumpeers": {"20", "0", "0"},
		"facet":    {"0", "1", "0"},
		"chain":    {"10", "", ""},
		"trueish":  {"true", "true", "true"},
		"p1":       {"!formula: column 4: unexpected end of formula"},
		"p2":       {"!formula: column 1: unknown function FOO (functions: IF, AND, OR, NOT, ISBLANK, MAX, MIN, SUM, COUNT, AVG)"},
		"p3":       {"!formula: column 1: rows is a list of rows; name one of its fields: rows[].<field>"},
		"p4":       {"!formula: column 1: a is a number, not a link: cannot read a.b"},
		"outside":  {"!formula: column 1: peers.a has several values; use it inside MAX, MIN, SUM, COUNT, AVG or ISBLANK"},
		"nofield":  {`!formula: column 1: Calc has no field "nope"`},
		"cyc1":     {"!formula depends on itself: Calc.cyc1 → Calc.cyc2 → Calc.cyc1"},
		"cyc2":     {"!formula depends on itself: Calc.cyc2 → Calc.cyc1 → Calc.cyc2"},
		"self":     {"!formula depends on itself: Calc.self → Calc.self"},
		"dep":      {"", "", ""}, // reads an invalid formula: blank
	}
	for name, w := range want {
		for i, id := range []string{"C-1", "C-2", "C-3"} {
			got := show(g.Entity(id).Field(name))
			exp := w[i]
			if strings.HasPrefix(w[0], "!") {
				exp = w[0] // invalid on every entity
			}
			ok := got == exp
			if strings.HasPrefix(exp, "!") {
				ok = strings.HasPrefix(got, exp)
			}
			if !ok {
				t.Errorf("%s on %s = %s, want %s", name, id, got, exp)
			}
		}
	}
}

func TestCalculatedRowFields(t *testing.T) {
	g := calcGraph(t)
	rows := g.Entity("C-1").Field("rows").Rows
	var got []string
	for _, row := range rows {
		got = append(got, show(row[2])+"/"+show(row[3]))
	}
	if strings.Join(got, " ") != "50/10 50/" {
		t.Errorf("rows y/z = %v", got)
	}
}

func TestQueriesReadCalculatedFields(t *testing.T) {
	g := graph(t, nil)
	if got := ids(t, g, "from: Risk\nwhere: {field: overall_risk_rating, operator: greater_than, value: 20}\norder_by: overall_risk_rating\n"); got != "RSK-0001" {
		t.Errorf("got %q", got)
	}
	if got := ids(t, g, "from: Risk\nwhere: {field: {list: failure_modes, subfield: row_rating}, operator: equals, value: 15}\n"); got != "RSK-0001" {
		t.Errorf("got %q", got)
	}
}

func TestExampleRiskRating(t *testing.T) {
	g := graph(t, nil)
	rsk := g.Entity("RSK-0001")
	var rr []string
	for _, row := range rsk.Field("failure_modes").Rows {
		rr = append(rr, show(row[2]))
	}
	if strings.Join(rr, " ") != "5 15 25" {
		t.Errorf("row_rating = %v, want 5 15 25", rr)
	}
	if got := show(rsk.Field("overall_risk_rating")); got != "25" {
		t.Errorf("overall_risk_rating = %s, want 25", got)
	}
}

func TestParseFormulaErrors(t *testing.T) {
	for src, want := range map[string]string{
		`"open`:     "column 1: unterminated string",
		"a [1]":     "column 3: expected [] (a list's rows)",
		"a # b":     "column 3: unexpected '#'",
		"(a + b":    "column 7: expected )",
		"MAX(a b)":  "column 7: expected , or )",
		"NOT(a, b)": "column 1: NOT takes 1 argument(s), got 2",
		"IF(a)":     "column 1: IF takes 2 or 3 arguments, got 1",
		"SUM()":     "column 1: SUM needs at least one argument",
		"a.":        "column 3: expected a field name after .",
		"a b":       `column 3: unexpected "b"`,
		"1.2.3":     `column 1: "1.2.3" is not a number`,
	} {
		_, err := ParseFormula(src)
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %s", src, err, want)
		}
	}
}
