package website

import (
	"fmt"
	"maps"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// Template data model (DES-0030): what a project template sees as
// "." for an entity. It is a map so that every declared field is present —
// nil when the entity does not set it — and a misspelt field is an error
// rather than silently empty output.
//
//	.ID .Type .Title .Body .Resolved    engine baseline
//	.LastChangedSHA .Revisions          git stamps (DES-0023)
//	.Citations                          on the entity a page is about, what the
//	                                    page cites (DES-0046); empty elsewhere
//	.<PascalCaseField>                  every declared field, calculated ones included
//	.<PascalCaseFacet>                  every incoming facet, e.g. .ImplementedBy
//
// Values: text-like fields are strings, numbers float64, booleans bool; a
// calculated field is its formula's value (a number, boolean or string), nil
// when blank or when the formula cannot be evaluated; a
// link with cardinality one is another entity's map (or nil), with
// cardinality many a list of them; a list field is a list of row maps keyed
// by PascalCase sub-field, or, when it names an item type with of:, a list of
// item values. An unresolved link target is a map with Resolved
// false and every field its declared target types have set to nil, so a
// template written for resolved targets still renders.
type entityData = map[string]any

// baseline is an entity map holding only the keys the engine provides on
// every entity, empty: the start of every entity's data, and all of an
// unresolved link target's but its declared fields.
func baseline(id string) entityData {
	return entityData{"ID": id, "Type": "", "Title": "", "Body": "", "Resolved": false,
		"LastChangedSHA": "", "Revisions": []entityData{}, "Citations": []entityData{}}
}

// citationKeys are added to each entity in a page's .Citations, and
// CitationLabel to the entity _cite.tmpl renders.
var citationKeys = []string{"CitationIndex", "TypeCitationIndex", "CitationLabel"}

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
	for _, n := range s.TypeNames() {
		t := s.Types[n]
		seen := map[string]string{}
		for b := range baseline("") {
			seen[b] = "the engine baseline"
		}
		for _, b := range citationKeys {
			seen[b] = "the engine's citations"
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
		d.byID[e.ID] = baseline(e.ID)
	}
	for _, e := range entities {
		m := d.byID[e.ID]
		m["Type"], m["Title"], m["Body"], m["Resolved"] = e.Type, e.Title(), e.Body, true
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
		if !v.Present || v.Invalid {
			return nil // blank, or a formula that cannot be evaluated
		}
		switch v.Result {
		case schema.Number:
			return v.Num
		case schema.Boolean:
			return v.Bool
		}
		return v.Str
	case f.Kind == schema.Link && f.Cardinality == "many":
		refs := []entityData{}
		if v.Present && !v.Invalid {
			for i, id := range v.IDs {
				refs = append(refs, d.ref(id, v.Targets[i], f))
			}
		}
		return refs
	case f.Kind == schema.List && f.Elem != nil:
		items := []any{}
		if v.Present && !v.Invalid {
			for _, item := range v.Items {
				items = append(items, d.value(item))
			}
		}
		return items
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
	m := baseline(id)
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

// citations is the template data of a page's citations (DES-0046), in order
// of first citation: each cited entity's map with .CitationIndex, its 1-based
// position among them all, and .TypeCitationIndex, its position among those
// of its type. A missing entity is a map with Resolved false, like an
// unresolved link target.
func (d *dataModel) citations(ids []string) []entityData {
	out := make([]entityData, len(ids))
	perType := map[string]int{}
	for i, id := range ids {
		var m entityData
		if e, ok := d.byID[id]; ok {
			m = maps.Clone(e)
		} else {
			m = d.ref(id, nil, &schema.Field{})
		}
		t, _ := m["Type"].(string)
		perType[t]++
		m["CitationIndex"], m["TypeCitationIndex"] = i+1, perType[t]
		out[i] = m
	}
	return out
}
