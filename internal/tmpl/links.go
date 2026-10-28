package tmpl

import (
	"fmt"
	"html/template"
	"io"
	"strings"
)

// Linker renders links to entity pages: the link and href template
// functions, which site pages and validation reports (DES-0049) share, and
// the links wikilinks and query blocks write.
type Linker struct {
	// base is the path from the page rendered to the entity pages.
	Base string
	// HasPage reports whether an entity has a page; nil is every entity.
	HasPage func(id string) bool
}

func (l Linker) pageOf(id string) bool { return l.HasPage == nil || l.HasPage(id) }

// link renders a link to an entity (template data), with optional text: the
// ID by default. nil renders nothing; an unresolved target is marked; one
// with no page is its text, unlinked.
func (l Linker) Link(v any, text ...any) (template.HTML, error) {
	ref, err := asRef(v, "link")
	if ref == nil || err != nil {
		return "", err
	}
	id, _ := ref["ID"].(string)
	label := id
	if len(text) > 0 {
		label = fmt.Sprint(text[0])
	}
	if resolved, _ := ref["Resolved"].(bool); !resolved {
		return template.HTML(UnresolvedHTML(label, "unresolved")), nil
	}
	title, _ := ref["Title"].(string)
	var b strings.Builder
	l.Anchor(&b, "ref", id, title, label)
	return template.HTML(b.String()), nil
}

// anchor writes a link with the given class to entity id's page showing
// text, with its title as the tooltip, or, when it has no page, the text
// alone (Requirements Spec §7: the plain citable ID for an ID).
func (l Linker) Anchor(w io.StringWriter, class, id, title, text string) {
	if !l.pageOf(id) {
		_, _ = w.WriteString(fmt.Sprintf(`<span class="%s out-of-scope">%s</span>`, class, template.HTMLEscapeString(text)))
		return
	}
	if title != "" {
		title = fmt.Sprintf(` title="%s"`, template.HTMLEscapeString(title))
	}
	_, _ = w.WriteString(fmt.Sprintf(`<a class="%s" href="%s"%s>%s</a>`, class,
		template.HTMLEscapeString(l.Base+id+".html"), title, template.HTMLEscapeString(text)))
}

// href is the relative URL of an entity's page, for custom links; "" for an
// unresolved entity or one with no page.
func (l Linker) Href(v any) (string, error) {
	ref, err := asRef(v, "href")
	if ref == nil || err != nil {
		return "", err
	}
	id, _ := ref["ID"].(string)
	if resolved, _ := ref["Resolved"].(bool); !resolved || !l.pageOf(id) {
		return "", nil
	}
	return l.Base + id + ".html", nil
}

func asRef(v any, fn string) (Data, error) {
	if v == nil {
		return nil, nil
	}
	ref, ok := v.(Data)
	if !ok {
		return nil, fmt.Errorf("%s: want an entity (a link field value), got %T", fn, v)
	}
	return ref, nil
}

// UnresolvedHTML shows text, an ID or reference the export could not
// resolve, marked with why.
func UnresolvedHTML(text, marker string) string {
	return fmt.Sprintf(`<span class="id unresolved-id">%s</span>`, template.HTMLEscapeString(text)) + UnresolvedNote(marker)
}

// UnresolvedNote marks what precedes it as unresolved, saying why.
func UnresolvedNote(marker string) string {
	return fmt.Sprintf(` <span class="unresolved">%s</span>`, template.HTMLEscapeString(marker))
}
