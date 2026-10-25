package query

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// The condition grammar (DES-0015), shared by query blocks, rule files,
// --scope query files and the HTTP API:
//
//	where: {field: status, operator: equals, value: approved}   one test
//	where: {field: due, operator: less_than, value_of: start}   against a field
//	where: [<condition>, <condition>]                          all must hold
//	where: {any_of: [<condition>, …]}                          at least one
//	where: {field: L, where: <condition>}                      a sub-query
//	where: {field: L, aggregation: {type: count}, operator: less_than, value: 1}
//	where: {from: T, where: <condition>}                       some entity of T
//	where: {field: L, transitive: true, where: <condition>}    at any depth
//
// field names a field or incoming facet of the entity, its id or type, or
// code_blocks, the list of its fenced blocks (schema.CodeBlocks). A
// sub-query holds when some row of list L, or some entity linked through
// link or facet L, matches its condition; inside it names are the item's,
// and ^name is the enclosing entity's field (^^name two levels out). A
// sub-query on from instead holds when some entity of its types matches:
// the way to compare an entity with others it has no link to, such as two
// requirements sharing a title. With
// aggregation, the sub-query's items are counted, or one of their fields
// summed, and the single result is tested. A transitive sub-query on a link
// or facet reaches the entities it leads to at any depth: the linked ones,
// those they link to through the same name, and so on.

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
	Missing        Operator = "missing"
	NotIn          Operator = "not_in"
)

// Operators lists every operator, in the order DES-0015 gives.
var Operators = []Operator{Equals, NotEquals, GreaterOrEqual, LessOrEqual, GreaterThan, LessThan, Exists, Missing, NotIn}

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

// Test is one field/operator/value test, or a sub-query.
type Test struct {
	Line  int
	Field Ref
	// Operator is "" for a sub-query without aggregation.
	Operator Operator
	// Value is a literal or value_of; nil for exists, missing, not_in and
	// a sub-query without aggregation.
	Value *Operand
	// In is not_in's values.
	In []datalog.Value
	// Where is a sub-query's condition, on each row or linked entity; nil
	// for an aggregation over every item.
	Where *Condition
	// Aggregation makes a sub-query a single value, which Operator tests.
	Aggregation *Aggregation
	// From makes a sub-query over the entities of these types (or any)
	// instead of over a field's rows or links; Field is then unset.
	From     []string
	FromLine int
	// Transitive makes a sub-query on a link or incoming facet reach every
	// entity the link leads to at any depth, not only the linked ones.
	Transitive bool
}

// SubQuery reports whether t tests the rows, linked entities or other
// entities of a sub-query rather than a value.
func (t *Test) SubQuery() bool {
	return t.Where != nil || t.Aggregation != nil || t.From != nil || t.Transitive
}

// Aggregation reduces a sub-query's items to one value (DES-0015), with the
// calculated-field aggregates' semantics (DES-0012).
type Aggregation struct {
	Line int
	Type AggregationType
	// Field is the item field sum, min, max and avg read; nil for count.
	Field *Ref
}

type AggregationType string

const (
	Count AggregationType = "count"
	Sum   AggregationType = "sum"
	Min   AggregationType = "min"
	Max   AggregationType = "max"
	Avg   AggregationType = "avg"
)

var aggregationTypes = []AggregationType{Count, Sum, Min, Max, Avg}

// Ref names a value: a field of the item a condition is about, or with Up
// set, of an entity enclosing it.
type Ref struct {
	Line int
	// Up counts the ^s: 1 is the enclosing entity, 2 the one around it.
	Up    int
	Field string
}

func (r Ref) String() string { return strings.Repeat("^", r.Up) + r.Field }

// Operand is a test's value: a literal, or another field (value_of).
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

