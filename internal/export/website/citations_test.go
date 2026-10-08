package website

import (
	"maps"
	"strings"
	"testing"
)

const docSchema = "type: Doc\nid_prefix: D\nfields:\n  - {name: title, type: string}\n"

// docTemplate lists the page's citations, as a project's Document template
// would.
const docTemplate = `<h1>{{.Title}}</h1>
{{markdown .Body}}
{{- with .Citations}}
<ol class="cites">
{{- range .}}
<li>{{link .}} {{.CitationIndex}} {{.TypeCitationIndex}}</li>
{{- end}}
</ol>
{{- end}}
`

func doc(id, body string) string {
	return "---\nid: " + id + "\ntype: Doc\ntitle: Doc " + id + "\n---\n" + body
}

// citationRepo: D-1 cites N-2, N-1 (labelled), D-3, N-2 again (by field),
// a missing N-404 and a caption on the page, then embeds D-2, which cites
// N-3, and N-4, which is embedded, not cited.
var citationRepo = map[string]string{
	"schema/Note.yaml":   noteSchema,
	"schema/Doc.yaml":    docSchema,
	"templates/Doc.tmpl": docTemplate,
	"N/N-1.md":           note("N-1", "title: One\n", "Body."),
	"N/N-2.md":           note("N-2", "title: Two\n", "Body."),
	"N/N-3.md":           note("N-3", "title: Three\n", "Body."),
	"N/N-4.md":           note("N-4", "title: Four\n", "Body."),
	"D/D-1.md": doc("D-1", "[[N-2]], [[N-1|one]], [[D-3]], [[N-2#title]], [[N-404]] and [[#nothing]].\n\n"+
		"![[D-2]]\n\n![[N-4]]\n"),
	"D/D-2.md": doc("D-2", "See [[N-3]].\n"),
	"D/D-3.md": doc("D-3", "Cites nothing.\n"),
}

func TestReferenceLists(t *testing.T) {
	files, err := exportRepo(t, maps.Clone(citationRepo))
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/D-1.html"])
	// First-citation order, each once; the embed's citation where the
	// embed is; positions overall and within each type (a missing ID has
	// no type).
	want := "<ol class=\"cites\">\n" +
		"<li><a class=\"ref\" href=\"N-2.html\" title=\"Two\">N-2</a> 1 1</li>\n" +
		"<li><a class=\"ref\" href=\"N-1.html\" title=\"One\">N-1</a> 2 2</li>\n" +
		"<li><a class=\"ref\" href=\"D-3.html\" title=\"Doc D-3\">D-3</a> 3 1</li>\n" +
		"<li><span class=\"id unresolved-id\">N-404</span> <span class=\"unresolved\">unresolved</span> 4 1</li>\n" +
		"<li><a class=\"ref\" href=\"N-3.html\" title=\"Three\">N-3</a> 5 3</li>\n" +
		"</ol>"
	if !strings.Contains(page, want) {
		t.Errorf("D-1's list:\n%s\nwant\n%s", page, want)
	}
	// The embedded D-2 renders through the same template without a list.
	if n := strings.Count(page, `<ol class="cites">`); n != 1 {
		t.Errorf("D-1 has %d lists, want 1", n)
	}
	// D-2's own page lists its own citation.
	if d2 := string(files["entities/D-2.html"]); !strings.Contains(d2, `title="Three">N-3</a> 1 1</li>`) {
		t.Errorf("D-2's list:\n%s", d2)
	}
	// A page citing nothing has an empty .Citations.
	if d3 := string(files["entities/D-3.html"]); strings.Contains(d3, "cites") {
		t.Errorf("D-3 lists citations:\n%s", d3)
	}
}

func TestReferenceListOutOfScope(t *testing.T) {
	// With D-1 as the scope, cited entities it does not pull in show as
	// their plain ID; the embedded D-2 is in scope, so N-3 is not either.
	files, err := exportScoped(t, maps.Clone(citationRepo), "D/D-1.md")
	if err != nil {
		t.Fatal(err)
	}
	index := string(files["index.html"])
	for _, want := range []string{
		`<li><span class="ref out-of-scope">N-2</span> 1 1</li>`,
		`<li><span class="ref out-of-scope">N-3</span> 5 3</li>`,
	} {
		if !strings.Contains(index, want) {
			t.Errorf("main page missing %q\n%s", want, index)
		}
	}
}

