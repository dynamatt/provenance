package entity

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/util"
	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/assets"
	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Markdown is the record of a Markdown entity file: the values of its YAML
// frontmatter, typed by their YAML tags, and the Markdown body below it.
type Markdown struct {
	path string
	// Front is the frontmatter's top-level mapping node. Node line numbers
	// are file line numbers.
	Front    *yaml.Node
	body     string
	bodyLine int
}

func (m *Markdown) Value(f *schema.Field) *Written { return yamlValues{m.Front}.Value(f) }
func (m *Markdown) Keys() []Key                    { return yamlValues{m.Front}.Keys() }
func (m *Markdown) IDLine() int                    { return yamlnode.Lookup(m.Front, "id").Line }
func (m *Markdown) TypeLine() int                  { return yamlnode.Lookup(m.Front, "type").Line }
func (m *Markdown) Body() (string, int)            { return m.body, m.bodyLine }

// Refs parses text as Markdown: its embeds, wikilinks, query blocks,
// images and fenced blocks. A misplaced caption or an image that is not a
// repository file is a TextError, and only the code blocks are returned.
func (m *Markdown) Refs(text string) (*Refs, error) {
	refs := &Refs{}
	for _, b := range markdown.CodeBlocks(text) {
		refs.CodeBlocks = append(refs.CodeBlocks, Block{Lang: b.Lang, Line: b.Line})
	}
	opts := markdown.Options{
		Fences: map[string]markdown.FenceFunc{"query": func(_ util.BufWriter, f markdown.Fence) error {
			refs.Queries = append(refs.Queries, Block{Lang: f.Lang, Source: f.Source, Line: f.Line})
			return nil
		}},
		Image: func(dest string) (string, error) {
			rel, inline, err := assets.Resolve(m.path, dest)
			if err == nil && !inline {
				refs.Assets = append(refs.Assets, rel)
			}
			return dest, err
		},
	}
	if _, err := markdown.Convert(text, (*refRecorder)(refs), opts); err != nil {
		var me *markdown.Error
		if errors.As(err, &me) {
			err = &TextError{Line: me.Line, Msg: me.Msg}
		}
		return &Refs{CodeBlocks: refs.CodeBlocks}, err
	}
	return refs, nil
}

// refRecorder is a Markdown resolver that only notes what a text refers to.
type refRecorder Refs

func (r *refRecorder) CaptionStart(util.BufWriter, markdown.Caption) error { return nil }
func (r *refRecorder) CaptionEnd(util.BufWriter, markdown.Caption) error   { return nil }

func (r *refRecorder) Reference(_ util.BufWriter, l markdown.Link) error {
	if l.Local == "" {
		r.Cited = append(r.Cited, l.ID)
	}
	return nil
}

// MisplacedEmbed renders as a reference, so it is a citation too.
func (r *refRecorder) MisplacedEmbed(_ util.BufWriter, l markdown.Link) error {
	r.Cited = append(r.Cited, l.ID)
	return nil
}

func (r *refRecorder) Embed(_ util.BufWriter, l markdown.Link) error {
	r.Embeds = append(r.Embeds, l.ID)
	return nil
}

// yamlValues are the values of a YAML mapping: frontmatter or a list row.
type yamlValues struct{ m *yaml.Node }

func (y yamlValues) Value(f *schema.Field) *Written {
	if n := yamlnode.Lookup(y.m, f.Name); n != nil {
		return written(n)
	}
	return nil
}

func (y yamlValues) Keys() []Key {
	var out []Key
	for i := 0; i+1 < len(y.m.Content); i += 2 {
		k := y.m.Content[i]
		out = append(out, Key{Name: k.Value, Line: k.Line, Set: y.m.Content[i+1].Tag != "!!null"})
	}
	return out
}

