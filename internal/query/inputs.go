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

// viaTypes are the types a condition reads across links ({via: L, field: F}):
// a change to any entity of them can change which entities match.
func viaTypes(types []*schema.Type, cond *Condition) []string {
	var out []string
	var walk func(*Condition)
	ref := func(r *Ref) {
		if r == nil || r.Via == "" {
			return
		}
		for _, t := range types {
			if f := t.Field(r.Via); f != nil && f.Kind == schema.Link {
				out = append(out, f.Target...)
			}
			if facet := facetOf(t, r.Via); facet != nil {
				for _, src := range facet.Sources {
					out = append(out, src.Type.Name)
				}
			}
		}
	}
	walk = func(c *Condition) {
		if c == nil {
			return
		}
		if c.Test != nil {
			ref(&c.Test.Field)
			if c.Test.Value != nil {
				ref(c.Test.Value.Field)
			}
		}
		for _, sub := range append(slices.Clone(c.AllOf), c.AnyOf...) {
			walk(sub)
		}
	}
	walk(cond)
	slices.Sort(out)
	return slices.Compact(out)
}
