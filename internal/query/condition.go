package query

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// The condition grammar (DES-0015), shared by query blocks, rule
// instances' where, --scope query files and the HTTP API:
//
//	where: {field: status, operator: equals, value: approved}   one condition
//	where: [<condition>, <condition>]                          all must hold
//	where: {any_of: [<condition or list>, …]}                  at least one
//
// field names a field or incoming facet of the entity, its id or type, a
// list row's sub-field ({list: L, subfield: S}) or a field across a link
// ({via: L, field: F}). value is a literal or {field: <field>}, comparing two
// fields of the same entity.

// Operator is a comparison word.
type Operator string

const (
	Equals         Operator = "equals"
	NotEquals      Operator = "not_equals"
	GreaterOrEqual Operator = "greater_or_equal"
	LessOrEqual    Operator = "less_or_equal"
	GreaterThan    Operator = "greater_than"
	LessThan       Operator = "less_than"
	Exists         Operator = "exists"
)

// Operators lists every operator, in the order DES-0015 gives.
var Operators = []Operator{Equals, NotEquals, GreaterOrEqual, LessOrEqual, GreaterThan, LessThan, Exists}

var cmpOps = map[Operator]datalog.Op{
	Equals: datalog.Eq, NotEquals: datalog.Eq, // not_equals is "no value equals"
	GreaterOrEqual: datalog.Ge, LessOrEqual: datalog.Le,
	GreaterThan: datalog.Gt, LessThan: datalog.Lt,
}

func validOperators() string {
	names := make([]string, len(Operators))
	for i, o := range Operators {
		names[i] = string(o)
	}
	return strings.Join(names, ", ")
}

// Condition is a parsed where clause: a single test, or a group.
type Condition struct {
	Line int
	// Exactly one of these is set.
	Test  *Test
	AllOf []*Condition
	AnyOf []*Condition
}

// Test is one field/operator/value condition.
type Test struct {
	Line     int
	Field    Ref
	Operator Operator
	// Value is nil for exists.
	Value *Operand
}

// Ref addresses a value on the entity being tested.
type Ref struct {
	Line int
	// Field alone: a field, incoming facet, "id" or "type". With List: the
	// sub-field Field of a row of List. With Via: the field Field of the
	// entities linked through Via.
	Field string
	List  string
	Via   string
}

func (r Ref) String() string {
	switch {
	case r.List != "":
		return fmt.Sprintf("{list: %s, subfield: %s}", r.List, r.Field)
	case r.Via != "":
		return fmt.Sprintf("{via: %s, field: %s}", r.Via, r.Field)
	}
	return r.Field
}

// Operand is a test's value: a literal, or another field of the entity.
type Operand struct {
	Line    int
	Literal datalog.Value
	Field   *Ref
}

// Error is a problem in a condition or query, at a line of its YAML.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