// ParseCondition reads a where clause.
func ParseCondition(n *yaml.Node) (*Condition, error) {
	switch n.Kind {
	case yaml.SequenceNode:
		c := &Condition{Line: n.Line}
		if len(n.Content) == 0 {
			return nil, errorf(n, "an empty list of conditions")
		}
		for _, item := range n.Content {
			sub, err := ParseCondition(item)
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
			if alts.Kind != yaml.SequenceNode || len(alts.Content) == 0 {
				return nil, errorf(alts, "any_of needs a list of conditions")
			}
			c := &Condition{Line: n.Line}
			for _, item := range alts.Content {
				sub, err := ParseCondition(item)
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
	if err := onlyKeys(n, "a condition", "field", "from", "operator", "value", "value_of", "where", "aggregation", "transitive"); err != nil {
		return nil, err
	}
	t := &Test{Line: n.Line}
	fieldNode, fromNode := yamlnode.Lookup(n, "field"), yamlnode.Lookup(n, "from")
	opNode := yamlnode.Lookup(n, "operator")
	valueNode, valueOf := yamlnode.Lookup(n, "value"), yamlnode.Lookup(n, "value_of")
	where, agg := yamlnode.Lookup(n, "where"), yamlnode.Lookup(n, "aggregation")
	if tn := yamlnode.Lookup(n, "transitive"); tn != nil {
		if tn.Kind != yaml.ScalarNode || tn.Tag != "!!bool" {
			return nil, errorf(tn, "transitive is true or false")
		}
		if err := tn.Decode(&t.Transitive); err != nil {
			return nil, errorf(tn, "transitive is true or false")
		}
		if t.Transitive && fromNode != nil {
			return nil, errorf(tn, "transitive follows a link field or incoming facet; from looks among other entities and follows none")
		}
	}

	switch {
	case fieldNode != nil && fromNode != nil:
		return nil, errorf(fromNode, "a condition tests a field, or with from looks among other entities, not both")
	case fromNode != nil:
		names, err := parseFrom(fromNode)
		if err != nil {
			return nil, err
		}
		t.From, t.FromLine = names, fromNode.Line
	case fieldNode == nil:
		return nil, errorf(n, "condition has no field")
	default:
		ref, err := parseRef(fieldNode, "field")
		if err != nil {
			return nil, err
		}
		t.Field = ref
	}

	if t.From != nil || where != nil || agg != nil || t.Transitive {
		if t.Field.Up > 0 {
			return nil, errorf(fieldNode, "a sub-query reads the item's own list or link, not ^%s", t.Field.Field)
		}
		if where != nil {
			cond, err := ParseCondition(where)
			if err != nil {
				return nil, err
			}
			t.Where = cond
		}
		if agg == nil {
			switch {
			case opNode != nil:
				return nil, errorf(opNode, "a sub-query holds when some item matches its where, and takes no operator; with aggregation, operator tests the aggregate")
			case valueNode != nil || valueOf != nil:
				return nil, errorf(cmpNode(valueNode, valueOf), "a sub-query takes no value")
			}
			return t, nil
		}
		a, err := parseAggregation(agg)
		if err != nil {
			return nil, err
		}
		t.Aggregation = a
		if opNode == nil {
			return nil, errorf(n, "an aggregation needs an operator to test it (valid operators: %s)", validOperators())
		}
	}

	if opNode == nil {
		return nil, errorf(n, "condition has no operator (valid operators: %s)", validOperators())
	}
	t.Operator = Operator(opNode.Value)
	if opNode.Kind != yaml.ScalarNode || !slices.Contains(Operators, t.Operator) {
		return nil, errorf(opNode, "unknown operator %q (valid operators: %s)", opNode.Value, validOperators())
	}
	switch {
	case valueNode != nil && valueOf != nil:
		return nil, errorf(valueOf, "use value (a literal) or value_of (a field), not both")
	case t.Operator == Exists || t.Operator == Missing:
		if v := cmpNode(valueNode, valueOf); v != nil {
			return nil, errorf(v, "%s takes no value", t.Operator)
		}
		return t, nil
	case t.Operator == NotIn:
		if valueOf != nil {
			return nil, errorf(valueOf, "not_in takes a list of values, not value_of")
		}
		if valueNode == nil || valueNode.Kind != yaml.SequenceNode || len(valueNode.Content) == 0 {
			return nil, errorf(cmpNode(valueNode, opNode), "not_in needs a list of values, e.g. value: [a, b]")
		}
		for _, item := range valueNode.Content {
			v, ok := literal(item)
			if !ok {
				return nil, errorf(item, "not_in's values must be single values")
			}
			t.In = append(t.In, v)
		}
		t.Value = &Operand{Line: valueNode.Line}
		return t, nil
	case valueOf != nil:
		r, err := parseRef(valueOf, "value_of")
		if err != nil {
			return nil, err
		}
		t.Value = &Operand{Line: valueOf.Line, Field: &r}
		return t, nil
	case valueNode == nil || valueNode.Tag == "!!null":
		return nil, errorf(n, "%s needs a value or value_of", t.Operator)
	case valueNode.Kind == yaml.MappingNode && yamlnode.Lookup(valueNode, "field") != nil:
		return nil, errorf(valueNode, "value: {field: %s} is now value_of: %s", scalar(yamlnode.Lookup(valueNode, "field")), scalar(yamlnode.Lookup(valueNode, "field")))
	}
	v, ok := literal(valueNode)
	if !ok {
		return nil, errorf(valueNode, "value must be a single value (only not_in takes a list)")
	}
	t.Value = &Operand{Line: valueNode.Line, Literal: v}
	return t, nil
}

// parseFrom reads the type names a from names: one, a list, or any. They
// are checked against the schema when the condition is compiled.
func parseFrom(n *yaml.Node) ([]string, error) {
	nodes := []*yaml.Node{n}
	if n.Kind == yaml.SequenceNode {
		nodes = n.Content
		if len(nodes) == 0 {
			return nil, errorf(n, "from: an empty list of types")
		}
	}
	var names []string
	for _, item := range nodes {
		name := scalar(item)
		if name == "" {
			return nil, errorf(item, "from names a type, a list of types, or any")
		}
		if slices.Contains(names, name) {
			return nil, errorf(item, "from: %s is listed twice", name)
		}
		names = append(names, name)
	}
	return names, nil
}

func parseAggregation(n *yaml.Node) (*Aggregation, error) {
	if n.Kind != yaml.MappingNode {
		return nil, errorf(n, "aggregation is {type: count} or {type: sum|min|max|avg, field: <field>}")
	}
	if err := onlyKeys(n, "an aggregation", "type", "field"); err != nil {
		return nil, err
	}
	a := &Aggregation{Line: n.Line}
	tn := yamlnode.Lookup(n, "type")
	var names []string
	for _, at := range aggregationTypes {
		names = append(names, string(at))
	}
	if tn == nil || !slices.Contains(aggregationTypes, AggregationType(scalar(tn))) {
		return nil, errorf(cmpNode(tn, n), "aggregation type must be one of %s", strings.Join(names, ", "))
	}
	a.Type = AggregationType(tn.Value)
	fn := yamlnode.Lookup(n, "field")
	switch {
	case a.Type == Count && fn != nil:
		return nil, errorf(fn, "count counts the items and reads no field")
	case a.Type != Count && fn == nil:
		return nil, errorf(n, "%s needs the field of each item it reads", a.Type)
	case fn != nil:
		r, err := parseRef(fn, "aggregation field")
		if err != nil {
			return nil, err
		}
		if r.Up > 0 {
			return nil, errorf(fn, "an aggregation reads the items' own field, not ^%s", r.Field)
		}
		a.Field = &r
	}
	return a, nil
}

// cmpNode returns the first of nodes that is set.
func cmpNode(nodes ...*yaml.Node) *yaml.Node {
	for _, n := range nodes {
		if n != nil {
			return n
		}
	}
	return nil
}

func parseRef(n *yaml.Node, key string) (Ref, error) {
	r := Ref{Line: n.Line}
	if n.Kind == yaml.MappingNode {
		// The forms sub-queries replaced (DES-0015, 2026-10-11).
		switch {
		case yamlnode.Lookup(n, "list") != nil:
			return r, errorf(n, "{list: L, subfield: S} is now a sub-query: {field: L, where: {field: S, …}}")
		case yamlnode.Lookup(n, "via") != nil:
			return r, errorf(n, "{via: L, field: F} is now a sub-query: {field: L, where: {field: F, …}}")
		}
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Value == "" {
		return r, errorf(n, "%s must name a field", key)
	}
	name := strings.TrimLeft(n.Value, "^")
	r.Up, r.Field = len(n.Value)-len(name), name
	if name == "" {
		return r, errorf(n, "%s must name a field after ^", key)
	}
	return r, nil
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

// target is a resolved reference: where and how to read its values.
type target struct {
	ref Ref
	// at is the scope whose node holds the value.
	at *scope
	// rel is RelField or RelLink for stored values; "" for id and type.
	rel   string
	class valueClass
	enum  *schema.EnumType
	// rows is set when the reference is a list of rows itself.
	rows bool
}

func (a target) sameKind(b target) bool {
	return a.class == b.class && a.rows == b.rows && a.enum == b.enum
}

// scope is the item names in a condition belong to: an entity of one type,
// or a row of one list. A sub-query opens a scope inside its enclosing one.
type scope struct {
	node   datalog.Term
	t      *schema.Type  // an entity of this type, or
	list   *schema.Field // a row of this list
	parent *scope
	// bind are the literals binding node, given the parent's.
	bind []datalog.Literal
	lv   *level
}

func (s *scope) depth() int {
	if s.parent == nil {
		return 0
	}
	return s.parent.depth() + 1
}

// chain is every node from the outermost scope to s.
func (s *scope) chain() []datalog.Term {
	if s.parent == nil {
		return []datalog.Term{s.node}
	}
	return append(s.parent.chain(), s.node)
}

// binds are the literals binding every node of the chain.
func (s *scope) binds() []datalog.Literal {
	if s.parent == nil {
		return slices.Clone(s.bind)
	}
	return append(s.parent.binds(), s.bind...)
}

// level is one level of a condition, the top or a sub-query, compiled once
// for each of its alternatives: every type from selects, or a link reaches.
// A name on some alternatives only is empty on the others; it must exist on
// at least one, with the same kind of value on every one that has it.
type level struct {
	alts  []string // the alternatives' type names
	names map[*Ref]*seen
	order []*Ref
}

type seen struct {
	found bool
	tg    target
	on    string
}

func newLevel(alts []string) *level { return &level{alts: alts, names: map[*Ref]*seen{}} }

func (lv *level) lenient() bool { return len(lv.alts) > 1 }

// record notes how alternative on resolves r (found is false when it lacks
// it) and rejects a second kind of value.
func (lv *level) record(r *Ref, on string, found bool, tg target) error {
	s := lv.names[r]
	if s == nil {
		s = &seen{}
		lv.names[r] = s
		lv.order = append(lv.order, r)
	}
	if !found {
		return nil
	}
	if s.found && !s.tg.sameKind(tg) {
		return &Error{Line: r.Line, Msg: fmt.Sprintf("%s is %s on %s but %s on %s", r, describe(s.tg), s.on, describe(tg), on)}
	}
	if !s.found {
		s.found, s.tg, s.on = true, tg, on
	}
	return nil
}

// check rejects a name no alternative has.
func (lv *level) check() error {
	for _, r := range lv.order {
		if !lv.names[r].found {
			return &Error{Line: r.Line, Msg: fmt.Sprintf("%s has a field %q", noneOfNames(lv.alts), r.Field)}
		}
	}
	return nil
}

func describe(tg target) string {
	if tg.rows {
		return "a list of rows"
	}
	return tg.class.String()
}

type compiler struct {
	s    *schema.Schema
	prog *datalog.Program
	// prefix keeps this compilation's relations apart from any other
	// program evaluated alongside.
	prefix string
	n      int
	// witnesses are, for each sub-query among the top-level conditions,
	// the relations holding (entity, item) for the items it matched.
	witnesses map[*Test][]string
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
	return strings.Join(append(names, schema.CodeBlocks.Name), ", ")
}

func subFieldNames(list *schema.Field) string {
	var names []string
	for _, f := range list.Fields {
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

// field returns the field named name of the scope's item.
func (sc *scope) field(name string) *schema.Field {
	if sc.list != nil {
		for _, f := range sc.list.Fields {
			if f.Name == name {
				return f
			}
		}
		return nil
	}
	if f := sc.t.Field(name); f != nil || name != schema.CodeBlocks.Name {
		return f
	}
	return schema.CodeBlocks
}

func (sc *scope) facet(name string) *schema.Facet {
	if sc.t == nil {
		return nil
	}
	return facetOf(sc.t, name)
}

// alt names the scope's alternative in its level.
func (sc *scope) alt() string {
	if sc.t != nil {
		return sc.t.Name
	}
	return sc.list.Name
}

// noName says the scope's item has no field name.
func (sc *scope) noName(name string) string {
	if sc.list != nil {
		return fmt.Sprintf("list %q has no sub-field %q (sub-fields: %s)", sc.list.Name, name, subFieldNames(sc.list))
	}
	return fmt.Sprintf("%s has no field %q (fields: %s)", sc.t.Name, name, fieldNames(sc.t))
}

// resolve finds what r names from scope sc. ok is false when r's scope is
// one of several alternatives and lacks it: the value is then empty.
func (c *compiler) resolve(r *Ref, sc *scope) (tg target, ok bool, err error) {
	at := sc
	for range r.Up {
		at = at.parent
		if at == nil {
			return target{}, false, &Error{Line: r.Line, Msg: fmt.Sprintf("%s: there is no enclosing entity %d level(s) out", r, r.Up)}
		}
	}
	tg = target{ref: *r, at: at}
	found := true
	switch f := at.field(r.Field); {
	case at.list == nil && r.Field == "id":
		tg.class = classID
	case at.list == nil && r.Field == "type":
		tg.class = classText
	case f != nil && f.Kind == schema.List && f.Elem == nil:
		tg.rows = true
	case f != nil:
		tg.class, tg.enum = classOf(c.s, f)
		tg.rel = RelField
		if f.Kind == schema.Link {
			tg.rel = RelLink
		}
	case at.facet(r.Field) != nil:
		tg.rel, tg.class = RelLink, classID
	default:
		found = false
	}
	if err := at.lv.record(r, at.alt(), found, tg); err != nil {
		return target{}, false, err
	}
	if !found && !at.lv.lenient() {
		msg := at.noName(r.Field)
		if at.list != nil && (r.Field == "id" || r.Field == "type") {
			msg = fmt.Sprintf("a row of %q has no %s; ^%s is the entity's", at.list.Name, r.Field, r.Field)
		}
		return target{}, false, &Error{Line: r.Line, Msg: msg}
	}
	return tg, found, nil
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
		if t.Operator != Exists && t.Operator != Missing {
			return fail(t.Field.Line, "%q is a list of rows: test its rows with {field: %s, where: …}, count them with aggregation, or use exists or missing", t.Field.Field, t.Field.Field)
		}
		return nil
	}
	switch t.Operator {
	case Exists, Missing:
		return nil
	case GreaterOrEqual, LessOrEqual, GreaterThan, LessThan:
		switch left.class {
		case classAny, classNumber, classDate, classText:
		default:
			return fail(t.Line, "%s compares numbers, dates and text; %s is %s", t.Operator, left.ref, left.class)
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
			return fail(t.Value.Line, "%s is %s but %s is %s", left.ref, left.class, right.ref, right.class)
		}
		return nil
	}
	values := t.In
	if t.Operator != NotIn {
		values = []datalog.Value{t.Value.Literal}
	}
	for _, v := range values {
		if err := checkLiteral(t, left, v); err != nil {
			return err
		}
	}
	return nil
}

func checkLiteral(t *Test, left target, v datalog.Value) error {
	fail := func(format string, args ...any) error {
		return &Error{Line: t.Value.Line, Msg: fmt.Sprintf(format, args...)}
	}
	if left.class == classAny {
		return nil
	}
	want := map[valueClass]datalog.Kind{classNumber: datalog.Num, classBool: datalog.Bool}[left.class]
	if want == 0 {
		want = datalog.Str
	}
	if v.Kind != want {
		return fail("%s is %s; %s is not", left.ref, left.class, v)
	}
	switch left.class {
	case classEnum:
		if left.enum != nil && !slices.Contains(left.enum.Values, v.S) {
			return fail("%q is not a value of %s (values: %s)", v.S, left.enum.Name, strings.Join(left.enum.Values, ", "))
		}
	case classDate:
		if _, err := time.Parse("2006-01-02", v.S); err != nil {
			return fail("%q is not a date (YYYY-MM-DD)", v.S)
		}
	}
	return nil
}

// compiled is a condition compiled to a program.
type compiled struct {
	prog *datalog.Program
	// match holds the matching entities, as a unary relation.
	match string
	// witnesses holds, for each sub-query among the top-level conditions,
	// the relation of (entity, item) for the items it matched.
	witnesses []witness
}

type witness struct {
	test *Test
	rel  string
}

// Compile turns a condition on entities of the given types into rules
// deriving the matching entities as the unary relation it returns. Relation
// names start with prefix.
//
// With several types (REQ-0082), a field a test names must exist on at
// least one of them, with the same type of value wherever it does; on an
// entity whose type lacks it, the field is empty, exactly like an unset
// field. The same holds inside a sub-query on a link reaching several types.
func Compile(s *schema.Schema, types []*schema.Type, cond *Condition, prefix string) (*datalog.Program, string, error) {
	cp, err := compile(s, types, cond, prefix)
	if err != nil {
		return nil, "", err
	}
	return cp.prog, cp.match, nil
}

func compile(s *schema.Schema, types []*schema.Type, cond *Condition, prefix string) (*compiled, error) {
	c := &compiler{s: s, prog: &datalog.Program{}, prefix: prefix, witnesses: map[*Test][]string{}}
	match := prefix + "match"
	E := datalog.Var("E")
	var names []string
	for _, t := range types {
		names = append(names, t.Name)
	}
	lv := newLevel(names)
	for _, t := range types {
		sc := &scope{node: E, t: t, bind: []datalog.Literal{datalog.P(RelEntity, E, datalog.Const(datalog.String(t.Name)))}, lv: lv}
		body := sc.binds()
		if cond != nil {
			lits, err := c.conjunction(cond, sc, true)
			if err != nil {
				return nil, err
			}
			body = append(body, lits...)
		}
		c.prog.Add(datalog.Rule{Head: datalog.NewAtom(match, E), Body: body})
	}
	if err := lv.check(); err != nil {
		return nil, err
	}
	cp := &compiled{prog: c.prog, match: match}
	I := datalog.Var("I")
	for _, t := range topSubQueries(cond) {
		rel := c.rel("witness")
		for _, sub := range c.witnesses[t] {
			c.prog.Add(datalog.Rule{Head: datalog.NewAtom(rel, E, I), Body: []datalog.Literal{datalog.P(match, E), datalog.P(sub, E, I)}})
		}
		cp.witnesses = append(cp.witnesses, witness{test: t, rel: rel})
	}
	return cp, nil
}

// flatten lists cond's items: a list inside a list is the same all-of group.
func flatten(cond *Condition) []*Condition {
	if cond == nil {
		return nil
	}
	if cond.AllOf == nil {
		return []*Condition{cond}
	}
	var out []*Condition
	for _, sub := range cond.AllOf {
		out = append(out, flatten(sub)...)
	}
	return out
}

// topSubQueries are the sub-queries every match of cond satisfies: those
// among its top-level conditions, not inside any_of.
func topSubQueries(cond *Condition) []*Test {
	var out []*Test
	for _, item := range flatten(cond) {
		if item.Test != nil && item.Test.SubQuery() && item.Test.Aggregation == nil {
			out = append(out, item.Test)
		}
	}
	return out
}

// conjunction compiles cond, a test or an all-of group, to body literals
// over sc's chain of nodes. top marks the outermost level's own conditions.
func (c *compiler) conjunction(cond *Condition, sc *scope, top bool) ([]datalog.Literal, error) {
	var lits []datalog.Literal
	for _, item := range flatten(cond) {
		switch {
		case item.AnyOf != nil:
			rel := c.rel("any")
			for _, alt := range item.AnyOf {
				sub, err := c.conjunction(alt, sc, false)
				if err != nil {
					return nil, err
				}
				c.prog.Add(datalog.Rule{Head: datalog.NewAtom(rel, sc.chain()...), Body: append(sc.binds(), sub...)})
			}
			lits = append(lits, datalog.P(rel, sc.chain()...))
		case item.Test.SubQuery():
			lit, err := c.subQuery(item.Test, sc, top)
			if err != nil {
				return nil, err
			}
			lits = append(lits, lit)
		default:
			lit, err := c.test(item.Test, sc)
			if err != nil {
				return nil, err
			}
			lits = append(lits, lit)
		}
	}
	return lits, nil
}

// subQuery compiles {field: L, where: …} or {from: T, where: …}: a
// relation of the chain and each row, linked entity or entity of T matching
// the inner condition, read with the item left free.
func (c *compiler) subQuery(t *Test, sc *scope, top bool) (datalog.Literal, error) {
	name := t.Field.Field
	item := datalog.Var(fmt.Sprintf("N%d", sc.depth()+1))
	lname := datalog.Const(datalog.String(name))
	var alts []*scope
	f, facet := sc.field(name), sc.facet(name)
	kind := target{ref: t.Field, at: sc, class: classID}
	if t.From != nil {
		types, err := FromTypes(c.s, t.From, t.FromLine)
		if err != nil {
			return datalog.Literal{}, err
		}
		for _, tt := range types {
			alts = append(alts, &scope{t: tt, bind: []datalog.Literal{datalog.P(RelEntity, item, datalog.Const(datalog.String(tt.Name)))}})
		}
		return c.items(t, sc, alts, top)
	}
	var reached []*schema.Type
	switch {
	case f != nil && f.Kind == schema.List && f.Elem == nil:
		if t.Transitive {
			return datalog.Literal{}, &Error{Line: t.Field.Line, Msg: fmt.Sprintf("%q is a list of rows: transitive follows a link field or incoming facet", name)}
		}
		kind = target{ref: t.Field, at: sc, rows: true}
		alts = []*scope{{list: f, bind: []datalog.Literal{datalog.P(RelRow, sc.node, lname, item)}}}
	case f != nil && f.Kind == schema.Link:
		reached = linkTypes(c.s, f)
	case f != nil:
		return datalog.Literal{}, &Error{Line: t.Field.Line, Msg: fmt.Sprintf("%q is %s: a sub-query reads the rows of a list or the entities a link reaches", name, valueKind(c.s, f))}
	case facet != nil:
		reached = facetTypes(facet)
	}
	if reached != nil {
		via := datalog.P(RelLink, sc.node, lname, item)
		if t.Transitive {
			reached = transitiveTypes(c.s, reached, name)
			via = datalog.P(c.reach(lname), sc.node, item)
		}
		for _, tt := range reached {
			alts = append(alts, &scope{t: tt, bind: []datalog.Literal{via, datalog.P(RelEntity, item, datalog.Const(datalog.String(tt.Name)))}})
		}
	}
	if err := sc.lv.record(&t.Field, sc.alt(), alts != nil, kind); err != nil {
		return datalog.Literal{}, err
	}
	if alts == nil {
		if !sc.lv.lenient() {
			return datalog.Literal{}, &Error{Line: t.Field.Line, Msg: sc.noName(name)}
		}
		return datalog.P(c.rel("absent"), sc.chain()...), nil
	}
	return c.items(t, sc, alts, top)
}

// items compiles a sub-query's condition on each of alts, the scopes its
// items may have, and its aggregation if it has one.
func (c *compiler) items(t *Test, sc *scope, alts []*scope, top bool) (datalog.Literal, error) {
	item := datalog.Var(fmt.Sprintf("N%d", sc.depth()+1))
	var names []string
	for _, a := range alts {
		names = append(names, a.alt())
	}
	lv := newLevel(names)
	rel := c.rel("sub")
	for _, a := range alts {
		a.node, a.parent, a.lv = item, sc, lv
		var lits []datalog.Literal
		if t.Where != nil {
			var err error
			if lits, err = c.conjunction(t.Where, a, false); err != nil {
				return datalog.Literal{}, err
			}
		}
		c.prog.Add(datalog.Rule{Head: datalog.NewAtom(rel, a.chain()...), Body: append(a.binds(), lits...)})
	}
	if t.Aggregation != nil {
		lit, err := c.aggregate(t, sc, alts, rel)
		if err != nil {
			return datalog.Literal{}, err
		}
		if err := lv.check(); err != nil {
			return datalog.Literal{}, err
		}
		return lit, nil
	}
	if err := lv.check(); err != nil {
		return datalog.Literal{}, err
	}
	if top {
		c.witnesses[t] = append(c.witnesses[t], rel)
	}
	return datalog.P(rel, append(sc.chain(), datalog.Var("_"))...), nil
}

// aggregate compiles a sub-query's aggregation to a relation of the chain
// and the aggregate, items being sub's, and returns the literal testing it.
// As in calculated fields (DES-0012): each item counts once; sum, min, max
// and avg read numbers and skip the rest; with nothing to read, count and
// sum are 0 and the others blank.
func (c *compiler) aggregate(t *Test, sc *scope, alts []*scope, sub string) (datalog.Literal, error) {
	a := t.Aggregation
	chain := sc.chain()
	A, V := datalog.Var("A"), datalog.Var("V")
	agg := c.rel("agg")
	items := append(slices.Clone(chain), alts[0].node)
	// in holds what the aggregate reads: the items, for count, or each
	// item's values of the field.
	in := sub
	inArgs := items
	fn := map[AggregationType]datalog.AggFunc{Count: datalog.Count, Sum: datalog.Sum, Min: datalog.Min, Max: datalog.Max, Avg: datalog.Avg}[a.Type]
	of := ""
	if a.Field != nil {
		in, of = c.rel("values"), "V"
		inArgs = append(slices.Clone(items), V)
		for _, alt := range alts {
			tg, ok, err := c.resolve(a.Field, alt)
			if err != nil {
				return datalog.Literal{}, err
			}
			if !ok {
				continue // empty on this type: nothing to read
			}
			if tg.rows || (tg.class != classNumber && tg.class != classAny) {
				return datalog.Literal{}, &Error{Line: a.Field.Line, Msg: fmt.Sprintf("%s reads numbers; %s is %s", a.Type, a.Field.Field, describe(tg))}
			}
			body := append(alt.binds(), datalog.P(sub, items...))
			c.prog.Add(datalog.Rule{Head: datalog.NewAtom(in, inArgs...), Body: append(body, read(tg, V)...)})
		}
	}
	c.prog.Add(datalog.Rule{
		Head: datalog.NewAtom(agg, append(slices.Clone(chain), A)...),
		Body: []datalog.Literal{datalog.P(in, inArgs...)},
		Agg:  &datalog.Aggregate{Func: fn, Pos: len(chain), Of: of},
	})
	if a.Type == Count || a.Type == Sum {
		has := c.rel("has")
		anon := slices.Clone(chain)
		for range len(inArgs) - len(chain) {
			anon = append(anon, datalog.Var("_"))
		}
		c.prog.Add(datalog.Rule{Head: datalog.NewAtom(has, chain...), Body: append(sc.binds(), datalog.P(in, anon...))})
		c.prog.Add(datalog.Rule{
			Head: datalog.NewAtom(agg, append(slices.Clone(chain), datalog.Const(datalog.Number(0)))...),
			Body: append(sc.binds(), datalog.N(has, chain...)),
		})
	}
	// Every aggregate is a number; it is named in errors as, e.g., "count
	// of verified_by" or "sum of failure_modes.score".
	what := t.Field.Field
	if t.From != nil {
		what = strings.Join(t.From, ", ")
	}
	label := fmt.Sprintf("%s of %s", a.Type, what)
	if a.Field != nil {
		label += "." + a.Field.Field
	}
	left := target{ref: Ref{Line: t.Field.Line, Field: label}, at: sc, class: classNumber}
	return c.compare(t, sc, left, []datalog.Literal{datalog.P(agg, append(slices.Clone(chain), V)...)})
}

// FromTypes resolves the type names of a from: every type for any, else
// each named one. line places an unknown name.
func FromTypes(s *schema.Schema, names []string, line int) ([]*schema.Type, error) {
	if len(names) == 1 && names[0] == "any" {
		var all []*schema.Type
		for _, n := range s.TypeNames() {
			all = append(all, s.Types[n])
		}
		return all, nil
	}
	var out []*schema.Type
	for _, n := range names {
		t := s.Types[n]
		if t == nil {
			return nil, &Error{Line: line, Msg: fmt.Sprintf("from: unknown type %q (types: %s)", n, strings.Join(s.TypeNames(), ", "))}
		}
		out = append(out, t)
	}
	return out, nil
}

// linkTypes are the declared types link field f targets.
func linkTypes(s *schema.Schema, f *schema.Field) []*schema.Type {
	var out []*schema.Type
	for _, tn := range f.Target {
		if tt := s.Types[tn]; tt != nil {
			out = append(out, tt)
		}
	}
	return out
}

// facetTypes are the types whose links feed facet, each once.
func facetTypes(facet *schema.Facet) []*schema.Type {
	var out []*schema.Type
	for _, src := range facet.Sources {
		if !slices.Contains(out, src.Type) {
			out = append(out, src.Type)
		}
	}
	return out
}

// transitiveTypes are the types a transitive sub-query on name reaches,
// given first, the types one step reaches: each further step follows the
// link field or incoming facet called name on the entity reached, as long
// as its type has one.
func transitiveTypes(s *schema.Schema, first []*schema.Type, name string) []*schema.Type {
	out := slices.Clone(first)
	for i := 0; i < len(out); i++ {
		var next []*schema.Type
		if f := out[i].Field(name); f != nil && f.Kind == schema.Link {
			next = linkTypes(s, f)
		} else if facet := facetOf(out[i], name); facet != nil {
			next = facetTypes(facet)
		}
		for _, tt := range next {
			if !slices.Contains(out, tt) {
				out = append(out, tt)
			}
		}
	}
	return out
}

// reach adds the transitive closure of the links called name, a link field
// or an incoming facet, as a relation of (node, reached entity), and returns
// it. A cycle reaches each entity once, so evaluation ends (REQ-0040), and
// an entity on a cycle reaches itself.
func (c *compiler) reach(name datalog.Term) string {
	rel := c.rel("reach")
	X, M, T := datalog.Var("X"), datalog.Var("M"), datalog.Var("T")
	c.prog.Add(datalog.Rule{Head: datalog.NewAtom(rel, X, T), Body: []datalog.Literal{datalog.P(RelLink, X, name, T)}})
	c.prog.Add(datalog.Rule{Head: datalog.NewAtom(rel, X, T), Body: []datalog.Literal{datalog.P(rel, X, M), datalog.P(RelLink, M, name, T)}})
	return rel
}

func valueKind(s *schema.Schema, f *schema.Field) string {
	if f.Kind == schema.List {
		return "a list of values"
	}
	c, _ := classOf(s, f)
	return c.String()
}

// test compiles one test to a relation of the chain holding where it is
// true, and returns the literal reading it: negated for not_equals, not_in
// and missing, which hold when no value matches (so also when the field is
// empty).
func (c *compiler) test(t *Test, sc *scope) (datalog.Literal, error) {
	left, okL, err := c.resolve(&t.Field, sc)
	if err != nil {
		return datalog.Literal{}, err
	}
	var right *target
	okR := true
	if t.Value != nil && t.Value.Field != nil {
		r, ok, err := c.resolve(t.Value.Field, sc)
		if err != nil {
			return datalog.Literal{}, err
		}
		right, okR = &r, ok
	}
	if !okL || !okR {
		// The value is empty on this alternative.
		return c.absent(t, sc), nil
	}
	return c.compareWith(t, sc, left, right, read(left, datalog.Var("V")))
}

// compare compiles t's operator and value against left, read into V by
// readLeft.
func (c *compiler) compare(t *Test, sc *scope, left target, readLeft []datalog.Literal) (datalog.Literal, error) {
	var right *target
	if t.Value != nil && t.Value.Field != nil {
		r, ok, err := c.resolve(t.Value.Field, sc)
		if err != nil {
			return datalog.Literal{}, err
		}
		if !ok {
			return c.absent(t, sc), nil
		}
		right = &r
	}
	return c.compareWith(t, sc, left, right, readLeft)
}

// negated reports whether t holds where no value matches, so also where
// the field is empty.
func negated(t *Test) bool {
	return t.Operator == NotEquals || t.Operator == NotIn || t.Operator == Missing
}

// absent is t on a value that is empty here: nothing derives the
// relation, so the test never holds, and its negations always do.
func (c *compiler) absent(t *Test, sc *scope) datalog.Literal {
	if negated(t) {
		return datalog.N(c.rel("absent"), sc.chain()...)
	}
	return datalog.P(c.rel("absent"), sc.chain()...)
}

func (c *compiler) compareWith(t *Test, sc *scope, left target, right *target, readLeft []datalog.Literal) (datalog.Literal, error) {
	if err := checkTest(t, left, right); err != nil {
		return datalog.Literal{}, err
	}
	lit := func(rel string) datalog.Literal {
		if negated(t) {
			return datalog.N(rel, sc.chain()...)
		}
		return datalog.P(rel, sc.chain()...)
	}
	rel := c.rel("test")
	head := datalog.NewAtom(rel, sc.chain()...)
	V, W := datalog.Var("V"), datalog.Var("W")
	body := append(sc.binds(), readLeft...)
	switch t.Operator {
	case Exists, Missing:
		c.prog.Add(datalog.Rule{Head: head, Body: body})
	case NotIn:
		for _, v := range t.In {
			c.prog.Add(datalog.Rule{Head: head, Body: append(slices.Clone(body), datalog.Cmp(V, datalog.Eq, datalog.Const(v)))})
		}
	default:
		other := datalog.Const(t.Value.Literal)
		if right != nil {
			body = append(body, read(*right, W)...)
			other = W
		}
		c.prog.Add(datalog.Rule{Head: head, Body: append(body, datalog.Cmp(V, cmpOps[t.Operator], other))})
	}
	return lit(rel), nil
}

// read returns literals binding v to each value of tg.
func read(tg target, v datalog.Term) []datalog.Literal {
	node := tg.at.node
	name := datalog.Const(datalog.String(tg.ref.Field))
	switch {
	case tg.rows:
		return []datalog.Literal{datalog.P(RelRow, node, name, v)}
	case tg.rel == "" && tg.ref.Field == "id":
		return []datalog.Literal{datalog.Cmp(v, datalog.Eq, node)}
	case tg.rel == "" && tg.ref.Field == "type":
		return []datalog.Literal{datalog.P(RelEntity, node, v)}
	}
	return []datalog.Literal{datalog.P(tg.rel, node, name, v)}
}

// noneOfNames names types as the subject of "has no …": "neither Risk nor
// Requirement", "none of Risk, Requirement, Design".
func noneOfNames(names []string) string {
	if len(names) == 2 {
		return "neither " + names[0] + " nor " + names[1]
	}
	return "none of " + strings.Join(names, ", ")
}

func noneOf(types []*schema.Type) string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.Name
	}
	return noneOfNames(names)
}
