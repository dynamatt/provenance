package validate

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// The schema conformance checks (DES-0048): the schema's own declarations,
// then every entity against its type.

// declared is a field declaration with the file it is written in.
type declared struct {
	path string
	f    *schema.Field
}

// declaredFields lists every field declaration once, in a stable order:
// each type's fields and the sub-fields of its inline lists, in its file,
// then each record's fields, in the record's file.
func declaredFields(s *schema.Schema) []declared {
	var out []declared
	for _, tn := range s.TypeNames() {
		t := s.Types[tn]
		for _, f := range t.Fields {
			out = append(out, declared{t.File, f})
			if f.Kind == schema.List && f.Record == "" {
				for _, sub := range f.Fields {
					out = append(out, declared{t.File, sub})
				}
			}
		}
	}
	for _, rn := range slices.Sorted(maps.Keys(s.Records)) {
		r := s.Records[rn]
		for _, f := range r.Fields {
			out = append(out, declared{r.File, f})
		}
	}
	return out
}

// schemaChecks reports problems in the schema's declarations that do not
// stop it loading, and declarations nothing uses.
func (r *Report) schemaChecks(s *schema.Schema, entities []*entity.Entity) {
	fields := declaredFields(s)
	enumsUsed, recordsUsed, targeted := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range fields {
		f := d.f
		enumsUsed[f.EnumName] = true
		if f.Elem != nil {
			enumsUsed[f.Elem.EnumName] = true
		}
		recordsUsed[f.Record] = true
		if f.Kind != schema.Link {
			continue
		}
		for _, tn := range f.Target {
			targeted[tn] = true
			if s.Types[tn] == nil {
				r.add(Violation{Rule: "schema/link-target", Severity: Error, Path: d.path, Line: f.Line,
					Message: fmt.Sprintf("field %q: target %q is not a declared type (declared: %s)", f.Name, tn, strings.Join(s.TypeNames(), ", "))})
			}
		}
	}

	for _, p := range query.FormulaProblems(s) {
		path := p.Type.File
		if p.List != nil && p.List.Record != "" {
			path = s.Records[p.List.Record].File
		}
		r.add(Violation{Rule: "schema/formula", Severity: Error, Path: path, Line: p.Field.Line,
			Message: fmt.Sprintf("field %q: %s", p.Field.Name, p.Msg)})
	}

	prefixes := map[string][]*schema.Type{}
	for _, tn := range s.TypeNames() {
		t := s.Types[tn]
		if t.IDPrefix == "" {
			r.add(Violation{Rule: "schema/id-format", Severity: Error, Path: t.File, Line: t.Line,
				Message: fmt.Sprintf("type %s has no id_prefix, so its IDs cannot be checked", t.Name)})
			continue
		}
		prefixes[t.IDPrefix] = append(prefixes[t.IDPrefix], t)
	}
	for _, p := range slices.Sorted(maps.Keys(prefixes)) {
		types := prefixes[p]
		for _, t := range types {
			for _, o := range types {
				if o != t {
					r.add(Violation{Rule: "schema/id-format", Severity: Error, Path: t.File, Line: t.Line,
						Message: fmt.Sprintf("type %s has the id_prefix %s, which %s (%s) also has", t.Name, p, o.Name, o.File)})
				}
			}
		}
	}

	hasEntities := map[string]bool{}
	for _, e := range entities {
		hasEntities[e.Type] = true
	}
	for _, en := range slices.Sorted(maps.Keys(s.Enums)) {
		if e := s.Enums[en]; !enumsUsed[en] {
			r.add(Violation{Rule: "schema/unused-enum", Severity: Warning, Path: e.File, Line: e.Line,
				Message: fmt.Sprintf("enum %s is not used by any field", en)})
		}
	}
	for _, rn := range slices.Sorted(maps.Keys(s.Records)) {
		if rec := s.Records[rn]; !recordsUsed[rn] {
			r.add(Violation{Rule: "schema/unused-record", Severity: Warning, Path: rec.File, Line: rec.Line,
				Message: fmt.Sprintf("record %s is not used by any list", rn)})
		}
	}
	// A type in a chain of links is part of the design even before it has
	// entities: the last artefact in a chain links out and is never a target.
	for _, tn := range s.TypeNames() {
		if t := s.Types[tn]; !hasEntities[tn] && !targeted[tn] && !linksOut(t) {
			r.add(Violation{Rule: "schema/unused-type", Severity: Warning, Path: t.File, Line: t.Line,
				Message: fmt.Sprintf("type %s has no entities and no links to or from it", tn)})
		}
	}
}