func errorf(n *yaml.Node, format string, args ...any) error {
	line := 0
	if n != nil {
		line = n.Line
	}
	return &Error{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// ParseCondition reads a where clause. nested allows any_of inside any_of,
// which only Query Assertion's where permits (DES-0015, REQ-0049); elsewhere
// a where is at most one level of OR-of-ANDs.
func ParseCondition(n *yaml.Node, nested bool) (*Condition, error) {
	return parseCondition(n, nested, false)
}

func parseCondition(n *yaml.Node, nested, inAny bool) (*Condition, error) {
	switch n.Kind {
	case yaml.SequenceNode:
		c := &Condition{Line: n.Line}
		if len(n.Content) == 0 {
			return nil, errorf(n, "an empty list of conditions")
		}
		for _, item := range n.Content {
			sub, err := parseCondition(item, nested, inAny)
			if err != nil {
				return nil, err
			}
			c.AllOf = append(c.AllOf, sub)
		}
		return c, nil
	case yaml.MappingNode:
		if alts := yamlnode.Lookup(n, "any_of"); alts != nil {
			if len(n.Content) != 2 {
				return nil, errorf(n, "any_of stands alone; put other conditions beside it in a list")
			}
			if inAny && !nested {
				return nil, errorf(n, "any_of cannot nest inside any_of here (only a QueryAssertion's where allows deeper nesting)")
			}
			if alts.Kind != yaml.SequenceNode || len(alts.Content) == 0 {
				return nil, errorf(alts, "any_of needs a list of conditions")
			}
			c := &Condition{Line: n.Line}
			for _, item := range alts.Content {
				sub, err := parseCondition(item, nested, true)
				if err != nil {
					return nil, err
				}
				c.AnyOf = append(c.AnyOf, sub)
			}
			return c, nil
		}
		t, err := parseTest(n)
		if err != nil {
			return nil, err
		}
		return &Condition{Line: n.Line, Test: t}, nil
	}
	return nil, errorf(n, "a condition is a mapping with field, operator and value, a list of conditions, or any_of")
}

func parseTest(n *yaml.Node) (*Test, error) {
	if err := onlyKeys(n, "a condition", "field", "operator", "value"); err != nil {
		return nil, err
	}
	t := &Test{Line: n.Line}
	fieldNode := yamlnode.Lookup(n, "field")
	if fieldNode == nil {
		return nil, errorf(n, "condition has no field")
	}
	ref, err := parseRef(fieldNode)
	if err != nil {
		return nil, err
	}
	t.Field = ref

	opNode := yamlnode.Lookup(n, "operator")
	if opNode == nil {
		return nil, errorf(n, "condition has no operator (valid operators: %s)", validOperators())
	}
	t.Operator = Operator(opNode.Value)
	if opNode.Kind != yaml.ScalarNode || !slices.Contains(Operators, t.Operator) {
		return nil, errorf(opNode, "unknown operator %q (valid operators: %s)", opNode.Value, validOperators())
	}

	valueNode := yamlnode.Lookup(n, "value")
	switch {
	case t.Operator == Exists && valueNode != nil:
		return nil, errorf(valueNode, "exists takes no value")
	case t.Operator == Exists:
		return t, nil
	case valueNode == nil || valueNode.Tag == "!!null":
		return nil, errorf(n, "%s needs a value", t.Operator)
	}
	t.Value, err = parseOperand(valueNode)
	return t, err
}

func parseRef(n *yaml.Node) (Ref, error) {
	r := Ref{Line: n.Line}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" || n.Tag != "!!str" {
			return r, errorf(n, "field must name a field")
		}
		r.Field = n.Value
		return r, nil
	case yaml.MappingNode:
		switch {
		case yamlnode.Lookup(n, "list") != nil:
			if err := onlyKeys(n, "a list field reference", "list", "subfield"); err != nil {
				return r, err
			}
			r.List, r.Field = scalar(yamlnode.Lookup(n, "list")), scalar(yamlnode.Lookup(n, "subfield"))
			if r.List == "" || r.Field == "" {
				return r, errorf(n, "a list field reference is {list: <list field>, subfield: <sub-field>}")
			}
			return r, nil
		case yamlnode.Lookup(n, "via") != nil:
			if err := onlyKeys(n, "a field across a link", "via", "field"); err != nil {
				return r, err
			}
			r.Via, r.Field = scalar(yamlnode.Lookup(n, "via")), scalar(yamlnode.Lookup(n, "field"))
			if r.Via == "" || r.Field == "" {
				return r, errorf(n, "a field across a link is {via: <link field>, field: <field>}")
			}
			return r, nil
		}
	}
	return r, errorf(n, "field must be a field name, {list: …, subfield: …} or {via: …, field: …}")
}

func parseOperand(n *yaml.Node) (*Operand, error) {
	o := &Operand{Line: n.Line}
	if n.Kind == yaml.MappingNode {
		if err := onlyKeys(n, "a value", "field"); err != nil {
			return nil, err
		}
		ref, err := parseRef(yamlnode.Lookup(n, "field"))
		if err != nil {
			return nil, err
		}
		o.Field = &ref
		return o, nil
	}
	v, ok := literal(n)
	if !ok {
		return nil, errorf(n, "value must be a single value or {field: <field>}")
	}
	o.Literal = v
	return o, nil
}

// literal reads a YAML scalar by its YAML type, as entity values are read
// (DES-0010): a quoted "2" is text, not a number.
func literal(n *yaml.Node) (datalog.Value, bool) {
	if n.Kind != yaml.ScalarNode {
		return datalog.Value{}, false
	}
	switch n.Tag {
	case "!!int", "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return datalog.Value{}, false
		}
		return datalog.Number(f), true
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return datalog.Value{}, false
		}
		return datalog.Boolean(b), true
	case "!!str", "!!timestamp":
		return datalog.String(n.Value), true
	}
	return datalog.Value{}, false
}

func scalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func onlyKeys(m *yaml.Node, what string, keys ...string) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; !slices.Contains(keys, k.Value) {
			return errorf(k, "unknown key %q in %s (expected %s)", k.Value, what, strings.Join(keys, ", "))
		}
	}
	return nil
}

// Checking and compiling against the schema.

// valueClass is what a referenced value compares as.
type valueClass int

const (
	classAny valueClass = iota // not known statically (calculated fields)
	classText
	classNumber
	classBool
	classDate
	classEnum
	classID // link targets, incoming facets, id
)

func (c valueClass) String() string {
	return [...]string{"a calculated value", "text", "a number", "true or false", "a date", "an enum value", "an entity ID"}[c]
}

// target is a resolved reference: how to read its values.
type target struct {
	ref Ref
	// rel is RelField or RelLink for stored values; "" for id and type.
	rel   string
	class valueClass
	enum  *schema.EnumType
	// rows is set when the reference is a row list itself (exists only).
	rows bool
}

type compiler struct {
	s    *schema.Schema
	t    *schema.Type
	prog *datalog.Program
	// lenient compiles a test naming something t lacks as never true: the
	// value is empty on t (several types in from; checked by checkAcross).
	lenient bool
	// prefix keeps this compilation's relations apart from any other
	// program evaluated alongside.
	prefix string
	n      int
}

func (c *compiler) rel(kind string) string {
	c.n++
	return fmt.Sprintf("%s%s_%d", c.prefix, kind, c.n)
}

// fieldNames lists what a reference to t may name, for error messages.
func fieldNames(t *schema.Type) string {
	names := []string{"id", "type"}
	for _, f := range t.Fields {
		names = append(names, f.Name)
	}
	for _, f := range t.Facets {
		names = append(names, f.Name)
	}
	return strings.Join(names, ", ")
}

func classOf(s *schema.Schema, f *schema.Field) (valueClass, *schema.EnumType) {
	if f.Kind == schema.List && f.Elem != nil {
		f = f.Elem
	}
	switch f.Kind {
	case schema.Number:
		return classNumber, nil
	case schema.Boolean:
		return classBool, nil
	case schema.Date:
		return classDate, nil
	case schema.Enum:
		return classEnum, s.Enums[f.EnumName]
	case schema.Link:
		return classID, nil
	case schema.Calculated:
		return classAny, nil
	}
	return classText, nil
}

func (c *compiler) resolve(r Ref) (target, error) {
	fail := func(format string, args ...any) (target, error) {
		return target{}, &Error{Line: r.Line, Msg: fmt.Sprintf(format, args...)}
	}
	tg := target{ref: r}
	switch {
	case r.List != "":
		list := c.t.Field(r.List)
		if list == nil || list.Kind != schema.List || list.Elem != nil {
			return fail("%s has no list of rows named %q", c.t.Name, r.List)
		}
		var sub *schema.Field
		for _, f := range list.Fields {
			if f.Name == r.Field {
				sub = f
			}
		}
		if sub == nil {
			var names []string
			for _, f := range list.Fields {
				names = append(names, f.Name)
			}
			return fail("list %q has no sub-field %q (sub-fields: %s)", r.List, r.Field, strings.Join(names, ", "))
		}
		if sub.Kind == schema.List {
			return fail("sub-field %q is itself a list; conditions read one level of rows", r.Field)
		}
		return c.stored(tg, sub)
	case r.Via != "":
		var types []*schema.Type
		if l := c.t.Field(r.Via); l != nil && l.Kind == schema.Link {
			for _, name := range l.Target {
				if t := c.s.Types[name]; t != nil {
					types = append(types, t)
				}
			}
		} else if facet := facetOf(c.t, r.Via); facet != nil {
			for _, src := range facet.Sources {
				types = append(types, src.Type)
			}
		} else {
			return fail("%s has no link or incoming facet named %q", c.t.Name, r.Via)
		}
		// The field must exist, with one meaning, on the linked types that
		// have it.
		var found *schema.Field
		for _, t := range types {
			f := t.Field(r.Field)
			if f == nil {
				continue
			}
			if found != nil && (f.Kind != found.Kind || f.EnumName != found.EnumName) {
				return fail("field %q has different types on the entities %q links to", r.Field, r.Via)
			}
			found = f
		}
		if found == nil {
			names := make([]string, len(types))
			for i, t := range types {
				names[i] = t.Name
			}
			return fail("no type %q links to (%s) has a field %q", r.Via, strings.Join(names, ", "), r.Field)
		}
		if found.Kind == schema.List && found.Elem == nil {
			return fail("field %q is a list of rows; use {list: …, subfield: …} on its own entity", r.Field)
		}
		return c.stored(tg, found)
	}
	switch r.Field {
	case "id":
		tg.class = classID
		return tg, nil
	case "type":
		tg.class = classText
		return tg, nil
	}
	if f := c.t.Field(r.Field); f != nil {
		if f.Kind == schema.List && f.Elem == nil {
			tg.rows = true
			return tg, nil
		}
		return c.stored(tg, f)
	}
	if facetOf(c.t, r.Field) != nil {
		tg.rel, tg.class = RelLink, classID
		return tg, nil
	}
	return fail("%s has no field %q (fields: %s)", c.t.Name, r.Field, fieldNames(c.t))
}

