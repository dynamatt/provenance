package website

import (
	"fmt"
	"html/template"
	"regexp"
	"strings"

	"github.com/dynamatt/provenance/internal/model"
)

// Captioned entities (Requirements Spec §7, Detailed Design §7) are numbered
// per page, in document order: the page's own entity first if it is
// captioned, then each captioned entity in the order it is first embedded.
// A number is only known once the whole page is rendered, so rendering
// leaves placeholders and finalize resolves them (a two-pass render):
//
//   - .CaptionNumber of a captioned entity is a caption placeholder, which
//     becomes "Figure 1", or "" if the entity is not numbered on the page;
//   - a [[ID]] or [[ID|label]] reference to a captioned entity is a
//     cross-reference placeholder holding its ordinary rendering: on a page
//     where the target is numbered it becomes a link to the target in the
//     page, showing "Figure 1" (or the label); elsewhere the ordinary
//     rendering stays.
//
// Placeholders are delimited by Unicode private-use characters, which
// neither Markdown, html/template nor the heading shift changes.
const (
	tokOpen  = ""
	tokSep   = ""
	tokBody  = ""
	tokClose = ""
)

func captionToken(id string) string { return tokOpen + "cap" + tokSep + id + tokClose }

// xrefToken wraps the ordinary rendering of a reference to id (label is
// [[ID|label]]'s, or "").
func xrefToken(id, label, ordinary string) string {
	return tokOpen + "xref" + tokSep + id + tokSep + label + tokBody + ordinary + tokClose
}

var (
	captionTok  = regexp.MustCompile(tokOpen + "cap" + tokSep + "([^" + tokClose + "]*)" + tokClose)
	xrefTok     = regexp.MustCompile(tokOpen + "xref" + tokSep + "([^" + tokSep + "]*)" + tokSep + "([^" + tokBody + "]*)" + tokBody + "([^" + tokClose + "]*)" + tokClose)
	embedOpener = regexp.MustCompile(`<section class="embed" data-entity="([^"]+)">`)
)

// captioned reports whether e is numbered, and in which sequence.
func (s *site) captioned(e *model.Entity) (label string, ok bool) {
	label, ok = s.templates.captions[e.Type]
	return label, ok
}

// anchorID is the in-page target of a cross-reference to id.
func anchorID(id string) string { return "caption-" + id }

// finalize numbers the captioned entities of one page's content and
// resolves its placeholders. root is the page's own entity, or nil.
func (s *site) finalize(content string, root *model.Entity) string {
	numbers := map[string]string{}
	next := map[string]int{}
	number := func(id string) {
		e := s.byID[id]
		if e == nil || numbers[id] != "" {
			return
		}
		if label, ok := s.captioned(e); ok {
			next[label]++
			numbers[id] = fmt.Sprintf("%s %d", label, next[label])
		}
	}
	if root != nil {
		number(root.ID)
	}
	for _, m := range embedOpener.FindAllStringSubmatch(content, -1) {
		number(m[1])
	}

	// The first embed of each numbered entity is the cross-references'
	// target.
	anchored := map[string]bool{}
	content = embedOpener.ReplaceAllStringFunc(content, func(open string) string {
		id := embedOpener.FindStringSubmatch(open)[1]
		if numbers[id] == "" || anchored[id] {
			return open
		}
		anchored[id] = true
		return strings.TrimSuffix(open, ">") + fmt.Sprintf(` id="%s">`, template.HTMLEscapeString(anchorID(id)))
	})
	content = captionTok.ReplaceAllStringFunc(content, func(tok string) string {
		return template.HTMLEscapeString(numbers[captionTok.FindStringSubmatch(tok)[1]])
	})
	return xrefTok.ReplaceAllStringFunc(content, func(tok string) string {
		m := xrefTok.FindStringSubmatch(tok)
		id, label, ordinary := m[1], m[2], m[3]
		n := numbers[id]
		if n == "" {
			return ordinary
		}
		text := n
		if label != "" {
			text = label
		}
		href := "#" + anchorID(id)
		if root != nil && root.ID == id {
			href = "#"
		}
		title := ""
		if t := s.byID[id].Title(); t != "" {
			title = fmt.Sprintf(` title="%s"`, template.HTMLEscapeString(t))
		}
		return fmt.Sprintf(`<a class="ref xref" href="%s"%s>%s</a>`, template.HTMLEscapeString(href), title, template.HTMLEscapeString(text))
	})
}
