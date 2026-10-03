// Package schema loads entity type, enum and record declarations
// (schema/*.yaml, schema/enums/*.yaml and schema/records/*.yaml) into the type
// model of Detailed Design §5.
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
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
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
	Help     string
	// Target and Cardinality ("one" or "many") apply to links.
	Target      []string
	Cardinality string
	ReverseName string
	// A list holds either rows or items. Rows have the sub-fields Fields:
	// declared inline under fields:, or a record's fields when the list names
	// one with of: (Record is then its name). Items have the single type
	// Elem, set when of: names a built-in type or an enum; Elem carries the
	// list's name.
	Fields  []*Field
	Record  string
	Elem    *Field
	Formula string
	// Default is kept as declared. Whether it applies to entities that omit
	// the field is not yet settled, so it is not applied when reading.
	Default *yaml.Node
	Line    int
}

// Type is one entity type.
type Type struct {
	Name     string
	IDPrefix string
	Fields   []*Field
	// BodyField is the text field declared body: true, or nil when the body
	// is freeform.
	BodyField *Field
	File      string
	// Facets are the incoming link facets other types' reverse_name
	// declarations give this type, in name order.
	Facets []*Facet
}

// Facet is an incoming link facet: every link field, on any type, whose
// reverse_name is Name and whose targets include the facet's type.
type Facet struct {
	Name    string
	Sources []FacetSource
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
}

// RecordType is a named row shape that lists use with of:, declared once
// and shared by every list that names it.
type RecordType struct {
	Name   string
	Fields []*Field
	File   string
}

// Schema is the full set of declarations.
type Schema struct {
	Types   map[string]*Type
	Enums   map[string]*EnumType
	Records map[string]*RecordType
}

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

// Dir is the schema folder at the repository root.
const Dir = "schema"

// loader resolves field declarations. Records may name other records, so
// each is resolved on first use; pending holds those not yet resolved and
// resolving the chain being resolved, outermost first.
type loader struct {
	*Schema
	pending   map[string]*rawDecl
	resolving []string
}

// Load reads every declaration under root/schema. A repository without a
// schema folder has no types.
func Load(root string) (*Schema, error) {
	s := &Schema{Types: map[string]*Type{}, Enums: map[string]*EnumType{}, Records: map[string]*RecordType{}}
	l := &loader{Schema: s, pending: map[string]*rawDecl{}}

	// Enums first: field types are resolved against them.
	enumFiles, err := yamlFiles(root, filepath.Join(Dir, "enums"))
	if err != nil {
		return nil, err
	}
	for _, rel := range enumFiles {
		if err := s.loadEnum(root, rel); err != nil {
			return nil, err
		}
	}
	// Every name is registered before any fields are resolved, so a list's
	// of: can name a record declared in any file.
	recordFiles, err := yamlFiles(root, filepath.Join(Dir, "records"))
	if err != nil {
		return nil, err
	}
	var records []string
	for _, rel := range recordFiles {
		rr, err := readDecl(root, rel, "record")
		if err != nil {
			return nil, err
		}
		if err := s.checkName("record", rr); err != nil {
			return nil, err
		}
		s.Records[rr.name] = &RecordType{Name: rr.name, File: rel}
		l.pending[rr.name] = rr
		records = append(records, rr.name)
	}
	typeFiles, err := yamlFiles(root, Dir)
	if err != nil {
		return nil, err
	}
	var raws []*rawDecl
	for _, rel := range typeFiles {
		rt, err := readDecl(root, rel, "type")
		if err != nil {
			return nil, err
		}
		if err := s.checkName("type", rt); err != nil {
			return nil, err
		}
		s.Types[rt.name] = &Type{Name: rt.name, IDPrefix: rt.idPrefix, File: rel}
		raws = append(raws, rt)
	}
	// Resolve every record, used or not, so a broken one is reported.
	for _, name := range records {
		if _, err := l.record(name); err != nil {
			return nil, err
		}
	}
	for _, rt := range raws {
		t := s.Types[rt.name]
		fields, err := l.fields(rt.path, rt.fields, "")
		if err != nil {
			return nil, err
		}
		t.Fields = fields
		for _, f := range fields {
			if !f.Body {
				continue
			}
			if t.BodyField != nil {
				return nil, &Error{Path: rt.path, Line: f.Line, Msg: fmt.Sprintf("field %q: only one field may set body: true (%q already does)", f.Name, t.BodyField.Name)}
			}
			t.BodyField = f
		}
	}
	if err := s.facets(); err != nil {
		return nil, err
	}
	return s, nil
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
func (l *loader) record(name string) (*RecordType, error) {
	r := l.Records[name]
	raw, pending := l.pending[name]
	if !pending {
		return r, nil
	}
	l.resolving = append(l.resolving, name)
	fields, err := l.fields(raw.path, raw.fields, fmt.Sprintf("record %q", name))
	l.resolving = l.resolving[:len(l.resolving)-1]
	if err != nil {
		return nil, err
	}
	r.Fields = fields
	delete(l.pending, name)
	return r, nil
}

// facets derives every type's incoming facets from reverse_name
// declarations. A link is declared once, on its source side (Detailed Design
// §6), so a reverse_name that repeats a field declared on the target type is
// a load error: the same relationship would be stored twice. Targets naming
// no declared type are left to validate.
func (s *Schema) facets() error {
	names := make([]string, 0, len(s.Types))
	for n := range s.Types {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		src := s.Types[n]
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
			for _, tn := range l.Field.Target {
				target := s.Types[tn]
				if target == nil {
					continue
				}
				if clash := target.Field(l.Field.ReverseName); clash != nil {
					path := src.File
					if l.List != nil && l.List.Record != "" {
						path = s.Records[l.List.Record].File
					}
					return &Error{Path: path, Line: l.Field.Line, Msg: fmt.Sprintf(
						"field %q: reverse_name %q repeats the field %s declared on %s (%s:%d); declare a link once, on its source side",
						l.Field.Name, l.Field.ReverseName, clash.Name, target.Name, target.File, clash.Line)}
				}
				target.addFacet(l.Field.ReverseName, l)
			}
		}
	}
	for _, t := range s.Types {
		sort.Slice(t.Facets, func(i, j int) bool { return t.Facets[i].Name < t.Facets[j].Name })
	}
	return nil
}