// written is n as a value, of the kind its YAML tag says.
func written(n *yaml.Node) *Written {
	w := &Written{Text: n.Value, Line: n.Line, TextLine: n.Line}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			w.TextLine = n.Line + 1 // content starts after the | or > line
		}
		switch n.Tag {
		case "!!null":
			w.Kind = Null
		case "!!str":
			w.Kind = Text
		case "!!int", "!!float":
			w.Kind, w.Text = Number, strings.ReplaceAll(n.Value, "_", "")
			w.Source = func() string { return n.Value }
		case "!!bool":
			w.Kind, w.Text = Boolean, strconv.FormatBool(n.Value == "true" || n.Value == "True" || n.Value == "TRUE")
			w.Source = func() string { return n.Value }
		default:
			w.Kind = Scalar
		}
		return w
	case yaml.SequenceNode:
		w.Kind = List
		for _, c := range n.Content {
			w.Items = append(w.Items, written(c))
		}
	case yaml.MappingNode:
		w.Kind, w.Row = Mapping, yamlValues{n}
	default:
		w.Kind = Other
	}
	w.Text, w.Source = "", func() string { return source(n) }
	return w
}

// source renders a node back to YAML text for display. Marshalling
// rewrites the tags of n's nodes, so it is done only for a message, after
// every value is read.
func source(n *yaml.Node) string {
	b, err := yaml.Marshal(n)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Parse parses one Markdown file. It returns nil, nil when the file is not an
// entity: no frontmatter, or frontmatter without both id and type.
// Frontmatter that is not valid YAML is an error — the file might be an
// entity, and silently leaving it out of an export would lose it.
func Parse(path string, raw []byte) (*Entity, error) {
	e, _, err := ParseFile(path, raw)
	return e, err
}

// ParseFile is Parse, also returning partial: frontmatter that declares one
// of id and type but not the other, which Parse leaves out as not an
// entity, and validate reports.
func ParseFile(path string, raw []byte) (e *Entity, partial *Error, err error) {
	front, body, bodyLine, ok := split(raw)
	if !ok {
		return nil, nil, nil
	}
	var doc yaml.Node
	if err := yamlnode.Unmarshal(front, 2, &doc); err != nil { // after the opening ---
		return nil, nil, yamlError(path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, nil
	}
	m := doc.Content[0]
	idNode, typeNode := yamlnode.Lookup(m, "id"), yamlnode.Lookup(m, "type")
	switch {
	case idNode == nil && typeNode == nil:
		return nil, nil, nil
	case typeNode == nil:
		return nil, &Error{Path: path, Line: idNode.Line, Msg: "frontmatter has an id but no type, so the file is not read as an entity"}, nil
	case idNode == nil:
		return nil, &Error{Path: path, Line: typeNode.Line, Msg: "frontmatter has a type but no id, so the file is not read as an entity"}, nil
	}
	for _, f := range []struct {
		key  string
		node *yaml.Node
	}{{"id", idNode}, {"type", typeNode}} {
		if f.node.Kind != yaml.ScalarNode || f.node.Tag != "!!str" || strings.TrimSpace(f.node.Value) == "" {
			return nil, nil, &Error{Path: path, Line: f.node.Line, Msg: f.key + " must be a non-empty string"}
		}
	}
	return &Entity{
		ID:     idNode.Value,
		Type:   typeNode.Value,
		Path:   path,
		Record: &Markdown{path: path, Front: m, body: body, bodyLine: bodyLine},
	}, nil, nil
}

// split separates frontmatter from body. Frontmatter is a first line of
// exactly "---" and runs to the next line of exactly "---"; a UTF-8 BOM and
// CRLF line endings are tolerated.
func split(raw []byte) (front []byte, body string, bodyLine int, ok bool) {
	text := bytes.TrimPrefix(raw, utf8BOM)
	lines := bytes.SplitAfter(text, []byte("\n"))
	if len(lines) == 0 || !isDelimiter(lines[0]) {
		return nil, "", 0, false
	}
	for i := 1; i < len(lines); i++ {
		if isDelimiter(lines[i]) {
			return bytes.Join(lines[1:i], nil), string(bytes.Join(lines[i+1:], nil)), i + 2, true
		}
	}
	return nil, "", 0, false
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func isDelimiter(line []byte) bool {
	return string(bytes.TrimRight(line, "\r\n")) == "---"
}

func yamlError(path string, err error) error {
	line, msg := yamlnode.ErrorLine(err)
	return &Error{Path: path, Line: line, Msg: "invalid frontmatter: " + msg}
}
