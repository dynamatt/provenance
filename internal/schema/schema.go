// Package schema loads entity type and enum declarations (schema/*.yaml and
// schema/enums/*.yaml) into the type model of Detailed Design §5.
//
// Only problems that prevent the schema from being interpreted are load
// errors: YAML syntax, unknown field types, duplicate names, ambiguous body
// fields, links without a cardinality. Everything else — a link target that
// names no type, a formula that does not parse — is schema meta-validation,
// reported by validate.
package schema

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	// Fields are a list's row sub-fields.
	Fields  []*Field
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
// inside a list's rows; the facet then comes from the entity holding the row.
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

// Schema is the full set of declarations.
type Schema struct {
	Types map[string]*Type
	Enums map[string]*EnumType
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

// Load reads every declaration under root/schema. A repository without a
// schema folder has no types.
func Load(root string) (*Schema, error) {
	s := &Schema{Types: map[string]*Type{}, Enums: map[string]*EnumType{}}

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
	typeFiles, err := yamlFiles(root, Dir)
	if err != nil {
		return nil, err
	}
	var raws []*rawTypeFile
	for _, rel := range typeFiles {
		rt, err := readType(root, rel)
		if err != nil {
			return nil, err
		}
		if prev, dup := s.Types[rt.name]; dup {
			return nil, &Error{Path: rel, Line: rt.nameLine, Msg: fmt.Sprintf("type %s is already declared in %s", rt.name, prev.File)}
		}
		if e, clash := s.Enums[rt.name]; clash {
			return nil, &Error{Path: rel, Line: rt.nameLine, Msg: fmt.Sprintf("type %s has the same name as the enum declared in %s", rt.name, e.File)}
		}
		s.Types[rt.name] = &Type{Name: rt.name, IDPrefix: rt.idPrefix, File: rel}
		raws = append(raws, rt)
	}
	for _, rt := range raws {
		t := s.Types[rt.name]
		fields, err := s.fields(rt.path, rt.fields, "")
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
					return &Error{Path: src.File, Line: l.Field.Line, Msg: fmt.Sprintf(
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

type rawTypeFile struct {
	path, name, idPrefix string
	nameLine             int
	fields               []*yaml.Node
}

func readType(root, rel string) (*rawTypeFile, error) {
	m, err := readMapping(root, rel)
	if err != nil {
		return nil, err
	}
	name := lookup(m, "type")
	if name == nil || name.Kind != yaml.ScalarNode || name.Value == "" {
		return nil, &Error{Path: rel, Line: m.Line, Msg: "missing type name (type: <Name>)"}
	}
	rt := &rawTypeFile{path: rel, name: name.Value, nameLine: name.Line}
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

// fields converts field declarations. within names the enclosing list field
// for sub-fields, for error messages.
func (s *Schema) fields(path string, nodes []*yaml.Node, within string) ([]*Field, error) {
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
			label = fmt.Sprintf("field %q in list %q", rf.Name, within)
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
		case s.Enums[rf.Type] != nil:
			f.Kind, f.EnumName = Enum, rf.Type
		default:
			return nil, &Error{Path: path, Line: typeLine, Msg: fmt.Sprintf("%s: unknown type %q (expected %s)", label, rf.Type, s.expected())}
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
			if sub == nil || sub.Kind != yaml.SequenceNode || len(sub.Content) == 0 {
				return nil, &Error{Path: path, Line: n.Line, Msg: label + ": a list needs fields for its rows"}
			}
			rows, err := s.fields(path, sub.Content, rf.Name)
			if err != nil {
				return nil, err
			}
			f.Fields = rows
		}
		out = append(out, f)
	}
	return out, nil
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
	names := make([]string, 0, len(s.Enums))
	for n := range s.Enums {
		names = append(names, n)
	}
	sort.Strings(names)
	return msg + ", or an enum: " + strings.Join(names, ", ")
}
