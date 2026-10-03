package datalog

import (
	"fmt"
	"math"
	"slices"
)

// Eval evaluates p over the facts in db to a fixpoint and returns a database
// holding db's facts and every derived fact. db is not changed.
func Eval(p *Program, db *Database) (*Database, error) {
	for _, r := range p.Rules {
		if err := checkRule(r); err != nil {
			return nil, err
		}
	}
	strata, err := stratify(p)
	if err != nil {
		return nil, err
	}
	derived := map[string]bool{}
	for _, r := range p.Rules {
		derived[r.Head.Rel] = true
	}
	out := db.overlay(derived)
	// Fix every arity up front, so a rule reading a relation nothing
	// derives sees it empty rather than missing.
	for _, r := range p.Rules {
		if _, err := out.relation(r.Head.Rel, len(r.Head.Args)); err != nil {
			return nil, &Error{Rule: r.String(), Msg: err.Error()}
		}
		for _, l := range r.Body {
			if l.Kind == Pos || l.Kind == Neg {
				if _, err := out.relation(l.Atom.Rel, len(l.Atom.Args)); err != nil {
					return nil, &Error{Rule: r.String(), Msg: err.Error()}
				}
			}
		}
	}
	for _, s := range strata {
		if err := evalStratum(out, s); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func evalStratum(db *Database, s stratum) error {
	plans := make([]*plan, len(s.rules))
	for i, r := range s.rules {
		pl, err := compile(r)
		if err != nil {
			return err
		}
		plans[i] = pl
	}
	if !s.recursive {
		for _, pl := range plans {
			var derived []Tuple
			if pl.rule.Agg != nil {
				derived = pl.aggregate(db)
			} else {
				derived = pl.run(db, -1, nil)
			}
			head := db.rels[pl.rule.Head.Rel]
			for _, t := range derived {
				head.add(t)
			}
		}
		return nil
	}

	// Semi-naive: the first round reads full relations; each later round
	// re-runs only rules reading a relation that grew, with that atom
	// reading just the new facts.
	in := map[string]bool{}
	for _, r := range s.rels {
		in[r] = true
	}
	var delta map[string]*Relation
	collect := func(pl *plan, derived []Tuple, next map[string]*Relation) {
		rel := pl.rule.Head.Rel
		head := db.rels[rel]
		for _, t := range derived {
			if head.Contains(t) {
				continue
			}
			if next[rel] == nil {
				next[rel] = newRelation(rel, head.Arity)
			}
			next[rel].add(t)
		}
	}
	next := map[string]*Relation{}
	for _, pl := range plans {
		collect(pl, pl.run(db, -1, nil), next)
	}
	for len(next) > 0 {
		for rel, r := range next {
			for _, t := range r.tuples {
				db.rels[rel].add(t)
			}
		}
		delta, next = next, map[string]*Relation{}
		for _, pl := range plans {
			for i, l := range pl.lits {
				if l.kind != Pos || !in[l.rel] || delta[l.rel] == nil {
					continue
				}
				collect(pl, pl.run(db, i, delta[l.rel]), next)
			}
		}
	}
	return nil
}

// plan is a rule compiled for evaluation: variables are numbered slots and
// literals are in evaluation order, with each atom's bound positions known.
type plan struct {
	rule  Rule
	lits  []clit
	nvars int
	names []string // slot → variable name
	head  []slotOrConst
	agg   int // slot of the aggregated variable, or -1
}

type slotOrConst struct {
	slot int // -1 for a constant
	val  Value
}

// clit is a compiled literal.
type clit struct {
	kind LitKind
	rel  string
	// For atoms: per argument, the slot (or -1 for a constant or "_"), and
	// whether it is bound before this literal runs.
	args  []slotOrConst
	bound []bool
	anon  []bool
	mask  uint64
	op    Op
	l, r  slotOrConst
	out   int
	// For Compare with Eq: which side, if any, this literal binds (0 none,
	// 1 left, 2 right).
	binds int
}

func compile(r Rule) (*plan, error) {
	lits, err := order(r.Body)
	if err != nil {
		return nil, &Error{Rule: r.String(), Msg: err.Error()}
	}
	pl := &plan{rule: r, agg: -1}
	slots := map[string]int{}
	slot := func(name string) int {
		if s, ok := slots[name]; ok {
			return s
		}
		slots[name] = pl.nvars
		pl.names = append(pl.names, name)
		pl.nvars++
		return pl.nvars - 1
	}
	bound := map[int]bool{}
	term := func(t Term) slotOrConst {
		if !t.IsVar() {
			return slotOrConst{slot: -1, val: t.Val}
		}
		return slotOrConst{slot: slot(t.Var)}
	}
	isBound := func(s slotOrConst) bool { return s.slot < 0 || bound[s.slot] }
	for _, l := range lits {
		c := clit{kind: l.Kind, rel: l.Atom.Rel, op: l.Op, out: -1}
		switch l.Kind {
		case Pos, Neg:
			var newly []int
			for i, t := range l.Atom.Args {
				if t.Var == "_" {
					c.args = append(c.args, slotOrConst{slot: -1})
					c.bound = append(c.bound, false)
					c.anon = append(c.anon, true)
					continue
				}
				s := term(t)
				b := isBound(s)
				c.args = append(c.args, s)
				c.bound = append(c.bound, b)
				c.anon = append(c.anon, false)
				if b {
					c.mask |= 1 << i
				} else {
					newly = append(newly, s.slot)
				}
			}
			if l.Kind == Pos {
				for _, s := range newly {
					bound[s] = true
				}
			}
		case Compare:
			c.l, c.r = term(l.L), term(l.R)
			if l.Op == Eq {
				switch {
				case !isBound(c.l):
					c.binds = 1
					bound[c.l.slot] = true
				case !isBound(c.r):
					c.binds = 2
					bound[c.r.slot] = true
				}
			}
		case Arith:
			c.l, c.r = term(l.L), term(l.R)
			c.out = slot(l.Out)
			if bound[c.out] {
				return nil, &Error{Rule: r.String(), Msg: fmt.Sprintf("%s is already bound; arithmetic assigns a new variable", l.Out)}
			}
			bound[c.out] = true
		}
		pl.lits = append(pl.lits, c)
	}
	for i, t := range r.Head.Args {
		if r.Agg != nil && i == r.Agg.Pos {
			pl.head = append(pl.head, slotOrConst{slot: -1})
			continue
		}
		pl.head = append(pl.head, term(t))
	}
	if r.Agg != nil && r.Agg.Func != Count {
		pl.agg = slots[r.Agg.Of]
	}
	return pl, nil
}

// solve enumerates every satisfying assignment, calling emit with the
// variable slots filled. Literal deltaAt (if ≥ 0) reads delta instead of its
// relation.
func (pl *plan) solve(db *Database, deltaAt int, delta *Relation, emit func(env []Value)) {
	env := make([]Value, pl.nvars)
	var keyBuf []byte
	var step func(i int)
	step = func(i int) {
		if i == len(pl.lits) {
			emit(env)
			return
		}
		c := &pl.lits[i]
		val := func(s slotOrConst) Value {
			if s.slot < 0 {
				return s.val
			}
			return env[s.slot]
		}
		switch c.kind {
		case Pos, Neg:
			rel := db.rels[c.rel]
			if i == deltaAt {
				rel = delta
			}
			keyBuf = keyBuf[:0]
			for j, a := range c.args {
				if c.mask&(1<<j) != 0 {
					keyBuf = appendKey(keyBuf, val(a))
				}
			}
			var candidates []int
			all := c.mask == 0
			if !all {
				candidates = rel.lookup(c.mask, keyBuf)
			}
			n := len(rel.tuples)
			if !all {
				n = len(candidates)
			}
			if c.kind == Neg {
				if n == 0 {
					step(i + 1)
				}
				return
			}
			for k := 0; k < n; k++ {
				var t Tuple
				if all {
					t = rel.tuples[k]
				} else {
					t = rel.tuples[candidates[k]]
				}
				ok := true
				// Bind unbound positions; a variable repeated within the
				// atom must match its first binding.
				var set []int
				for j, a := range c.args {
					if c.bound[j] || c.anon[j] {
						continue
					}
					if slices.Contains(set, a.slot) {
						if env[a.slot] != t[j] {
							ok = false
							break
						}
						continue
					}
					env[a.slot] = t[j]
					set = append(set, a.slot)
				}
				if ok {
					step(i + 1)
				}
			}
		case Compare:
			l, r := val(c.l), val(c.r)
			switch c.binds {
			case 1:
				env[c.l.slot] = r
				step(i + 1)
				return
			case 2:
				env[c.r.slot] = l
				step(i + 1)
				return
			}
			if compare(c.op, l, r) {
				step(i + 1)
			}
		case Arith:
			if v, ok := arith(c.op, val(c.l), val(c.r)); ok {
				env[c.out] = v
				step(i + 1)
			}
		}
	}
	step(0)
}

func (pl *plan) run(db *Database, deltaAt int, delta *Relation) []Tuple {
	var out []Tuple
	pl.solve(db, deltaAt, delta, func(env []Value) {
		t := make(Tuple, len(pl.head))
		for i, h := range pl.head {
			if h.slot < 0 {
				t[i] = h.val
			} else {
				t[i] = env[h.slot]
			}
		}
		out = append(out, t)
	})
	return out
}

// aggregate evaluates an aggregate rule: distinct assignments, grouped by
// the head's other arguments.
func (pl *plan) aggregate(db *Database) []Tuple {
	type group struct {
		head    Tuple
		assigns []Tuple
	}
	groups := map[string]*group{}
	seen := map[string]bool{}
	var order []string
	pl.solve(db, -1, nil, func(env []Value) {
		assign := slices.Clone(Tuple(env))
		k := string(tupleKey(nil, assign))
		if seen[k] {
			return
		}
		seen[k] = true
		head := make(Tuple, len(pl.head))
		for i, h := range pl.head {
			switch {
			case i == pl.rule.Agg.Pos:
			case h.slot < 0:
				head[i] = h.val
			default:
				head[i] = env[h.slot]
			}
		}
		var hk []byte
		for i, v := range head {
			if i != pl.rule.Agg.Pos {
				hk = appendKey(hk, v)
			}
		}
		g := groups[string(hk)]
		if g == nil {
			g = &group{head: head}
			groups[string(hk)] = g
			order = append(order, string(hk))
		}
		g.assigns = append(g.assigns, assign)
	})
	var out []Tuple
	for _, k := range order {
		g := groups[k]
		// Floating-point sums depend on order: always add in sorted order.
		slices.SortFunc(g.assigns, compareTuples)
		v, ok := fold(pl.rule.Agg.Func, g.assigns, pl.agg)
		if !ok {
			continue
		}
		g.head[pl.rule.Agg.Pos] = v
		out = append(out, g.head)
	}
	return out
}

func fold(f AggFunc, assigns []Tuple, slot int) (Value, bool) {
	if f == Count {
		return Number(float64(len(assigns))), true
	}
	var nums []float64
	for _, a := range assigns {
		if a[slot].Kind == Num {
			nums = append(nums, a[slot].N)
		}
	}
	if len(nums) == 0 {
		return Number(0), f == Sum
	}
	switch f {
	case Max:
		return Number(slices.Max(nums)), true
	case Min:
		return Number(slices.Min(nums)), true
	}
	sum := 0.0
	for _, n := range nums {
		sum += n
	}
	if f == Avg {
		return finite(sum / float64(len(nums)))
	}
	return finite(sum)
}

func finite(n float64) (Value, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return Value{}, false
	}
	return Number(n), true
}

func compare(op Op, l, r Value) bool {
	switch op {
	case Eq:
		return l == r
	case Ne:
		return l != r
	case IsKind:
		return r.Kind == Str && l.Kind.String() == r.S
	}
	c, ok := CompareValues(l, r)
	if !ok {
		return false
	}
	switch op {
	case Lt:
		return c < 0
	case Le:
		return c <= 0
	case Gt:
		return c > 0
	case Ge:
		return c >= 0
	}
	return false
}

func arith(op Op, l, r Value) (Value, bool) {
	if l.Kind != Num || r.Kind != Num {
		return Value{}, false
	}
	switch op {
	case Add:
		return finite(l.N + r.N)
	case Sub:
		return finite(l.N - r.N)
	case Mul:
		return finite(l.N * r.N)
	case Div:
		if r.N == 0 {
			return Value{}, false
		}
		return finite(l.N / r.N)
	}
	return Value{}, false
}
