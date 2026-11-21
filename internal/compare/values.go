// Package compare compares an entity's values and texts at two versions
// (DES-0055): typed, so a change of formatting alone is none, and text by
// Markdown block and word. `diff` shows what it finds; history reads which
// fields a commit changed (DES-0058).
package compare

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/schema"
)

// Field is a field whose value differs.
type Field struct {
	Name, Label string
	// Derived marks a calculated field or a query field's results.
	Derived bool
	// Old and New are a single value as shown, "" when not set: a link's
	// IDs, comma-separated, a query as written, a list whose items were
	// only reordered.
	Old, New string
	// Added and Removed are a many-link's IDs, a query's results or a
	// list's items gained and lost, in their side's order.
	Added, Removed []string
	// Rows are a list of records' rows that differ, by position.
	Rows []*Row
	// Text is a text field or body, compared by block.
	Text *Text
}

// Row is a list row that differs.
type Row struct {
	// Index counts from 1.
	Index  int
	Op     Op
	Fields []*Field
}

// Values compares every field either side's type declares, and the body
// when it is no field, as `diff` does (DES-0055): typed, so formatting
// alone is no change. Calculated fields and query results are compared
// too, marked Derived.
func Values(a, b *model.Entity) []*Field {
	var out []*Field
	seen := map[string]bool{}
	for _, f := range b.Schema.Fields {
		seen[f.Name] = true
		out = appendField(out, f.Name, Label(f), a.Field(f.Name), b.Field(f.Name))
	}
	for _, f := range a.Schema.Fields {
		if !seen[f.Name] {
			out = appendField(out, f.Name, Label(f), a.Field(f.Name), nil)
		}
	}
	if t := CompareText(a.Body, b.Body); t != nil {
		out = append(out, &Field{Name: "body", Label: "Body", Text: t})
	}
	return out
}

// Label is how a field is named to a reader: its friendly name, or else
// its name.
func Label(f *schema.Field) string {
	if f.FriendlyName != "" {
		return f.FriendlyName
	}
	return f.Name
}

func appendField(out []*Field, name, label string, a, b *model.Value) []*Field {
	kind := func(v *model.Value) schema.Kind {
		if v == nil {
			return ""
		}
		return v.Field.Kind
	}
	if kind(a) == schema.Query || kind(b) == schema.Query {
		// The query as written is the entity's own; its results are
		// derived from the rest.
		if f := compareValue(name, label, queryText(a), queryText(b)); f != nil {
			out = append(out, f)
		}
		if f := compareList(name, label, results(a), results(b)); f != nil {
			f.Derived = true
			out = append(out, f)
		}
		return out
	}
	f := compare(name, label, a, b)
	if f == nil {
		return out
	}
	f.Derived = kind(a) == schema.Calculated || kind(b) == schema.Calculated
	return append(out, f)
}

// compare compares two values of a field; nil when they read the same.
func compare(name, label string, a, b *model.Value) *Field {
	set := func(v *model.Value) bool { return v != nil && !v.Blank() }
	sa, sb := set(a), set(b)
	if !sa && !sb {
		return nil
	}
	switch {
	case sa && sb && !a.Invalid && !b.Invalid && a.Field.Kind == schema.Text && b.Field.Kind == schema.Text:
		if t := CompareText(a.Str, b.Str); t != nil {
			return &Field{Name: name, Label: label, Text: t}
		}
		return nil
	case isRows(a) || isRows(b):
		return compareRows(name, label, a, b)
	case isMany(a) || isMany(b):
		return compareList(name, label, items(a), items(b))
	}
	return compareValue(name, label, Shown(a), Shown(b))
}

func compareValue(name, label, a, b string) *Field {
	if a == b {
		return nil
	}
	return &Field{Name: name, Label: label, Old: a, New: b}
}

