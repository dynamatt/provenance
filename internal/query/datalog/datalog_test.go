package datalog

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

var (
	s = String
	n = Number
	X = Var("X")
	Y = Var("Y")
	Z = Var("Z")
	W = Var("_") // anonymous
)

func c(v Value) Term { return Const(v) }

func eval(t *testing.T, p *Program, db *Database) *Database {
	t.Helper()
	out, err := Eval(p, db)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// dump renders a relation as sorted tuples, one per line.
func dump(db *Database, rel string) string {
	var b strings.Builder
	if r := db.Relation(rel); r != nil {
		for _, t := range r.Tuples() {
			parts := make([]string, len(t))
			for i, v := range t {
				parts[i] = v.String()
			}
			fmt.Fprintf(&b, "(%s)\n", strings.Join(parts, " "))
		}
	}
	return b.String()
}

func check(t *testing.T, db *Database, rel, want string) {
	t.Helper()
	if got := dump(db, rel); got != want {
		t.Errorf("%s:\n%s\nwant:\n%s", rel, got, want)
	}
}

func TestRecursion(t *testing.T) {
	db := NewDatabase()
	for _, e := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "b"}} {
		db.MustAdd("parent", s(e[0]), s(e[1]))
	}
	var p Program
	p.Add(
		Rule{Head: NewAtom("anc", X, Y), Body: []Literal{P("parent", X, Y)}},
		Rule{Head: NewAtom("anc", X, Z), Body: []Literal{P("anc", X, Y), P("parent", Y, Z)}},
	)
	out := eval(t, &p, db)
	if got := out.Relation("anc").Len(); got != 12 {
		// a reaches b c d; b, c and d each reach b c d (a cycle).
		t.Errorf("anc has %d tuples, want 12:\n%s", got, dump(out, "anc"))
	}
	if db.Relation("anc") != nil {
		t.Error("Eval changed its input database")
	}
}

func TestNegationAndBuiltins(t *testing.T) {
	db := NewDatabase()
	db.MustAdd("field", s("R1"), s("status"), s("approved"))
	db.MustAdd("field", s("R2"), s("status"), s("draft"))
	db.MustAdd("field", s("R1"), s("order"), n(2))
	db.MustAdd("field", s("R2"), s("order"), n(1))
	db.MustAdd("entity", s("R3"), s("Requirement"))
	for _, id := range []string{"R1", "R2"} {
		db.MustAdd("entity", s(id), s("Requirement"))
	}
	var p Program
	p.Add(
		Rule{Head: NewAtom("approved", X), Body: []Literal{P("field", X, c(s("status")), c(s("approved")))}},
		Rule{Head: NewAtom("unapproved", X), Body: []Literal{P("entity", X, W), N("approved", X)}},
		Rule{Head: NewAtom("doubled", X, Z), Body: []Literal{Calc("Z", Y, Mul, c(n(2))), P("field", X, c(s("order")), Y)}},
		Rule{Head: NewAtom("late", X), Body: []Literal{P("field", X, c(s("order")), Y), Cmp(Y, Gt, c(n(1)))}},
		Rule{Head: NewAtom("numeric", X), Body: []Literal{P("field", X, W, Y), Cmp(Y, IsKind, c(s("number")))}},
		Rule{Head: NewAtom("bound", X, Y), Body: []Literal{P("entity", X, W), Cmp(Y, Eq, c(s("k")))}},
	)
	out := eval(t, &p, db)
	check(t, out, "unapproved", "(\"R2\")\n(\"R3\")\n")
	check(t, out, "doubled", "(\"R1\" 4)\n(\"R2\" 2)\n")
	check(t, out, "late", "(\"R1\")\n")
	check(t, out, "numeric", "(\"R1\")\n(\"R2\")\n")
	check(t, out, "bound", "(\"R1\" \"k\")\n(\"R2\" \"k\")\n(\"R3\" \"k\")\n")
}