func TestLayoutReceivesCitations(t *testing.T) {
	repo := maps.Clone(citationRepo)
	delete(repo, "templates/Doc.tmpl")
	repo["templates/_layout.tmpl"] = "{{template \"content\" .}}<p class=\"cited\">{{range .Citations}}[{{.ID}} {{.CitationIndex}}]{{end}}</p>\n"
	files, err := exportRepo(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	if page := string(files["entities/D-1.html"]); !strings.Contains(page, `<p class="cited">[N-2 1][N-1 2][D-3 3][N-404 4][N-3 5]</p>`) {
		t.Errorf("layout citations:\n%s", page)
	}
	if index := string(files["index.html"]); !strings.Contains(index, `<p class="cited"></p>`) {
		t.Errorf("the index has citations:\n%s", index)
	}
}

func TestCitationNamesAreReserved(t *testing.T) {
	repo := maps.Clone(citationRepo)
	repo["schema/Doc.yaml"] = docSchema + "  - {name: citation_index, type: number}\n"
	_, err := exportRepo(t, repo)
	want := `schema/Doc.yaml:5: field "citation_index": its template name .CitationIndex is already used by the engine's citations`
	if err == nil || err.Error() != want {
		t.Fatalf("got  %v\nwant %s", err, want)
	}
}

// citeTemplate shows everything _cite.tmpl receives. Its final line break
// is not part of the citation.
const citeTemplate = "<cite>{{.ID}}/{{.CitationIndex}}/{{.TypeCitationIndex}}/{{.CitationLabel}}/{{.Resolved}}</cite>\n"

func TestCiteTemplate(t *testing.T) {
	repo := maps.Clone(citationRepo)
	repo["templates/_cite.tmpl"] = citeTemplate
	files, err := exportRepo(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/D-1.html"])
	// Every [[ID]] and [[ID|label]], a missing one and one inside an embed
	// included, with the page's positions; [[ID#field]] and [[#id]] render
	// as before.
	for _, want := range []string{
		"<cite>N-2/1/1//true</cite>, <cite>N-1/2/2/one/true</cite>, <cite>D-3/3/1//true</cite>, " +
			`<a class="ref" href="N-2.html" title="Two">Two</a>, <cite>N-404/4/1//false</cite> and ` +
			`<span class="id unresolved-id">#nothing</span>`,
		"See <cite>N-3/5/3//true</cite>.",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("D-1 missing %q\n%s", want, page)
		}
	}
	// D-2's own page numbers its own citations.
	if d2 := string(files["entities/D-2.html"]); !strings.Contains(d2, "See <cite>N-3/1/1//true</cite>.") {
		t.Errorf("D-2:\n%s", d2)
	}
}

func TestCiteTemplateOnIndex(t *testing.T) {
	// The site index is about no entity and has no citations: Markdown it
	// renders cites with the built-in rendering.
	repo := maps.Clone(citationRepo)
	repo["templates/_cite.tmpl"] = citeTemplate
	repo["templates/_index.tmpl"] = `{{range .Types}}{{range .Entities}}{{if eq .ID "D-2"}}{{markdown .Body}}{{end}}{{end}}{{end}}`
	files, err := exportRepo(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	if index := string(files["index.html"]); !strings.Contains(index, `See <a class="ref" href="entities/N-3.html" title="Three">N-3</a>.`) {
		t.Errorf("index:\n%s", index)
	}
}

func TestCiteTemplateError(t *testing.T) {
	repo := maps.Clone(citationRepo)
	repo["templates/_cite.tmpl"] = "[{{.Nope}}]\n"
	_, err := exportRepo(t, repo)
	if err == nil || !strings.HasPrefix(err.Error(), `templates/_cite.tmpl:1:3: executing "templates/_cite.tmpl" at <.Nope>: map has no entry for key "Nope"`) {
		t.Fatalf("got %v", err)
	}
}

func TestCitationLabelIsReserved(t *testing.T) {
	repo := maps.Clone(citationRepo)
	repo["schema/Doc.yaml"] = docSchema + "  - {name: citation_label, type: string}\n"
	_, err := exportRepo(t, repo)
	want := `schema/Doc.yaml:5: field "citation_label": its template name .CitationLabel is already used by the engine's citations`
	if err == nil || err.Error() != want {
		t.Fatalf("got  %v\nwant %s", err, want)
	}
}
