package website

import (
	"errors"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/markdown"
)

const rankedNoteSchema = noteSchema + "  - {name: rank, type: number}\n"

func queryRepo(t *testing.T, hostFront, hostBody string) (map[string]string, error) {
	t.Helper()
	files, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": rankedNoteSchema,
		"N/N-1.md":         note("N-1", "title: Third\nrank: 3\n", "# One\n\nFirst body."),
		"N/N-2.md":         note("N-2", "title: First\nrank: 1\nsee: [N-1]\n", "Second body."),
		"N/N-3.md":         note("N-3", "title: Unranked\n", "Third body."),
		"H/H-1.md":         note("H-1", hostFront, hostBody),
	})
	pages := map[string]string{}
	for k, v := range files {
		pages[k] = string(v)
	}
	return pages, err
}

func TestQueryBlockRendering(t *testing.T) {
	block := func(lines ...string) string { return "```query\n" + strings.Join(lines, "\n") + "\n```\n" }
	pages, err := queryRepo(t, "title: Host\n", "## Full\n\n"+
		block("from: Note", "where: {field: rank, operator: exists}", "order_by: rank")+
		"\n## IDs\n\n"+block("from: Note", "where: {field: id, operator: not_equals, value: H-1}", "render: id")+
		"\n## Titles\n\n"+block("from: Note", "where: {field: seen_by, operator: exists}", "render: field:title")+
		"\n## None\n\n"+block("from: Note", "where: {field: rank, operator: greater_than, value: 10}"))
	if err != nil {
		t.Fatal(err)
	}
	page := pages["entities/H-1.html"]
	for _, want := range []string{
		// Full: embedded in rank order, headings shifted under the section.
		"<h2>Full</h2>\n<div class=\"query\">\n<section class=\"embed\" data-entity=\"N-2\">",
		"</section>\n<section class=\"embed\" data-entity=\"N-1\">",
		"<h3>One</h3>",
		// IDs: every note but the host, by ID.
		"<ul class=\"query\">\n<li><a class=\"ref\" href=\"N-1.html\" title=\"Third\">N-1</a></li>\n<li><a class=\"ref\" href=\"N-2.html\"",
		// A field's value, linked.
		"<li><a class=\"ref\" href=\"N-1.html\" title=\"Third\">Third</a></li>",
		"<p class=\"query-empty\">No Note matches this query.</p>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("H-1 page missing %q\n%s", want, page)
		}
	}
	if strings.Contains(page, `data-entity="N-3"`) {
		t.Error("N-3 has no rank but was embedded")
	}
}

func TestQueryBlockErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		front, body string
		want        string
	}{
		"in the body": {"title: Host\n", "Intro.\n\n```query\nfrom: Note\nwhere:\n  field: rank\n  operator: \"=\"\n  value: 1\n```\n",
			`H/H-1.md:12: query block: unknown operator "="`},
		"after blank lines": {"title: Host\n", "\n\n```query\nfrom: Nope\n```\n",
			`H/H-1.md:9: query block: from: unknown type "Nope"`},
		"in a text field": {"title: Host\nsummary: |\n  Text.\n\n  ```query\n  from: Note\n  order_by: see\n  ```\n", "",
			`H/H-1.md:10: query block: order_by: "see" has several values`},
		"cycle through a query": {"title: Host\n", "```query\nfrom: Note\nwhere: {field: id, operator: equals, value: H-1}\n```\n",
			"H/H-1.md: embed cycle H-1 → H-1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := queryRepo(t, tc.front, tc.body)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
			var qe *QueryError
			var cycle *EmbedCycleError
			if !errors.As(err, &qe) && !errors.As(err, &cycle) {
				t.Errorf("error is a %T", err)
			}
		})
	}
}

