package website

import (
	"html/template"

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

// ReportFuncs are the template functions of a validation report: link,
// href and short, as in entity templates. Its links are relative to the
// exported site's main page, where validate --output <name>=_site/<file>
// puts it, and the whole site is exported, so every entity validate loaded
// has a page; one it could not load is marked unresolved, as on the site.
func ReportFuncs() template.FuncMap {
	l := linker{base: "entities/"}
	return template.FuncMap{"link": l.link, "href": l.href, "short": history.Short}
}
