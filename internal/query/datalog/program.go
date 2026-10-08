// Package datalog is the query engine (DES-0013, DES-0014, decided by
// spike E1.7): a small semi-naive Datalog evaluator with stratified negation,
// arithmetic and comparison built-ins, and stratified aggregation.
//
// Programs are built as data, by the compilers for the condition grammar
// (DES-0015) and calculated-field formulas (DES-0012), never parsed from
// user text. Termination is guaranteed by construction: there are no function
// symbols; arithmetic, which is the only way to create a value not already in
// the database, is rejected inside recursion; and negated or aggregated
// relations must be fully computed in a lower stratum before they are read.
package datalog

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Term is a variable or a constant.
type Term struct {
	Var string // non-empty for a variable
	Val Value
}

// Var returns a variable term. "_" is anonymous: each use is a fresh variable.
func Var(name string) Term { return Term{Var: name} }

// Const returns a constant term.
func Const(v Value) Term { return Term{Val: v} }

func (t Term) IsVar() bool { return t.Var != "" }

func (t Term) String() string {
	if t.IsVar() {
		return t.Var
	}
	return t.Val.String()
}

// Atom is a relation applied to terms.
type Atom struct {
	Rel  string
	Args []Term
}

func NewAtom(rel string, args ...Term) Atom { return Atom{Rel: rel, Args: args} }

func (a Atom) String() string {
	parts := make([]string, len(a.Args))
	for i, t := range a.Args {
		parts[i] = t.String()
	}
	return a.Rel + "(" + strings.Join(parts, ", ") + ")"
}

// Op is a built-in operation.
type Op string

const (
	Eq Op = "="
	Ne Op = "!="
	Lt Op = "<"
	Le Op = "<="
	Gt Op = ">"
	Ge Op = ">="

	Add Op = "+"
	Sub Op = "-"
	Mul Op = "*"
	Div Op = "/"

	// IsKind holds when L's value has the kind R names (R a String constant
	// of a Kind's name, e.g. "number").
	IsKind Op = "is"
)

// LitKind is what a body literal does.
type LitKind uint8

const (
	Pos     LitKind = iota // Atom holds
	Neg                    // Atom does not hold
	Compare                // L Op R, Op a comparison or IsKind; Eq binds an unbound variable side
	Arith                  // Out = L Op R, Op arithmetic; division by zero has no result
)

// Literal is one body condition.
type Literal struct {
	Kind LitKind
	Atom Atom
	Op   Op
	L, R Term
	Out  string
}

func P(rel string, args ...Term) Literal { return Literal{Kind: Pos, Atom: NewAtom(rel, args...)} }
func N(rel string, args ...Term) Literal { return Literal{Kind: Neg, Atom: NewAtom(rel, args...)} }
func Cmp(l Term, op Op, r Term) Literal  { return Literal{Kind: Compare, L: l, Op: op, R: r} }
func Calc(out string, l Term, op Op, r Term) Literal {
	return Literal{Kind: Arith, Out: out, L: l, Op: op, R: r}
}

func (l Literal) String() string {
	switch l.Kind {
	case Pos:
		return l.Atom.String()
	case Neg:
		return "not " + l.Atom.String()
	case Compare:
		return fmt.Sprintf("%s %s %s", l.L, l.Op, l.R)
	}
	return fmt.Sprintf("%s = %s %s %s", l.Out, l.L, l.Op, l.R)
}

// AggFunc is an aggregate function.
type AggFunc string

const (
	Max   AggFunc = "max"
	Min   AggFunc = "min"
	Sum   AggFunc = "sum"
	Count AggFunc = "count"
	Avg   AggFunc = "avg"
)

// Aggregate makes a rule aggregate: the head argument at Pos (a variable,
// named nowhere in the body) receives Func over Of, grouped by the head's
// other arguments. Aggregation ranges over the distinct satisfying
// assignments of all body variables, so two rows with equal values both
// count. Max, Min, Sum and Avg read only Num values and ignore the rest; a
// group with none gets no result, except Sum, which is 0. Count counts
// assignments and ignores Of.
type Aggregate struct {
	Func AggFunc
	Pos  int
	Of   string
}

