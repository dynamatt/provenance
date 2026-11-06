package tmpl

import (
	"html/template"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/schema"
)

// ReportFuncs are the template functions of a validation report (DES-0049):
// link, href and short, as in entity templates. Its links are relative to
// the exported site's main page, where validate --output <name>=_site/<file>
// puts it, and the whole site is exported, so every entity validate loaded
// has a page; one it could not load is marked unresolved, as on the site.
// An entity of a type with pages: false links to its place on the type's
// page (DES-0054).
func ReportFuncs(s *schema.Schema) template.FuncMap {
	l := Linker{Base: "entities/", NoPages: map[string]bool{}, Schema: s}
	if s != nil {
		for _, t := range s.Types {
			if !t.Pages {
				l.NoPages[t.Name] = true
			}
		}
	}
	return template.FuncMap{"link": l.Link, "href": l.Href, "title": l.Title, "short": history.Short}
}