// linksOut reports whether t has a link field, on itself or its list rows.
func linksOut(t *schema.Type) bool {
	for _, f := range t.Fields {
		if f.Kind == schema.Link {
			return true
		}
		for _, sub := range f.Fields {
			if sub.Kind == schema.Link {
				return true
			}
		}
	}
	return false
}

// conformance checks every typed entity against its type. Their calculated
// values were evaluated when they were loaded, so a value a formula cannot
// give is reported; one whose formula is broken is reported once, on the
// schema.
func (r *Report) conformance(s *schema.Schema, typed []*model.Entity) {
	broken := map[*schema.Field]bool{}
	for _, p := range query.FormulaProblems(s) {
		broken[p.Field] = true
	}
	for _, e := range typed {
		c := &entityCheck{r: r, s: s, e: e, broken: broken}
		c.id()
		c.keys()
		for _, v := range e.Fields {
			c.value(v)
		}
	}
}

type entityCheck struct {
	r      *Report
	s      *schema.Schema
	e      *model.Entity
	broken map[*schema.Field]bool
}

func (c *entityCheck) add(rule string, line int, format string, args ...any) {
	c.r.add(Violation{Rule: rule, Severity: Error, Entity: c.e.ID, Path: c.e.Path, Line: line, Message: fmt.Sprintf(format, args...)})
}

// id checks the ID is the type's prefix and a number of at least four
// digits (REQ-0013, DES-0007).
func (c *entityCheck) id() {
	p := c.e.Schema.IDPrefix
	if p == "" {
		return // reported on the schema
	}
	if !regexp.MustCompile(`^` + regexp.QuoteMeta(p) + `-[0-9]{4,}$`).MatchString(c.e.ID) {
		c.add("schema/id-format", c.e.IDLine(), "ID %s is not %s-NNNN, the form of a %s ID", c.e.ID, p, c.e.Schema.Name)
	}
}

// keys checks every frontmatter key names a field the file may set.
func (c *entityCheck) keys() {
	t := c.e.Schema
	front := c.e.Front
	for i := 0; i+1 < len(front.Content); i += 2 {
		k := front.Content[i]
		name := k.Value
		if name == "id" || name == "type" {
			continue
		}
		f := t.Field(name)
		switch {
		case f == nil:
			c.add("schema/unknown-field", k.Line, "%s", c.unknownField(name))
		case f.Kind == schema.Calculated:
			c.add("schema/calculated-set", k.Line, "%s is calculated (%s); it cannot be written in the file", name, f.Formula)
		case f == t.BodyField:
			c.add("schema/unknown-field", k.Line, "%s is the Markdown body below the frontmatter, not a frontmatter field", name)
		}
	}
}

func (c *entityCheck) unknownField(name string) string {
	t := c.e.Schema
	for _, fc := range t.Facets {
		if fc.Name != name {
			continue
		}
		var from []string
		for _, src := range fc.Sources {
			from = append(from, src.Type.Name+"."+src.Field.Name)
		}
		return fmt.Sprintf("%s is derived from %s, set on the linking entity; a %s does not store it", name, strings.Join(from, ", "), t.Name)
	}
	var names []string
	for _, f := range t.Fields {
		if f != t.BodyField && f.Kind != schema.Calculated {
			names = append(names, f.Name)
		}
	}
	return fmt.Sprintf("%s has no field %q (fields: %s)", t.Name, name, strings.Join(names, ", "))
}

