package website

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
)

// Rows of CSV files (DES-0053): a page each unless pages: false, a table
// page per file, and links to a row without a page pointing at its row on
// the table page.

const tableSchemas = `type: Bug
id_prefix: BUG
source: {csv: data/bugs.csv, id: Key, pages: false}
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

func tableRepo(extra map[string]string) map[string]string {
	files := map[string]string{
		"schema/Note.yaml": noteSchema,
		"schema/Bug.yaml":  tableSchemas,
		"schema/Part.yaml": partSchema,
		"N/N-1.md":         note("N-1", "title: One\n", "Cites [[BUG-7]] and [[PRT-0001]].\n"),
		"data/bugs.csv":    "Key,Summary,affects,notes\nBUG-7,Crash,N-1,\"See [[N-1]].\"\nBUG-9,Slow,,\n",
		"data/parts.csv":   "id,title\nPRT-0001,Lead\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

func TestTablePages(t *testing.T) {
	files, err := exportRepo(t, tableRepo(nil))
	if err != nil {
		t.Fatal(err)
	}
	got := pages(files)
	for _, want := range []string{"entities/Bug.html", "entities/Part.html", "entities/PRT-0001.html"} {
		if !slices.Contains(got, want) {
			t.Errorf("no %s in %v", want, got)
		}
	}
	if slices.Contains(got, "entities/BUG-7.html") {
		t.Errorf("a row of a file with pages: false has a page: %v", got)
	}
	n1 := string(files["entities/N-1.html"])
	for _, want := range []string{
		`<a class="ref" href="Bug.html#BUG-7" title="Crash">BUG-7</a>`,
		`<a class="ref" href="PRT-0001.html" title="Lead">PRT-0001</a>`,
	} {
		if !strings.Contains(n1, want) {
			t.Errorf("N-1 lacks %s:\n%s", want, n1)
		}
	}
	bug := string(files["entities/Bug.html"])
	for _, want := range []string{
		`<tr><th>File</th><td><code>data/bugs.csv</code></td></tr>`,
		`None: the table is kept in the repository`,
		`<tr><th>Rows</th><td>2</td></tr>`,
		`<tr id="BUG-7"><td class="id">BUG-7</td><td>Crash</td><td><a class="id" href="N-1.html" title="One">N-1</a></td>`,
		`See <a class="ref" href="N-1.html" title="One">N-1</a>.`,
	} {
		if !strings.Contains(bug, want) {
			t.Errorf("Bug.html lacks %s:\n%s", want, bug)
		}
	}
	part := string(files["entities/Part.html"])
	if !strings.Contains(part, `<tr id="PRT-0001"><td class="id"><a class="id" href="PRT-0001.html" title="Lead">PRT-0001</a></td><td>Lead</td></tr>`) {
		t.Errorf("Part.html:\n%s", part)
	}
	row := string(files["entities/PRT-0001.html"])
	if !strings.Contains(row, `<p class="table-row">Row 2 of <a href="Part.html">data/parts.csv</a>, a table kept in the repository.</p>`) {
		t.Errorf("PRT-0001.html:\n%s", row)
	}
	index := string(files["index.html"])
	if !strings.Contains(index, `Read from <a href="entities/Bug.html">data/bugs.csv</a>.`) {
		t.Errorf("index:\n%s", index)
	}
}

// Every entity's .File is the file it is written in, the same keys for an
// entity file and a CSV row; links to a row without a page point at its
// table page.
func TestFileTemplateData(t *testing.T) {
	files, err := exportRepo(t, tableRepo(map[string]string{
		"data/bugs.csv":       "Key,Summary,affects,notes\nBUG-7,Crash,N-1,\n",
		"schema/Bug.yaml":     strings.Replace(tableSchemas, "id: Key,", "id: Key, system: Tracker,", 1),
		"data/bugs.csv.yaml":  "exported: 2026-10-15\nsha256: " + sha256Hex("Key,Summary,affects,notes\nBUG-7,Crash,N-1,\n") + "\n",
		"templates/Note.tmpl": `{{template "file" .File}}{{range .Bugs}} {{link .}} {{href .}} {{template "file" .File}}{{end}}` + `{{define "file"}}[{{.Path}}:{{.Line}} {{.System}} {{.Exported}}]{{end}}` + "\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	n1 := string(files["entities/N-1.html"])
	want := `[N/N-1.md:2  ] <a class="ref" href="Bug.html#BUG-7" title="Crash">BUG-7</a> Bug.html#BUG-7 [data/bugs.csv:2 Tracker 2026-10-15]`
	if !strings.Contains(n1, want) {
		t.Errorf("N-1 lacks %s:\n%s", want, n1)
	}
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// A problem in a text cell of a row without a page is found on its table
// page, at the row's line.
func TestTableCheck(t *testing.T) {
	got := check(t, tableRepo(map[string]string{"data/bugs.csv": "Key,Summary,affects,notes\nBUG-7,Crash,,\"See\n[[N-404]].\"\n"}))
	want := "data/bugs.csv:3 links/unresolved [[N-404]]: no entity N-404"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A scope that holds none of a file's rows exports no table page for it.
func TestTableScope(t *testing.T) {
	files, err := exportScoped(t, tableRepo(map[string]string{
		"scopes/notes.yaml": "from: Note\n",
	}), "scopes/notes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := pages(files)
	if slices.Contains(got, "entities/Bug.html") || slices.Contains(got, "entities/Part.html") {
		t.Errorf("table pages out of scope: %v", got)
	}
	n1 := string(files["entities/N-1.html"])
	if !strings.Contains(n1, `<span class="ref out-of-scope">BUG-7</span>`) {
		t.Errorf("N-1:\n%s", n1)
	}
}
