// Package markdown converts entity Markdown to HTML (Detailed Design §7):
// CommonMark plus tables, raw HTML passed through, and Obsidian-style
// wikilinks (High-Level Design §4.3a).
//
// The package only parses wikilinks. What a reference or an embed turns into
// is decided by a Resolver, so rendering policy lives with the exporter.
package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Link is one parsed wikilink.
type Link struct {
	ID string
	// Label is the text after '|', or "".
	Label string
	// Field is the field name after '#', or "".
	Field string
	// Embed is true for ![[ID]].
	Embed bool
}

// Resolver renders wikilinks. It writes HTML directly; anything it writes is
// trusted.
type Resolver interface {
	// Reference renders an inline [[ID]], [[ID|label]] or [[ID#field]].
	Reference(w util.BufWriter, l Link) error
	// Embed renders ![[ID]] standing alone in its paragraph, as a block.
	Embed(w util.BufWriter, l Link) error
	// MisplacedEmbed renders ![[ID]] written inside running text, where a
	// block cannot go.
	MisplacedEmbed(w util.BufWriter, l Link) error
}

// Convert renders src to HTML, resolving wikilinks through r.
func Convert(src string, r Resolver) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(
			parser.WithInlineParsers(util.Prioritized(&wikilinkParser{}, 199)),
			parser.WithASTTransformers(util.Prioritized(&embedTransformer{}, 100)),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(), // raw HTML passes through (Detailed Design §7)
			renderer.WithNodeRenderers(util.Prioritized(&wikilinkRenderer{r: r}, 100)),
		),
	)
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

// wikilinkNode is an inline [[…]] or ![[…]].
type wikilinkNode struct {
	ast.BaseInline
	Link Link
}

var kindWikilink = ast.NewNodeKind("Wikilink")

func (n *wikilinkNode) Kind() ast.NodeKind { return kindWikilink }

func (n *wikilinkNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"ID": n.Link.ID}, nil)
}

// embedBlock is a paragraph that held nothing but one ![[ID]].
type embedBlock struct {
	ast.BaseBlock
	Link Link
}

var kindEmbed = ast.NewNodeKind("WikilinkEmbed")

func (n *embedBlock) Kind() ast.NodeKind { return kindEmbed }

func (n *embedBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"ID": n.Link.ID}, nil)
}

type wikilinkParser struct{}

func (p *wikilinkParser) Trigger() []byte { return []byte{'[', '!'} }

func (p *wikilinkParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	embed := false
	start := 2
	if bytes.HasPrefix(line, []byte("![[")) {
		embed, start = true, 3
	} else if !bytes.HasPrefix(line, []byte("[[")) {
		return nil
	}
	end := bytes.Index(line[start:], []byte("]]"))
	if end < 0 {
		return nil
	}
	inner := string(line[start : start+end])
	if strings.ContainsAny(inner, "[]\n") {
		return nil
	}
	l := Link{Embed: embed}
	target, label, hasLabel := strings.Cut(inner, "|")
	if hasLabel {
		l.Label = strings.TrimSpace(label)
	}
	id, field, _ := strings.Cut(target, "#")
	l.ID, l.Field = strings.TrimSpace(id), strings.TrimSpace(field)
	if l.ID == "" {
		return nil
	}
	block.Advance(start + end + 2)
	return &wikilinkNode{Link: l}
}

// embedTransformer turns a paragraph whose only content is one ![[ID]] into
// an embed block.
type embedTransformer struct{}

func (t *embedTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	var paras []*ast.Paragraph
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if p, ok := n.(*ast.Paragraph); ok && entering {
			paras = append(paras, p)
		}
		return ast.WalkContinue, nil
	})
	for _, p := range paras {
		link, ok := soleEmbed(p, reader.Source())
		if !ok {
			continue
		}
		p.Parent().ReplaceChild(p.Parent(), p, &embedBlock{Link: link})
	}
}

// soleEmbed reports whether p holds one embed wikilink and only whitespace
// besides.
func soleEmbed(p *ast.Paragraph, source []byte) (Link, bool) {
	var found *wikilinkNode
	for c := p.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *wikilinkNode:
			if !n.Link.Embed || found != nil {
				return Link{}, false
			}
			found = n
		case *ast.Text:
			if len(bytes.TrimSpace(n.Segment.Value(source))) > 0 {
				return Link{}, false
			}
		default:
			return Link{}, false
		}
	}
	if found == nil {
		return Link{}, false
	}
	return found.Link, true
}

type wikilinkRenderer struct{ r Resolver }

func (r *wikilinkRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindWikilink, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		l := n.(*wikilinkNode).Link
		var err error
		if l.Embed {
			err = r.r.MisplacedEmbed(w, l)
		} else {
			err = r.r.Reference(w, l)
		}
		return ast.WalkSkipChildren, err
	})
	reg.Register(kindEmbed, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		return ast.WalkSkipChildren, r.r.Embed(w, n.(*embedBlock).Link)
	})
}
