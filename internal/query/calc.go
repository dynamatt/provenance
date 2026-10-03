package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
)

// Calculated fields (Detailed Design §5) compile to Datalog rules, one
// relation per field holding (node, value), and are evaluated together. The
// semantics are Excel's where they can be, with blank as the absence of a
// fact:
//
//   - Arithmetic and comparison of a blank, or of values of the wrong type,
//     are blank. So is division by zero.
//   - IF needs a TRUE or FALSE condition; AND, OR and NOT need TRUE or FALSE
//     arguments and are blank if one is blank and the others do not decide.
//   - MAX, MIN, SUM, AVG read numbers and skip everything else; COUNT counts
//     every value that is not blank. With no values, SUM and COUNT are 0 and
//     the others blank. Each row (or linked entity) counts once, so equal
//     values on two rows both count.
//   - A reference with several values (list[].field, a many link, a facet)
//     can only be used inside an aggregate or ISBLANK.
//
// A formula that does not parse or names a field that does not exist is
// reported on the value (judging the schema is validate's job), as is one
// that depends on itself.

// calcField is one calculated field: on an entity type, or on the rows of
// one of its lists.
type calcField struct {
	schema *schema.Schema
	typ    *schema.Type
	list   *schema.Field // nil for an entity field
	field  *schema.Field
	rel    string
	rules  []datalog.Rule
	deps   []string // relations of other calculated fields read
	err    string
}

func calcRel(t *schema.Type, list *schema.Field, f *schema.Field) string {
	if list != nil {
		return fmt.Sprintf("calc:%s.%s.%s", t.Name, list.Name, f.Name)
	}
	return fmt.Sprintf("calc:%s.%s", t.Name, f.Name)
}

// shape is what a formula node is: an entity of one of types, or a row of
// list (with types holding its one entity type).
type shape struct {
	types []*schema.Type
	list  *schema.Field
}

func (sh shape) nodeRel() string {
	if sh.list != nil {
		return "node:" + sh.types[0].Name + "." + sh.list.Name
	}
	names := make([]string, len(sh.types))
	for i, t := range sh.types {
		names[i] = t.Name
	}
	return "node:" + strings.Join(names, "|")
}

func (sh shape) nodeRules() []datalog.Rule {
	N, E := datalog.Var("N"), datalog.Var("E")
	str := func(s string) datalog.Term { return datalog.Const(datalog.String(s)) }
	if sh.list != nil {
		return []datalog.Rule{{Head: datalog.NewAtom(sh.nodeRel(), N), Body: []datalog.Literal{
			datalog.P(RelEntity, E, str(sh.types[0].Name)), datalog.P(RelRow, E, str(sh.list.Name), N)}}}
	}
	var rules []datalog.Rule
	for _, t := range sh.types {
		rules = append(rules, datalog.Rule{Head: datalog.NewAtom(sh.nodeRel(), N), Body: []datalog.Literal{datalog.P(RelEntity, N, str(t.Name))}})
	}
	return rules
}

func (sh shape) describe() string {
	if sh.list != nil {
		return fmt.Sprintf("a row of %q", sh.list.Name)
	}
	names := make([]string, len(sh.types))
	for i, t := range sh.types {
		names[i] = t.Name
	}
	return strings.Join(names, " or ")
}

// member is what a name means on a shape.
type member struct {
	field *schema.Field // a representative declaration; nil for a facet
	facet []*schema.Type
	// owners are the shape's types declaring the field, for calculated
	// fields, whose relation is per type.
	owners []*schema.Type
}

