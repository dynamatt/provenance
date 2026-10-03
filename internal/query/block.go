package query

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
)

// Graph is the entity graph prepared for queries, built once per command.
type Graph struct {
	Schema   *schema.Schema
	Entities []*model.Entity
	byID     map[string]*model.Entity
	facts    *datalog.Database
}

// NewGraph prepares entities (sorted by ID) for queries. It evaluates every
// calculated field, filling in its model value (see Calculate).
func NewGraph(s *schema.Schema, entities []*model.Entity) (*Graph, error) {
	g := &Graph{Schema: s, Entities: entities, byID: make(map[string]*model.Entity, len(entities)), facts: Facts(entities)}
	for _, e := range entities {
		g.byID[e.ID] = e
	}
	if err := g.Calculate(); err != nil {
		return nil, err
	}
	return g, nil
}

// Entity returns the entity with the ID, or nil.
func (g *Graph) Entity(id string) *model.Entity { return g.byID[id] }

// Facts are the graph's facts: the base relations, plus anything added
// since (calculated values).
func (g *Graph) Facts() *datalog.Database { return g.facts }

// Render is how a query block shows each matching entity (High-Level Design
// §4.3a).
type Render struct {
	Mode  RenderMode
	Field string // for RenderField
}

type RenderMode string

const (
	RenderFull  RenderMode = "full"  // the entity's own template, embedded
	RenderID    RenderMode = "id"    // the bare citable ID, linked
	RenderField RenderMode = "field" // one field's current value, linked
)

// Block is a parsed query block: a fenced ```query in Markdown.
type Block struct {
	From    *schema.Type
	Where   *Condition
	OrderBy []string
	Render  Render
}

var blockKeys = []string{"from", "where", "order_by", "render"}

// ParseBlock reads a query block's YAML against the schema. Error lines
// count from the block's first line.
func ParseBlock(src string, s *schema.Schema) (*Block, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return nil, yamlError(err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, &Error{Line: 1, Msg: "a query block is a mapping with " + strings.Join(blockKeys, ", ")}
	}
	m := doc.Content[0]
	if err := onlyKeys(m, "a query block", blockKeys...); err != nil {
		return nil, err
	}

	b := &Block{Render: Render{Mode: RenderFull}}
	from := lookup(m, "from")
	if from == nil {
		return nil, errorf(m, "query block has no from: <type>")
	}
	b.From = s.Types[scalar(from)]
	if b.From == nil {
		return nil, errorf(from, "from: unknown type %q (types: %s)", from.Value, strings.Join(typeNames(s), ", "))
	}

	if where := lookup(m, "where"); where != nil {
		cond, err := ParseCondition(where, false)
		if err != nil {
			return nil, err
		}
		b.Where = cond
		// Compile now, so every schema error is reported while parsing.
		if _, _, err := Compile(s, b.From, cond, "q_"); err != nil {
			return nil, err
		}
	}

	if ob := lookup(m, "order_by"); ob != nil {
		nodes := []*yaml.Node{ob}
		if ob.Kind == yaml.SequenceNode {
			nodes = ob.Content
		}
		for _, n := range nodes {
			name := scalar(n)
			if err := checkOrderField(b.From, name); err != nil {
				return nil, errorf(n, "order_by: %v", err)
			}
			b.OrderBy = append(b.OrderBy, name)
		}
	}

	if r := lookup(m, "render"); r != nil {
		mode, field, _ := strings.Cut(scalar(r), ":")
		switch RenderMode(strings.TrimSpace(mode)) {
		case RenderFull, RenderID:
			if field != "" {
				return nil, errorf(r, "render: %s takes no field", mode)
			}
			b.Render.Mode = RenderMode(strings.TrimSpace(mode))
		case RenderField:
			field = strings.TrimSpace(field)
			if b.From.Field(field) == nil && facetOf(b.From, field) == nil {
				return nil, errorf(r, "render: %s has no field %q (fields: %s)", b.From.Name, field, fieldNames(b.From))
			}
			b.Render = Render{Mode: RenderField, Field: field}
		default:
			return nil, errorf(r, "render: unknown mode %q (expected full, id or field:<name>)", r.Value)
		}
	}
	return b, nil
}

// checkOrderField accepts fields with at most one value: sorting by a list
// or a many-link would have to pick one of its values.
func checkOrderField(t *schema.Type, name string) error {
	if name == "id" {
		return nil
	}
	f := t.Field(name)
	switch {
	case name == "":
		return fmt.Errorf("expected a field name")
	case f == nil:
		return fmt.Errorf("%s has no field %q (fields: %s)", t.Name, name, fieldNames(t))
	case f.Kind == schema.List || (f.Kind == schema.Link && f.Cardinality != "one"):
		return fmt.Errorf("%q has several values and cannot order entities", name)
	}
	return nil
}

func typeNames(s *schema.Schema) []string {
	names := make([]string, 0, len(s.Types))
	for n := range s.Types {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

func yamlError(err error) error {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "unmarshal errors:"))
	msg, _, _ = strings.Cut(msg, "\n")
	var line int
	if n, _ := fmt.Sscanf(msg, "line %d:", &line); n == 1 {
		return &Error{Line: line, Msg: strings.TrimSpace(strings.TrimPrefix(msg, fmt.Sprintf("line %d:", line)))}
	}
	return &Error{Msg: msg}
}

// Run returns the entities matching b, in order: by each order_by field
// ascending, entities without a value after those with one, then by ID.
func (g *Graph) Run(b *Block) ([]*model.Entity, error) {
	ids, err := g.Match(b.From, b.Where)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Entity, len(ids))
	for i, id := range ids {
		out[i] = g.byID[id]
	}
	keys := make(map[string][]datalog.Value, len(out))
	for _, e := range out {
		for _, name := range b.OrderBy {
			keys[e.ID] = append(keys[e.ID], g.sortValue(e.ID, name))
		}
	}
	slices.SortStableFunc(out, func(a, b *model.Entity) int {
		for i := range keys[a.ID] {
			if c := compareSortValues(keys[a.ID][i], keys[b.ID][i]); c != 0 {
				return c
			}
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// Match returns the IDs of the entities of type t satisfying cond (nil for
// all), sorted.
func (g *Graph) Match(t *schema.Type, cond *Condition) ([]string, error) {
	prog, match, err := Compile(g.Schema, t, cond, "q_")
	if err != nil {
		return nil, err
	}
	out, err := datalog.Eval(prog, g.facts)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, tuple := range out.Relation(match).Tuples() {
		ids = append(ids, tuple[0].S)
	}
	return ids, nil
}

// sortValue is an entity's single value for an order_by field, or the zero
// Value when it has none.
func (g *Graph) sortValue(id, name string) datalog.Value {
	if name == "id" {
		return datalog.String(id)
	}
	e := datalog.String(id)
	n := datalog.String(name)
	for _, rel := range []string{RelField, RelLink} {
		if ts := g.facts.Lookup(rel, e, n); len(ts) > 0 {
			return ts[0][2]
		}
	}
	return datalog.Value{}
}

// compareSortValues orders values, missing ones last; values of different
// kinds (an invalid entry beside valid ones) order by kind.
func compareSortValues(a, b datalog.Value) int {
	switch {
	case a.Kind == 0 && b.Kind == 0:
		return 0
	case a.Kind == 0:
		return 1
	case b.Kind == 0:
		return -1
	case a.Kind != b.Kind:
		return int(a.Kind) - int(b.Kind)
	}
	c, _ := datalog.CompareValues(a, b)
	return c
}
