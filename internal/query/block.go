package query

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
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

// Render is how a query block shows each matching entity (DES-0032).
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
	// From is the selected types: one, or several (REQ-0082).
	From    []*schema.Type
	Where   *Condition
	OrderBy []string
	Render  Render
	// Templates are the named presentation templates the block chooses
	// (REQ-0079): one for every result (Type ""), from
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

// ParseScope reads a --scope query file (DES-0033): from and an
// optional where, nothing about presentation.
func ParseScope(src string, s *schema.Schema) (*Block, error) {
	return parseBlock(src, s, []string{"from", "where"}, "scope query file")
}

func parseBlock(src string, s *schema.Schema, keys []string, what string) (*Block, error) {
	var doc yaml.Node
	if err := yamlnode.Unmarshal([]byte(src), 1, &doc); err != nil {
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
	from := yamlnode.Lookup(m, "from")
	if from == nil {
		return nil, errorf(m, "%s has no from: <type>, from: [<type>, …] or from: any", what)
	}
	fromNodes := []*yaml.Node{from}
	if from.Kind == yaml.ScalarNode && from.Value == "any" {
		// Every entity type (DES-0015).
		fromNodes = nil
		for _, name := range s.TypeNames() {
			b.From = append(b.From, s.Types[name])
		}
	}
	if from.Kind == yaml.SequenceNode {
		fromNodes = from.Content
		if len(fromNodes) == 0 {
			return nil, errorf(from, "from: an empty list of types")
		}
	}
	for _, n := range fromNodes {
		t := s.Types[scalar(n)]
		if t == nil {
			return nil, errorf(n, "from: unknown type %q (types: %s)", n.Value, strings.Join(s.TypeNames(), ", "))
		}
		if slices.Contains(b.From, t) {
			return nil, errorf(n, "from: %s is listed twice", t.Name)
		}
		b.From = append(b.From, t)
	}

	if where := yamlnode.Lookup(m, "where"); where != nil {
		cond, err := ParseCondition(where)
		if err != nil {
			return nil, err
		}
		b.Where = cond
		// Compile now, so every schema error is reported while parsing.
		if _, _, err := Compile(s, b.From, cond, "q_"); err != nil {
			return nil, err
		}
	}

	if ob := yamlnode.Lookup(m, "order_by"); ob != nil {
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

	if r := yamlnode.Lookup(m, "render"); r != nil {
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
	one, perType := yamlnode.Lookup(m, "template"), yamlnode.Lookup(m, "templates")
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
		return errorf(yamlnode.Lookup(m, "render"), "%s: applies only to render: full, which embeds each result through a template", key)
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

func yamlError(err error) error {
	line, msg := yamlnode.ErrorLine(err)
	return &Error{Line: line, Msg: msg}
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
	ms, err := g.Matches(types, cond)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.Entity.ID
	}
	return ids, nil
}

// Match is an entity a condition matched, with what matched it.
type Match struct {
	Entity *model.Entity
	// Items are the rows and linked entities the sub-queries among the
	// condition's top-level conditions matched (not those inside any_of),
	// in the order the sub-queries are written, then file order.
	Items []Item
}

// Item is a row or linked entity a sub-query matched, and where it is
// written: a row in the matched entity's file; a link value where the link
// is written, which for an incoming facet is the linking entity's file; an
// entity a sub-query on from found, at its id.
type Item struct {
	// Field is the sub-query's list, link or incoming facet; "" for a
	// sub-query on from.
	Field string
	// Row is the row's index from 0, for a list; -1 otherwise.
	Row int
	// ID is the linked entity, for a link or facet, or the entity a
	// sub-query on from found.
	ID   string
	Path string
	Line int
}

// Matches returns the entities of the given types satisfying cond (nil for
// all), sorted by ID, each with the items that matched it.
func (g *Graph) Matches(types []*schema.Type, cond *Condition) ([]Match, error) {
	cp, err := compile(g.Schema, types, cond, "q_")
	if err != nil {
		return nil, err
	}
	out, err := datalog.Eval(cp.prog, g.facts)
	if err != nil {
		return nil, err
	}
	var ms []Match
	index := map[string]int{}
	for _, tuple := range out.Relation(cp.match).Tuples() {
		index[tuple[0].S] = len(ms)
		ms = append(ms, Match{Entity: g.byID[tuple[0].S]})
	}
	for _, w := range cp.witnesses {
		found := map[int][]Item{}
		for _, tuple := range out.Relation(w.rel).Tuples() {
			i := index[tuple[0].S]
			found[i] = append(found[i], g.item(ms[i].Entity, w.test, tuple[1]))
		}
		for i, items := range found {
			slices.SortFunc(items, func(a, b Item) int {
				return cmp.Or(strings.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Row, b.Row), strings.Compare(a.ID, b.ID))
			})
			ms[i].Items = append(ms[i].Items, items...)
		}
	}
	return ms, nil
}

// item describes v, a row node, linked ID or other entity's ID that
// sub-query t matched on e.
func (g *Graph) item(e *model.Entity, t *Test, v datalog.Value) Item {
	field := t.Field.Field
	it := Item{Field: field, Row: -1, Path: e.Path}
	if t.From != nil {
		// Another entity, found by its type: placed at its own id.
		it.ID = v.S
		if other := g.byID[v.S]; other != nil {
			it.Path, it.Line = other.Path, other.IDLine()
		}
		return it
	}
	if v.Kind == datalog.Node {
		// RowNode: id/list/index; the list name and index have no slash.
		it.Row, _ = strconv.Atoi(v.S[strings.LastIndex(v.S, "/")+1:])
		if list := yamlnode.Lookup(e.Front, field); list != nil && it.Row < len(list.Content) {
			it.Line = list.Content[it.Row].Line
		}
		return it
	}
	it.ID = v.S
	if f := e.Schema.Field(field); f != nil {
		it.Line = valueLine(yamlnode.Lookup(e.Front, field), it.ID)
		return it
	}
	// An incoming facet: the link is written on the linking entity, as a
	// field of its own or of one of its list rows.
	src := g.byID[it.ID]
	facet := facetOf(e.Schema, field)
	if src == nil || facet == nil {
		return it
	}
	it.Path = src.Path
	for _, fs := range facet.Sources {
		if fs.Type != src.Schema {
			continue
		}
		if fs.List == nil {
			if line := valueLine(yamlnode.Lookup(src.Front, fs.Field.Name), e.ID); line > 0 {
				it.Line = line
				return it
			}
			continue
		}
		if list := yamlnode.Lookup(src.Front, fs.List.Name); list != nil && list.Kind == yaml.SequenceNode {
			for _, row := range list.Content {
				if line := valueLine(yamlnode.Lookup(row, fs.Field.Name), e.ID); line > 0 {
					it.Line = line
					return it
				}
			}
		}
	}
	return it
}

// valueLine is the line of n, a link field's value, where it names id: the
// scalar, or the sequence item. 0 when it doesn't.
func valueLine(n *yaml.Node, id string) int {
	switch {
	case n == nil:
	case n.Kind == yaml.ScalarNode && n.Value == id:
		return n.Line
	case n.Kind == yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind == yaml.ScalarNode && item.Value == id {
				return item.Line
			}
		}
	}
	return 0
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
