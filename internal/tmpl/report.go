package tmpl

import (
	"html/template"

	"github.com/dynamatt/provenance/internal/history"
)

// ReportFuncs are the template functions of a validation report (DES-0049):
// link, href and short, as in entity templates. Its links are relative to
// the exported site's main page, where validate --output <name>=_site/<file>
// puts it, and the whole site is exported, so every entity validate loaded
// has a page; one it could not load is marked unresolved, as on the site.
func ReportFuncs() template.FuncMap {
	l := Linker{Base: "entities/"}
	return template.FuncMap{"link": l.Link, "href": l.Href, "short": history.Short}
}
