package query

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// The graph's derived values, calculated fields (DES-0012) and query fields
// (DES-0010, DES-0032), are one Datalog program: a query field may select
// by a calculated field, a formula may count a query field's results, and
// one query field may select by the facet another feeds. The engine's
// stratification orders them; a loop through missing, not_in or an
// aggregation has no answer and is reported on the query fields in it.
//
// Inside the program a stored value is a base fact, but a derived one is a
// relation of its own: a query field's results are qf:<ID>.<field>(holder,
// result), and calculated values calc:… (calc.go). derived routes a read
// of a name to the union of both, dl:<name> for links and facets, cv:<name>
// for values, so the program depends on exactly the fields it reads. After
// evaluation every derived value is written back as a base fact, so query
// blocks and rules read them like stored ones.

// derived names the relations a read of a link, facet or calculated field
// goes to while derived values are evaluated. A nil *derived reads the
// stored facts.
type derived struct {
	links  map[string]string // link or facet name → relation (node, target)
	values map[string]string // calculated field name → relation (node, value)
}

// link is the literal reading name's links from node to item.
func (d *derived) link(node datalog.Term, name string, item datalog.Term) datalog.Literal {
	if d != nil {
		if rel, ok := d.links[name]; ok {
			return datalog.P(rel, node, item)
		}
	}
	return datalog.P(RelLink, node, datalog.Const(datalog.String(name)), item)
}

// field is the literal reading name's values on node.
func (d *derived) field(node datalog.Term, name string, v datalog.Term) datalog.Literal {
	if d != nil {
		if rel, ok := d.values[name]; ok {
			return datalog.P(rel, node, v)
		}
	}
	return datalog.P(RelField, node, datalog.Const(datalog.String(name)), v)
}

// queryField is one entity's query field and its query.
type queryField struct {
	e     *model.Entity
	v     *model.Value
	block *Block
	rel   string
	rules []datalog.Rule
}

func (q *queryField) name() string { return q.e.ID + "." + q.v.Field.Name }

// queryFieldKeys are the keys of a query field's value: a query block's
// selection, not its presentation (DES-0032).
var queryFieldKeys = []string{"from", "where", "order_by"}

// ParseQueryField reads a query field's value n, written on an entity of
// type holder, against the schema: from: must name types of the field's
// of:, and where: and order_by: are a query block's. ^field in where:
// reads the holder. Error lines are n's file lines.
func ParseQueryField(s *schema.Schema, holder *schema.Type, f *schema.Field, n *yaml.Node) (*Block, error) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, errorf(n, "a query is a mapping with from:, and where: and order_by: if wanted")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		switch k := n.Content[i]; k.Value {
		case "render", "template", "templates":
			return nil, errorf(k, "%s: a query field gives its results to its entity's template, which shows them; leave %s out", k.Value, k.Value)
		}
	}
	if err := onlyKeys(n, "a query field", queryFieldKeys...); err != nil {
		return nil, err
	}
	from := yamlnode.Lookup(n, "from")
	if from != nil && from.Kind == yaml.ScalarNode && from.Value == "any" {
		return nil, errorf(from, "from: any: %s selects only %s (of: in %s)", f.Name, strings.Join(f.Target, ", "), holder.Owner(f).File)
	}
	b, named, err := parseFromTypes(n, s, "a query field")
	if err != nil {
		return nil, err
	}
	nodes := []*yaml.Node{from}
	if from.Kind == yaml.SequenceNode {
		nodes = from.Content
	}
	for i, t := range named {
		if !slices.ContainsFunc(f.Target, t.IsA) {
			return nil, errorf(nodes[i], "from: %s is not a type %s may select (of: [%s] in %s)", t.Name, f.Name, strings.Join(f.Target, ", "), holder.Owner(f).File)
		}
	}
	if where := yamlnode.Lookup(n, "where"); where != nil {
		if b.Where, err = ParseCondition(where); err != nil {
			return nil, err
		}
	}
	if b.OrderBy, err = parseOrderBy(n, b.From); err != nil {
		return nil, err
	}
	return b, nil
}

