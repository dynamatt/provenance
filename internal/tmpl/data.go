package tmpl

import (
	"maps"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// Template data model (DES-0030): what a project template sees as
// "." for an entity. It is a map so that every declared field is present —
// nil when the entity does not set it — and a misspelt field is an error
// rather than silently empty output.
//
//	.ID .Type .Body .Resolved           engine baseline; an entity's title is
//	                                    its title: true field's, which the
//	                                    title function reads (DES-0010)
//	.LastChangedSHA .Revisions          git stamps (DES-0023)
//	.File                               the file the entity is written in:
//	                                    .Path, and .Line, where its ID is
//	                                    written; a CSV file's row too (DES-0053)
//	.Citations                          on the entity a page is about, what the
//	                                    page cites (DES-0046); empty elsewhere
//	.<field>                            every declared field, by its name as
//	                                    the schema writes it (.verified_by),
//	                                    calculated ones included
//	.<facet>                            every incoming facet, by its
//	                                    reverse_name, e.g. .implemented_by
//
// The engine's keys are capitalised and a field's name is lower-case
// snake_case (the schema checks it), so the two can never clash: a type
// may declare a field named title, resolved or file (DES-0030).
// Values: text-like fields are strings, numbers float64, booleans bool; a
// calculated field is its formula's value (a number, boolean or string), nil
// when blank or when the formula cannot be evaluated; a
// link with cardinality one is another entity's map (or nil), with
// cardinality many a list of them; a list field is a list of row maps keyed
// by sub-field name, or, when it names an item type with of:, a list of
// item values. An unresolved link target is a map with Resolved
// false and every field its declared target types have set to nil, so a
// template written for resolved targets still renders.
type Data = map[string]any

// baseline is an entity map holding only the keys the engine provides on
// every entity, empty: the start of every entity's data, and all of an
// unresolved link target's but its declared fields.
func Baseline(id string) Data {
	return Data{"ID": id, "Type": "", "Body": "", "Resolved": false,
		"LastChangedSHA": "", "Revisions": []Data{}, "Citations": []Data{}, "File": FileData("", 0)}
}

// FileData is the file an entity is written in, as a template sees it
// (.File, DES-0053): .Path, and .Line, where the entity's ID is written,
// the same for an entity file and a row of a CSV file.
func FileData(path string, line int) Data {
	return Data{"Path": path, "Line": line}
}

// TableFile is a CSV file as a whole, for its table page and its type on
// the main page: .File without .Line.
func TableFile(t *entity.Table) Data {
	return Data{"Path": t.Path}
}

// Model holds every entity's template data, built once per export.
type Model struct {
	schema *schema.Schema
	ByID   map[string]Data
}

func NewModel(s *schema.Schema, entities []*model.Entity) *Model {
	d := &Model{schema: s, ByID: make(map[string]Data, len(entities))}
	// Two passes: links point at other entities' maps.
	for _, e := range entities {
		d.ByID[e.ID] = Baseline(e.ID)
	}
	for _, e := range entities {
		m := d.ByID[e.ID]
		m["Type"], m["Body"], m["Resolved"] = e.Type, e.Body, true
		m["File"] = FileData(e.Path, e.IDLine())
		for _, v := range e.Fields {
			m[v.Field.Name] = d.value(v)
			if v.Field == e.Schema.BodyField {
				m["Body"] = v.Str
			}
		}
		for _, in := range e.Incoming {
			refs := make([]Data, len(in.From))
			for i, from := range in.From {
				refs[i] = d.ByID[from.ID]
			}
			m[in.Facet.Name] = refs
		}
	}
	return d
}

func (d *Model) value(v *model.Value) any {
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
		refs := []Data{}
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
		rows := []Data{}
		if v.Present && !v.Invalid {
			for _, row := range v.Rows {
				r := Data{}
				for _, cell := range row {
					r[cell.Field.Name] = d.value(cell)
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

func (d *Model) ref(id string, target *model.Entity, f *schema.Field) Data {
	if target != nil {
		return d.ByID[target.ID]
	}
	m := Baseline(id)
	for _, tn := range f.Target {
		t := d.schema.Types[tn]
		if t == nil {
			continue
		}
		for _, tf := range t.Fields {
			m[tf.Name] = nil
		}
		for _, facet := range t.Facets {
			m[facet.Name] = []Data{}
		}
	}
	return m
}

// citations is the template data of a page's citations (DES-0046), in order
// of first citation: each cited entity's map with .CitationIndex, its 1-based
// position among them all, and .TypeCitationIndex, its position among those
// of its type. A missing entity is a map with Resolved false, like an
// unresolved link target.
func (d *Model) Citations(ids []string) []Data {
	out := make([]Data, len(ids))
	perType := map[string]int{}
	for i, id := range ids {
		var m Data
		if e, ok := d.ByID[id]; ok {
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