func TestCalculatedFieldsRender(t *testing.T) {
	files, err := exportRepo(t, map[string]string{
		"schema/Item.yaml": `type: Item
fields:
  - {name: title, type: string}
  - {name: price, type: number}
  - {name: total, type: calculated, formula: "price * 2"}
  - {name: dear, type: calculated, formula: "total > 10"}
  - {name: broken, type: calculated, formula: "price +"}
`,
		"templates/_index.tmpl": `{{range .Types}}{{range .Entities}}[{{.ID}} {{.Total}} {{.Dear}} {{.Broken}}]{{end}}{{end}}`,
		"I/I-1.md":              "---\nid: I-1\ntype: Item\ntitle: Cheap\nprice: 4\n---\nTotal: [[I-1#total]], dear: [[I-1#dear]].\n",
		"I/I-2.md":              "---\nid: I-2\ntype: Item\ntitle: Unpriced\n---\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/I-1.html"])
	for _, want := range []string{
		"<tr><th>Total</th><td>8</td></tr>",
		"<tr><th>Dear</th><td>false</td></tr>",
		`<tr><th>Broken</th><td><span class="invalid-value">price &#43;</span> <span class="invalid">formula: column 8: unexpected end of formula</span></td></tr>`,
		`Total: <a class="ref" href="I-1.html" title="Cheap">8</a>, dear: <a class="ref" href="I-1.html" title="Cheap">false</a>.`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("I-1 page missing %s\n%s", want, page)
		}
	}
	if !strings.Contains(string(files["entities/I-2.html"]), `<tr><th>Total</th><td><span class="absent">—</span></td></tr>`) {
		t.Error("blank calculated value is not shown as absent")
	}
	if got := string(files["index.html"]); !strings.Contains(got, "[I-1 8 false ][I-2   ]") {
		t.Errorf("template data = %s", got)
	}
}

// A rendered language without a fence would fall back to a code listing,
// publishing a query's or diagram's source as if it were the content.
func TestEveryRenderedLanguageHasAFence(t *testing.T) {
	fences := (&resolver{}).fences()
	for _, lang := range markdown.RenderedLanguages {
		if fences[lang] == nil {
			t.Errorf("no fence registered for rendered language %q", lang)
		}
	}
}

func TestUnrenderedLanguagesAreMarked(t *testing.T) {
	pages, err := queryRepo(t, "title: Host\n", "```mermaid\ngraph TD\n  A --> B\n```\n\n```yaml\nkey: value\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	page := pages["entities/H-1.html"]
	for _, want := range []string{
		"<div class=\"unrendered\"><p class=\"unrendered-note\">mermaid blocks are not rendered by this version of Provenance</p>\n<pre><code class=\"language-mermaid\">graph TD\n  A --&gt; B\n</code></pre></div>",
		"<pre><code class=\"language-yaml\">key: value\n</code></pre>", // not a rendered language: plain code
	} {
		if !strings.Contains(page, want) {
			t.Errorf("H-1 page missing %q\n%s", want, page)
		}
	}
}

func TestMultiTypeQueryRendering(t *testing.T) {
	files, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": rankedNoteSchema,
		"schema/Task.yaml": "type: Task\nfields:\n  - {name: title, type: string}\n  - {name: rank, type: number}\n  - {name: owner, type: string}\n",
		"N/N-1.md":         note("N-1", "title: Note one\nrank: 2\n", ""),
		"T/T-1.md":         "---\nid: T-1\ntype: Task\ntitle: Task one\nrank: 1\nowner: Ann\n---\n",
		"N/N-2.md": note("N-2", "title: Host\n", "```query\nfrom: [Note, Task]\nwhere: {field: rank, operator: exists}\norder_by: rank\nrender: field:owner\n```\n\n"+
			"```query\nfrom: [Note, Task]\nwhere: {field: rank, operator: greater_than, value: 5}\n```\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/N-2.html"])
	for _, want := range []string{
		"<li><a class=\"ref\" href=\"T-1.html\" title=\"Task one\">Ann</a></li>\n<li><a class=\"ref\" href=\"N-1.html\" title=\"Note one\">N-1#owner</a> <span class=\"unresolved\">empty</span></li>",
		"<p class=\"query-empty\">No Note or Task matches this query.</p>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("N-2 page missing %q\n%s", want, page)
		}
	}
}
