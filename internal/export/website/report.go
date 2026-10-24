package website

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// What validation report templates (DES-0049) share with the site: the
// entities as entity templates see them, and links to their pages.

// EntityData is every entity's template data (DES-0030) by ID. Its git
// stamps are empty: a report has no page history.
func EntityData(s *schema.Schema, entities []*model.Entity) map[string]map[string]any {
	return buildData(s, entities).byID
}

// UnresolvedEntity is the template data of an ID no entity has: its ID,
// Resolved false and the other baseline keys empty.
func UnresolvedEntity(id string) map[string]any { return baseline(id) }

// ReportFuncs are the template functions of a validation report. Its links
// are relative to the exported site's main page, where validate --out
// _site/<file> puts it: link renders a link to an entity (the ID unless
// text is given), href is the URL of its page, and short abbreviates a
// commit hash. An entity no file defines has no page; link shows its text
// unlinked, and href is "".
func ReportFuncs() template.FuncMap {
	return template.FuncMap{
		"link": func(v any, text ...any) (template.HTML, error) {
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
				return template.HTML(fmt.Sprintf(`<span class="ref">%s</span>`, template.HTMLEscapeString(label))), nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, `<a class="ref" href="%s"`, template.HTMLEscapeString(reportHref(id)))
			if t, _ := ref["Title"].(string); t != "" {
				fmt.Fprintf(&b, ` title="%s"`, template.HTMLEscapeString(t))
			}
			fmt.Fprintf(&b, `>%s</a>`, template.HTMLEscapeString(label))
			return template.HTML(b.String()), nil
		},
		"href": func(v any) (string, error) {
			ref, err := asRef(v, "href")
			if ref == nil || err != nil {
				return "", err
			}
			if resolved, _ := ref["Resolved"].(bool); !resolved {
				return "", nil
			}
			id, _ := ref["ID"].(string)
			return reportHref(id), nil
		},
		"short": history.Short,
	}
}

// reportHref is an entity page's URL from the site's main page.
func reportHref(id string) string { return "entities/" + id + ".html" }