func (sh shape) find(name string) (member, error) {
	if sh.list != nil {
		for _, f := range sh.list.Fields {
			if f.Name == name {
				return member{field: f, owners: sh.types}, nil
			}
		}
		var names []string
		for _, f := range sh.list.Fields {
			names = append(names, f.Name)
		}
		return member{}, fmt.Errorf("%s has no field %q (fields: %s)", sh.describe(), name, strings.Join(names, ", "))
	}
	var m member
	for _, t := range sh.types {
		if f := t.Field(name); f != nil {
			if m.field != nil && (f.Kind != m.field.Kind || f.Cardinality != m.field.Cardinality) {
				return member{}, fmt.Errorf("%q has different types on %s", name, sh.describe())
			}
			m.field = f
			m.owners = append(m.owners, t)
		}
		if facet := facetOf(t, name); facet != nil {
			for _, src := range facet.Sources {
				if !slices.Contains(m.facet, src.Type) {
					m.facet = append(m.facet, src.Type)
				}
			}
		}
	}
	switch {
	case m.field != nil && m.facet != nil:
		return member{}, fmt.Errorf("%q is a field on some of %s and an incoming facet on others", name, sh.describe())
	case m.field == nil && m.facet == nil:
		var names []string
		for _, t := range sh.types {
			for _, f := range t.Fields {
				names = append(names, f.Name)
			}
			for _, f := range t.Facets {
				names = append(names, f.Name)
			}
		}
		return member{}, fmt.Errorf("%s has no field %q (fields: %s)", sh.describe(), name, strings.Join(names, ", "))
	}
	return m, nil
}

// fcomp compiles one formula.
type fcomp struct {
	cf     *calcField
	home   shape
	n      int
	shapes map[string]shape // node relations used, by name
}

func (c *fcomp) fresh() string {
	c.n++
	return fmt.Sprintf("%s#%d", c.cf.rel, c.n)
}

func (c *fcomp) add(head datalog.Atom, body ...datalog.Literal) {
	c.cf.rules = append(c.cf.rules, datalog.Rule{Head: head, Body: body})
}

func (c *fcomp) node(N datalog.Term) datalog.Literal {
	c.shapes[c.home.nodeRel()] = c.home
	return datalog.P(c.home.nodeRel(), N)
}

func ferr(e Expr, format string, args ...any) error {
	return &FormulaError{Pos: e.pos(), Msg: fmt.Sprintf(format, args...)}
}

var (
	vN = datalog.Var("N")
	vV = datalog.Var("V")
	vA = datalog.Var("A")
	vB = datalog.Var("B")
	vX = datalog.Var("X")
)

func boolC(b bool) datalog.Term { return datalog.Const(datalog.Boolean(b)) }