func (c *compiler) stored(tg target, f *schema.Field) (target, error) {
	tg.class, tg.enum = classOf(c.s, f)
	tg.rel = RelField
	if f.Kind == schema.Link {
		tg.rel = RelLink
	}
	return tg, nil
}

func facetOf(t *schema.Type, name string) *schema.Facet {
	for _, f := range t.Facets {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// checkTest rejects tests that could never mean what they say: a literal of
// the wrong type, an enum value the enum lacks, an order on unordered values.
func checkTest(t *Test, left target, right *target) error {
	fail := func(line int, format string, args ...any) error {
		return &Error{Line: line, Msg: fmt.Sprintf(format, args...)}
	}
	if left.rows {
		if t.Operator != Exists {
			return fail(t.Field.Line, "%q is a list of rows: test its sub-fields with {list: %s, subfield: …}, or use exists", t.Field.Field, t.Field.Field)
		}
		return nil
	}
	if t.Operator == Exists {
		return nil
	}
	if _, ordering := map[Operator]bool{GreaterOrEqual: true, LessOrEqual: true, GreaterThan: true, LessThan: true}[t.Operator]; ordering {
		switch left.class {
		case classAny, classNumber, classDate, classText:
		default:
			return fail(t.Line, "%s compares numbers, dates and text; %s is %s", t.Operator, t.Field, left.class)
		}
	}
	if right != nil {
		if right.rows {
			return fail(t.Value.Line, "%q is a list of rows and has no single value to compare", right.ref.Field)
		}
		comparable := func(a, b valueClass) bool {
			text := func(c valueClass) bool { return c == classText || c == classDate || c == classEnum || c == classID }
			return a == classAny || b == classAny || a == b || (text(a) && text(b))
		}
		if !comparable(left.class, right.class) {
			return fail(t.Value.Line, "%s is %s but %s is %s", t.Field, left.class, right.ref, right.class)
		}
		return nil
	}
	v := t.Value.Literal
	want := map[valueClass]datalog.Kind{classNumber: datalog.Num, classBool: datalog.Bool}[left.class]
	if left.class == classAny {
		return nil
	}
	if want == 0 {
		want = datalog.Str
	}
	if v.Kind != want {
		return fail(t.Value.Line, "%s is %s; %s is not", t.Field, left.class, v)
	}
	switch left.class {
	case classEnum:
		if left.enum != nil && !slices.Contains(left.enum.Values, v.S) {
			return fail(t.Value.Line, "%q is not a value of %s (values: %s)", v.S, left.enum.Name, strings.Join(left.enum.Values, ", "))
		}
	case classDate:
		if _, err := time.Parse("2006-01-02", v.S); err != nil {
			return fail(t.Value.Line, "%q is not a date (YYYY-MM-DD)", v.S)
		}
	}
	return nil
}

// Compile turns a condition on entities of the given types into rules
// deriving the matching entities as the unary relation it returns. Relation
// names start with prefix.
//
// With several types (REQ-0082), a field, facet, list or link
// a test names must exist on at least one of them, with the same type of
// value wherever it does; on an entity whose type lacks it, the field is
// empty, exactly like an unset field.
func Compile(s *schema.Schema, types []*schema.Type, cond *Condition, prefix string) (*datalog.Program, string, error) {
	if len(types) > 1 && cond != nil {
		if err := checkAcross(s, types, cond); err != nil {
			return nil, "", err
		}
	}
	prog := &datalog.Program{}
	match := prefix + "match"
	E := datalog.Var("E")
	for _, t := range types {
		c := &compiler{s: s, t: t, prog: prog, prefix: prefix + t.Name + "_", lenient: len(types) > 1}
		body := []datalog.Literal{datalog.P(RelEntity, E, datalog.Const(datalog.String(t.Name)))}
		if cond != nil {
			lits, err := c.conjunction(cond, E)
			if err != nil {
				return nil, "", err
			}
			body = append(body, lits...)
		}
		prog.Add(datalog.Rule{Head: datalog.NewAtom(match, E), Body: body})
	}
	return prog, match, nil
}

// hasRef reports whether t has what r starts from: the field or facet, the
// list, or the link it crosses.
func hasRef(t *schema.Type, r Ref) bool {
	name := r.Field
	switch {
	case r.List != "":
		name = r.List
	case r.Via != "":
		name = r.Via
	case name == "id" || name == "type":
		return true
	}
	return t.Field(name) != nil || facetOf(t, name) != nil
}

// checkAcross checks every reference in cond against several types: at
// least one must have it, and every type that has it must give it the same
// type of value.
func checkAcross(s *schema.Schema, types []*schema.Type, cond *Condition) error {
	var refs []Ref
	var walk func(*Condition)
	walk = func(c *Condition) {
		switch {
		case c.Test != nil:
			refs = append(refs, c.Test.Field)
			if c.Test.Value != nil && c.Test.Value.Field != nil {
				refs = append(refs, *c.Test.Value.Field)
			}
		default:
			for _, sub := range append(slices.Clone(c.AllOf), c.AnyOf...) {
				walk(sub)
			}
		}
	}
	walk(cond)
	for _, r := range refs {
		var first *target
		var firstType *schema.Type
		for _, t := range types {
			if !hasRef(t, r) {
				continue
			}
			tg, err := (&compiler{s: s, t: t}).resolve(r)
			if err != nil {
				return err
			}
			if first == nil {
				first, firstType = &tg, t
				continue
			}
			if tg.class != first.class || tg.rows != first.rows || (tg.enum != nil) != (first.enum != nil) || (tg.enum != nil && tg.enum != first.enum) {
				return &Error{Line: r.Line, Msg: fmt.Sprintf("%s is %s on %s but %s on %s", r, first.class, firstType.Name, tg.class, t.Name)}
			}
		}
		if first == nil {
			return &Error{Line: r.Line, Msg: noField(types, refName(r))}
		}
	}
	return nil
}

func refName(r Ref) string {
	switch {
	case r.List != "":
		return r.List
	case r.Via != "":
		return r.Via
	}
	return r.Field
}

// noneOf names types as the subject of "has no …": "neither Risk nor
// Requirement", "none of Risk, Requirement, Design".
func noneOf(types []*schema.Type) string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.Name
	}
	if len(names) == 2 {
		return "neither " + names[0] + " nor " + names[1]
	}
	return "none of " + strings.Join(names, ", ")
}

