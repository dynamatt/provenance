// Package query evaluates queries over the entity graph: it turns the typed
// model into Datalog facts and evaluates programs over them with the
// internal/query/datalog engine.
package query

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
)

// Base relations, the facts every program reads:
//
//	entity(E, Type)       every entity, by ID
//	field(N, Name, V)     a stored value of node N: an entity's field, or a
//	                      list row's sub-field; one fact per item of a list
//	                      that names an item type with of:
//	link(N, Name, T)      N links to the ID T through field Name, or through
//	                      incoming facet Name (a reverse link); T need not
//	                      resolve
//	row(E, List, R)       R is a row of entity E's list field List
//	item(N, List, I, V)   item I (from 0) of a list that names an item
//	                      type; field holds the same values, but as a set
//
// Every entity also has the rows of code_blocks (schema.CodeBlocks), derived
// from the fenced blocks its record finds, as row and field facts like a
// declared list's.
//
// A node is an entity's ID (a string) or a list row (a datalog Node value),
// so a row's sub-fields and links read exactly like an entity's. Values not
// matching their declared type are left out: they have no typed value to
// compare.
const (
	RelEntity = "entity"
	RelField  = "field"
	RelLink   = "link"
	RelRow    = "row"
	RelItem   = "item"
)

// RowNode names row i of entity id's list field list.
func RowNode(id, list string, i int) datalog.Value {
	return datalog.NodeValue(fmt.Sprintf("%s/%s/%d", id, list, i))
}

// Facts converts entities to base facts.
func Facts(entities []*model.Entity) *datalog.Database {
	db := datalog.NewDatabase()
	str := datalog.String
	for _, e := range entities {
		id := str(e.ID)
		db.MustAdd(RelEntity, id, str(e.Type))
		for _, v := range e.Fields {
			if v.Field.Kind == schema.List && v.Field.Elem == nil {
				if !v.Present || v.Invalid {
					continue
				}
				for i, row := range v.Rows {
					node := RowNode(e.ID, v.Field.Name, i)
					db.MustAdd(RelRow, id, str(v.Field.Name), node)
					for _, cell := range row {
						addValue(db, node, cell)
					}
				}
				continue
			}
			addValue(db, id, v)
		}
		for _, in := range e.Incoming {
			for _, from := range in.From {
				db.MustAdd(RelLink, id, str(in.Facet.Name), str(from.ID))
			}
		}
		list := str(schema.CodeBlocks.Name)
		for i, b := range CodeBlocks(e) {
			node := RowNode(e.ID, schema.CodeBlocks.Name, i)
			db.MustAdd(RelRow, id, list, node)
			db.MustAdd(RelField, node, str("language"), str(b.Lang))
			db.MustAdd(RelField, node, str("line"), datalog.Number(float64(b.Line)))
		}
	}
	return db
}

// CodeBlocks are e's code_blocks rows (schema.CodeBlocks): every fenced
// block its record finds in its body and text fields, with the language
// "none" for a bare fence and the opening fence's file line, in file order.
// A text with a problem, such as a misplaced caption, still has its blocks.
func CodeBlocks(e *model.Entity) []entity.Block {
	var out []entity.Block
	for _, text := range textSources(e) {
		refs, _ := e.Record.Refs(text)
		for _, b := range refs.CodeBlocks {
			if b.Lang == "" {
				b.Lang = "none"
			}
			b.Line = e.TextFileLine(text, b.Line)
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(a, b entity.Block) int { return cmp.Compare(a.Line, b.Line) })
	return out
}

func addValue(db *datalog.Database, node datalog.Value, v *model.Value) {
	if !v.Present || v.Invalid {
		return
	}
	name := datalog.String(v.Field.Name)
	switch v.Field.Kind {
	case schema.Link:
		for _, id := range v.IDs {
			db.MustAdd(RelLink, node, name, datalog.String(id))
		}
	case schema.List:
		for i, item := range v.Items {
			if val, ok := Scalar(item); ok {
				db.MustAdd(RelField, node, name, val)
				db.MustAdd(RelItem, node, name, datalog.Number(float64(i)), val)
			}
		}
	default:
		if val, ok := Scalar(v); ok {
			db.MustAdd(RelField, node, name, val)
		}
	}
}

// Scalar is a stored scalar value as a Datalog value.
func Scalar(v *model.Value) (datalog.Value, bool) {
	if !v.Present || v.Invalid {
		return datalog.Value{}, false
	}
	kind := v.Field.Kind
	if kind == schema.Calculated {
		kind = v.Result
	}
	switch kind {
	case schema.Number:
		return datalog.Number(v.Num), true
	case schema.Boolean:
		return datalog.Boolean(v.Bool), true
	case schema.String, schema.Text, schema.Enum, schema.Date:
		return datalog.String(v.Str), true
	}
	return datalog.Value{}, false
}
