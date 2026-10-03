package website

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// Template data model (Detailed Design §7): what a project template sees as
// "." for an entity. It is a map so that every declared field is present —
// nil when the entity does not set it — and a misspelt field is an error
// rather than silently empty output.
//
//	.ID .Type .Title .Body .Resolved    engine baseline
//	.<PascalCaseField>                  every declared field, calculated ones included
//	.<PascalCaseFacet>                  every incoming facet, e.g. .ImplementedBy
//
// Values: text-like fields are strings, numbers float64, booleans bool; a
// link with cardinality one is another entity's map (or nil), with
// cardinality many a list of them; a list field is a list of row maps keyed
// by PascalCase sub-field. An unresolved link target is a map with Resolved
// false and every field its declared target types have set to nil, so a
// template written for resolved targets still renders.
type entityData = map[string]any

// baseline keys the engine provides on every entity map.
var baseline = []string{"ID", "Type", "Title", "Body", "Resolved"}

// templateName converts a snake_case field name to its template accessor:
// verified_by -> VerifiedBy.
func templateName(field string) string {
	var b strings.Builder
	for _, part := range strings.Split(field, "_") {
		if part == "" {
			continue
		}
		r, size := utf8.DecodeRuneInString(part)
		b.WriteRune(unicode.ToUpper(r))
		b.WriteString(part[size:])
	}
	return b.String()
}

// checkTemplateNames fails when two of a type's fields or facets map to the
// same accessor, or one hides a baseline accessor ("title" is allowed: the
// field is the title).
func checkTemplateNames(s *schema.Schema) error {
	names := make([]string, 0, len(s.Types))
	for n := range s.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := s.Types[n]
		seen := map[string]string{}
		for _, b := range baseline {
			seen[b] = "the engine baseline"
		}
		delete(seen, "Title")
		check := func(name, what string, line int) error {
			key := templateName(name)
			if prev, dup := seen[key]; dup {
				return fmt.Errorf("%s:%d: %s %q: its template name .%s is already used by %s", t.File, line, what, name, key, prev)
			}
			seen[key] = fmt.Sprintf("%s %q", what, name)
			return nil
		}
		for _, f := range t.Fields {
			if err := check(f.Name, "field", f.Line); err != nil {
				return err
			}
		}
		for _, f := range t.Facets {
			if err := check(f.Name, "incoming facet", f.Sources[0].Field.Line); err != nil {
				return err
			}
		}
	}
	return nil
}

// dataModel holds every entity's template data, built once per export.
type dataModel struct {
	schema *schema.Schema
	byID   map[string]entityData
}

func buildData(s *schema.Schema, entities []*model.Entity) *dataModel {
	d := &dataModel{schema: s, byID: make(map[string]entityData, len(entities))}
	// Two passes: links point at other entities' maps.
	for _, e := range entities {
		d.byID[e.ID] = entityData{}
	}
	for _, e := range entities {
		m := d.byID[e.ID]
		m["ID"], m["Type"], m["Title"], m["Resolved"] = e.ID, e.Type, e.Title(), true
		m["Body"] = e.Body
		for _, v := range e.Fields {
			m[templateName(v.Field.Name)] = d.value(v)
			if v.Field == e.Schema.BodyField {
				m["Body"] = v.Str
			}
		}
		for _, in := range e.Incoming {
			refs := make([]entityData, len(in.From))
			for i, from := range in.From {
				refs[i] = d.byID[from.ID]
			}
			m[templateName(in.Facet.Name)] = refs
		}
	}
	return d
}

func (d *dataModel) value(v *model.Value) any {
	f := v.Field
	switch {
	case f.Kind == schema.Calculated:
		return nil // evaluated from E1.9
	case f.Kind == schema.Link && f.Cardinality == "many":
		refs := []entityData{}
		if v.Present && !v.Invalid {
			for i, id := range v.IDs {
				refs = append(refs, d.ref(id, v.Targets[i], f))
			}
		}
		return refs
	case f.Kind == schema.List:
		rows := []entityData{}
		if v.Present && !v.Invalid {
			for _, row := range v.Rows {
				r := entityData{}
				for _, cell := range row {
					r[templateName(cell.Field.Name)] = d.value(cell)
				}
				rows = append(rows, r)
			}
		}
		return rows
	case !v.Present:
		return nil
	case v.Invalid:
		return v.Raw
	}
	switch f.Kind {
	case schema.Number:
		return v.Num
	case schema.Boolean:
		return v.Bool
	case schema.Link:
		return d.ref(v.IDs[0], v.Targets[0], f)
	default:
		return v.Str
	}
}

func (d *dataModel) ref(id string, target *model.Entity, f *schema.Field) entityData {
	if target != nil {
		return d.byID[target.ID]
	}
	m := entityData{"ID": id, "Type": "", "Title": "", "Body": "", "Resolved": false}
	for _, tn := range f.Target {
		t := d.schema.Types[tn]
		if t == nil {
			continue
		}
		for _, tf := range t.Fields {
			m[templateName(tf.Name)] = nil
		}
		for _, facet := range t.Facets {
			m[templateName(facet.Name)] = []entityData{}
		}
	}
	return m
}
