package entity

import (
	"bytes"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/schema"
)

// Format is the file in canonical form against its type t (DES-0038): the
// frontmatter's keys in schema order, one field per line, lists in block
// style, empty fields left out and each value written one way; the body
// as written but for LF line endings and one final newline. A value that
// is not of its field's type, and a key t does not declare, is kept as
// written: validate reports it, and formatting it could change it.
func (m *Markdown) Format(t *schema.Type) ([]byte, error) { return Format(m.raw, t) }

// Raw is the file as read.
func (m *Markdown) Raw() []byte { return m.raw }

// Format is Markdown.Format of raw, a Markdown entity file.
func Format(raw []byte, t *schema.Type) ([]byte, error) {
	front, body, _, ok := split(raw)
	if !ok {
		return nil, errors.New("no frontmatter")
	}
	front = bytes.ReplaceAll(front, []byte("\r\n"), []byte("\n"))
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("frontmatter is not a mapping")
	}
	f := formatter{lines: strings.Split(string(front), "\n")}
	fields := append([]*schema.Field{{Name: "id", Kind: schema.String}, {Name: "type", Kind: schema.String}}, t.Fields...)
	f.mapping(doc.Content[0], fields)

	var out bytes.Buffer
	out.WriteString("---\n")
	d, err := yaml.NewDumper(&out, yaml.WithV3Defaults(), yaml.WithLineWidth(-1))
	if err != nil {
		return nil, err
	}
	d.SetIndent(2)
	if err := d.Dump(&doc); err != nil {
		return nil, err
	}
	if err := d.Close(); err != nil {
		return nil, err
	}
	out.WriteString("---\n")
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if strings.TrimRight(body, "\n") != "" {
		out.WriteString(strings.TrimRight(body, "\n") + "\n")
	}
	return out.Bytes(), nil
}

// formatter rewrites a frontmatter's nodes in place. lines are the
// frontmatter's, for the line breaks a folded value was written with.
type formatter struct{ lines []string }

// mapping orders m's keys: fields in order, then the keys fields don't
// declare, in the order written. A declared field with an empty value is
// left out, unless a comment is written on it: fmt drops no comment.
func (f formatter) mapping(m *yaml.Node, fields []*schema.Field) {
	m.Style = 0
	type pair struct{ k, v *yaml.Node }
	var pairs []pair
	for i := 0; i+1 < len(m.Content); i += 2 {
		pairs = append(pairs, pair{m.Content[i], m.Content[i+1]})
	}
	var out []*yaml.Node
	used := make([]bool, len(pairs))
	for _, fd := range fields {
		for i, p := range pairs {
			if used[i] || p.k.Value != fd.Name {
				continue
			}
			used[i] = true
			if empty(p.v) && !commented(p.k, p.v) {
				continue
			}
			p.k.Style = 0
			if p.v.Kind != yaml.ScalarNode && p.v.LineComment != "" {
				// A flow list's comment, which a block list has no line for.
				p.k.LineComment = strings.TrimSpace(p.k.LineComment + " " + p.v.LineComment)
				p.v.LineComment = ""
			}
			f.value(fd, p.v)
			out = append(out, p.k, p.v)
		}
	}
	for i, p := range pairs {
		if !used[i] {
			out = append(out, p.k, p.v)
		}
	}
	m.Content = out
}

// value formats n as a value of fd. A value not of fd's type is kept.
func (f formatter) value(fd *schema.Field, n *yaml.Node) {
	switch {
	case fd.Kind == schema.Calculated:
	case fd.Kind == schema.Link && fd.Cardinality != "one", fd.Kind == schema.List:
		if n.Kind != yaml.SequenceNode {
			return
		}
		n.Style = 0
		for _, item := range n.Content {
			switch {
			case fd.Kind == schema.Link:
				f.scalar(schema.String, item)
			case fd.Elem != nil:
				f.value(fd.Elem, item)
			case item.Kind == yaml.MappingNode:
				if item.LineComment != "" && len(item.Content) > 0 {
					// A flow row's comment goes on its first key's line.
					k := item.Content[0]
					k.LineComment = strings.TrimSpace(k.LineComment + " " + item.LineComment)
					item.LineComment = ""
				}
				f.mapping(item, fd.Fields)
			}
		}
	case fd.Kind == schema.Link:
		f.scalar(schema.String, n)
	default:
		f.scalar(fd.Kind, n)
	}
}

