package query

import (
	"slices"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// FormulaInputs are the other entities e's calculated fields read: every
// entity a formula reaches through a link, an incoming facet or a list row
// (RSK-0001's ratings read its severity and occurrence levels), and what
// their own calculated fields read in turn. Sorted, without e.
func (g *Graph) FormulaInputs(e *model.Entity) []string {
	seen := map[string]bool{e.ID: true}
	var out []string
	var visit func(e *model.Entity)
	add := func(t *model.Entity) {
		if t == nil || seen[t.ID] {
			return
		}
		seen[t.ID] = true
		out = append(out, t.ID)
		visit(t) // a calculated field read on t reads t's own inputs
	}
	visit = func(e *model.Entity) {
		for _, v := range e.Fields {
			if v.Field.Kind == schema.Calculated {
				g.followFormula(v.Field.Formula, []fnode{{e: e}}, add)
			}
			if v.Field.Kind == schema.List && v.Field.Elem == nil && v.Present && !v.Invalid {
				for _, row := range v.Rows {
					for _, cell := range row {
						if cell.Field.Kind == schema.Calculated {
							g.followFormula(cell.Field.Formula, []fnode{{e: e, row: row}}, add)
						}
					}
				}
			}
		}
	}
	visit(e)
	slices.Sort(out)
	return out
}

// fnode is where a formula reference stands: an entity, or one of its list
// rows.
type fnode struct {
	e   *model.Entity
	row model.Row
}

// followFormula walks every reference in formula from start, calling add for
// each entity reached. A formula that does not parse reads nothing.
func (g *Graph) followFormula(formula string, start []fnode, add func(*model.Entity)) {
	expr, err := ParseFormula(formula)
	if err != nil {
		return
	}
	var refs []*RefExpr
	var walk func(Expr)
	walk = func(x Expr) {
		switch x := x.(type) {
		case *RefExpr:
			refs = append(refs, x)
		case *Binary:
			walk(x.L)
			walk(x.R)
		case *Negate:
			walk(x.X)
		case *Call:
			for _, a := range x.Args {
				walk(a)
			}
		}
	}
	walk(expr)
	for _, ref := range refs {
		nodes := start
		for _, st := range ref.Steps {
			var next []fnode
			for _, n := range nodes {
				v := n.value(st.Name)
				switch {
				case v != nil && v.Field.Kind == schema.Link && !v.Invalid:
					for _, t := range v.Targets {
						if t != nil {
							add(t)
							next = append(next, fnode{e: t})
						}
					}
				case v != nil && v.Field.Kind == schema.List && v.Field.Elem == nil && !v.Invalid:
					for _, row := range v.Rows {
						next = append(next, fnode{e: n.e, row: row})
					}
				case v == nil && n.row == nil:
					if in := n.e.Facet(st.Name); in != nil {
						for _, from := range in.From {
							add(from)
							next = append(next, fnode{e: from})
						}
					}
				}
			}
			nodes = next
		}
	}
}

func (n fnode) value(name string) *model.Value {
	if n.row != nil {
		for _, cell := range n.row {
			if cell.Field.Name == name {
				return cell
			}
		}
		return nil
	}
	return n.e.Field(name)
}

// viaTypes are the types a condition's sub-queries reach through links,
// incoming facets and from, at any depth: a change to any entity of them
// can change which entities match.
func viaTypes(s *schema.Schema, types []*schema.Type, cond *Condition) []string {
	var out []string
	// walk visits cond, about an entity of one of types or a row of list.
	var walk func(c *Condition, types []*schema.Type, list *schema.Field)
	walk = func(c *Condition, types []*schema.Type, list *schema.Field) {
		if c == nil {
			return
		}
		for _, sub := range append(slices.Clone(c.AllOf), c.AnyOf...) {
			walk(sub, types, list)
		}
		if c.Test == nil || (c.Test.Where == nil && c.Test.Aggregation == nil && c.Test.From == nil) {
			return
		}
		if c.Test.From != nil {
			reached, _ := FromTypes(s, c.Test.From, 0)
			for _, t := range reached {
				out = append(out, t.Name)
			}
			walk(c.Test.Where, reached, nil)
			return
		}
		name := c.Test.Field.Field
		scopes := []*scope{{list: list}}
		if list == nil {
			scopes = nil
			for _, t := range types {
				scopes = append(scopes, &scope{t: t})
			}
		}
		for _, sc := range scopes {
			var reached []*schema.Type
			switch f := sc.field(name); {
			case f != nil && f.Kind == schema.List && f.Elem == nil:
				walk(c.Test.Where, nil, f)
				continue
			case f != nil && f.Kind == schema.Link:
				for _, tn := range f.Target {
					if t := s.Types[tn]; t != nil {
						reached = append(reached, t)
					}
				}
				out = append(out, f.Target...)
			case sc.facet(name) != nil:
				for _, src := range sc.facet(name).Sources {
					reached = append(reached, src.Type)
					out = append(out, src.Type.Name)
				}
			}
			walk(c.Test.Where, reached, nil)
		}
	}
	walk(cond, types, nil)
	slices.Sort(out)
	return slices.Compact(out)
}