// path compiles a reference to literals binding v to each of its values on
// node N. keys are the variables that tell the values apart when there can
// be several: one per row, item or many-link crossed.
func (c *fcomp) path(ref *RefExpr, v datalog.Term) (lits []datalog.Literal, keys []datalog.Term, err error) {
	cur := vN
	sh := c.home
	str := func(s string) datalog.Term { return datalog.Const(datalog.String(s)) }
	for i, st := range ref.Steps {
		last := i == len(ref.Steps)-1
		next := datalog.Var(fmt.Sprintf("P%d", i))
		if last {
			next = v
		}
		m, err := sh.find(st.Name)
		if err != nil {
			return nil, nil, ferr(ref, "%v", err)
		}
		f := m.field
		isRows := f != nil && f.Kind == schema.List && f.Elem == nil
		isItems := f != nil && f.Kind == schema.List && f.Elem != nil
		if st.Each && !isRows && !isItems {
			return nil, nil, ferr(ref, "%s is not a list; [] applies to lists", st.Name)
		}
		switch {
		case m.facet != nil:
			lits = append(lits, datalog.P(RelLink, cur, str(st.Name), next))
			keys = append(keys, next)
			sh = shape{types: m.facet}
		case f.Kind == schema.Link:
			lits = append(lits, datalog.P(RelLink, cur, str(st.Name), next))
			if f.Cardinality == "many" {
				keys = append(keys, next)
			}
			var targets []*schema.Type
			for _, t := range f.Target {
				if t := c.cf.schema.Types[t]; t != nil {
					targets = append(targets, t)
				}
			}
			if !last && len(targets) == 0 {
				return nil, nil, ferr(ref, "%s links to no declared type", st.Name)
			}
			sh = shape{types: targets}
		case isRows:
			if last {
				return nil, nil, ferr(ref, "%s is a list of rows; name one of its fields: %s[].<field>", st.Name, st.Name)
			}
			if !st.Each {
				return nil, nil, ferr(ref, "%s is a list of rows: write %s[].%s", st.Name, st.Name, ref.Steps[i+1].Name)
			}
			if len(m.owners) != 1 {
				return nil, nil, ferr(ref, "%s is a list on more than one type; read it on its own entity", st.Name)
			}
			lits = append(lits, datalog.P(RelRow, cur, str(st.Name), next))
			keys = append(keys, next)
			sh = shape{types: m.owners, list: f}
		case !last:
			return nil, nil, ferr(ref, "%s is %s, not a link: cannot read %s.%s", st.Name, kindName(f), st.Name, ref.Steps[i+1].Name)
		case isItems:
			I := datalog.Var(fmt.Sprintf("I%d", i))
			lits = append(lits, datalog.P(RelItem, cur, str(st.Name), I, next))
			keys = append(keys, I)
		case f.Kind == schema.Calculated:
			rel, deps := c.calcRead(sh, m, st.Name)
			c.cf.deps = append(c.cf.deps, deps...)
			lits = append(lits, datalog.P(rel, cur, next))
		default:
			lits = append(lits, datalog.P(RelField, cur, str(st.Name), next))
		}
		cur = next
	}
	return lits, keys, nil
}

func kindName(f *schema.Field) string {
	if f.Kind == schema.Enum {
		return "an enum"
	}
	k := string(f.Kind)
	if strings.ContainsAny(k[:1], "aeiou") {
		return "an " + k
	}
	return "a " + k
}

// calcRead is the relation holding a calculated field's values on shape sh:
// its own relation, or the union of the relations of every type declaring it.
func (c *fcomp) calcRead(sh shape, m member, name string) (string, []string) {
	var rels []string
	for _, t := range m.owners {
		var list *schema.Field
		if sh.list != nil {
			list = sh.list
		}
		f := m.field
		if sh.list == nil {
			f = t.Field(name)
		}
		rels = append(rels, calcRel(t, list, f))
	}
	if len(rels) == 1 {
		return rels[0], rels
	}
	union := c.fresh()
	for _, r := range rels {
		c.add(datalog.NewAtom(union, vN, vV), datalog.P(r, vN, vV))
	}
	return union, rels
}

