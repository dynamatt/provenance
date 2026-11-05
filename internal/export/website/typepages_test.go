package website

import (
	"slices"
	"strings"
	"testing"
)

// Type pages (DES-0054): a type with pages: false, Markdown or CSV-backed,
// has no page per entity; its entities are shown together on the type's
// page, and every link to one points there. Other types have no type page.

const tableSchemas = `type: Bug
id_prefix: BUG
pages: false
source: {csv: data/bugs.csv, id: Key}
fields:
  - {name: title, type: string, column: Summary}
  - {name: affects, type: link, target: [Note], cardinality: many, reverse_name: bugs}
  - {name: notes, type: text}
`

const partSchema = `type: Part
id_prefix: PRT
source: {csv: data/parts.csv, id: id}
fields:
  - {name: title, type: string}
`

// Term has bodies, so its page has a section per entity; Level has none,
// so a table.
const termSchema = "type: Term\nid_prefix: TRM\npages: false\nfields:\n  - {name: title, type: string}\n  - {name: definition, type: text, body: true}\n"
const levelSchema = "type: Level\nid_prefix: LVL\npages: false\nfields:\n  - {name: title, type: string}\n  - {name: score, type: number}\n"

func tableRepo(extra map[string]string) map[string]string {
	files := map[string]string{
		"schema/Note.yaml":  noteSchema,
		"schema/Bug.yaml":   tableSchemas,
		"schema/Part.yaml":  partSchema,
		"schema/Term.yaml":  termSchema,
		"schema/Level.yaml": levelSchema,
		"N/N-1.md":          note("N-1", "title: One\n", "Cites [[BUG-7]], [[PRT-0001]], [[TRM-0001]] and [[LVL-0001]].\n"),
		"T/TRM-0001.md":     "---\nid: TRM-0001\ntype: Term\ntitle: Lead\n---\nThe wire from the implant to the electrodes.\n\n# Kinds\n",
		"L/LVL-0001.md":     "---\nid: LVL-0001\ntype: Level\ntitle: Minor\nscore: 1\n---\n",
		"data/bugs.csv":     "Key,Summary,affects,notes\nBUG-7,Crash,N-1,\"See [[N-1]].\"\nBUG-9,Slow,,\n",
		"data/parts.csv":    "id,title\nPRT-0001,Lead\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

func TestTypePages(t *testing.T) {
	files, err := exportRepo(t, tableRepo(nil))
	if err != nil {
		t.Fatal(err)
	}
	got := pages(files)
	for _, want := range []string{"entities/Bug.html", "entities/Term.html", "entities/Level.html", "entities/PRT-0001.html"} {
		if !slices.Contains(got, want) {
			t.Errorf("no %s in %v", want, got)
		}
	}
	for _, not := range []string{"entities/Part.html", "entities/BUG-7.html", "entities/TRM-0001.html", "entities/LVL-0001.html"} {
		if slices.Contains(got, not) {
			t.Errorf("%s exported: %v", not, got)
		}
	}
	n1 := string(files["entities/N-1.html"])
	for _, want := range []string{
		`<a class="ref" href="Bug.html#BUG-7" title="Crash">BUG-7</a>`,
		`<a class="ref" href="PRT-0001.html" title="Lead">PRT-0001</a>`,
		`<a class="ref" href="Term.html#TRM-0001" title="Lead">TRM-0001</a>`,
		`<a class="ref" href="Level.html#LVL-0001" title="Minor">LVL-0001</a>`,
	} {
		if !strings.Contains(n1, want) {
			t.Errorf("N-1 lacks %s:\n%s", want, n1)
		}
	}
	for page, wants := range map[string][]string{
		"entities/Bug.html": {
			`<h1>Bug</h1>`,
			`Read from <code>data/bugs.csv</code>.`,
			`<tr id="BUG-7"><td class="id">BUG-7</td><td>Crash</td><td><a class="id" href="N-1.html" title="One">N-1</a></td>`,
			`See <a class="ref" href="N-1.html" title="One">N-1</a>.`,
		},
		"entities/Level.html": {
			`<tr id="LVL-0001"><td class="id">LVL-0001</td><td>Minor</td><td>1</td></tr>`,
		},
		// An entity with a body is a section, rendered as its type
		// template would, one heading level down, at its own anchor.
		"entities/Term.html": {
			`<section class="type-entity">`,
			`<article class="entity" id="TRM-0001">`,
			`<h2><span class="id">TRM-0001</span> Lead</h2>`,
			`<h2>Kinds</h2>`,
		},
		"entities/PRT-0001.html": {`<p class="table-row">Row 2 of <code>data/parts.csv</code>.</p>`},
		"index.html": {
			`<h2><a href="entities/Bug.html">Bug</a></h2>`,
			`Read from <code>data/bugs.csv</code>.`,
			`<h2><a href="entities/Term.html">Term</a></h2>`,
			`<h2>Part</h2>`,
		},
	} {
		html := string(files[page])
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s lacks %s:\n%s", page, want, html)
			}
		}
	}
}