// conjunction compiles cond, a test or an all-of group, to body literals
// over entity variable E. Tests on the same list within one all-of group
// read the same row.
func (c *compiler) conjunction(cond *Condition, E datalog.Term) ([]datalog.Literal, error) {
	// A list inside a list is the same all-of group.
	var items []*Condition
	var flatten func(*Condition)
	flatten = func(c *Condition) {
		if c.AllOf == nil {
			items = append(items, c)
			return
		}
		for _, sub := range c.AllOf {
			flatten(sub)
		}
	}
	flatten(cond)
	rows := map[string]datalog.Term{}
	var lits []datalog.Literal
	var rowLits []datalog.Literal
	rowVar := func(list string) datalog.Term {
		if v, ok := rows[list]; ok {
			return v
		}
		v := datalog.Var(fmt.Sprintf("R%d", len(rows)))
		rows[list] = v
		rowLits = append(rowLits, datalog.P(RelRow, E, datalog.Const(datalog.String(list)), v))
		return v
	}
	for _, item := range items {
		switch {
		case item.AnyOf != nil:
			rel := c.rel("any")
			for _, alt := range item.AnyOf {
				sub, err := c.conjunction(alt, E)
				if err != nil {
					return nil, err
				}
				body := append([]datalog.Literal{datalog.P(RelEntity, E, datalog.Const(datalog.String(c.t.Name)))}, sub...)
				c.prog.Add(datalog.Rule{Head: datalog.NewAtom(rel, E), Body: body})
			}
			lits = append(lits, datalog.P(rel, E))
		default:
			lit, err := c.test(item.Test, E, rowVar)
			if err != nil {
				return nil, err
			}
			lits = append(lits, lit)
		}
	}
	return append(rowLits, lits...), nil
}

