package tmpl

import (
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/dynamatt/provenance/internal/schema"
)

// Linker renders links to entity pages: the link and href template
// functions, which site pages and validation reports (DES-0049) share, and
// the links wikilinks and query blocks write.
type Linker struct {
	// base is the path from the page rendered to the entity pages.
	Base string
	// HasPage reports whether an entity has a page; nil is every entity.
	HasPage func(id string) bool
	// ShownOn is the page, relative to the entity pages and with its
	// anchor, that shows an entity without a page of its own, or "": an
	// entity of a type with pages: false is shown on the type's page
	// (DES-0054). nil is none.
	ShownOn func(id string) string
	// NoPages are the types whose entities have no pages of their own,
	// for a linker without HasPage, which knows an entity only by its
	// data.
	NoPages map[string]bool
	// Schema tells which field of each type is its title.
	Schema *schema.Schema
}

// TitleOf is the title of entity data d: the value of the field its type
// declares title: true, or "" (DES-0010).
func TitleOf(s *schema.Schema, d Data) string {
	if s == nil {
		return ""
	}
	typ, _ := d["Type"].(string)
	t := s.Types[typ]
	if t == nil || t.TitleField == nil {
		return ""
	}
	v, _ := d[t.TitleField.Name].(string)
	return v
}

// Title is the template function title: an entity's title, "" for nil or
// an entity whose type has no title field.
func (l Linker) Title(v any) (string, error) {
	ref, err := asRef(v, "title")
	if ref == nil || err != nil {
		return "", err
	}
	return TitleOf(l.Schema, ref), nil
}

// page is the URL, relative to the entity pages, that shows entity id, or
// "" when none does. ref, when known, is its template data: an entity of
// a type with pages: false is shown on the type's page.
func (l Linker) page(id string, ref Data) string {
	if l.HasPage != nil && !l.HasPage(id) {
		if l.ShownOn != nil {
			return l.ShownOn(id)
		}
		return ""
	}
	if typ, _ := ref["Type"].(string); l.NoPages[typ] {
		return fmt.Sprintf("%s.html#%s", typ, id)
	}
	return id + ".html"
}

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
	title := TitleOf(l.Schema, ref)
	var b strings.Builder
	l.anchor(&b, "ref", l.page(id, ref), title, label)
	return template.HTML(b.String()), nil
}

// Anchor writes a link with the given class to the page showing entity
// id, with text and its title as the tooltip, or, when no page shows it,
// the text alone (Requirements Spec §7: the plain citable ID for an ID).
func (l Linker) Anchor(w io.StringWriter, class, id, title, text string) {
	l.anchor(w, class, l.page(id, nil), title, text)
}

func (l Linker) anchor(w io.StringWriter, class, page, title, text string) {
	if page == "" {
		_, _ = w.WriteString(fmt.Sprintf(`<span class="%s out-of-scope">%s</span>`, class, template.HTMLEscapeString(text)))
		return
	}
	if title != "" {
		title = fmt.Sprintf(` title="%s"`, template.HTMLEscapeString(title))
	}
	_, _ = w.WriteString(fmt.Sprintf(`<a class="%s" href="%s"%s>%s</a>`, class,
		template.HTMLEscapeString(l.Base+page), title, template.HTMLEscapeString(text)))
}

// href is the relative URL of an entity's page, for custom links; "" for an
// unresolved entity or one with no page.
func (l Linker) Href(v any) (string, error) {
	ref, err := asRef(v, "href")
	if ref == nil || err != nil {
		return "", err
	}
	id, _ := ref["ID"].(string)
	page := l.page(id, ref)
	if resolved, _ := ref["Resolved"].(bool); !resolved || page == "" {
		return "", nil
	}
	return l.Base + page, nil
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
