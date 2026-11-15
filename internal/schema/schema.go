// Package schema loads entity type, enum and record declarations
// (schema/*.yaml, schema/enums/*.yaml and schema/records/*.yaml) into the type
// model of DES-0010.
//
// Only problems that prevent the schema from being interpreted are load
// errors: YAML syntax, unknown field types, duplicate names, ambiguous body
// fields, links without a cardinality, lists without an item type, records
// that contain themselves. Everything else — a link target that
// names no type, a formula that does not parse — is schema meta-validation,
// reported by validate.
package schema

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Kind is a field's type category.
type Kind string

const (
	String     Kind = "string"
	Text       Kind = "text"
	Number     Kind = "number"
	Date       Kind = "date"
	Boolean    Kind = "boolean"
	Enum       Kind = "enum" // the field's type names a declared enum
	Link       Kind = "link"
	List       Kind = "list"
	Calculated Kind = "calculated"
)

// DateLayout is how a date field's value is written: YYYY-MM-DD.
const DateLayout = "2006-01-02"

// builtinKinds are the type keywords a field may use directly.
var builtinKinds = []Kind{String, Text, Number, Date, Boolean, Link, List, Calculated}

// itemKinds are the built-in types a list may name with of:, one value per
// item. Enums and records may be named too.
var itemKinds = []Kind{String, Text, Number, Date, Boolean}

// Field is one declared field, or a sub-field of a list.
type Field struct {
	Name     string
	Kind     Kind
	EnumName string // set when Kind is Enum
	Required bool
	Body     bool
	// Title marks the field whose value is the entity's title (title:
	// true), at most one per type; FriendlyName is how the built-in pages
	// label the field, its name as written when "" (DES-0010).
	Title        bool
	FriendlyName string
	Help         string
	// Target and Cardinality ("one" or "many") apply to links. Target is
	// as written; Targets are the types a link may name: each target and
	// every subtype of it, in name order.
	Target      []string
	Targets     []string
	Cardinality string
	ReverseName string
	// ReverseFriendlyName is how the built-in pages label the reverse_name
	// facet on the link's targets, the reverse_name as written when ""
	// (DES-0010).
	ReverseFriendlyName string
	// A list holds either rows or items. Rows have the sub-fields Fields:
	// declared inline under fields:, or a record's fields when the list names
	// one with of: (Record is then its name). Items have the single type
	// Elem, set when of: names a built-in type or an enum; Elem carries the
	// list's name.
	Fields  []*Field
	Record  string
	Elem    *Field
	Formula string
	// Column is the CSV header a field of a CSV-backed type reads, when it
	// is not the field's name (DES-0053).
	Column string
	Line   int
}

// Label is how the built-in pages show f: its friendly_name, else its name
// as written (DES-0010).
func (f *Field) Label() string {
	if f.FriendlyName != "" {
		return f.FriendlyName
	}
	return f.Name
}

// ColumnName is the CSV header f reads: its column:, else its name.
func (f *Field) ColumnName() string {
	if f.Column != "" {
		return f.Column
	}
	return f.Name
}

// CodeBlocks is the list every entity derives from its Markdown, one row
// per fenced code block in document order, read by conditions like a
// declared list (DES-0015): language, the first word of the fence's info
// string or "none" for a bare fence, and line, the opening fence's line in
// the file. No type may declare a field or reverse_name of its name.
var CodeBlocks = &Field{Name: "code_blocks", Kind: List, Fields: []*Field{
	{Name: "language", Kind: String},
	{Name: "line", Kind: Number},
}}

// Type is one entity type.
type Type struct {
	Name string
	// IDPrefix is the prefix of the type's IDs: its own id_prefix, else
	// its nearest ancestor's, so a subtype numbers in its parent's series
	// unless it declares its own. PrefixType is the type declaring it;
	// nil when no type in the chain has one.
	IDPrefix   string
	PrefixType *Type
	// Fields are every field of the type: its parent's, in the parent's
	// order, then its own, Declared.
	Fields   []*Field
	Declared []*Field
	// Parent is the type named by extends:, nil for a type without one or
	// whose extends: names no type; Extends is the name as written, and
	// ExtendsLine its line.
	Parent      *Type
	Extends     string
	ExtendsLine int
	// Abstract types have no entities of their own: they declare fields
	// for their subtypes (abstract: true).
	Abstract bool
	// BodyField is the text field declared body: true, or nil when the body
	// is freeform.
	BodyField *Field
	// TitleField is the string field declared title: true, or nil when
	// the type's entities have no title.
	TitleField *Field
	File       string
	Line       int // of type:
	// Facets are the incoming link facets other types' reverse_name
	// declarations give this type, in name order.
	Facets []*Facet
	// Source is the CSV file the type's entities are read from, one per
	// row (DES-0053); nil for a type whose entities are Markdown files.
	Source *Source
	// Pages is false when the type's entities get no page of their own:
	// they are shown, all together, on the type's page (DES-0054).
	Pages bool
}

// Source is a CSV file a type's entities are read from (DES-0053).
type Source struct {
	// CSV is the file, slash-separated and relative to the repository root.
	CSV string
	// ID is the header of the column holding each row's ID.
	ID string
	// Delimiter separates a row's cells; Separator the IDs or items in one
	// cell.
	Delimiter rune
	Separator string
	Line      int // of source:
}