// templates/_<Type>.tmpl renders a type page, receiving the type, its
// entities and, for a CSV-backed type, its file.
func TestTypePageTemplate(t *testing.T) {
	files, err := exportRepo(t, tableRepo(map[string]string{
		"templates/_Term.tmpl": `<dl>{{range .Entities}}<dt id="{{.ID}}">{{.Title}}</dt><dd>{{markdown .Body}}</dd>{{end}}</dl>` + "\n",
		"templates/_Bug.tmpl":  `<h1>{{.Type}} from {{.File.Path}}</h1>{{range .Entities}}<p id="{{.ID}}">{{link .}}</p>{{end}}` + "\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if term := string(files["entities/Term.html"]); !strings.Contains(term, `<dl><dt id="TRM-0001">Lead</dt><dd><p>The wire from the implant to the electrodes.</p>`) {
		t.Errorf("Term.html:\n%s", term)
	}
	if bug := string(files["entities/Bug.html"]); !strings.Contains(bug, `<h1>Bug from data/bugs.csv</h1><p id="BUG-7"><a class="ref" href="Bug.html#BUG-7" title="Crash">BUG-7</a></p>`) {
		t.Errorf("Bug.html:\n%s", bug)
	}
}

// A _<Type>.tmpl for a type whose entities have their own pages renders
// nothing, and validate says why.
func TestTypePageTemplateUnused(t *testing.T) {
	got := check(t, tableRepo(map[string]string{"templates/_Part.tmpl": "x\n"}))
	want := "templates/_Part.tmpl:0 content/unknown-override (warning) _Part.tmpl would render the page of type Part, which has none: its entities have pages of their own (set pages: false on Part to show them together)"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Every entity's .File is the file it is written in, the same keys for an
// entity file and a CSV row; links to a row without a page point at its
// table page.
func TestFileTemplateData(t *testing.T) {
	files, err := exportRepo(t, tableRepo(map[string]string{
		"data/bugs.csv":       "Key,Summary,affects,notes\nBUG-7,Crash,N-1,\n",
		"templates/Note.tmpl": `{{template "file" .File}}{{range .Bugs}} {{link .}} {{href .}} {{template "file" .File}}{{end}}` + `{{define "file"}}[{{.Path}}:{{.Line}}]{{end}}` + "\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	n1 := string(files["entities/N-1.html"])
	want := `[N/N-1.md:2] <a class="ref" href="Bug.html#BUG-7" title="Crash">BUG-7</a> Bug.html#BUG-7 [data/bugs.csv:2]`
	if !strings.Contains(n1, want) {
		t.Errorf("N-1 lacks %s:\n%s", want, n1)
	}
}

// A problem in a text cell of a row without a page is found on its type
// page, at the row's line.
func TestTypePageCheck(t *testing.T) {
	got := check(t, tableRepo(map[string]string{"data/bugs.csv": "Key,Summary,affects,notes\nBUG-7,Crash,,\"See\n[[N-404]].\"\n"}))
	want := "data/bugs.csv:3 links/unresolved [[N-404]]: no entity N-404"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A scope that holds none of a type's entities exports no page for it.
func TestTypePageScope(t *testing.T) {
	files, err := exportScoped(t, tableRepo(map[string]string{
		"scopes/notes.yaml": "from: Note\n",
	}), "scopes/notes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := pages(files)
	if slices.Contains(got, "entities/Bug.html") || slices.Contains(got, "entities/Term.html") {
		t.Errorf("type pages out of scope: %v", got)
	}
	n1 := string(files["entities/N-1.html"])
	if !strings.Contains(n1, `<span class="ref out-of-scope">BUG-7</span>`) {
		t.Errorf("N-1:\n%s", n1)
	}
}
