// Package markdown converts entity Markdown to HTML (Detailed Design §7):
// CommonMark plus tables, raw HTML passed through, and Obsidian-style
// wikilinks (High-Level Design §4.3a).
//
// The package parses; the caller decides. What a wikilink turns into is up
// to a Resolver, and a fenced block is handed to the caller's FenceFunc for
// its language (query blocks, diagrams), so rendering policy lives with the
// exporter and a new block language never changes this package.
package markdown

import (
	"bytes"
	"strconv"
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

// Fence is a fenced code block handed to the caller to render.
type Fence struct {
	// Lang is the first word of the info string: "query", "mermaid", …
	Lang string
	// Source is the block's content, without the fences.
	Source string
	// Line is the line of Markdown source the content starts on, from 1.
	Line int
}

// FenceFunc renders a fenced block. Like a Resolver, it writes trusted HTML.
type FenceFunc func(w util.BufWriter, f Fence) error

// RenderedLanguages are the fence languages the product renders instead of
// showing as code (High-Level Design §4.3a, Requirements Spec §7). Every
// exporter must give each one a FenceFunc, even if only to say it cannot
// render it yet: falling back to a code listing would publish a query's or
// diagram's source as if it were the content. The BlockLanguage rule
// (Requirements Spec §6) reports the ones a binary cannot render.
var RenderedLanguages = []string{"query", "mermaid", "drawio"}

// Resolver renders wikilinks, the syntax this package adds to Markdown. It
// writes HTML directly; anything it writes is trusted.
type Resolver interface {
	// Reference renders an inline [[ID]], [[ID|label]] or [[ID#field]].
	Reference(w util.BufWriter, l Link) error
	// Embed renders ![[ID]] standing alone in its paragraph, as a block.
	Embed(w util.BufWriter, l Link) error
	// MisplacedEmbed renders ![[ID]] written inside running text, where a
	// block cannot go.
	MisplacedEmbed(w util.BufWriter, l Link) error
}

// Convert renders src to HTML, resolving wikilinks through r. A fenced block
// whose language has an entry in fences is rendered by it; any other fenced
// block is shown as code.
func Convert(src string, r Resolver, fences map[string]FenceFunc) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(
			parser.WithInlineParsers(util.Prioritized(&wikilinkParser{}, 199)),
			parser.WithASTTransformers(
				util.Prioritized(&embedTransformer{}, 100),
				util.Prioritized(&fenceTransformer{fences: fences}, 101),
			),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(), // raw HTML passes through (Detailed Design §7)
			renderer.WithNodeRenderers(util.Prioritized(&wikilinkRenderer{r: r, fences: fences}, 100)),
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

// fenceNode replaces a fenced code block the caller renders.
type fenceNode struct {
	ast.BaseBlock
	Fence Fence
}

var kindFence = ast.NewNodeKind("Fence")

func (n *fenceNode) Kind() ast.NodeKind { return kindFence }

func (n *fenceNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Lang": n.Fence.Lang, "Line": strconv.Itoa(n.Fence.Line)}, nil)
}

// fenceTransformer replaces each fenced code block whose language has a
// FenceFunc with a fenceNode.
type fenceTransformer struct{ fences map[string]FenceFunc }

func (t *fenceTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var blocks []*ast.FencedCodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if b, ok := n.(*ast.FencedCodeBlock); ok && entering {
			if _, handled := t.fences[string(b.Language(source))]; handled {
				blocks = append(blocks, b)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, b := range blocks {
		var src bytes.Buffer
		lines := b.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			src.Write(seg.Value(source))
		}
		// Content starts on the line after the opening fence.
		line := bytes.Count(source[:b.Info.Segment.Start], []byte("\n")) + 2
		f := Fence{Lang: string(b.Language(source)), Source: src.String(), Line: line}
		b.Parent().ReplaceChild(b.Parent(), b, &fenceNode{Fence: f})
	}
}

type wikilinkRenderer struct {
	r      Resolver
	fences map[string]FenceFunc
}

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
	reg.Register(kindFence, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		f := n.(*fenceNode).Fence
		return ast.WalkSkipChildren, r.fences[f.Lang](w, f)
	})
	reg.Register(kindEmbed, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		return ast.WalkSkipChildren, r.r.Embed(w, n.(*embedBlock).Link)
	})
}