// SourceTypes are the CSV-backed types, in name order.
func (s *Schema) SourceTypes() []*Type {
	var out []*Type
	for _, n := range s.TypeNames() {
		if t := s.Types[n]; t.Source != nil {
			out = append(out, t)
		}
	}
	return out
}

// Facet is an incoming link facet: every link field, on any type, whose
// reverse_name is Name and whose targets include the facet's type.
type Facet struct {
	Name    string
	Sources []FacetSource
}

// Label is how the built-in pages show the facet: the
// reverse_friendly_name its links give, else its name as written
// (DES-0010). Links that share a reverse_name and give it a label give
// the same one (a load error otherwise).
func (f *Facet) Label() string {
	for _, s := range f.Sources {
		if s.Field.ReverseFriendlyName != "" {
			return s.Field.ReverseFriendlyName
		}
	}
	return f.Name
}

// FacetSource is one link field feeding a facet. List marks a link declared
// inside a list's rows (inline or in a record); the facet then comes from the
// entity holding the row.
type FacetSource struct {
	Type  *Type
	Field *Field
	List  *Field
}

// Field returns the named top-level field, or nil.
func (t *Type) Field(name string) *Field {
	for _, f := range t.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// EnumType is a named enum.
type EnumType struct {
	Name   string
	Values []string
	File   string
	Line   int // of enum:
}

// RecordType is a named row shape that lists use with of:, declared once
// and shared by every list that names it.
type RecordType struct {
	Name   string
	Fields []*Field
	File   string
	Line   int // of record:
}

// Schema is the full set of declarations.
type Schema struct {
	Types   map[string]*Type
	Enums   map[string]*EnumType
	Records map[string]*RecordType
	// KeyProblems are keys the DSL doesn't have, or that don't apply where
	// they are written. They don't stop the schema loading: the key is
	// ignored, and validate reports it.
	KeyProblems []*Error
}

var (
	typeKeys   = []string{"type", "extends", "abstract", "id_prefix", "pages", "source", "fields"}
	sourceKeys = []string{"csv", "id", "delimiter", "separator"}
	recordKeys = []string{"record", "fields"}
	enumKeys   = []string{"enum", "values"}
	// fieldKeys maps each field key to the kinds of field it applies to,
	// nil for every kind.
	fieldKeys = map[string][]Kind{
		"name": nil, "type": nil, "required": nil, "help": nil,
		"body":  {Text},
		"title": {String}, "friendly_name": nil,
		"target": {Link}, "cardinality": {Link}, "reverse_name": {Link}, "reverse_friendly_name": {Link},
		"formula": {Calculated},
		"fields":  {List}, "of": {List},
		"column": nil,
	}
	fieldKeyOrder = []string{"name", "type", "required", "title", "friendly_name", "help", "body", "target", "cardinality", "reverse_name", "reverse_friendly_name", "formula", "fields", "of", "column"}
)

// checkKeys records the keys of a declaration's mapping m that are not in
// allowed.
func (s *Schema) checkKeys(path string, m *yaml.Node, what string, allowed []string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; !slices.Contains(allowed, k.Value) {
			s.KeyProblems = append(s.KeyProblems, &Error{Path: path, Line: k.Line,
				Msg: fmt.Sprintf("%s: unknown key %q (expected %s)", what, k.Value, strings.Join(allowed, ", "))})
		}
	}
}

// checkFieldKeys records the keys of a field declaration n that the DSL
// doesn't have or that don't apply to the field's kind.
func (s *Schema) checkFieldKeys(path string, n *yaml.Node, f *Field, label string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		kinds, known := fieldKeys[k.Value]
		var msg string
		switch {
		case k.Value == "default":
			msg = fmt.Sprintf("%s: unknown key \"default\": fields have no defaults; a field that must have a value says required: true", label)
		case !known:
			msg = fmt.Sprintf("%s: unknown key %q (expected %s)", label, k.Value, strings.Join(fieldKeyOrder, ", "))
		case kinds != nil && !slices.Contains(kinds, f.Kind):
			names := make([]string, len(kinds))
			for i, k := range kinds {
				names[i] = string(k)
			}
			msg = fmt.Sprintf("%s: %s applies only to a %s field", label, k.Value, strings.Join(names, " or "))
		default:
			continue
		}
		s.KeyProblems = append(s.KeyProblems, &Error{Path: path, Line: k.Line, Msg: msg})
	}
}

// TypeNames lists the declared types in name order.
func (s *Schema) TypeNames() []string { return slices.Sorted(maps.Keys(s.Types)) }

// Error is a schema problem, reported as path:line: message.
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

// nameForm is the form of a field's name and a link's reverse_name:
// lower-case snake_case. A template reads a field by its name (.verified_by),
// which can then never clash with the engine's capitalised keys (.ID,
// .Title), and is a valid template identifier (DES-0030).
var nameForm = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// Dir is the schema folder at the repository root.
const Dir = "schema"

// loader resolves field declarations. Records may name other records, so
// each is resolved on first use; pending holds those not yet resolved and
// resolving the chain being resolved, outermost first. problems collects
// every load error in the order they are found.
type loader struct {
	*Schema
	pending   map[string]*rawDecl
	resolving []string
	problems  []*Error
}

// keep records a schema problem and reports whether loading can go on: it
// can after an *Error, which leaves out only the declaration or field at
// fault, but not after an I/O error.
func (l *loader) keep(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		l.problems = append(l.problems, e)
		return true
	}
	return false
}