func (t *Type) addFacet(name string, src FacetSource) {
	for _, f := range t.Facets {
		if f.Name == name {
			f.Sources = append(f.Sources, src)
			return
		}
	}
	t.Facets = append(t.Facets, &Facet{Name: name, Sources: []FacetSource{src}})
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
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, yamlError(rel, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, &Error{Path: rel, Msg: "expected a mapping of keys to values"}
	}
	return doc.Content[0], nil
}

func yamlError(path string, err error) error {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	// Type mismatches arrive as "unmarshal errors:\n  line N: ..."; report
	// the first.
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "unmarshal errors:"))
	msg, _, _ = strings.Cut(msg, "\n")
	var line int
	if n, _ := fmt.Sscanf(msg, "line %d:", &line); n == 1 {
		return &Error{Path: path, Line: line, Msg: strings.TrimSpace(strings.TrimPrefix(msg, fmt.Sprintf("line %d:", line)))}
	}
	return &Error{Path: path, Msg: msg}
}

func lookup(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
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
	line := lookup(m, "enum").Line
	if isBuiltin(raw.Enum) {
		return &Error{Path: rel, Line: line, Msg: fmt.Sprintf("enum %s has the same name as a built-in field type", raw.Enum)}
	}
	if prev, dup := s.Enums[raw.Enum]; dup {
		return &Error{Path: rel, Line: line, Msg: fmt.Sprintf("enum %s is already declared in %s", raw.Enum, prev.File)}
	}
	s.Enums[raw.Enum] = &EnumType{Name: raw.Enum, Values: raw.Values, File: rel}
	return nil
}

// rawDecl is a type or record declaration before its fields are resolved.
type rawDecl struct {
	path, name, idPrefix string
	nameLine             int
	fields               []*yaml.Node
}

// readDecl reads a declaration whose name is under key: "type" or "record".
func readDecl(root, rel, key string) (*rawDecl, error) {
	m, err := readMapping(root, rel)
	if err != nil {
		return nil, err
	}
	name := lookup(m, key)
	if name == nil || name.Kind != yaml.ScalarNode || name.Value == "" {
		return nil, &Error{Path: rel, Line: m.Line, Msg: fmt.Sprintf("missing %s name (%s: <Name>)", key, key)}
	}
	rt := &rawDecl{path: rel, name: name.Value, nameLine: name.Line}
	if p := lookup(m, "id_prefix"); p != nil {
		rt.idPrefix = p.Value
	}
	if f := lookup(m, "fields"); f != nil {
		if f.Kind != yaml.SequenceNode {
			return nil, &Error{Path: rel, Line: f.Line, Msg: "fields must be a list"}
		}
		rt.fields = f.Content
	}
	return rt, nil
}

type rawField struct {
	Name        string    `yaml:"name"`
	Type        string    `yaml:"type"`
	Required    bool      `yaml:"required"`
	Body        bool      `yaml:"body"`
	Help        string    `yaml:"help"`
	Target      []string  `yaml:"target"`
	Cardinality string    `yaml:"cardinality"`
	ReverseName string    `yaml:"reverse_name"`
	Formula     string    `yaml:"formula"`
	Default     yaml.Node `yaml:"default"`
}

// fields converts field declarations. within names what encloses sub-fields
// (`list "rows"`, `record "Equipment"`) for error messages; it is empty for a
// type's own fields.
func (l *loader) fields(path string, nodes []*yaml.Node, within string) ([]*Field, error) {
	var out []*Field
	seen := map[string]int{}
	for _, n := range nodes {
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
		if prev, dup := seen[rf.Name]; dup {
			return nil, &Error{Path: path, Line: n.Line, Msg: fmt.Sprintf("%s is declared twice (first at line %d)", label, prev)}
		}
		seen[rf.Name] = n.Line

		f := &Field{
			Name: rf.Name, Required: rf.Required, Body: rf.Body, Help: rf.Help,
			Target: rf.Target, Cardinality: rf.Cardinality, ReverseName: rf.ReverseName,
			Formula: rf.Formula, Line: n.Line,
		}
		if rf.Default.Kind != 0 {
			d := rf.Default
			f.Default = &d
		}
		typeLine := n.Line
		if tn := lookup(n, "type"); tn != nil {
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
		of := lookup(n, "of")
		if of != nil && f.Kind != List {
			return nil, &Error{Path: path, Line: of.Line, Msg: label + ": of: applies only to type: list"}
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
			sub := lookup(n, "fields")
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
				rows, err := l.fields(path, sub.Content, fmt.Sprintf("list %q", rf.Name))
				if err != nil {
					return nil, err
				}
				f.Fields = rows
			}
		}
		out = append(out, f)
	}
	return out, nil
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
		r, err := l.record(name)
		if err != nil {
			return err
		}
		f.Record, f.Fields = name, r.Fields
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
	return msg + ", or an enum: " + strings.Join(sortedKeys(s.Enums), ", ")
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
	}{{"an enum", sortedKeys(s.Enums)}, {"a record", sortedKeys(s.Records)}} {
		if len(named.names) > 0 {
			msg += ", or " + named.what + ": " + strings.Join(named.names, ", ")
		}
	}
	return msg
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
