package query

import (
	"fmt"
	"regexp"
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
	// From is the selected types: one, or several (Requirements Spec §4a).
	From    []*schema.Type
	Where   *Condition
	OrderBy []string
	Render  Render
	// Templates are the named presentation templates the block chooses
	// (Requirements Spec §7): one for every result (Type ""), from
	// template:, or one per type, from templates:.
	Templates []TemplateChoice
}

// TemplateChoice is a named template chosen for a block's results of Type,
// or all of them when Type is "". Line is where the name is written.
type TemplateChoice struct {
	Type, Name string
	Line       int
}

// TemplateFor returns the named template chosen for results of type typ,
// or "" when they use their type's default.
func (b *Block) TemplateFor(typ string) TemplateChoice {
	for _, c := range b.Templates {
		if c.Type == typ || c.Type == "" {
			return c
		}
	}
	return TemplateChoice{}
}

// templateName is the form of a named template's name, and of its file
// templates/<name>.tmpl: lower-case kebab-case, so it cannot be mistaken
// for a CamelCase type template or an _-prefixed site override.
var templateName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// IsTemplateName reports whether name is a valid named template name.
func IsTemplateName(name string) bool { return templateName.MatchString(name) }

var blockKeys = []string{"from", "where", "order_by", "render", "template", "templates"}

// ParseBlock reads a query block's YAML against the schema. Error lines
// count from the block's first line.
func ParseBlock(src string, s *schema.Schema) (*Block, error) {
	return parseBlock(src, s, blockKeys, "query block")
}

// ParseScope reads a --scope query file (Detailed Design §2): from and an
// optional where, nothing about presentation.
func ParseScope(src string, s *schema.Schema) (*Block, error) {
	return parseBlock(src, s, []string{"from", "where"}, "scope query file")
}

func parseBlock(src string, s *schema.Schema, keys []string, what string) (*Block, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return nil, yamlError(err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, &Error{Line: 1, Msg: "a " + what + " is a mapping with " + strings.Join(keys, ", ")}
	}
	m := doc.Content[0]
	if err := onlyKeys(m, "a "+what, keys...); err != nil {
		return nil, err
	}

	b := &Block{Render: Render{Mode: RenderFull}}
	from := lookup(m, "from")
	if from == nil {
		return nil, errorf(m, "%s has no from: <type> or from: [<type>, …]", what)
	}
	fromNodes := []*yaml.Node{from}
	if from.Kind == yaml.SequenceNode {
		fromNodes = from.Content
		if len(fromNodes) == 0 {
			return nil, errorf(from, "from: an empty list of types")
		}
	}
	for _, n := range fromNodes {
		t := s.Types[scalar(n)]
		if t == nil {
			return nil, errorf(n, "from: unknown type %q (types: %s)", n.Value, strings.Join(typeNames(s), ", "))
		}
		if slices.Contains(b.From, t) {
			return nil, errorf(n, "from: %s is listed twice", t.Name)
		}
		b.From = append(b.From, t)
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
			if !slices.ContainsFunc(b.From, func(t *schema.Type) bool { return t.Field(field) != nil || facetOf(t, field) != nil }) {
				return nil, errorf(r, "render: %s", noField(b.From, field))
			}
			b.Render = Render{Mode: RenderField, Field: field}
		default:
			return nil, errorf(r, "render: unknown mode %q (expected full, id or field:<name>)", r.Value)
		}
	}
	if err := b.parseTemplates(m); err != nil {
		return nil, err
	}
	return b, nil
}

// parseTemplates reads template: or templates:.
func (b *Block) parseTemplates(m *yaml.Node) error {
	one, perType := lookup(m, "template"), lookup(m, "templates")
	switch {
	case one == nil && perType == nil:
		return nil
	case one != nil && perType != nil:
		return errorf(perType, "use template: (every result) or templates: (per type), not both")
	}
	key := "template"
	if perType != nil {
		key = "templates"
	}
	if b.Render.Mode != RenderFull {
		return errorf(lookup(m, "render"), "%s: applies only to render: full, which embeds each result through a template", key)
	}
	name := func(n *yaml.Node) (string, error) {
		v := scalar(n)
		if n.Kind != yaml.ScalarNode || !IsTemplateName(v) {
			return "", errorf(n, "%s: %q is not a template name: named templates are lower-case kebab-case, as in templates/requirement-checklist.tmpl", key, v)
		}
		return v, nil
	}
	if one != nil {
		v, err := name(one)
		if err != nil {
			return err
		}
		b.Templates = []TemplateChoice{{Name: v, Line: one.Line}}
		return nil
	}
	if perType.Kind != yaml.MappingNode || len(perType.Content) == 0 {
		return errorf(perType, "templates: expected a mapping of type to template name, e.g. {Requirement: requirement-checklist}")
	}
	var selected []string
	for _, t := range b.From {
		selected = append(selected, t.Name)
	}
	for i := 0; i+1 < len(perType.Content); i += 2 {
		k, v := perType.Content[i], perType.Content[i+1]
		if !slices.Contains(selected, k.Value) {
			return errorf(k, "templates: %s is not selected by from (%s)", k.Value, strings.Join(selected, ", "))
		}
		n, err := name(v)
		if err != nil {
			return err
		}
		b.Templates = append(b.Templates, TemplateChoice{Type: k.Value, Name: n, Line: v.Line})
	}
	return nil
}

// checkOrderField accepts a field that at least one selected type has, with
// at most one value wherever it is declared: sorting by a list or a
// many-link would have to pick one of its values.
func checkOrderField(types []*schema.Type, name string) error {
	if name == "id" {
		return nil
	}
	if name == "" {
		return fmt.Errorf("expected a field name")
	}
	found := false
	for _, t := range types {
		f := t.Field(name)
		if f == nil {
			continue
		}
		found = true
		if f.Kind == schema.List || (f.Kind == schema.Link && f.Cardinality != "one") {
			return fmt.Errorf("%q has several values and cannot order entities", name)
		}
	}
	if !found {
		return fmt.Errorf("%s", noField(types, name))
	}
	return nil
}

// noField says that no selected type has the field, listing the fields of a
// single type.
func noField(types []*schema.Type, name string) string {
	if len(types) == 1 {
		return fmt.Sprintf("%s has no field %q (fields: %s)", types[0].Name, name, fieldNames(types[0]))
	}
	return fmt.Sprintf("%s has a field %q", noneOf(types), name)
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

// Match returns the IDs of the entities of the given types satisfying cond
// (nil for all), sorted.
func (g *Graph) Match(types []*schema.Type, cond *Condition) ([]string, error) {
	prog, match, err := Compile(g.Schema, types, cond, "q_")
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