// Load reads every declaration under root/schema and fails with the first
// problem. A repository without a schema folder has no types.
func Load(root string) (*Schema, error) {
	s, problems, err := Read(root)
	if len(problems) > 0 {
		return nil, problems[0]
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Read reads every declaration under root/schema like Load, but collects
// every problem instead of stopping at the first: a declaration or field at
// fault is left out and the rest is still read, so the schema it returns is
// usable but incomplete when there are problems. err is an I/O error, which
// stops reading.
func Read(root string) (s *Schema, problems []*Error, err error) {
	s = &Schema{Types: map[string]*Type{}, Enums: map[string]*EnumType{}, Records: map[string]*RecordType{}}
	l := &loader{Schema: s, pending: map[string]*rawDecl{}}
	err = l.read(root)
	return s, l.problems, err
}

func (l *loader) read(root string) error {
	s := l.Schema
	// Enums first: field types are resolved against them.
	enumFiles, err := yamlFiles(root, filepath.Join(Dir, "enums"))
	if err != nil {
		return err
	}
	for _, rel := range enumFiles {
		if err := s.loadEnum(root, rel); err != nil && !l.keep(err) {
			return err
		}
	}
	// Every name is registered before any fields are resolved, so a list's
	// of: can name a record declared in any file.
	recordFiles, err := yamlFiles(root, filepath.Join(Dir, "records"))
	if err != nil {
		return err
	}
	var records []string
	for _, rel := range recordFiles {
		rr, err := readDecl(root, rel, "record")
		if err == nil {
			err = s.checkName("record", rr)
		}
		if err != nil {
			if !l.keep(err) {
				return err
			}
			continue
		}
		s.Records[rr.name] = &RecordType{Name: rr.name, File: rel, Line: rr.nameLine}
		s.checkKeys(rel, rr.mapping, "record "+rr.name, recordKeys)
		l.pending[rr.name] = rr
		records = append(records, rr.name)
	}
	typeFiles, err := yamlFiles(root, Dir)
	if err != nil {
		return err
	}
	var raws []*rawDecl
	for _, rel := range typeFiles {
		rt, err := readDecl(root, rel, "type")
		if err == nil {
			err = s.checkName("type", rt)
		}
		if err != nil {
			if !l.keep(err) {
				return err
			}
			continue
		}
		t := &Type{Name: rt.name, IDPrefix: rt.idPrefix, File: rel, Line: rt.nameLine, Pages: true}
		s.Types[rt.name] = t
		if e := yamlnode.Lookup(rt.mapping, "extends"); e != nil {
			if e.Kind != yaml.ScalarNode || e.Value == "" || e.Tag == "!!null" {
				l.keep(&Error{Path: rel, Line: e.Line, Msg: fmt.Sprintf("type %s: extends: names the type it extends", rt.name)})
			} else {
				t.Extends, t.ExtendsLine = e.Value, e.Line
			}
		}
		if a := yamlnode.Lookup(rt.mapping, "abstract"); a != nil {
			if a.Kind != yaml.ScalarNode || a.Tag != "!!bool" {
				l.keep(&Error{Path: rel, Line: a.Line, Msg: fmt.Sprintf("type %s: abstract: must be true or false", rt.name)})
			} else {
				t.Abstract = a.Value == "true" || a.Value == "True" || a.Value == "TRUE"
			}
		}
		if p := yamlnode.Lookup(rt.mapping, "pages"); p != nil {
			if p.Kind != yaml.ScalarNode || p.Tag != "!!bool" {
				l.keep(&Error{Path: rel, Line: p.Line, Msg: fmt.Sprintf("type %s: pages: must be true or false", rt.name)})
			} else {
				t.Pages = p.Value == "true" || p.Value == "True" || p.Value == "TRUE"
			}
		}
		s.checkKeys(rel, rt.mapping, "type "+rt.name, typeKeys)
		if n := yamlnode.Lookup(rt.mapping, "source"); n != nil {
			if t.Source, err = s.source(rel, rt.name, n); err != nil {
				l.keep(err)
			}
		}
		raws = append(raws, rt)
	}
	// Resolve every record, used or not, so a broken one is reported.
	for _, name := range records {
		l.record(name)
	}
	for _, rt := range raws {
		s.Types[rt.name].Declared = l.fields(rt.path, rt.fields, "")
	}
	l.inherit(raws)
	for _, rt := range raws {
		t := s.Types[rt.name]
		for _, f := range t.Fields {
			own := slices.Contains(t.Declared, f)
			if f.Title {
				if !own {
					if t.TitleField == nil {
						t.TitleField = f
					}
					continue
				}
				if t.TitleField != nil {
					l.keep(&Error{Path: rt.path, Line: f.Line, Msg: fmt.Sprintf("field %q: only one field may set title: true (%q already does)", f.Name, t.TitleField.Name)})
				} else {
					t.TitleField = f
				}
			}
			if !f.Body {
				continue
			}
			if !own {
				if t.BodyField == nil {
					t.BodyField = f
				}
				continue
			}
			if t.BodyField != nil {
				l.keep(&Error{Path: rt.path, Line: f.Line, Msg: fmt.Sprintf("field %q: only one field may set body: true (%q already does)", f.Name, t.BodyField.Name)})
				continue
			}
			t.BodyField = f
		}
		l.columns(t, rt.fields)
	}
	s.targets()
	s.facets(l)
	return nil
}

// inherit resolves each type's extends: (DES-0010): its parent, its
// fields (the parent's, then its own) and its ID prefix. A type whose
// extends: names no type, or whose chain leads back to it, is left
// without a parent.
func (l *loader) inherit(raws []*rawDecl) {
	s := l.Schema
	for _, rt := range raws {
		t := s.Types[rt.name]
		if t.Extends == "" {
			continue
		}
		p := s.Types[t.Extends]
		if p == nil {
			l.keep(&Error{Path: t.File, Line: t.ExtendsLine, Msg: fmt.Sprintf("type %s: extends: unknown type %q (types: %s)", t.Name, t.Extends, strings.Join(s.TypeNames(), ", "))})
			continue
		}
		t.Parent = p
	}
	// A chain leading back to where it started is a cycle: report it once
	// at each of its types, and break it there.
	for _, rt := range raws {
		t := s.Types[rt.name]
		chain := []string{t.Name}
		for p := t.Parent; p != nil; p = p.Parent {
			chain = append(chain, p.Name)
			if p == t {
				l.keep(&Error{Path: t.File, Line: t.ExtendsLine, Msg: fmt.Sprintf("type %s extends itself (%s)", t.Name, strings.Join(chain, " → "))})
				break
			}
			if len(chain) > len(s.Types)+1 {
				break // a cycle further up, reported at its own types
			}
		}
	}
	for _, rt := range raws {
		if t := s.Types[rt.name]; t.Parent != nil && t.inCycle() {
			t.Parent = nil
		}
	}
	done := map[*Type]bool{}
	var resolve func(t *Type)
	resolve = func(t *Type) {
		if done[t] {
			return
		}
		done[t] = true
		if t.IDPrefix != "" {
			t.PrefixType = t
		}
		if t.Parent == nil {
			t.Fields = t.Declared
			return
		}
		p := t.Parent
		resolve(p)
		t.Fields = slices.Clone(p.Fields)
		for _, f := range t.Declared {
			if from := p.Field(f.Name); from != nil {
				owner := p
				for owner.Parent != nil && slices.Contains(owner.Parent.Fields, from) {
					owner = owner.Parent
				}
				l.keep(&Error{Path: t.File, Line: f.Line, Msg: fmt.Sprintf("field %q is inherited from %s (%s:%d); a subtype adds fields, it does not declare its parent's again", f.Name, owner.Name, owner.File, from.Line)})
				continue
			}
			t.Fields = append(t.Fields, f)
		}
		if t.PrefixType == nil {
			t.IDPrefix, t.PrefixType = p.IDPrefix, p.PrefixType
		}
	}
	for _, rt := range raws {
		resolve(s.Types[rt.name])
	}
	for _, rt := range raws {
		t := s.Types[rt.name]
		switch {
		case t.Abstract && rt.idPrefix != "":
			l.keep(&Error{Path: t.File, Line: yamlnode.Lookup(rt.mapping, "id_prefix").Line, Msg: fmt.Sprintf("type %s is abstract: it has no entities, so no id_prefix; give it to the types that extend it", t.Name)})
			t.IDPrefix, t.PrefixType = "", nil
		case t.Abstract && t.Source != nil:
			l.keep(&Error{Path: t.File, Line: t.Source.Line, Msg: fmt.Sprintf("type %s is abstract: it has no entities, so no source", t.Name)})
			t.Source = nil
		}
	}
}

// inCycle reports whether t's chain of parents leads back to t.
func (t *Type) inCycle() bool {
	seen := map[*Type]bool{}
	for p := t.Parent; p != nil; p = p.Parent {
		if p == t {
			return true
		}
		if seen[p] {
			return false
		}
		seen[p] = true
	}
	return false
}

// Owner is the type among t and its ancestors declaring f, a top-level
// field of t; t when none does.
func (t *Type) Owner(f *Field) *Type {
	for a := t; a != nil; a = a.Parent {
		if slices.Contains(a.Declared, f) {
			return a
		}
	}
	return t
}

// IsA reports whether t is the type named name or one of its subtypes.
func (t *Type) IsA(name string) bool {
	for a := t; a != nil; a = a.Parent {
		if a.Name == name {
			return true
		}
	}
	return false
}

// Ancestors are t's parent, its parent's parent and so on, nearest first.
func (t *Type) Ancestors() []*Type {
	var out []*Type
	for p := t.Parent; p != nil; p = p.Parent {
		out = append(out, p)
	}
	return out
}

// Concrete are the types a from: written as t stands for: t unless it is
// abstract, then its subtypes that are not, in name order.
func (s *Schema) Concrete(t *Type) []*Type {
	var out []*Type
	if !t.Abstract {
		out = append(out, t)
	}
	for _, o := range s.Subtypes(t) {
		if !o.Abstract {
			out = append(out, o)
		}
	}
	return out
}

// Subtypes are the types extending t, directly or further down, in name
// order.
func (s *Schema) Subtypes(t *Type) []*Type {
	var out []*Type
	for _, n := range s.TypeNames() {
		if o := s.Types[n]; o != t && o.IsA(t.Name) {
			out = append(out, o)
		}
	}
	return out
}

// Expand lists the types names stand for wherever a type is named (a
// link's target:, a query's from:): each named type and every subtype of
// it, each once, in name order, leaving out abstract types, which have
// no entities.
func (s *Schema) Expand(names []string) []string {
	var out []string
	for _, n := range s.TypeNames() {
		if s.Types[n].Abstract {
			continue
		}
		for _, want := range names {
			if s.Types[n].IsA(want) {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// targets sets every link field's Targets.
func (s *Schema) targets() {
	set := func(fields []*Field) {
		for _, f := range fields {
			if f.Kind == Link {
				f.Targets = s.Expand(f.Target)
			}
			for _, sub := range f.Fields {
				if sub.Kind == Link {
					sub.Targets = s.Expand(sub.Target)
				}
			}
		}
	}
	for _, n := range s.TypeNames() {
		set(s.Types[n].Declared)
	}
	for _, r := range s.Records {
		set(r.Fields)
	}
}

// checkName fails when a type or record declaration reuses a name already
// declared. Types and records share one namespace with enums: a field's type
// or a list's of: names any of them.
func (s *Schema) checkName(kind string, d *rawDecl) error {
	clash := func(what, file string) error {
		return &Error{Path: d.path, Line: d.nameLine, Msg: fmt.Sprintf("%s %s %s", kind, d.name, what) + " declared in " + file}
	}
	switch {
	case kind == "record" && isBuiltin(d.name):
		return &Error{Path: d.path, Line: d.nameLine, Msg: fmt.Sprintf("record %s has the same name as a built-in field type", d.name)}
	case s.Enums[d.name] != nil:
		return clash("has the same name as the enum", s.Enums[d.name].File)
	case s.Records[d.name] != nil && kind == "record":
		return clash("is already", s.Records[d.name].File)
	case s.Records[d.name] != nil:
		return clash("has the same name as the record", s.Records[d.name].File)
	case s.Types[d.name] != nil:
		return clash("is already", s.Types[d.name].File)
	}
	return nil
}

// record returns the named record, resolving its fields on first use.
func (l *loader) record(name string) *RecordType {
	r := l.Records[name]
	raw, pending := l.pending[name]
	if !pending {
		return r
	}
	l.resolving = append(l.resolving, name)
	r.Fields = l.fields(raw.path, raw.fields, fmt.Sprintf("record %q", name))
	l.resolving = l.resolving[:len(l.resolving)-1]
	delete(l.pending, name)
	return r
}

// facets derives every type's incoming facets from reverse_name
// declarations. A link is declared once, on its source side (DES-0011),
// so a reverse_name that repeats a field declared on the target type is
// a load error: the same relationship would be stored twice, and the facet
// is left out. Targets naming no declared type are left to validate.
func (s *Schema) facets(ld *loader) {
	// An inherited link feeds the facet from each type that has it; its
	// problems are reported once, where it is declared.
	reported := map[*Field]bool{}
	keep := func(f *Field, e *Error) {
		if !reported[f] {
			reported[f] = true
			ld.keep(e)
		}
	}
	for _, n := range s.TypeNames() {
		src := s.Types[n]
		if src.Abstract {
			continue
		}
		var links []FacetSource
		for _, f := range src.Fields {
			switch f.Kind {
			case Link:
				links = append(links, FacetSource{Type: src, Field: f})
			case List:
				for _, sub := range f.Fields {
					if sub.Kind == Link {
						links = append(links, FacetSource{Type: src, Field: sub, List: f})
					}
				}
			}
		}
		for _, l := range links {
			if l.Field.ReverseName == "" {
				continue
			}
			path := src.Owner(l.Field).File
			if l.List != nil {
				path = src.Owner(l.List).File
			}
			if l.List != nil && l.List.Record != "" {
				path = s.Records[l.List.Record].File
			}
			if l.Field.ReverseName == CodeBlocks.Name {
				keep(l.Field, &Error{Path: path, Line: l.Field.Line, Msg: fmt.Sprintf(
					"field %q: reverse_name code_blocks is the list every entity derives from its Markdown; choose another name", l.Field.Name)})
				continue
			}
			for _, tn := range l.Field.Targets {
				target := s.Types[tn]
				if clash := target.Field(l.Field.ReverseName); clash != nil {
					keep(l.Field, &Error{Path: path, Line: l.Field.Line, Msg: fmt.Sprintf(
						"field %q: reverse_name %q repeats the field %s declared on %s (%s:%d); declare a link once, on its source side",
						l.Field.Name, l.Field.ReverseName, clash.Name, target.Name, target.File, clash.Line)})
					continue
				}
				if other := target.addFacet(l.Field.ReverseName, l); other != nil {
					keep(l.Field, &Error{Path: path, Line: l.Field.Line, Msg: fmt.Sprintf(
						"field %q: reverse_friendly_name %q differs from %q, given by the field %s of %s for the same reverse_name %s; give one label, or leave it out on all but one",
						l.Field.Name, l.Field.ReverseFriendlyName, other.Field.ReverseFriendlyName, other.Field.Name, other.Type.Name, l.Field.ReverseName)})
				}
			}
		}
	}
	for _, t := range s.Types {
		sort.Slice(t.Facets, func(i, j int) bool { return t.Facets[i].Name < t.Facets[j].Name })
	}
}

// addFacet adds src to the facet name, and returns a source already there
// whose reverse_friendly_name differs from src's, both given.
func (t *Type) addFacet(name string, src FacetSource) *FacetSource {
	for _, f := range t.Facets {
		if f.Name == name {
			var clash *FacetSource
			for i, s := range f.Sources {
				a, b := s.Field.ReverseFriendlyName, src.Field.ReverseFriendlyName
				if a != "" && b != "" && a != b && clash == nil {
					clash = &f.Sources[i]
				}
			}
			f.Sources = append(f.Sources, src)
			return clash
		}
	}
	t.Facets = append(t.Facets, &Facet{Name: name, Sources: []FacetSource{src}})
	return nil
}

// yamlFiles lists *.yaml directly in root/dir, slash-separated and relative to
// root, in name order.
func yamlFiles(root, dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".yaml") {
			files = append(files, filepath.ToSlash(filepath.Join(dir, e.Name())))
		}
	}
	sort.Strings(files)
	return files, nil
}

func readMapping(root, rel string) (*yaml.Node, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yamlnode.Unmarshal(data, 1, &doc); err != nil {
		return nil, yamlError(rel, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, &Error{Path: rel, Msg: "expected a mapping of keys to values"}
	}
	return doc.Content[0], nil
}

func yamlError(path string, err error) error {
	line, msg := yamlnode.ErrorLine(err)
	return &Error{Path: path, Line: line, Msg: msg}
}

func (s *Schema) loadEnum(root, rel string) error {
	m, err := readMapping(root, rel)
	if err != nil {
		return err
	}
	var raw struct {
		Enum   string   `yaml:"enum"`
		Values []string `yaml:"values"`
	}
	if err := m.Decode(&raw); err != nil {
		return yamlError(rel, err)
	}
	if raw.Enum == "" {
		return &Error{Path: rel, Line: m.Line, Msg: "missing enum name (enum: <Name>)"}
	}
	line := yamlnode.Lookup(m, "enum").Line
	if isBuiltin(raw.Enum) {
		return &Error{Path: rel, Line: line, Msg: fmt.Sprintf("enum %s has the same name as a built-in field type", raw.Enum)}
	}
	if prev, dup := s.Enums[raw.Enum]; dup {
		return &Error{Path: rel, Line: line, Msg: fmt.Sprintf("enum %s is already declared in %s", raw.Enum, prev.File)}
	}
	s.Enums[raw.Enum] = &EnumType{Name: raw.Enum, Values: raw.Values, File: rel, Line: line}
	s.checkKeys(rel, m, "enum "+raw.Enum, enumKeys)
	return nil
}

// rawDecl is a type or record declaration before its fields are resolved.
type rawDecl struct {
	path, name, idPrefix string
	nameLine             int
	fields               []*yaml.Node
	mapping              *yaml.Node
}

// readDecl reads a declaration whose name is under key: "type" or "record".
func readDecl(root, rel, key string) (*rawDecl, error) {
	m, err := readMapping(root, rel)
	if err != nil {
		return nil, err
	}
	name := yamlnode.Lookup(m, key)
	if name == nil || name.Kind != yaml.ScalarNode || name.Value == "" {
		return nil, &Error{Path: rel, Line: m.Line, Msg: fmt.Sprintf("missing %s name (%s: <Name>)", key, key)}
	}
	rt := &rawDecl{path: rel, name: name.Value, nameLine: name.Line, mapping: m}
	if p := yamlnode.Lookup(m, "id_prefix"); p != nil {
		rt.idPrefix = p.Value
	}
	if f := yamlnode.Lookup(m, "fields"); f != nil {
		if f.Kind != yaml.SequenceNode {
			return nil, &Error{Path: rel, Line: f.Line, Msg: "fields must be a list"}
		}
		rt.fields = f.Content
	}
	return rt, nil
}

type rawField struct {
	Name            string   `yaml:"name"`
	Type            string   `yaml:"type"`
	Required        bool     `yaml:"required"`
	Body            bool     `yaml:"body"`
	Title           bool     `yaml:"title"`
	Friendly        string   `yaml:"friendly_name"`
	Help            string   `yaml:"help"`
	Target          []string `yaml:"target"`
	Cardinality     string   `yaml:"cardinality"`
	ReverseName     string   `yaml:"reverse_name"`
	ReverseFriendly string   `yaml:"reverse_friendly_name"`
	Formula         string   `yaml:"formula"`
	Column          string   `yaml:"column"`
}

// fields converts field declarations. within names what encloses sub-fields
// (`list "rows"`, `record "Equipment"`) for error messages; it is empty for a
// type's own fields.
func (l *loader) fields(path string, nodes []*yaml.Node, within string) []*Field {
	var out []*Field
	seen := map[string]int{}
	for _, n := range nodes {
		if f, err := l.field(path, n, within, seen); err != nil {
			l.keep(err)
		} else {
			out = append(out, f)
		}
	}
	return out
}

// field converts one field declaration; seen holds the names declared so
// far beside it, with their lines.
func (l *loader) field(path string, n *yaml.Node, within string, seen map[string]int) (*Field, error) {
	var rf rawField
	if n.Kind != yaml.MappingNode {
		return nil, &Error{Path: path, Line: n.Line, Msg: "each field must be a mapping with name and type"}
	}
	if err := n.Decode(&rf); err != nil {
		return nil, yamlError(path, err)
	}
	label := fmt.Sprintf("field %q", rf.Name)
	if within != "" {
		label = fmt.Sprintf("field %q in %s", rf.Name, within)
	}
	if rf.Name == "" {
		return nil, &Error{Path: path, Line: n.Line, Msg: "field without a name"}
	}
	if !nameForm.MatchString(rf.Name) {
		return nil, &Error{Path: path, Line: n.Line, Msg: fmt.Sprintf("%s: a field's name is lower-case snake_case: a letter, then letters, digits and single underscores, such as verified_by", label)}
	}
	if rf.ReverseName != "" && !nameForm.MatchString(rf.ReverseName) {
		return nil, &Error{Path: path, Line: n.Line, Msg: fmt.Sprintf("%s: reverse_name %q is not lower-case snake_case: a letter, then letters, digits and single underscores, such as verified_by", label, rf.ReverseName)}
	}
	if rf.ReverseFriendly != "" && rf.ReverseName == "" && rf.Type == string(Link) {
		return nil, &Error{Path: path, Line: n.Line, Msg: fmt.Sprintf("%s: reverse_friendly_name labels the reverse_name, and there is none; give reverse_name too", label)}
	}
	if prev, dup := seen[rf.Name]; dup {
		return nil, &Error{Path: path, Line: n.Line, Msg: fmt.Sprintf("%s is declared twice (first at line %d)", label, prev)}
	}
	seen[rf.Name] = n.Line
	if within == "" && rf.Name == CodeBlocks.Name {
		return nil, &Error{Path: path, Line: n.Line, Msg: label + ": code_blocks is the list every entity derives from its Markdown; choose another name"}
	}

	f := &Field{
		Name: rf.Name, Required: rf.Required, Body: rf.Body, Title: rf.Title, FriendlyName: rf.Friendly, Help: rf.Help,
		Target: rf.Target, Cardinality: rf.Cardinality, ReverseName: rf.ReverseName, ReverseFriendlyName: rf.ReverseFriendly,
		Formula: rf.Formula, Column: rf.Column, Line: n.Line,
	}
	typeLine := n.Line
	if tn := yamlnode.Lookup(n, "type"); tn != nil {
		typeLine = tn.Line
	}
	switch {
	case rf.Type == "":
		return nil, &Error{Path: path, Line: n.Line, Msg: label + ": missing type"}
	case isBuiltin(rf.Type):
		f.Kind = Kind(rf.Type)
	case l.Enums[rf.Type] != nil:
		f.Kind, f.EnumName = Enum, rf.Type
	case l.Records[rf.Type] != nil:
		return nil, &Error{Path: path, Line: typeLine, Msg: fmt.Sprintf("%s: record %s holds a list's rows; declare type: list with of: %s", label, rf.Type, rf.Type)}
	default:
		return nil, &Error{Path: path, Line: typeLine, Msg: fmt.Sprintf("%s: unknown type %q (expected %s)", label, rf.Type, l.expected())}
	}
	of := yamlnode.Lookup(n, "of")
	if of != nil && f.Kind != List {
		return nil, &Error{Path: path, Line: of.Line, Msg: label + ": of: applies only to type: list"}
	}

	if f.Title && (f.Kind != String || within != "") {
		return nil, &Error{Path: path, Line: n.Line, Msg: label + ": title: true is only allowed on a top-level string field"}
	}
	if f.Body && (f.Kind != Text || within != "") {
		return nil, &Error{Path: path, Line: n.Line, Msg: label + ": body: true is only allowed on a top-level text field"}
	}
	switch f.Kind {
	case Link:
		if f.Cardinality != "one" && f.Cardinality != "many" {
			return nil, &Error{Path: path, Line: n.Line, Msg: fmt.Sprintf("%s: link cardinality must be one or many, got %q", label, f.Cardinality)}
		}
	case List:
		sub := yamlnode.Lookup(n, "fields")
		switch {
		case of != nil && sub != nil:
			return nil, &Error{Path: path, Line: of.Line, Msg: label + ": a list takes fields: or of:, not both"}
		case of != nil:
			if err := l.listOf(path, f, of, label); err != nil {
				return nil, err
			}
		case sub == nil || sub.Kind != yaml.SequenceNode || len(sub.Content) == 0:
			return nil, &Error{Path: path, Line: n.Line, Msg: label + ": a list needs fields: for its rows, or of: naming its item type"}
		default:
			f.Fields = l.fields(path, sub.Content, fmt.Sprintf("list %q", rf.Name))
		}
	}
	l.checkFieldKeys(path, n, f, label)
	return f, nil
}

// listOf resolves a list's of: — a built-in item type, an enum, or a record
// for its rows.
func (l *loader) listOf(path string, f *Field, of *yaml.Node, label string) error {
	name := of.Value
	fail := func(msg string) error { return &Error{Path: path, Line: of.Line, Msg: label + ": " + msg} }
	switch {
	case of.Kind != yaml.ScalarNode || name == "":
		return fail("of: must name a type")
	case slices.Contains(itemKinds, Kind(name)):
		f.Elem = &Field{Name: f.Name, Kind: Kind(name), Line: of.Line}
	case l.Enums[name] != nil:
		f.Elem = &Field{Name: f.Name, Kind: Enum, EnumName: name, Line: of.Line}
	case l.Records[name] != nil:
		if i := slices.Index(l.resolving, name); i >= 0 {
			chain := append(slices.Clone(l.resolving[i:]), name)
			return fail(fmt.Sprintf("record %s contains itself (%s)", name, strings.Join(chain, " → ")))
		}
		f.Record, f.Fields = name, l.record(name).Fields
	case Kind(name) == Link:
		return fail("a list of links is declared as type: link with cardinality: many")
	case l.Types[name] != nil:
		return fail(fmt.Sprintf("a list cannot hold %s entities; link to them with type: link, target: [%s], cardinality: many", name, name))
	default:
		return fail(fmt.Sprintf("unknown list item type %q (expected %s)", name, l.expectedItems()))
	}
	return nil
}

func isBuiltin(name string) bool {
	for _, k := range builtinKinds {
		if string(k) == name {
			return true
		}
	}
	return false
}

func (s *Schema) expected() string {
	var kinds []string
	for _, k := range builtinKinds {
		kinds = append(kinds, string(k))
	}
	msg := strings.Join(kinds, ", ")
	if len(s.Enums) == 0 {
		return msg
	}
	return msg + ", or an enum: " + strings.Join(slices.Sorted(maps.Keys(s.Enums)), ", ")
}

// expectedItems lists what a list's of: may name.
func (s *Schema) expectedItems() string {
	var kinds []string
	for _, k := range itemKinds {
		kinds = append(kinds, string(k))
	}
	msg := strings.Join(kinds, ", ")
	for _, named := range []struct {
		what  string
		names []string
	}{{"an enum", slices.Sorted(maps.Keys(s.Enums))}, {"a record", slices.Sorted(maps.Keys(s.Records))}} {
		if len(named.names) > 0 {
			msg += ", or " + named.what + ": " + strings.Join(named.names, ", ")
		}
	}
	return msg
}

// source reads a type's source: mapping (DES-0053).
func (s *Schema) source(path, typeName string, n *yaml.Node) (*Source, error) {
	label := fmt.Sprintf("type %s: source", typeName)
	if n.Kind != yaml.MappingNode {
		return nil, &Error{Path: path, Line: n.Line, Msg: label + " must be a mapping with csv: and id:"}
	}
	s.checkKeys(path, n, label, sourceKeys)
	src := &Source{Delimiter: ',', Separator: ";", Line: n.Line}
	str := func(key string) (string, int, error) {
		v := yamlnode.Lookup(n, key)
		if v == nil {
			return "", 0, nil
		}
		if v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
			return "", v.Line, &Error{Path: path, Line: v.Line, Msg: fmt.Sprintf("%s: %s must be a string", label, key)}
		}
		return v.Value, v.Line, nil
	}
	csv, line, err := str("csv")
	if err != nil {
		return nil, err
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(csv)))
	switch {
	case csv == "":
		return nil, &Error{Path: path, Line: n.Line, Msg: label + ": csv: names the file, a path from the repository root"}
	case filepath.IsAbs(csv) || strings.HasPrefix(csv, "/") || clean == ".." || strings.HasPrefix(clean, "../") || clean != csv:
		return nil, &Error{Path: path, Line: line, Msg: fmt.Sprintf("%s: csv: %q must be a path from the repository root, written plainly (%s)", label, csv, clean)}
	}
	src.CSV = csv
	if src.ID, _, err = str("id"); err != nil {
		return nil, err
	}
	if src.ID == "" {
		return nil, &Error{Path: path, Line: n.Line, Msg: label + ": id: names the column holding each row's ID"}
	}
	d, line, err := str("delimiter")
	if err != nil {
		return nil, err
	}
	if d != "" {
		r := []rune(d)
		if len(r) != 1 || r[0] == '"' || r[0] == '\r' || r[0] == '\n' || r[0] == 0xFFFD {
			return nil, &Error{Path: path, Line: line, Msg: fmt.Sprintf("%s: delimiter: %q must be one character, not a quote or a line break", label, d)}
		}
		src.Delimiter = r[0]
	}
	sep, line, err := str("separator")
	if err != nil {
		return nil, err
	}
	if sep != "" {
		if strings.TrimSpace(sep) != sep || strings.ContainsAny(sep, "\r\n") || strings.ContainsRune(sep, src.Delimiter) {
			return nil, &Error{Path: path, Line: line, Msg: fmt.Sprintf("%s: separator: %q must not be blank, hold a line break or the delimiter", label, sep)}
		}
		src.Separator = sep
	}
	return src, nil
}

// columns checks the fields of t against its source: every field has a
// form in a cell, and no two read the same column. column: on a type
// without a source applies to nothing.
func (l *loader) columns(t *Type, nodes []*yaml.Node) {
	if t.Source == nil {
		for _, f := range t.Fields {
			if f.Column != "" {
				l.KeyProblems = append(l.KeyProblems, &Error{Path: t.File, Line: f.Line,
					Msg: fmt.Sprintf("field %q: column applies only to a field of a type with a source", f.Name)})
			}
		}
		return
	}
	fail := func(f *Field, msg string) {
		l.keep(&Error{Path: t.File, Line: f.Line, Msg: fmt.Sprintf("field %q: %s", f.Name, msg)})
	}
	read := map[string]*Field{}
	for _, f := range t.Fields {
		switch {
		case f.Body:
			fail(f, "a row read from a CSV file has no Markdown body; leave out body: true")
			continue
		case f.Kind == List && f.Elem == nil:
			fail(f, "a list of rows has no form in a CSV cell; a type with a source can list only values (of: a built-in type or an enum)")
			continue
		case f.Kind == Calculated:
			if f.Column != "" {
				fail(f, "a calculated field is never read from the file, so it has no column")
			}
			continue
		}
		col := f.ColumnName()
		if col == t.Source.ID {
			fail(f, fmt.Sprintf("column %q is the ID column (source id:)", col))
			continue
		}
		if prev := read[col]; prev != nil {
			fail(f, fmt.Sprintf("column %q is already read by field %q", col, prev.Name))
			continue
		}
		read[col] = f
	}
}
