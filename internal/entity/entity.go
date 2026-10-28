// Package entity is an entity as its source read it: its ID, type and file,
// and its record, the values and positions the source decoded (G4). The
// Markdown file parser (DES-0004, DES-0006) is here too.
package entity

import (
	"fmt"

	"github.com/dynamatt/provenance/internal/schema"
)

// Entity is one entity read from a file. Several entities may share a
// file, so a path does not name an entity.
type Entity struct {
	ID   string
	Type string
	// Path is slash-separated and relative to the repository root.
	Path string
	// Record is what the source wrote for the entity.
	Record Record
}

// IDLine is the file line of the entity's id.
func (e *Entity) IDLine() int { return e.Record.IDLine() }

// Record is an entity as its source wrote it: its values, not yet typed
// against the schema, each with its position. A source decodes values its
// own way; the typing rules every source shares (internal/model) see only
// this.
type Record interface {
	Values
	// IDLine and TypeLine are the file lines of the entity's id and type.
	IDLine() int
	TypeLine() int
	// Body is the freeform text written after the values, as written, and
	// the file line it starts on; "" when there is none.
	Body() (text string, line int)
	// Refs is what text, the body or one of the record's text values,
	// refers to. Its lines count from text's first line.
	Refs(text string) (*Refs, error)
}

// Values are what an entity, or one row of a list, writes for its fields.
type Values interface {
	// Value is what is written for f, nil when nothing is.
	Value(f *schema.Field) *Written
	// Keys are the names written, in the order written; an entity's id
	// and type are among them when the source writes them as values.
	Keys() []Key
}

// Key is one name a record writes.
type Key struct {
	Name string
	Line int
	// Set is false when the name is written with no value (Null).
	Set bool
}

// Kind is what a source read a written value as.
type Kind int

const (
	// Null is a value written as nothing, the same as not writing it.
	Null Kind = iota
	// Text, Number and Boolean are single values; Text holds a number's
	// digits, or "true" or "false".
	Text
	Number
	Boolean
	// Scalar is a single value of another kind, such as a YAML timestamp.
	Scalar
	// List is a list of values, Items.
	List
	// Mapping is a set of named values, Row: a list row.
	Mapping
	// Other is none of these, such as a YAML alias.
	Other
)

// Written is one value as its source wrote it.
type Written struct {
	Kind  Kind
	Text  string
	Items []*Written
	Row   Values
	// Line is the value's file line, 0 when not known. TextLine is the
	// line its text starts on, which is later for a YAML block scalar.
	Line, TextLine int
	// Source renders the value as written, for saying what is wrong with
	// it; nil for a single value, whose Source is its Text as written.
	Source func() string
}

// SourceText is the value as written.
func (w *Written) SourceText() string {
	if w.Source != nil {
		return w.Source()
	}
	return w.Text
}

// Refs is what a text value refers to (G8), in the order written.
type Refs struct {
	// Embeds are the entities written to be shown in full (![[ID]]).
	Embeds []string
	// Cited are the entities referred to by ID ([[ID]]), a misplaced
	// embed included.
	Cited []string
	// Queries are the query blocks.
	Queries []Block
	// Assets are the image files shown, as repository paths.
	Assets []string
	// CodeBlocks are every fenced block, query blocks included.
	CodeBlocks []Block
}

// Block is a fenced block. Line is its content's first line for a query
// block, its opening fence's for a code block.
type Block struct {
	// Lang is the first word of the info string, "" for a bare fence.
	Lang   string
	Source string
	Line   int
}

// TextError is a problem in a text value, such as a misplaced caption, at
// a line of the text (0 when not known).
type TextError struct {
	Line int
	Msg  string
}

func (e *TextError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// Error is a problem with one file, reported as path:line: message.
type Error struct {
	Path string
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return e.Path + ": " + e.Msg
}

// ConfigDirs are the repository-root folders that hold configuration rather
// than entities (DES-0005).
var ConfigDirs = []string{"schema", "rules", "templates", ".signatures", "assets"}

// DuplicateIDError reports two files declaring the same ID, with the line
// of the id in each.
type DuplicateIDError struct {
	ID                  string
	First, Then         string
	FirstLine, ThenLine int
}

func (e *DuplicateIDError) Error() string {
	return fmt.Sprintf("duplicate entity ID %s in %s and %s", e.ID, e.First, e.Then)
}