// scalar writes n, a value of kind, one way (DES-0038): plain unless YAML
// would read another value, a value with line breaks a literal block.
func (f formatter) scalar(kind schema.Kind, n *yaml.Node) {
	if n.Kind != yaml.ScalarNode {
		return
	}
	switch kind {
	case schema.Number:
		if n.Tag != "!!int" && n.Tag != "!!float" {
			return
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64)
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return
		}
		n.Value, n.Style = strconv.FormatFloat(v, 'f', -1, 64), 0
		n.Tag = "!!int"
		if strings.Contains(n.Value, ".") {
			n.Tag = "!!float"
		}
	case schema.Boolean:
		if n.Tag == "!!bool" {
			n.Value, n.Style = strconv.FormatBool(strings.EqualFold(n.Value, "true")), 0
		}
	case schema.Date:
		if _, err := time.Parse("2006-01-02", n.Value); err == nil && (n.Tag == "!!str" || n.Tag == "!!timestamp") {
			n.Tag, n.Style = "!!timestamp", 0
		}
	case schema.String, schema.Text, schema.Enum:
		if n.Tag != "!!str" {
			return
		}
		if kind == schema.Text && n.Style&yaml.FoldedStyle != 0 {
			n.Value = f.unfolded(n)
		}
		switch {
		case strings.Contains(n.Value, "\n"):
			n.Style = yaml.LiteralStyle
		case plain(n.Value):
			n.Style = 0
		default:
			n.Style = yaml.DoubleQuotedStyle
		}
	}
}

// unfolded is a folded text value with the line breaks it was written
// with (DES-0038): YAML folds a written break into a space, and Markdown
// renders a break inside a paragraph as one. It is n's value when the
// block can't be read back that way: a keep (+) or indentation indicator,
// or lines that would fold to another value.
func (f formatter) unfolded(n *yaml.Node) string {
	if n.Line < 1 || n.Line > len(f.lines) {
		return n.Value
	}
	header := strings.TrimSpace(f.lines[n.Line-1][min(n.Column-1, len(f.lines[n.Line-1])):])
	header, _, _ = strings.Cut(header, " #")
	if header != ">" && header != ">-" {
		return n.Value
	}
	indent := -1
	var lines []string
	for _, l := range f.lines[n.Line:] {
		trimmed := strings.TrimLeft(l, " ")
		if trimmed == "" {
			lines = append(lines, "")
			continue
		}
		at := len(l) - len(trimmed)
		if indent < 0 {
			indent = at
		}
		if at < indent {
			break
		}
		lines = append(lines, l[indent:])
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return n.Value
	}
	text := strings.Join(lines, "\n")
	if header == ">" {
		text += "\n"
	}
	if Fold(text) != n.Value {
		return n.Value
	}
	return text
}

// Fold is text as a YAML folded block reads it: a line break between two
// lines of a paragraph is a space. It is how DES-0038 compares a folded
// text value with the literal block fmt writes for it.
func Fold(text string) string {
	if !strings.Contains(strings.TrimRight(text, "\n"), "\n") {
		return text
	}
	chomp := "-"
	if strings.HasSuffix(text, "\n") {
		chomp = ""
	}
	var b strings.Builder
	b.WriteString("v: >" + chomp + "\n")
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if l != "" {
			b.WriteString("  " + l)
		}
		b.WriteString("\n")
	}
	var v struct{ V string }
	if err := yaml.Unmarshal([]byte(b.String()), &v); err != nil {
		return text
	}
	return v.V
}

// plain reports whether s can be written as a plain scalar and read back
// as the string s, by YAML 1.2 and by readers of YAML 1.1, which read yes,
// no, on and off as booleans.
func plain(s string) bool {
	switch strings.ToLower(s) {
	case "y", "n", "yes", "no", "on", "off":
		return false
	}
	b, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s})
	return err == nil && !bytes.HasPrefix(b, []byte("'")) && !bytes.HasPrefix(b, []byte(`"`)) &&
		!bytes.HasPrefix(b, []byte("|")) && !bytes.HasPrefix(b, []byte(">"))
}

// empty reports a value written as nothing: null, a blank string or an
// empty list, which reads as an omitted field (DES-0010).
func empty(n *yaml.Node) bool {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Tag == "!!null" || n.Tag == "!!str" && strings.TrimSpace(n.Value) == ""
	case yaml.SequenceNode:
		return len(n.Content) == 0
	}
	return false
}

func commented(nodes ...*yaml.Node) bool {
	for _, n := range nodes {
		if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
			return true
		}
	}
	return false
}