// single compiles e to a relation r(N, V) holding e's value on each node.
func (c *fcomp) single(e Expr) (string, error) {
	r := c.fresh()
	head := datalog.NewAtom(r, vN, vV)
	switch e := e.(type) {
	case *NumberLit:
		c.add(datalog.NewAtom(r, vN, datalog.Const(datalog.Number(e.Value))), c.node(vN))
	case *StringLit:
		c.add(datalog.NewAtom(r, vN, datalog.Const(datalog.String(e.Value))), c.node(vN))
	case *BoolLit:
		c.add(datalog.NewAtom(r, vN, boolC(e.Value)), c.node(vN))
	case *RefExpr:
		lits, keys, err := c.path(e, vV)
		if err != nil {
			return "", err
		}
		if len(keys) > 0 {
			return "", ferr(e, "%s has several values; use it inside MAX, MIN, SUM, COUNT, AVG or ISBLANK", e)
		}
		c.add(head, append([]datalog.Literal{c.node(vN)}, lits...)...)
	case *Negate:
		a, err := c.single(e.X)
		if err != nil {
			return "", err
		}
		c.add(datalog.NewAtom(r, vN, vX), datalog.P(a, vN, vA), datalog.Calc("X", datalog.Const(datalog.Number(0)), datalog.Sub, vA))
	case *Binary:
		a, err := c.single(e.L)
		if err != nil {
			return "", err
		}
		b, err := c.single(e.R)
		if err != nil {
			return "", err
		}
		operands := []datalog.Literal{datalog.P(a, vN, vA), datalog.P(b, vN, vB)}
		if op, ok := map[string]datalog.Op{"+": datalog.Add, "-": datalog.Sub, "*": datalog.Mul, "/": datalog.Div}[e.Op]; ok {
			c.add(datalog.NewAtom(r, vN, vX), append(operands, datalog.Calc("X", vA, op, vB))...)
			break
		}
		// A comparison is TRUE where it holds and FALSE where its opposite
		// does; values that cannot be compared are neither, so blank.
		ops := map[string][2]datalog.Op{
			"=": {datalog.Eq, datalog.Ne}, "<>": {datalog.Ne, datalog.Eq},
			"<": {datalog.Lt, datalog.Ge}, ">": {datalog.Gt, datalog.Le},
			"<=": {datalog.Le, datalog.Gt}, ">=": {datalog.Ge, datalog.Lt},
		}[e.Op]
		c.add(datalog.NewAtom(r, vN, boolC(true)), append(slices.Clone(operands), datalog.Cmp(vA, ops[0], vB))...)
		c.add(datalog.NewAtom(r, vN, boolC(false)), append(slices.Clone(operands), datalog.Cmp(vA, ops[1], vB))...)
	case *Call:
		return r, c.call(r, e)
	default:
		return "", ferr(e, "unsupported expression")
	}
	return r, nil
}

func (c *fcomp) call(r string, e *Call) error {
	args := make([]string, 0, len(e.Args))
	compileArgs := func() error {
		for _, a := range e.Args {
			rel, err := c.single(a)
			if err != nil {
				return err
			}
			args = append(args, rel)
		}
		return nil
	}
	switch e.Func {
	case "IF":
		if err := compileArgs(); err != nil {
			return err
		}
		c.add(datalog.NewAtom(r, vN, vV), datalog.P(args[0], vN, boolC(true)), datalog.P(args[1], vN, vV))
		if len(args) == 3 {
			c.add(datalog.NewAtom(r, vN, vV), datalog.P(args[0], vN, boolC(false)), datalog.P(args[2], vN, vV))
		} else {
			c.add(datalog.NewAtom(r, vN, boolC(false)), datalog.P(args[0], vN, boolC(false)))
		}
	case "AND", "OR":
		if err := compileArgs(); err != nil {
			return err
		}
		// AND is TRUE when every argument is, FALSE when any is FALSE; OR
		// the other way round.
		all, decides := e.Func == "AND", e.Func == "OR"
		var every []datalog.Literal
		for _, a := range args {
			every = append(every, datalog.P(a, vN, boolC(all)))
			c.add(datalog.NewAtom(r, vN, boolC(decides)), datalog.P(a, vN, boolC(decides)))
		}
		c.add(datalog.NewAtom(r, vN, boolC(all)), every...)
	case "NOT":
		if err := compileArgs(); err != nil {
			return err
		}
		for _, b := range []bool{true, false} {
			c.add(datalog.NewAtom(r, vN, boolC(!b)), datalog.P(args[0], vN, boolC(b)))
		}
	case "ISBLANK":
		has := c.fresh()
		if ref, ok := e.Args[0].(*RefExpr); ok {
			lits, _, err := c.path(ref, datalog.Var("_"))
			if err != nil {
				return err
			}
			c.add(datalog.NewAtom(has, vN), append([]datalog.Literal{c.node(vN)}, lits...)...)
		} else {
			a, err := c.single(e.Args[0])
			if err != nil {
				return err
			}
			c.add(datalog.NewAtom(has, vN), datalog.P(a, vN, datalog.Var("_")))
		}
		c.add(datalog.NewAtom(r, vN, boolC(false)), datalog.P(has, vN))
		c.add(datalog.NewAtom(r, vN, boolC(true)), c.node(vN), datalog.N(has, vN))
	default:
		return c.aggregate(r, e)
	}
	return nil
}

