package markdown

import (
	"bytes"
	"regexp"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Tags (DES-0034) name a heading or an ordered list item so that a
// reference, [[#tag]] or [[ID#tag]], can show the number it has where it is
// rendered. A tag is written {#tag} at the end of a heading's line or of an
// ordered list item's first paragraph:
//
//	## Device Under Test {#dut}
//
//	1. Connect the current probe. {#connect}
//
// The tag is taken out of the text and left on the element as data-tag
// (and data-owner, the entity whose Markdown it is, when the caller gives
// one), for the exporter to number and give an id. A tag anywhere else, or
// one not written in lower-case letters, digits and '-', is an error, and
// is left as written.

// tagSuffix matches {#…} at the end of a line of text, with the spaces
// before it.
var tagSuffix = regexp.MustCompile(`[ \t]*\{#([^{}\s]*)\}[ \t]*$`)

// TagPattern is what a tag may be: lower-case letters, digits and '-',
// starting with a letter or digit. Caption ids share its namespace.
var TagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type tagTransformer struct {
	owner string
	found func(Tag)
	errs  *[]error
}

func (t *tagTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			t.take(n, n, source, "")
			return ast.WalkSkipChildren, nil
		case *ast.Paragraph, *ast.TextBlock:
			misplaced := "a tag goes at the end of a heading or of an ordered list item's first paragraph"
			var target ast.Node
			if item, ok := n.Parent().(*ast.ListItem); ok && item.FirstChild() == n {
				if list, ok := item.Parent().(*ast.List); ok && list.IsOrdered() {
					target, misplaced = item, ""
				} else {
					misplaced = "a tag on an unordered list item: it has no number to show"
				}
			}
			t.take(n, target, source, misplaced)
		}
		return ast.WalkContinue, nil
	})
}

// take moves a {#tag} ending block's text onto target as data-tag. With no
// target, the tag is misplaced: an error, left as written.
func (t *tagTransformer) take(block, target ast.Node, source []byte, misplaced string) {
	// The block's text may end in several text nodes: the inline parser
	// splits text at characters such as '_'.
	var run []*ast.Text
	for c := block.LastChild(); c != nil; c = c.PreviousSibling() {
		tn, ok := c.(*ast.Text)
		if !ok || (len(run) > 0 && tn.Segment.Stop != run[0].Segment.Start) {
			break
		}
		run = append([]*ast.Text{tn}, run...)
		if tn.SoftLineBreak() || tn.HardLineBreak() {
			break
		}
	}
	if len(run) == 0 {
		return
	}
	start, stop := run[0].Segment.Start, run[len(run)-1].Segment.Stop
	value := source[start:stop]
	m := tagSuffix.FindSubmatchIndex(value)
	if m == nil {
		return
	}
	tag := string(value[m[2]:m[3]])
	line := bytes.Count(source[:start+m[0]], []byte("\n")) + 1
	switch {
	case misplaced != "":
		*t.errs = append(*t.errs, &Error{Line: line, Msg: "{#" + tag + "}: " + misplaced, Tag: true})
		return
	case !TagPattern.MatchString(tag):
		*t.errs = append(*t.errs, &Error{Line: line, Msg: "tag {#" + tag + "}: use lower-case letters, digits and '-'", Tag: true})
		return
	}
	cut := start + m[0]
	for _, tn := range run {
		switch {
		case tn.Segment.Start >= cut:
			block.RemoveChild(block, tn)
		case tn.Segment.Stop > cut:
			tn.Segment = tn.Segment.WithStop(cut)
		}
	}
	target.SetAttributeString("data-tag", []byte(tag))
	if t.found != nil {
		t.found(Tag{Name: tag, Line: line})
	}
	if t.owner != "" {
		target.SetAttributeString("data-owner", []byte(t.owner))
	}
}

// Tag is one tag written in Markdown.
type Tag struct {
	Name string
	// Line is the line of Markdown source it is on, from 1.
	Line int
}
