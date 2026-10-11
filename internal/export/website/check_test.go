package website

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// check builds a repository and returns Check's problems, one per line as
// "path:line rule msg", warnings marked.
func check(t *testing.T, files map[string]string) string {
	t.Helper()
	in, _ := input(t, files)
	problems, err := New().Check(in)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, p := range problems {
		w := ""
		if p.Warning {
			w = " (warning)"
		}
		lines = append(lines, fmt.Sprintf("%s:%d %s%s %s", p.Path, p.Line, p.Rule, w, p.Msg))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

// Check reports every problem, where Export stops at the first; one found
// on several pages (N-2 is embedded in N-1) is reported once.
func TestCheckReportsEveryProblem(t *testing.T) {
	got := check(t, map[string]string{
		"schema/Note.yaml": noteSchema,
		"N/N-1.md": note("N-1", "title: One\n", `See [[N-404]] and [[N-2#nope]], and ![[N-2]] inline.

![[N-2]]

![missing](no.png)

`+"```query\nfrom: Note\nwhere: {field: title, operator: \"=\", value: x}\n```\n"+`
`+"```mermaid\ngraph TD\n```\n"+`
Just text.
`+"```caption\nkind: figure\n```\n"),
		"N/N-2.md":                  note("N-2", "title: Two\n", "Back to ![[N-1]] here:\n\n![[N-1]]\n\n[[#nowhere]]\n"),
		"templates/Note.tmpl":       "{{markdown .Body}}{{ .Nope }}\n",
		"templates/_footer.tmpl":    "x\n",
		"templates/Widget.tmpl":     "x\n",
		"templates/spare-part.tmpl": "{{ .Title }\n",
	})
	want := `N/N-1.md:10 content/image image N/no.png does not exist
N/N-1.md:14 content/query query block: unknown operator "=" (valid operators: equals, not_equals, greater_or_equal, less_or_equal, greater_than, less_than, exists)
N/N-1.md:17 content/unrendered-block mermaid block: this version of Provenance cannot render it, so it would be published as its source
N/N-1.md:22 content/caption a caption must come right after the figure, table, equation or embed it captions
N/N-1.md:6 content/embed ![[N-2]] is inside running text, so it is shown as a reference: an embed must stand on its own line
N/N-1.md:6 links/unknown-field [[N-2#nope]]: Note has no field "nope"
N/N-1.md:6 links/unresolved [[N-404]]: no entity N-404
N/N-1.md:8 content/embed-cycle embed cycle N-2 → N-1 → N-2
N/N-2.md:10 content/caption [[#nowhere]]: no caption with id "nowhere" on this page
N/N-2.md:6 content/embed ![[N-1]] is inside running text, so it is shown as a reference: an embed must stand on its own line
N/N-2.md:8 content/embed-cycle embed cycle N-1 → N-2 → N-1
templates/Note.tmpl:1 content/template executing "templates/Note.tmpl" at <.Nope>: map has no entry for key "Nope"
templates/Widget.tmpl:0 content/template-name (warning) Widget.tmpl names no declared type and is not a named template (lower-case kebab-case), so it is never used
templates/_footer.tmpl:0 content/unknown-override (warning) _footer.tmpl is not a site override (_layout.tmpl, _index.tmpl, _cite.tmpl), so it is never used
templates/spare-part.tmpl:0 content/unused-template (warning) named template spare-part is not chosen by any query block
templates/spare-part.tmpl:1 content/template unexpected "}" in operand`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Check leaves Export as it was: it stops at the first problem.
func TestExportStillStopsAtTheFirstProblem(t *testing.T) {
	_, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": noteSchema,
		"N/N-1.md":         note("N-1", "title: One\n", "![a](a.png)\n\n![b](b.png)\n"),
	})
	if err == nil || err.Error() != "N/N-1.md:6: image N/a.png does not exist" {
		t.Errorf("err = %v", err)
	}
}
