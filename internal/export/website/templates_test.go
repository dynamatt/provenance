package website

import (
	"errors"
	"strings"
	"testing"
)

func TestShiftHeadingsKeepsEverythingElse(t *testing.T) {
	in := `<h1 class="t" id="x">A</h1><H2>B</H2><p>h1 text</p><script>var s = "<h1>";</script><h5>C</h5><h6>D</h6>`
	want := `<h3 class="t" id="x">A</h3><H4>B</H4><p>h1 text</p><script>var s = "<h1>";</script><h6>C</h6><h6>D</h6>`
	if got := shift(in, 2); got != want {
		t.Errorf("shift:\n got  %s\n want %s", got, want)
	}
	if got := shift(in, 0); got != in {
		t.Error("shift by 0 must be the identity")
	}
}

func TestSpliceShiftsByPrecedingHeading(t *testing.T) {
	host := `<h1>Doc</h1>` + embedPlaceholder(0) + `<h2>Section</h2><p>x</p>` + embedPlaceholder(1) + `<!--provenance-embed 9--><!-- other -->`
	got, err := splice(host, []string{`<h1>First</h1>`, `<h1>Second</h1><h2>Sub</h2>`})
	if err != nil {
		t.Fatal(err)
	}
	want := `<h1>Doc</h1><h2>First</h2><h2>Section</h2><p>x</p><h3>Second</h3><h4>Sub</h4><!--provenance-embed 9--><!-- other -->`
	if got != want {
		t.Errorf("splice:\n got  %s\n want %s", got, want)
	}
}

const reqSchema = `type: Req
fields:
  - {name: title, type: string}
  - {name: statement, type: text, body: true}
  - {name: rationale, type: text}
  - {name: parent, type: link, target: [Req], cardinality: one, reverse_name: children}
  - {name: verified_by, type: link, target: [Ver], cardinality: many}
`

const verSchema = "type: Ver\nfields:\n  - {name: title, type: string}\n  - {name: score, type: number}\n"

func req(id, front, body string) string {
	return "---\nid: " + id + "\ntype: Req\n" + front + "---\n" + body
}

