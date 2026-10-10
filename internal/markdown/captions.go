package markdown

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.yaml.in/yaml/v3"

	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Captions (DES-0034) are written at the place a figure, table or
// equation is used, as a ```caption block directly after it:
//
//	![Control loop](../assets/control-loop.svg)
//
//	```caption
//	kind: figure
//	id: control-loop
//	text: The blocks of the control loop, *as built*.
//	```
//
// kind names the numbering sequence (the exporter's configuration decides
// which kinds exist); id, optional, lets [[#id]] refer to the caption; text
// is Markdown. The block captions the block right before it: a paragraph
// holding only an image, a table, an embed, a fenced block or raw HTML.
// Anything else there is an error, rather than a caption silently attached
// to the wrong thing.

// Caption is a parsed caption block.
type Caption struct {
	Kind, ID, Text string
	// Line is the line of the caption block's content, from 1.
	Line int
}

// captionNode wraps the captioned block (its only child).
type captionNode struct {
	ast.BaseBlock
	Caption Caption
}

var kindCaption = ast.NewNodeKind("Caption")

func (n *captionNode) Kind() ast.NodeKind { return kindCaption }

func (n *captionNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Kind": n.Caption.Kind, "ID": n.Caption.ID}, nil)
}

var captionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type captionTransformer struct{ errs *[]error }

func (t *captionTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	for _, b := range fencedBlocks(doc, source, func(lang string) bool { return lang == "caption" }) {
		content, line := fenceContent(b, source)
		c, err := parseCaption(content, line)
		if err != nil {
			*t.errs = append(*t.errs, err)
			continue
		}
		target := b.PreviousSibling()
		if !captionable(target, source) {
			*t.errs = append(*t.errs, &Error{Line: line - 1, Msg: "a caption must come right after the figure, table, equation or embed it captions"})
			continue
		}
		parent := b.Parent()
		node := &captionNode{Caption: c}
		parent.InsertBefore(parent, target, node)
		parent.RemoveChild(parent, target)
		node.AppendChild(node, target)
		parent.RemoveChild(parent, b)
	}
}

// captionable reports whether a block can carry a caption.
func captionable(n ast.Node, source []byte) bool {
	switch n := n.(type) {
	case nil:
		return false
	case *ast.Paragraph:
		images := 0
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Image:
				images++
			case *ast.Link:
				if _, ok := c.FirstChild().(*ast.Image); !ok || c.ChildCount() != 1 {
					return false
				}
				images++
			case *ast.Text:
				if len(bytes.TrimSpace(c.Segment.Value(source))) > 0 {
					return false
				}
			default:
				return false
			}
		}
		return images == 1
	case *east.Table, *embedBlock, *fenceNode, *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock:
		return true
	}
	return false
}

func parseCaption(src string, line int) (Caption, error) {
	c := Caption{Line: line}
	fail := func(at int, format string, args ...any) error {
		return &Error{Line: line + at - 1, Msg: fmt.Sprintf(format, args...)}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		at, msg := yamlnode.ErrorLine(err)
		return c, fail(max(at, 1), "caption: %s", msg)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return c, fail(1, "a caption is a mapping with kind, and optionally id and text")
	}
	m := doc.Content[0]
	keys := []string{"kind", "id", "text"}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if !slices.Contains(keys, k.Value) {
			return c, fail(k.Line, "unknown key %q in a caption (expected kind, id, text)", k.Value)
		}
		if v.Kind != yaml.ScalarNode {
			return c, fail(v.Line, "caption %s must be text", k.Value)
		}
		switch k.Value {
		case "kind":
			c.Kind = v.Value
		case "id":
			if !captionID.MatchString(v.Value) {
				return c, fail(v.Line, "caption id %q: use letters, digits, '-' and '_'", v.Value)
			}
			c.ID = v.Value
		case "text":
			c.Text = v.Value
		}
	}
	if c.Kind == "" {
		return c, fail(m.Line, "a caption needs a kind, such as figure, table or equation")
	}
	return c, nil
}

// imageTransformer passes every image destination through rewrite.
type imageTransformer struct {
	rewrite func(string) (string, error)
	errs    *[]error
}

func (t *imageTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	if t.rewrite == nil {
		return
	}
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		img, ok := n.(*ast.Image)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		dest, err := t.rewrite(string(img.Destination))
		if err != nil {
			*t.errs = append(*t.errs, &Error{Line: blockLine(img, source), Msg: err.Error()})
			return ast.WalkContinue, nil
		}
		img.Destination = []byte(dest)
		return ast.WalkContinue, nil
	})
}

// blockLine is the first line of the block holding n, or 0.
func blockLine(n ast.Node, source []byte) int {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type() == ast.TypeBlock && p.Lines().Len() > 0 {
			return bytes.Count(source[:p.Lines().At(0).Start], []byte("\n")) + 1
		}
	}
	return 0
}