// compareList compares two lists: the items gained and lost, or, when
// only their order differs, the lists.
func compareList(name, label string, a, b []string) *Field {
	if slices.Equal(a, b) {
		return nil
	}
	f := &Field{Name: name, Label: label}
	for _, e := range Script(a, b) {
		switch {
		case e.Deleted():
			f.Removed = append(f.Removed, a[e.A])
		case e.Inserted():
			f.Added = append(f.Added, b[e.B])
		}
	}
	if slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))) {
		f.Added, f.Removed = nil, nil
		f.Old, f.New = strings.Join(a, ", "), strings.Join(b, ", ")
	}
	return f
}

// compareRows compares a list of records row by row, by position.
func compareRows(name, fieldLabel string, a, b *model.Value) *Field {
	rows := func(v *model.Value) []model.Row {
		if v == nil || v.Blank() || v.Invalid {
			return nil
		}
		return v.Rows
	}
	ra, rb := rows(a), rows(b)
	f := &Field{Name: name, Label: fieldLabel}
	for i := range max(len(ra), len(rb)) {
		r := &Row{Index: i + 1, Op: Changed}
		switch {
		case i >= len(ra):
			r.Op = Added
		case i >= len(rb):
			r.Op = Removed
		default:
			cells := map[string]*model.Value{}
			for _, c := range ra[i] {
				cells[c.Field.Name] = c
			}
			for _, c := range rb[i] {
				if g := compare(c.Field.Name, Label(c.Field), cells[c.Field.Name], c); g != nil {
					r.Fields = append(r.Fields, g)
				}
				delete(cells, c.Field.Name)
			}
			for _, c := range ra[i] {
				if cells[c.Field.Name] != nil {
					if g := compare(c.Field.Name, Label(c.Field), c, nil); g != nil {
						r.Fields = append(r.Fields, g)
					}
				}
			}
			if len(r.Fields) == 0 {
				continue
			}
		}
		f.Rows = append(f.Rows, r)
	}
	if len(f.Rows) == 0 {
		if (a != nil && a.Invalid) || (b != nil && b.Invalid) {
			return compareValue(name, fieldLabel, Shown(a), Shown(b))
		}
		return nil
	}
	return f
}

func isRows(v *model.Value) bool {
	return v != nil && v.Field.Kind == schema.List && v.Field.Elem == nil && !v.Invalid
}

func isMany(v *model.Value) bool {
	if v == nil || v.Invalid {
		return false
	}
	return (v.Field.Kind == schema.Link && v.Field.Cardinality == "many") || (v.Field.Kind == schema.List && v.Field.Elem != nil)
}

// items are a many-link's IDs or a list's items, as shown.
func items(v *model.Value) []string {
	if v == nil || v.Blank() {
		return nil
	}
	if v.Field.Kind == schema.Link {
		return v.IDs
	}
	out := make([]string, len(v.Items))
	for i, it := range v.Items {
		out[i] = Shown(it)
	}
	return out
}

func queryText(v *model.Value) string {
	if v == nil || v.Field.Kind != schema.Query {
		return ""
	}
	if v.Invalid || v.Query == nil {
		return v.Raw
	}
	return strings.TrimSpace(query.QueryText(v.Query))
}

func results(v *model.Value) []string {
	if v == nil || v.Field.Kind != schema.Query {
		return nil
	}
	return v.IDs
}

// Shown is a single value as compared and shown: "" when not set, the
// source text of an invalid value.
func Shown(v *model.Value) string {
	if v == nil || v.Blank() {
		return ""
	}
	if v.Invalid {
		return v.Raw
	}
	kind := v.Field.Kind
	if kind == schema.Calculated {
		kind = v.Result
	}
	switch kind {
	case schema.Number:
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	case schema.Boolean:
		return strconv.FormatBool(v.Bool)
	case schema.Link:
		return strings.Join(v.IDs, ", ")
	case schema.List:
		return strings.Join(items(v), ", ")
	}
	return v.Str
}
