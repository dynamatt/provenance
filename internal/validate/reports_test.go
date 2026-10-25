package validate

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/repo"
)

// reportRepo is ruleRepo with a rule that fails PRT-0001, a rule that
// passes, and the report templates given.
func reportRepo(t *testing.T, templates map[string]string) string {
	t.Helper()
	root := ruleRepo(t, map[string]string{
		"draft.yaml":    "severity: error\nmessage: status is <draft>\nfrom: Part\nwhere: {field: status, operator: equals, value: draft}\n",
		"no-owner.yaml": "severity: warning\nmessage: owned by nobody\nfrom: Part\nwhere: {field: owner, operator: equals, value: nobody}\n",
	})
	for name, src := range templates {
		write(t, root, ReportsDir+"/"+name, src)
	}
	return root
}

func runReport(t *testing.T, root string) *Report {
	t.Helper()
	r, err := Run(&repo.Repo{Root: root, Component: repo.Component{Code: "PRT", Name: "Parts & more"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func renderReport(t *testing.T, r *Report, name string) string {
	t.Helper()
	var b bytes.Buffer
	if err := r.WriteTemplate(&b, name, Stamps{GitSHA: "abc123", ContentHash: "hash-dirty", Dirty: true}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// A report template receives the violations, every check and rule with
// its own, the summary, the component and the stamps; an .html name
// escapes as HTML, any other is text.
const listReport = `{{.Component.Name}} {{.Summary.Errors}}/{{.Summary.Warnings}}/{{.Summary.RulesSkipped}}
{{range .Violations}}{{.Severity}} {{.Rule}} {{.Path}}:{{.Line}} {{link .Entity}} {{.Entity.Title}} {{href .Entity}} {{.Message}}
{{end}}{{range .Rules}}{{if not .Builtin}}{{.ID}} {{.Severity}} {{len .Violations}} {{.Path}} {{.Message}} [{{.Skipped}}]
{{end}}{{end}}{{len .Rules}} {{.GitSHA}} {{.ContentHash}} {{.Dirty}}
`

func TestReportTemplate(t *testing.T) {
	root := reportRepo(t, map[string]string{"list.html.tmpl": listReport, "list.md.tmpl": listReport})
	r := runReport(t, root)
	n := len(Checks) + 2
	if got, want := renderReport(t, r, "list.html"), fmt.Sprintf(`Parts &amp; more 1/0/0
error draft PRT/PRT-0001.md:5 <a class="ref" href="entities/PRT-0001.html" title="Frame">PRT-0001</a> Frame entities/PRT-0001.html status is &lt;draft&gt;
draft error 1 rules/draft.yaml status is &lt;draft&gt; []
no-owner warning 0 rules/no-owner.yaml owned by nobody []
%d abc123 hash-dirty true
`, n); got != want {
		t.Errorf("html:\n%s\nwant:\n%s", got, want)
	}
	if got, want := renderReport(t, r, "list.md"), fmt.Sprintf(`Parts & more 1/0/0
error draft PRT/PRT-0001.md:5 <a class="ref" href="entities/PRT-0001.html" title="Frame">PRT-0001</a> Frame entities/PRT-0001.html status is <draft>
draft error 1 rules/draft.yaml status is <draft> []
no-owner warning 0 rules/no-owner.yaml owned by nobody []
%d abc123 hash-dirty true
`, n); got != want {
		t.Errorf("text:\n%s\nwant:\n%s", got, want)
	}
}

// The built-in checks come first, in ID order, each with its catalogue
// title; a rule carries its file's text as .Query.
func TestReportRules(t *testing.T) {
	root := reportRepo(t, map[string]string{"rules.tmpl": `{{range .Rules}}{{.ID}}|{{.Builtin}}|{{.Message}}|{{.Query}}
{{end}}`})
	got := strings.Split(renderReport(t, runReport(t, root), "rules"), "\n")
	if got[0] != "content/caption|true|A caption that cannot be placed or referred to|" {
		t.Errorf("first = %q", got[0])
	}
	if i := len(Checks); got[i] != "draft|false|status is <draft>|severity: error" || got[i+1] != "message: status is <draft>" {
		t.Errorf("draft = %q", got[i:i+5])
	}
}

// An entity validate could not load has no page: link shows its ID and
// href is empty. A violation of no entity has a nil .Entity.
func TestReportEntities(t *testing.T) {
	root := reportRepo(t, map[string]string{"e.tmpl": `{{range .Violations}}{{.Rule}} [{{link .Entity}}] [{{href .Entity}}]{{with .Entity}} {{.Resolved}}{{end}}
{{end}}`})
	write(t, root, "WID/WID-0001.md", "---\nid: WID-0001\ntype: Widget\n---\n")
	write(t, root, "rules/Bad.yaml", "severity: error\nmessage: m\nfrom: Part\n")
	got := renderReport(t, runReport(t, root), "e")
	for _, want := range []string{
		`schema/unknown-type [<span class="ref">WID-0001</span>] [] false`,
		"rules/name [] []\n",
		`draft [<a class="ref" href="entities/PRT-0001.html" title="Frame">PRT-0001</a>] [entities/PRT-0001.html] true`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A report template that does not parse, or names a missing field, is a
// content/template violation at its line, and cannot be rendered; so is
// one named like a format, which --output takes as the format. Report
// templates are never warned of as unused.
func TestReportTemplateErrors(t *testing.T) {
	root := reportRepo(t, map[string]string{
		"parse.tmpl":      "line one\n{{.Summary.Errors}\n",
		"field.html.tmpl": "<p>ok</p>\n{{range .Violations}}{{.Entity.Title}}{{end}}{{.Summary.Nope}}\n",
		"fine.tmpl":       "{{.Summary.Errors}}\n",
		"json.tmpl":       "{{.Summary.Errors}}\n",
	})
	write(t, root, ReportsDir+"/notes.txt", "not a template\n")
	r := runReport(t, root)
	var got []string
	for _, v := range r.Violations {
		if strings.HasPrefix(v.Rule, "content/") {
			got = append(got, textLine(v))
		}
	}
	want := []string{
		`templates/reports/field.html.tmpl:2: error [content/template] executing "templates/reports/field.html.tmpl" at <.Summary.Nope>: map has no entry for key "Nope"`,
		`templates/reports/json.tmpl: error [content/template] a report template cannot be named json, which --output takes as the json format`,
		`templates/reports/parse.tmpl:2: error [content/template] bad character U+007D '}'`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := renderReport(t, r, "fine"); got != "4\n" {
		t.Errorf("fine = %q", got)
	}
	var b bytes.Buffer
	if err := r.WriteOutput(&b, "json", "dev", Stamps{}); err != nil || !strings.Contains(b.String(), `"errors": 4,`) {
		t.Errorf("WriteOutput json = %q, %v; want the json format", b.String(), err)
	}
	var te *TemplateError
	if err := r.WriteTemplate(&bytes.Buffer{}, "field.html", Stamps{}); !errors.As(err, &te) || te.Line != 2 {
		t.Errorf("err = %v", err)
	}
}

func TestUnknownReport(t *testing.T) {
	r := runReport(t, reportRepo(t, map[string]string{"b.tmpl": "", "a.html.tmpl": ""}))
	var unknown *UnknownReportError
	err := r.WriteTemplate(&bytes.Buffer{}, "a", Stamps{})
	if !errors.As(err, &unknown) || err.Error() != `unknown report "a": not a format (text, json, junit, sarif) and there is no templates/reports/a.tmpl (reports: a.html, b)` {
		t.Errorf("err = %v", err)
	}
	r = runReport(t, ruleRepo(t, nil))
	if err := r.WriteTemplate(&bytes.Buffer{}, "../rules/x", Stamps{}); err == nil || err.Error() != `unknown report "../rules/x": not a format (text, json, junit, sarif) and there is no templates/reports/../rules/x.tmpl` {
		t.Errorf("err = %v", err)
	}
}