// aggregate compiles MAX, MIN, SUM, COUNT and AVG. Every argument's values
// go into one relation in(N, Arg, Key…, V): the keys tell apart values from
// different rows or linked entities, so equal values are all counted.
func (c *fcomp) aggregate(r string, e *Call) error {
	type arg struct {
		lits []datalog.Literal
		keys []datalog.Term
	}
	var parts []arg
	width := 0
	for _, a := range e.Args {
		if ref, ok := a.(*RefExpr); ok {
			lits, keys, err := c.path(ref, vV)
			if err != nil {
				return err
			}
			parts = append(parts, arg{append([]datalog.Literal{c.node(vN)}, lits...), keys})
			width = max(width, len(keys))
			continue
		}
		rel, err := c.single(a)
		if err != nil {
			return err
		}
		parts = append(parts, arg{lits: []datalog.Literal{datalog.P(rel, vN, vV)}})
	}
	in := c.fresh()
	pad := datalog.Const(datalog.String(""))
	for i, p := range parts {
		head := []datalog.Term{vN, datalog.Const(datalog.Number(float64(i)))}
		head = append(head, p.keys...)
		for len(head) < width+2 {
			head = append(head, pad)
		}
		c.add(datalog.NewAtom(in, append(head, vV)...), p.lits...)
	}
	body := []datalog.Term{vN, datalog.Var("Arg")}
	for i := range width {
		body = append(body, datalog.Var(fmt.Sprintf("K%d", i)))
	}
	body = append(body, vV)
	fn := map[string]datalog.AggFunc{"MAX": datalog.Max, "MIN": datalog.Min, "SUM": datalog.Sum, "COUNT": datalog.Count, "AVG": datalog.Avg}[e.Func]
	c.cf.rules = append(c.cf.rules, datalog.Rule{
		Head: datalog.NewAtom(r, vN, datalog.Var("Agg")),
		Body: []datalog.Literal{datalog.P(in, body...)},
		Agg:  &datalog.Aggregate{Func: fn, Pos: 1, Of: "V"},
	})
	if fn == datalog.Sum || fn == datalog.Count {
		has := c.fresh()
		anon := make([]datalog.Term, len(body)-1)
		for i := range anon {
			anon[i] = datalog.Var("_")
		}
		c.add(datalog.NewAtom(has, vN), datalog.P(in, append([]datalog.Term{vN}, anon...)...))
		c.add(datalog.NewAtom(r, vN, datalog.Const(datalog.Number(0))), c.node(vN), datalog.N(has, vN))
	}
	return nil
}