// test compiles one test to a relation holding where it is true, and
// returns the literal reading it: negated for not_equals, which holds when
// no value equals (so it also holds when the field is empty).
func (c *compiler) test(t *Test, E datalog.Term, rowVar func(string) datalog.Term) (datalog.Literal, error) {
	if c.lenient && (!hasRef(c.t, t.Field) || (t.Value != nil && t.Value.Field != nil && !hasRef(c.t, *t.Value.Field))) {
		// Nothing derives this relation, so the test never holds, and
		// not_equals, its negation, always does.
		absent := c.rel("absent")
		if t.Operator == NotEquals {
			return datalog.N(absent, E), nil
		}
		return datalog.P(absent, E), nil
	}
	left, err := c.resolve(t.Field)
	if err != nil {
		return datalog.Literal{}, err
	}
	var right *target
	if t.Value != nil && t.Value.Field != nil {
		r, err := c.resolve(*t.Value.Field)
		if err != nil {
			return datalog.Literal{}, err
		}
		right = &r
	}
	if err := checkTest(t, left, right); err != nil {
		return datalog.Literal{}, err
	}

	// The relation's arguments: the entity, then the row of each list the
	// test reads, so the conjunction can share rows between tests.
	args := []datalog.Term{E}
	var lists []string
	for _, tg := range []*target{&left, right} {
		if tg != nil && tg.ref.List != "" && !slices.Contains(lists, tg.ref.List) {
			lists = append(lists, tg.ref.List)
		}
	}
	slices.Sort(lists)
	rowOf := map[string]datalog.Term{}
	body := []datalog.Literal{datalog.P(RelEntity, E, datalog.Const(datalog.String(c.t.Name)))}
	for i, l := range lists {
		v := datalog.Var(fmt.Sprintf("R%d", i))
		rowOf[l] = v
		args = append(args, v)
		body = append(body, datalog.P(RelRow, E, datalog.Const(datalog.String(l)), v))
	}

	V, W := datalog.Var("V"), datalog.Var("W")
	if left.rows {
		body = append(body, datalog.P(RelRow, E, datalog.Const(datalog.String(left.ref.Field)), datalog.Var("_")))
	} else {
		body = append(body, read(left, E, rowOf, V, "L")...)
	}
	if t.Operator != Exists {
		other := datalog.Const(t.Value.Literal)
		if right != nil {
			body = append(body, read(*right, E, rowOf, W, "M")...)
			other = W
		}
		body = append(body, datalog.Cmp(V, cmpOps[t.Operator], other))
	}
	rel := c.rel("test")
	c.prog.Add(datalog.Rule{Head: datalog.NewAtom(rel, args...), Body: body})

	outer := []datalog.Term{E}
	for _, l := range lists {
		outer = append(outer, rowVar(l))
	}
	if t.Operator == NotEquals {
		return datalog.N(rel, outer...), nil
	}
	return datalog.P(rel, outer...), nil
}

// read returns literals binding v to each value of tg on entity E. tmp
// prefixes any intermediate variables.
func read(tg target, E datalog.Term, rowOf map[string]datalog.Term, v datalog.Term, tmp string) []datalog.Literal {
	name := datalog.Const(datalog.String(tg.ref.Field))
	switch {
	case tg.ref.List != "":
		return []datalog.Literal{datalog.P(tg.rel, rowOf[tg.ref.List], name, v)}
	case tg.ref.Via != "":
		T := datalog.Var(tmp + "T")
		return []datalog.Literal{
			datalog.P(RelLink, E, datalog.Const(datalog.String(tg.ref.Via)), T),
			datalog.P(tg.rel, T, name, v),
		}
	case tg.ref.Field == "id":
		return []datalog.Literal{datalog.Cmp(v, datalog.Eq, E)}
	case tg.ref.Field == "type":
		return []datalog.Literal{datalog.P(RelEntity, E, v)}
	}
	return []datalog.Literal{datalog.P(tg.rel, E, name, v)}
}