// Rule is Head :- Body. With Agg set it is an aggregate rule.
type Rule struct {
	Head Atom
	Body []Literal
	Agg  *Aggregate
}

func (r Rule) String() string {
	head := r.Head.String()
	if r.Agg != nil {
		args := make([]string, len(r.Head.Args))
		for i, t := range r.Head.Args {
			args[i] = t.String()
		}
		of := r.Agg.Of
		if r.Agg.Func == Count {
			of = ""
		}
		args[r.Agg.Pos] = fmt.Sprintf("%s<%s>", r.Agg.Func, of)
		head = r.Head.Rel + "(" + strings.Join(args, ", ") + ")"
	}
	body := make([]string, len(r.Body))
	for i, l := range r.Body {
		body[i] = l.String()
	}
	return head + " :- " + strings.Join(body, ", ") + "."
}

// Program is a set of rules.
type Program struct {
	Rules []Rule
}

func (p *Program) Add(r ...Rule) { p.Rules = append(p.Rules, r...) }

func (p *Program) String() string {
	var b strings.Builder
	for _, r := range p.Rules {
		b.WriteString(r.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// Error is a program that cannot be evaluated.
type Error struct {
	Rule string // the offending rule, when there is one
	Msg  string
}

func (e *Error) Error() string {
	if e.Rule == "" {
		return e.Msg
	}
	return e.Msg + " in " + e.Rule
}

// checkRule enforces safety: every variable in the head, a negated atom or a
// built-in is bound by the body, and literals can be ordered so each is
// evaluated with its inputs bound.
func checkRule(r Rule) error {
	fail := func(format string, args ...any) error {
		return &Error{Rule: r.String(), Msg: fmt.Sprintf(format, args...)}
	}
	if _, err := order(r.Body); err != nil {
		return fail("%v", err)
	}
	bound := map[string]bool{}
	for _, l := range r.Body {
		switch l.Kind {
		case Pos:
			for _, t := range l.Atom.Args {
				bound[t.Var] = true
			}
		case Compare:
			if l.Op == Eq {
				bound[l.L.Var], bound[l.R.Var] = true, true
			}
		case Arith:
			bound[l.Out] = true
		}
	}
	for i, t := range r.Head.Args {
		if r.Agg != nil && i == r.Agg.Pos {
			if !t.IsVar() || bound[t.Var] {
				return fail("aggregate result %s must be a variable not used in the body", t)
			}
			continue
		}
		if t.Var == "_" {
			return fail("anonymous variable in head")
		}
		if t.IsVar() && !bound[t.Var] {
			return fail("head variable %s is not bound by the body", t.Var)
		}
	}
	if a := r.Agg; a != nil {
		if a.Pos < 0 || a.Pos >= len(r.Head.Args) {
			return fail("aggregate position %d is out of range", a.Pos)
		}
		switch a.Func {
		case Max, Min, Sum, Avg:
			if !bound[a.Of] {
				return fail("aggregated variable %s is not bound by the body", a.Of)
			}
		case Count:
		default:
			return fail("unknown aggregate %q", a.Func)
		}
	}
	return nil
}

// order returns the body literals in an evaluable order: atoms as they come,
// each negation and built-in as soon as its inputs are bound. It fails when
// some literal can never have its inputs bound.
func order(body []Literal) ([]Literal, error) {
	bound := map[string]bool{}
	var out []Literal
	pending := slices.Clone(body)
	ready := func(l Literal) bool {
		isBound := func(t Term) bool { return !t.IsVar() || bound[t.Var] }
		switch l.Kind {
		case Pos:
			return true
		case Neg:
			for _, t := range l.Atom.Args {
				if t.Var != "_" && !isBound(t) {
					return false
				}
			}
			return true
		case Compare:
			if l.Op == Eq {
				return isBound(l.L) || isBound(l.R)
			}
			return isBound(l.L) && isBound(l.R)
		}
		return isBound(l.L) && isBound(l.R)
	}
	bind := func(l Literal) {
		switch l.Kind {
		case Pos:
			for _, t := range l.Atom.Args {
				if t.IsVar() {
					bound[t.Var] = true
				}
			}
		case Compare:
			if l.Op == Eq {
				for _, t := range []Term{l.L, l.R} {
					if t.IsVar() {
						bound[t.Var] = true
					}
				}
			}
		case Arith:
			bound[l.Out] = true
		}
	}
	for len(pending) > 0 {
		// Prefer a ready filter (negation, built-in) over a new atom: it
		// prunes before the join widens.
		pick := -1
		for i, l := range pending {
			if l.Kind != Pos && ready(l) {
				pick = i
				break
			}
		}
		if pick < 0 {
			for i, l := range pending {
				if l.Kind == Pos {
					pick = i
					break
				}
			}
		}
		if pick < 0 {
			return nil, fmt.Errorf("%s has an unbound variable", pending[0])
		}
		out = append(out, pending[pick])
		bind(pending[pick])
		pending = slices.Delete(pending, pick, pick+1)
	}
	return out, nil
}

// stratum is a set of mutually recursive relations, evaluated together.
type stratum struct {
	rels      []string
	rules     []Rule
	recursive bool
}

// stratify orders the rules into strata: a relation is computed before any
// relation reading it, and a relation read through negation or aggregation is
// computed in a strictly lower stratum. Strata come out in dependency order,
// ties broken by relation name, so evaluation order never depends on map
// iteration.
func stratify(p *Program) ([]stratum, error) {
	defined := map[string]bool{}
	for _, r := range p.Rules {
		defined[r.Head.Rel] = true
	}
	rels := slices.Sorted(maps.Keys(defined))

	type edge struct {
		to     string
		strict bool // negation or aggregation
	}
	deps := map[string][]edge{} // head → relations its rules read
	for _, r := range p.Rules {
		for _, l := range r.Body {
			if (l.Kind == Pos || l.Kind == Neg) && defined[l.Atom.Rel] {
				deps[r.Head.Rel] = append(deps[r.Head.Rel], edge{l.Atom.Rel, l.Kind == Neg || r.Agg != nil})
			}
		}
	}
	for _, d := range deps {
		slices.SortFunc(d, func(a, b edge) int { return strings.Compare(a.to, b.to) })
	}

	// Tarjan's algorithm: SCCs come out with dependencies first.
	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var sccs [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, e := range deps[v] {
			if _, seen := index[e.to]; !seen {
				visit(e.to)
				low[v] = min(low[v], low[e.to])
			} else if onStack[e.to] {
				low[v] = min(low[v], index[e.to])
			}
		}
		if low[v] == index[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			slices.Sort(scc)
			sccs = append(sccs, scc)
		}
	}
	for _, r := range rels {
		if _, seen := index[r]; !seen {
			visit(r)
		}
	}

	var out []stratum
	for _, scc := range sccs {
		in := map[string]bool{}
		for _, r := range scc {
			in[r] = true
		}
		s := stratum{rels: scc, recursive: len(scc) > 1}
		for _, r := range scc {
			for _, e := range deps[r] {
				if !in[e.to] {
					continue
				}
				s.recursive = true
				if e.strict {
					return nil, &Error{Msg: fmt.Sprintf("%s depends on itself through negation or aggregation (cycle: %s)", r, strings.Join(scc, ", "))}
				}
			}
		}
		for _, r := range p.Rules {
			if !in[r.Head.Rel] {
				continue
			}
			if s.recursive {
				for _, l := range r.Body {
					if l.Kind == Arith {
						return nil, &Error{Rule: r.String(), Msg: "arithmetic inside recursion could create values without end"}
					}
				}
			}
			s.rules = append(s.rules, r)
		}
		out = append(out, s)
	}
	return out, nil
}