func TestAggregationCountsEqualValuesSeparately(t *testing.T) {
	db := NewDatabase()
	// Two rows with the same rating: a sum over the set of values would
	// count 3 once.
	db.MustAdd("rating", s("E"), NodeValue("E/0"), n(3))
	db.MustAdd("rating", s("E"), NodeValue("E/1"), n(3))
	db.MustAdd("rating", s("E"), NodeValue("E/2"), n(5))
	db.MustAdd("rating", s("F"), NodeValue("F/0"), s("not a number"))
	R, V, A := Var("R"), Var("V"), Var("A")
	var p Program
	for _, f := range []AggFunc{Max, Min, Sum, Count, Avg} {
		p.Add(Rule{Head: NewAtom(string(f), X, A), Body: []Literal{P("rating", X, R, V)}, Agg: &Aggregate{Func: f, Pos: 1, Of: "V"}})
	}
	out := eval(t, &p, db)
	check(t, out, "max", "(\"E\" 5)\n")
	check(t, out, "min", "(\"E\" 3)\n")
	check(t, out, "sum", "(\"E\" 11)\n(\"F\" 0)\n")
	check(t, out, "count", "(\"E\" 3)\n(\"F\" 1)\n")
	check(t, out, "avg", "(\"E\" 3.6666666666666665)\n")
}

func TestRejectedPrograms(t *testing.T) {
	A := Var("A")
	for name, tc := range map[string]struct {
		rules []Rule
		want  string
	}{
		"unbound head": {
			[]Rule{{Head: NewAtom("p", X, Y), Body: []Literal{P("q", X)}}},
			"head variable Y is not bound",
		},
		"unbound negation": {
			[]Rule{{Head: NewAtom("p", X), Body: []Literal{P("q", X), N("r", Y)}}},
			"unbound variable",
		},
		"unbound comparison": {
			[]Rule{{Head: NewAtom("p", X), Body: []Literal{P("q", X), Cmp(Y, Lt, c(n(1)))}}},
			"unbound variable",
		},
		"negation cycle": {
			[]Rule{
				{Head: NewAtom("p", X), Body: []Literal{P("q", X), N("r", X)}},
				{Head: NewAtom("r", X), Body: []Literal{P("p", X)}},
			},
			"depends on itself through negation",
		},
		"aggregation cycle": {
			[]Rule{{Head: NewAtom("p", X, A), Body: []Literal{P("p", X, Y)}, Agg: &Aggregate{Func: Max, Pos: 1, Of: "Y"}}},
			"depends on itself through negation or aggregation",
		},
		"arithmetic in recursion": {
			[]Rule{
				{Head: NewAtom("count", c(n(0))), Body: []Literal{P("start")}},
				{Head: NewAtom("count", Y), Body: []Literal{P("count", X), Calc("Y", X, Add, c(n(1)))}},
			},
			"arithmetic inside recursion",
		},
		"arity mismatch": {
			[]Rule{{Head: NewAtom("p", X), Body: []Literal{P("q", X), P("q", X, X)}}},
			"used with 1 arguments and with 2",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Eval(&Program{Rules: tc.rules}, NewDatabase())
			var de *Error
			if !errors.As(err, &de) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestRepeatedVariableInAtom(t *testing.T) {
	db := NewDatabase()
	db.MustAdd("edge", s("a"), s("a"))
	db.MustAdd("edge", s("a"), s("b"))
	var p Program
	p.Add(Rule{Head: NewAtom("loop", X), Body: []Literal{P("edge", X, X)}})
	check(t, eval(t, &p, db), "loop", "(\"a\")\n")
}

func TestKindsNeverMix(t *testing.T) {
	db := NewDatabase()
	db.MustAdd("v", s("2"))
	db.MustAdd("v", n(2))
	db.MustAdd("v", NodeValue("2"))
	var p Program
	p.Add(Rule{Head: NewAtom("two", X), Body: []Literal{P("v", X), Cmp(X, Eq, c(n(2)))}})
	p.Add(Rule{Head: NewAtom("small", X), Body: []Literal{P("v", X), Cmp(X, Lt, c(n(3)))}})
	out := eval(t, &p, db)
	check(t, out, "two", "(2)\n")
	check(t, out, "small", "(2)\n")
	if got := out.Relation("v").Len(); got != 3 {
		t.Errorf("v has %d tuples, want 3 distinct kinds", got)
	}
}