func TestProjectTypeTemplateAndDataModel(t *testing.T) {
	files, err := exportRepo(t, map[string]string{
		"schema/Req.yaml": reqSchema,
		"schema/Ver.yaml": verSchema,
		"templates/Req.tmpl": "<h1 class=\"r\">{{.ID}} {{.Title}}</h1>\r\n" +
			"{{markdown .Statement}}|{{if .Rationale}}R{{else}}no-rationale{{end}}|parent={{link .Parent}}|" +
			"{{range .VerifiedBy}}[{{link .}} {{.Title}} {{.Score}} resolved={{.Resolved}}]{{end}}|" +
			"children={{range .Children}}{{.ID}}{{end}}|href={{href .Parent}}|body={{.Body}}",
		"templates/README.md":   "ignored",
		"templates/Nobody.tmpl": "{{ this would not parse",
		"R/R-1.md":              req("R-1", "title: Top\nverified_by: [V-1, V-404]\n", "The *shall*."),
		"R/R-2.md":              req("R-2", "title: Child\nparent: R-1\n", "Child shall."),
		"V/V-1.md":              "---\nid: V-1\ntype: Ver\ntitle: Bench\nscore: 2.5\n---\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	r1 := string(files["entities/R-1.html"])
	for _, want := range []string{
		"<h1 class=\"r\">R-1 Top</h1>\n", // CRLF in the template normalized
		"<p>The <em>shall</em>.</p>",
		"|no-rationale|parent=|",
		`[<a class="ref" href="V-1.html" title="Bench">V-1</a> Bench 2.5 resolved=true]`,
		`[<span class="id unresolved-id">V-404</span> <span class="unresolved">unresolved</span>   resolved=false]`, // nil fields render empty
		"children=R-2|href=|body=The *shall*.",
	} {
		if !strings.Contains(r1, want) {
			t.Errorf("R-1 page missing %q\n%s", want, r1)
		}
	}
	if !strings.Contains(string(files["entities/R-2.html"]), `parent=<a class="ref" href="R-1.html" title="Top">R-1</a>|`) {
		t.Error("R-2: link to parent missing")
	}
	// A type without a project template keeps the built-in page.
	if !strings.Contains(string(files["entities/V-1.html"]), `<table class="fields">`) {
		t.Error("V-1 should use the built-in page")
	}
}

func TestSiteOverridesAreIndependent(t *testing.T) {
	base := map[string]string{
		"schema/Ver.yaml": verSchema,
		"V/V-1.md":        "---\nid: V-1\ntype: Ver\ntitle: Bench\n---\n",
	}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	files, err := exportRepo(t, with(map[string]string{"templates/style.css": "body{color:red}"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(files["style.css"]) != "body{color:red}" || !strings.Contains(string(files["index.html"]), `class="site-header"`) {
		t.Error("style override should leave the built-in layout")
	}

	files, err = exportRepo(t, with(map[string]string{
		"templates/_layout.tmpl": `<html><title>{{.Title}}</title><link href="{{.Root}}style.css">{{template "content" .}}</html>`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/V-1.html"])
	if !strings.HasPrefix(page, `<html><title>V-1 Bench</title><link href="../style.css">`) || !strings.Contains(page, `<table class="fields">`) {
		t.Errorf("layout override:\n%s", page)
	}
	if string(files["style.css"]) != string(styleCSS) {
		t.Error("layout override should keep the built-in stylesheet")
	}

	files, err = exportRepo(t, with(map[string]string{
		"templates/_index.tmpl": `{{range .Types}}{{.Type}}:{{range .Entities}}{{link .}}={{index . "Title"}}{{index . "Nope"}}{{end}}{{end}}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["index.html"]), `Ver:<a class="ref" href="entities/V-1.html" title="Bench">V-1</a>=Bench`) {
		t.Errorf("index override:\n%s", files["index.html"])
	}
}

func TestTemplateErrorsNameTheFile(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"layout syntax": {map[string]string{"templates/_layout.tmpl": "line one\n{{template \"content\" .}\n"},
			"templates/_layout.tmpl:2: "},
		"type template syntax": {map[string]string{"templates/Req.tmpl": "{{if}}"},
			"templates/Req.tmpl:1: "},
		"misspelt field": {map[string]string{"templates/Req.tmpl": "ok\n{{.Rationle}}"},
			`templates/Req.tmpl:2:2: executing "templates/Req.tmpl" at <.Rationle>: map has no entry for key "Rationle"`},
		"misspelt field inside an embed": {map[string]string{
			"templates/Req.tmpl": "{{if eq .ID \"R-2\"}}{{.Nope}}{{else}}{{markdown .Statement}}{{end}}",
			"R/R-1.md":           req("R-1", "", "![[R-2]]\n"),
		}, `templates/Req.tmpl:1:21: executing "templates/Req.tmpl" at <.Nope>`},
	} {
		t.Run(name, func(t *testing.T) {
			tc.files["schema/Req.yaml"] = reqSchema
			tc.files["schema/Ver.yaml"] = verSchema
			tc.files["R/R-2.md"] = req("R-2", "", "leaf")
			_, err := exportRepo(t, tc.files)
			var te *TemplateError
			if !errors.As(err, &te) || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("err = %v\nwant prefix %s", err, tc.want)
			}
		})
	}
}

func TestTemplateNameClashes(t *testing.T) {
	_, err := exportRepo(t, map[string]string{
		"schema/Req.yaml": "type: Req\nfields:\n  - {name: body, type: string}\n",
	})
	want := `schema/Req.yaml:3: field "body": its template name .Body is already used by the engine baseline`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v\nwant %s", err, want)
	}
}