// value checks one field's value: required, of its type, in its enum, and
// for a list each row or item.
func (c *entityCheck) value(v *model.Value) {
	f := v.Field
	n := yamlnode.Lookup(c.e.Front, f.Name)
	switch {
	case f.Kind == schema.Calculated:
		if v.Invalid && !c.broken[f] {
			c.add("schema/invalid-value", c.e.IDLine(), "%s: %s", f.Name, v.Problem)
		}
		return
	case f.Required && empty(v):
		c.add("schema/required", c.e.IDLine(), "required field %s is not set", f.Name)
		return
	case !v.Present || n == nil:
		return
	}
	c.check(f.Name, n, v)
	if f.Kind != schema.List || v.Invalid {
		return
	}
	for i, item := range v.Items {
		c.check(fmt.Sprintf("%s item %d", f.Name, i+1), n.Content[i], item)
	}
	for i, row := range v.Rows {
		c.row(fmt.Sprintf("%s row %d", f.Name, i+1), f, n.Content[i], row)
	}
}

// check reports a present value that is not of its type, not in its enum,
// or a link to no entity or to one of a type its field doesn't allow.
func (c *entityCheck) check(label string, n *yaml.Node, v *model.Value) {
	switch {
	case v.Invalid:
		c.add("schema/invalid-value", n.Line, "%s: %s, got %s", label, v.Problem, v.Raw)
	case v.Field.Kind == schema.Enum:
		if e := c.s.Enums[v.Field.EnumName]; e != nil && !slices.Contains(e.Values, v.Str) {
			c.add("schema/enum-value", n.Line, "%s: %q is not one of %s: %s", label, v.Str, e.Name, strings.Join(e.Values, ", "))
		}
	case v.Field.Kind == schema.Link:
		for i, id := range v.IDs {
			line := n.Line
			if n.Kind == yaml.SequenceNode {
				line = n.Content[i].Line
			}
			switch t := v.Targets[i]; {
			case t == nil:
				c.add("links/unresolved", line, "%s: no entity %s", label, id)
			case len(v.Field.Target) > 0 && !slices.Contains(v.Field.Target, t.Type):
				c.add("links/target-type", line, "%s: %s is of type %s, but %s links only to %s", label, id, t.Type, v.Field.Name, strings.Join(v.Field.Target, ", "))
			}
		}
	}
}

// row checks one list row: its keys, and each sub-field's value.
func (c *entityCheck) row(label string, list *schema.Field, n *yaml.Node, row model.Row) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		var sub *schema.Field
		for _, f := range list.Fields {
			if f.Name == k.Value {
				sub = f
			}
		}
		switch {
		case sub == nil:
			var names []string
			for _, f := range list.Fields {
				if f.Kind != schema.Calculated {
					names = append(names, f.Name)
				}
			}
			c.add("schema/list-row", k.Line, "%s: no sub-field %q (sub-fields: %s)", label, k.Value, strings.Join(names, ", "))
		case sub.Kind == schema.Calculated:
			c.add("schema/calculated-set", k.Line, "%s: %s is calculated (%s); it cannot be written in the file", label, sub.Name, sub.Formula)
		}
	}
	for _, cell := range row {
		sub := cell.Field
		cn := yamlnode.Lookup(n, sub.Name)
		switch {
		case sub.Kind == schema.Calculated:
			if cell.Invalid && !c.broken[sub] {
				c.add("schema/invalid-value", n.Line, "%s: %s: %s", label, sub.Name, cell.Problem)
			}
		case sub.Required && empty(cell):
			c.add("schema/required", n.Line, "%s: required sub-field %s is not set", label, sub.Name)
		case cell.Present && cn != nil:
			c.check(label+": "+sub.Name, cn, cell)
		}
	}
}

// empty reports a value that does not count as set: absent, null, blank
// text, or a list or many-link with nothing in it.
func empty(v *model.Value) bool {
	if !v.Present {
		return true
	}
	if v.Invalid {
		return false // set, wrongly: reported as invalid
	}
	switch v.Field.Kind {
	case schema.String, schema.Text, schema.Enum:
		return strings.TrimSpace(v.Str) == ""
	case schema.Link:
		return len(v.IDs) == 0
	case schema.List:
		return len(v.Rows) == 0 && len(v.Items) == 0
	}
	return false
}