// Calculate evaluates every calculated field of every entity, fills in
// their model values (Present with a Result, blank, or Invalid with the
// problem), and adds the values to the graph's facts so queries and order_by
// read them like stored fields.
func (g *Graph) Calculate() error {
	var fields []*calcField
	for _, tn := range typeNames(g.Schema) {
		t := g.Schema.Types[tn]
		for _, f := range t.Fields {
			if f.Kind == schema.Calculated {
				fields = append(fields, &calcField{typ: t, field: f, schema: g.Schema})
			}
			if f.Kind == schema.List && f.Elem == nil {
				for _, sub := range f.Fields {
					if sub.Kind == schema.Calculated {
						fields = append(fields, &calcField{typ: t, list: f, field: sub, schema: g.Schema})
					}
				}
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}

	shapes := map[string]shape{}
	byRel := map[string]*calcField{}
	for _, cf := range fields {
		cf.rel = calcRel(cf.typ, cf.list, cf.field)
		byRel[cf.rel] = cf
		home := shape{types: []*schema.Type{cf.typ}, list: cf.list}
		expr, err := ParseFormula(cf.field.Formula)
		if strings.TrimSpace(cf.field.Formula) == "" {
			err = &FormulaError{Msg: "no formula"}
		}
		if err == nil {
			c := &fcomp{cf: cf, home: home, shapes: shapes}
			var top string
			top, err = c.single(expr)
			if err == nil {
				c.add(datalog.NewAtom(cf.rel, vN, vV), datalog.P(top, vN, vV))
			}
		}
		if err != nil {
			cf.err = "formula: " + err.Error()
			cf.rules, cf.deps = nil, nil
		}
	}
	markCycles(fields, byRel)

	prog := &datalog.Program{}
	for _, name := range sortedShapeNames(shapes) {
		prog.Add(shapes[name].nodeRules()...)
	}
	for _, cf := range fields {
		if cf.err == "" {
			prog.Add(cf.rules...)
		}
	}
	out, err := datalog.Eval(prog, g.facts)
	if err != nil {
		return fmt.Errorf("evaluating calculated fields: %w", err)
	}

	for _, e := range g.Entities {
		for _, cf := range fields {
			if cf.typ.Name != e.Type {
				continue
			}
			if cf.list == nil {
				g.setCalc(out, cf, e.Field(cf.field.Name), datalog.String(e.ID))
				continue
			}
			list := e.Field(cf.list.Name)
			if !list.Present || list.Invalid {
				continue
			}
			for i, row := range list.Rows {
				for _, cell := range row {
					if cell.Field == cf.field {
						g.setCalc(out, cf, cell, RowNode(e.ID, cf.list.Name, i))
					}
				}
			}
		}
	}
	return nil
}

func sortedShapeNames(m map[string]shape) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// markCycles marks every field whose formula depends on itself, directly or
// through other calculated fields.
func markCycles(fields []*calcField, byRel map[string]*calcField) {
	for _, cf := range fields {
		if cf.err != "" {
			continue
		}
		// Depth-first from cf: reaching cf again is a cycle.
		var path []string
		seen := map[string]bool{}
		var visit func(rel string) bool
		visit = func(rel string) bool {
			dep := byRel[rel]
			if dep == nil {
				return false
			}
			path = append(path, displayName(dep))
			if dep == cf && len(path) > 1 {
				return true
			}
			if seen[rel] {
				path = path[:len(path)-1]
				return false
			}
			seen[rel] = true
			deps := slices.Clone(dep.deps)
			slices.Sort(deps)
			for _, d := range slices.Compact(deps) {
				if visit(d) {
					return true
				}
			}
			path = path[:len(path)-1]
			return false
		}
		if visit(cf.rel) {
			cf.err = "formula depends on itself: " + strings.Join(path, " → ")
		}
	}
}

func displayName(cf *calcField) string {
	if cf.list != nil {
		return cf.typ.Name + "." + cf.list.Name + "[]." + cf.field.Name
	}
	return cf.typ.Name + "." + cf.field.Name
}

// setCalc stores a calculated field's value on node in v and in the facts.
func (g *Graph) setCalc(out *datalog.Database, cf *calcField, v *model.Value, node datalog.Value) {
	if v == nil {
		return
	}
	if cf.err != "" {
		v.Present, v.Invalid, v.Raw, v.Problem = true, true, cf.field.Formula, cf.err
		return
	}
	tuples := out.Lookup(cf.rel, node)
	switch len(tuples) {
	case 0:
		return // blank
	case 1:
	default:
		v.Present, v.Invalid, v.Raw, v.Problem = true, true, cf.field.Formula, "formula has several values here"
		return
	}
	val := tuples[0][1]
	switch val.Kind {
	case datalog.Num:
		v.Result, v.Num = schema.Number, val.N
	case datalog.Bool:
		v.Result, v.Bool = schema.Boolean, val.B
	case datalog.Str:
		v.Result, v.Str = schema.String, val.S
	default:
		return
	}
	v.Present = true
	g.facts.MustAdd(RelField, node, datalog.String(cf.field.Name), val)
}
