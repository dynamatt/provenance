package diff

import (
	"errors"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/tmpl"
)

func page(body string) []byte {
	return []byte("<!DOCTYPE html><html><head><title>x</title></head><body><main>" + body + "</main></body></html>")
}

// A page is marked block by block: a changed paragraph word by word in
// the new page's markup, an added row, a removed list item in a list of
// its own, and unchanged runs folded.
func TestComparePages(t *testing.T) {
	old := page(`<h1>Title</h1><p>Intro.</p><p>The ceiling is <b>8</b> mA under (<a href="DES-0001.html">DES-0001</a>) any fault.</p>
<table><tbody><tr><td>1</td><td>one</td></tr></tbody></table><ul><li>keep</li><li>gone item</li></ul><h2>Later</h2><p>Same.</p>`)
	new := page(`<h1>Title</h1><p>Intro.</p><p>The ceiling is <b>6</b> mA under (<a href="DES-0001.html">DES-0001</a>) any single fault.</p>
<table><tbody><tr><td>1</td><td>one</td></tr><tr><td>2</td><td>two</td></tr></tbody></table><ul><li>keep</li></ul><h2>Later</h2><p>Same.</p>`)
	got, n, err := comparePages(old, new, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		`<details class="pd-fold"><summary>2 unchanged blocks: Title</summary>`,
		`<p class="pd-changed">The ceiling is <b><del class="pd">8</del><ins class="pd">6</ins></b> mA under (<a href="#pd-page-DES-0001">DES-0001</a>) any <ins class="pd">single</ins> fault.</p>`,
		`<tr class="pd-added"><td>2</td><td>two</td></tr>`,
		`<li>keep</li><li class="pd-removed">gone item</li>`,
		`<summary>2 unchanged blocks: Later</summary>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s\nin %s", want, s)
		}
	}
	if n != 3 {
		t.Errorf("%d blocks marked, want 3", n)
	}
	if _, n, _ := comparePages(old, old, nil, nil); n != 0 {
		t.Errorf("a page differs from itself in %d blocks", n)
	}
}

// The html output holds each page that differs, an image inline, and
// says a page changed only by another change; the same refs give the same
// bytes. A project's template is an output, and replaces a built-in of
// its name; an unknown output and a broken template are errors.
func TestRenderAndOutputs(t *testing.T) {
	tr := newRepo(t)
	old := tr.commit(with(schemaFiles, map[string]string{
		"P/PRT-0001.md": "---\nid: PRT-0001\ntype: Part\ntitle: Housing\nuses: [PRT-0002]\n---\nIt is 8 mm thick.\n\n![The housing](../assets/a.png)\n",
		"P/PRT-0002.md": "---\nid: PRT-0002\ntype: Part\ntitle: Board\n---\nA board.\n",
		"assets/a.png":  "png1",
	}))
	new := tr.commit(map[string]string{
		"P/PRT-0001.md":                  "---\nid: PRT-0001\ntype: Part\ntitle: Housing\nuses: [PRT-0002]\n---\nIt is 6 mm thick.\n\n![The housing](../assets/a.png)\n",
		"P/PRT-0002.md":                  "---\nid: PRT-0002\ntype: Part\ntitle: Main board\n---\nA board.\n",
		"assets/a.png":                   "png2",
		"templates/diff/summary.md.tmpl": "{{range .Entities}}- {{.ID}} {{.Op}}\n{{end}}",
		"templates/diff/text.tmpl":       "mine: {{summary .}}\n",
		"templates/diff/broken.tmpl":     "{{.Nope}}",
	})
	d := tr.diff(old, new)
	if err := d.Render(); err != nil || d.NotRendered != "" {
		t.Fatal(err, d.NotRendered)
	}
	var names []string
	for _, p := range d.Pages {
		names = append(names, p.Name+" "+string(p.Op))
	}
	if got := strings.Join(names, ", "); got != "PRT-0001 changed, PRT-0002 changed" {
		t.Errorf("pages %s", got)
	}
	b, err := d.Output("html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`<del class="pd">8</del><ins class="pd">6</ins>`,
		`class="pd-image-changed"`,
		`src="data:image/png;base64,cG5nMg=="`,
		`<code>` + new + `</code>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("html output lacks %s", want)
		}
	}
	again, _ := tr.diff(old, new).renderHTML(t)
	if again != s {
		t.Error("the html output differs between two runs")
	}
	if b, err := d.Output("summary.md"); err != nil || string(b) != "- PRT-0001 changed\n- PRT-0002 changed\n" {
		t.Errorf("summary.md: %q, %v", b, err)
	}
	if b, err := d.Output("text"); err != nil || string(b) != "mine: 2 entities: 2 changed; 4 other files changed\n" {
		t.Errorf("a project's text.tmpl: %q, %v", b, err)
	}
	var unknown *UnknownOutputError
	if _, err := d.Output("nope"); !errors.As(err, &unknown) || err.Error() != "--output nope: no such output (text, html, broken, summary.md)" {
		t.Errorf("unknown output: %v", err)
	}
	var te *tmpl.Error
	if _, err := d.Output("broken"); !errors.As(err, &te) || te.Path != "templates/diff/broken.tmpl" {
		t.Errorf("broken template: %v", err)
	}
}

func (d *Diff) renderHTML(t *testing.T) (string, error) {
	t.Helper()
	if err := d.Render(); err != nil {
		t.Fatal(err)
	}
	b, err := d.Output("html")
	return string(b), err
}