// QueryText is a query field's query as YAML, in block style, for a
// template to show what it selects (DES-0030).
func QueryText(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	c := yamlnode.Copy(n)
	var clear func(*yaml.Node)
	clear = func(n *yaml.Node) {
		if n.Kind != yaml.ScalarNode {
			n.Style = 0
		}
		n.HeadComment, n.LineComment, n.FootComment = "", "", ""
		for _, c := range n.Content {
			clear(c)
		}
	}
	clear(c)
	var b strings.Builder
	d, err := yaml.NewDumper(&b, yaml.WithV3Defaults(), yaml.WithIndent(2), yaml.WithLineWidth(-1))
	if err != nil {
		return ""
	}
	if d.Dump(c) != nil || d.Close() != nil {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// compileQueryField parses q's query and compiles it, against d, to rules
// deriving q.rel(holder, result). The query is compiled as a sub-query on
// from of the holder, so ^ reads it.
func compileQueryField(s *schema.Schema, d *derived, q *queryField, n int) error {
	b, err := ParseQueryField(s, q.e.Schema, q.v.Field, q.v.Query)
	if err != nil {
		return err
	}
	q.block = b
	from := yamlnode.Lookup(q.v.Query, "from")
	var names []string
	for _, t := range b.From {
		names = append(names, t.Name)
	}
	test := &Test{Line: q.v.Query.Line, Field: Ref{Line: from.Line}, From: names, FromLine: from.Line, Where: b.Where}
	cp, err := compileWith(s, d, []*schema.Type{q.e.Schema}, &Condition{Test: test}, fmt.Sprintf("qf%d_", n))
	if err != nil {
		return err
	}
	E, I := datalog.Var("E"), datalog.Var("I")
	q.rules = append(cp.prog.Rules, datalog.Rule{
		Head: datalog.NewAtom(q.rel, E, I),
		Body: []datalog.Literal{datalog.P(cp.witnesses[0].rel, E, I), datalog.Cmp(E, datalog.Eq, datalog.Const(datalog.String(q.e.ID)))},
	})
	return nil
}

// cycleRels finds the relations an evaluation error names as a cycle.
var cycleRels = regexp.MustCompile(`cycle: ([^)]*)\)`)

// derive evaluates every calculated and query field as one program, and
// writes their values back to the model and the facts.
func (g *Graph) derive() error {
	s := g.Schema
	var queries []*queryField
	for _, e := range g.Entities {
		for _, v := range e.Fields {
			if v.Field.Kind == schema.Query && v.Query != nil && !v.Invalid {
				queries = append(queries, &queryField{e: e, v: v, rel: "qf:" + e.ID + "." + v.Field.Name})
			}
		}
	}
	d := &derived{links: map[string]string{}, values: map[string]string{}}
	for _, tn := range s.TypeNames() {
		for _, f := range s.Types[tn].Fields {
			if f.Kind == schema.Query {
				d.links[f.Name] = "dl:" + f.Name
				if f.ReverseName != "" {
					d.links[f.ReverseName] = "dl:" + f.ReverseName
				}
			}
		}
	}
	fields, shapes := compileCalc(s, d)
	if len(fields) == 0 && len(queries) == 0 {
		return nil
	}
	for _, cf := range fields {
		d.values[cf.field.Name] = "cv:" + cf.field.Name
	}

	base := &datalog.Program{}
	for _, name := range slices.Sorted(maps.Keys(shapes)) {
		base.Add(shapes[name].nodeRules()...)
	}
	for _, cf := range fields {
		if cf.err == "" {
			base.Add(cf.rules...)
		}
	}
	N, X := datalog.Var("N"), datalog.Var("X")
	str := func(s string) datalog.Term { return datalog.Const(datalog.String(s)) }
	for _, name := range slices.Sorted(maps.Keys(d.values)) {
		rel := d.values[name]
		base.Add(datalog.Rule{Head: datalog.NewAtom(rel, N, X), Body: []datalog.Literal{datalog.P(RelField, N, str(name), X)}})
	}
	for _, cf := range fields {
		if cf.err == "" {
			base.Add(datalog.Rule{Head: datalog.NewAtom(d.values[cf.field.Name], N, X), Body: []datalog.Literal{datalog.P(cf.rel, N, X)}})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(d.links)) {
		base.Add(datalog.Rule{Head: datalog.NewAtom(d.links[name], N, X), Body: []datalog.Literal{datalog.P(RelLink, N, str(name), X)}})
	}

	for i, q := range queries {
		if err := compileQueryField(s, d, q, i); err != nil {
			g.invalidQuery(q, err)
		}
	}
	live := slices.DeleteFunc(slices.Clone(queries), func(q *queryField) bool { return q.v.Invalid })

	var out *datalog.Database
	for {
		prog := &datalog.Program{Rules: slices.Clone(base.Rules)}
		for _, q := range live {
			prog.Add(q.rules...)
			prog.Add(datalog.Rule{Head: datalog.NewAtom(d.links[q.v.Field.Name], N, X), Body: []datalog.Literal{datalog.P(q.rel, N, X)}})
			if rev := q.v.Field.ReverseName; rev != "" {
				prog.Add(datalog.Rule{Head: datalog.NewAtom(d.links[rev], X, N), Body: []datalog.Literal{datalog.P(q.rel, N, X)}})
			}
		}
		var err error
		out, err = datalog.Eval(prog, g.facts)
		if err == nil {
			break
		}
		// A loop with no answer: report the query fields in it, and
		// evaluate the rest without them.
		var inLoop []*queryField
		if m := cycleRels.FindStringSubmatch(err.Error()); m != nil {
			rels := strings.Split(m[1], ", ")
			for _, q := range live {
				if slices.Contains(rels, q.rel) {
					inLoop = append(inLoop, q)
				}
			}
		}
		if len(inLoop) == 0 {
			return fmt.Errorf("evaluating calculated and query fields: %w", err)
		}
		var names []string
		for _, q := range inLoop {
			names = append(names, q.name())
		}
		msg := fmt.Sprintf("the queries of %s depend on each other", strings.Join(names, ", "))
		if len(inLoop) == 1 {
			msg = "the query depends on its own results"
		}
		for _, q := range inLoop {
			g.invalidQuery(q, fmt.Errorf("%s through missing, not_in, not_equals or an aggregation, so it has no answer", msg))
		}
		live = slices.DeleteFunc(live, func(q *queryField) bool { return q.v.Invalid })
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
	// Results are set once every calculated value is, so order_by can sort
	// by one.
	touched := map[*model.Incoming]bool{}
	for _, q := range live {
		var results []*model.Entity
		for _, t := range out.Lookup(q.rel, datalog.String(q.e.ID)) {
			if r := g.byID[t[1].S]; r != nil {
				results = append(results, r)
			}
		}
		g.sortEntities(results, q.block.OrderBy)
		q.v.IDs, q.v.Targets = nil, nil
		name, rev := q.v.Field.Name, q.v.Field.ReverseName
		for _, r := range results {
			q.v.IDs = append(q.v.IDs, r.ID)
			q.v.Targets = append(q.v.Targets, r)
			g.facts.MustAdd(RelLink, datalog.String(q.e.ID), datalog.String(name), datalog.String(r.ID))
			if rev == "" {
				continue
			}
			g.facts.MustAdd(RelLink, datalog.String(r.ID), datalog.String(rev), datalog.String(q.e.ID))
			if in := r.Facet(rev); in != nil && !slices.Contains(in.From, q.e) {
				in.From = append(in.From, q.e)
				touched[in] = true
			}
		}
	}
	for in := range touched {
		slices.SortFunc(in.From, func(a, b *model.Entity) int { return strings.Compare(a.ID, b.ID) })
	}
	return nil
}

// invalidQuery marks q's value invalid with err, at its line in the query
// when it has one.
func (g *Graph) invalidQuery(q *queryField, err error) {
	q.v.Invalid, q.v.Problem = true, err.Error()
	var qe *Error
	if errors.As(err, &qe) {
		q.v.Problem, q.v.ProblemLine = qe.Msg, qe.Line
	}
	q.v.Raw = QueryText(q.v.Query)
}
