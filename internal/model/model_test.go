package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/schema"
)

const typeYAML = `type: Thing
fields:
  - {name: title, type: string}
  - {name: label, type: string}
  - {name: statement, type: text, body: true}
  - {name: note, type: text}
  - {name: status, type: Status}
  - {name: score, type: number}
  - {name: done, type: boolean}
  - {name: when, type: date}
  - {name: parent, type: link, target: [Thing], cardinality: one}
  - {name: refs, type: link, target: [Thing], cardinality: many}
  - name: rows
    type: list
    fields:
      - {name: name, type: string}
      - {name: due, type: date}
      - {name: rating, type: calculated, formula: "1"}
  - {name: total, type: calculated, formula: "MAX(rows[].rating)"}
`

func load(t *testing.T, front, body string) *Entity {
	t.Helper()
	root := t.TempDir()
	for rel, content := range map[string]string{
		"schema/Thing.yaml":        typeYAML,
		"schema/enums/Status.yaml": "enum: Status\nvalues: [draft, approved]\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := schema.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	e, err := entity.Parse("T/T-1.md", []byte("---\nid: T-1\ntype: Thing\n"+front+"---\n"+body))
	if err != nil || e == nil {
		t.Fatalf("parse: %v", err)
	}
	built, err := Build(s, []*entity.Entity{e})
	if err != nil {
		t.Fatal(err)
	}
	return built[0]
}

func TestTypedValues(t *testing.T) {
	e := load(t, `title: A thing
note: |
  Two
  lines
status: approved
score: 2.5
done: true
when: 2026-08-04
parent: T-2
refs: [T-3, T-4]
rows:
  - name: probe
    due: 2026-11-01
  - name: meter
total: 99
`, "\nThe body.\n\n")

	check := func(name string, ok bool) {
		t.Helper()
		v := e.Field(name)
		if v == nil || !v.Present || v.Invalid || !ok {
			t.Errorf("%s = %+v", name, v)
		}
	}
	check("title", e.Field("title").Str == "A thing")
	check("note", e.Field("note").Str == "Two\nlines\n")
	check("status", e.Field("status").Str == "approved")
	check("score", e.Field("score").Num == 2.5)
	check("done", e.Field("done").Bool)
	check("when", e.Field("when").Str == "2026-08-04")
	check("parent", strings.Join(e.Field("parent").IDs, ",") == "T-2")
	check("refs", strings.Join(e.Field("refs").IDs, ",") == "T-3,T-4")
	check("statement", e.Field("statement").Str == "The body.")

	rows := e.Field("rows").Rows
	if len(rows) != 2 || rows[0][0].Str != "probe" || rows[0][1].Str != "2026-11-01" || rows[1][1].Present {
		t.Errorf("rows = %+v", rows)
	}
	if rows[0][2].Present {
		t.Error("calculated sub-field must never be read from frontmatter")
	}
	if e.Field("total").Present {
		t.Error("calculated field must never be read from frontmatter")
	}
	if e.Field("label").Present {
		t.Error("absent field reported present")
	}
	if e.Body != "" {
		t.Errorf("freeform Body = %q, want empty when a body field exists", e.Body)
	}

	var order []string
	for _, v := range e.Fields {
		order = append(order, v.Field.Name)
	}
	if strings.Join(order, " ") != "title label statement note status score done when parent refs rows total" {
		t.Errorf("fields not in schema order: %v", order)
	}
}

func TestInvalidValuesAreKeptNotFatal(t *testing.T) {
	e := load(t, `title: 42
score: "2"
done: maybe
when: 4 August
parent: [T-2]
refs: T-3
rows: nope
`, "")
	for name, raw := range map[string]string{
		"title": "42", "score": "2", "done": "maybe", "when": "4 August",
		"parent": "[T-2]", "refs": "T-3", "rows": "nope",
	} {
		v := e.Field(name)
		if !v.Present || !v.Invalid || v.Raw != raw || v.Problem == "" {
			t.Errorf("%s = %+v, want invalid with raw %q", name, v, raw)
		}
	}
}

// A Windows checkout with core.autocrlf=true has CRLF files for the same
// commit; typed values must not depend on that.
func TestLineEndingsDoNotChangeValues(t *testing.T) {
	front := "title: T\nnote: |\n  one\n  two\n"
	body := "\nLine one\nLine two\n"
	lf := load(t, front, body)
	crlf := load(t, strings.ReplaceAll(front, "\n", "\r\n"), strings.ReplaceAll(body, "\n", "\r\n"))
	for _, name := range []string{"statement", "note", "title"} {
		if lf.Field(name).Str != crlf.Field(name).Str {
			t.Errorf("%s: LF %q vs CRLF %q", name, lf.Field(name).Str, crlf.Field(name).Str)
		}
	}
}

func TestNullIsAbsent(t *testing.T) {
	e := load(t, "title:\nscore: ~\n", "")
	if e.Field("title").Present || e.Field("score").Present {
		t.Error("null values should be absent")
	}
}

func TestTitleFallsBackToFirstStringField(t *testing.T) {
	if got := load(t, "label: Critical\n", "").Title(); got != "Critical" {
		t.Errorf("Title() = %q", got)
	}
	if got := load(t, "title: Main\nlabel: Other\n", "").Title(); got != "Main" {
		t.Errorf("Title() = %q", got)
	}
}

func TestUnknownTypeFails(t *testing.T) {
	e, _ := entity.Parse("REQ/R.md", []byte("---\nid: R\ntype: Requirment\n---\n"))
	_, err := Build(&schema.Schema{Types: map[string]*schema.Type{}}, []*entity.Entity{e})
	var ute *UnknownTypeError
	if !errors.As(err, &ute) || err.Error() != `REQ/R.md:3: type "Requirment" is not declared in schema/` {
		t.Errorf("err = %v", err)
	}
}

func buildRepo(t *testing.T, files map[string]string) []*Entity {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := schema.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := entity.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	built, err := Build(s, parsed)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestLinksResolveAndDeriveFacets(t *testing.T) {
	es := buildRepo(t, map[string]string{
		"schema/Req.yaml": `type: Req
fields:
  - {name: verified_by, type: link, target: [Ver], cardinality: many, reverse_name: verifies}
  - {name: parent, type: link, target: [Req], cardinality: one, reverse_name: children}
`,
		"schema/Ver.yaml": `type: Ver
fields:
  - name: steps
    type: list
    fields:
      - {name: checks, type: link, target: [Req], cardinality: one, reverse_name: checked_in}
`,
		"R/R-2.md": "---\nid: R-2\ntype: Req\nverified_by: [V-1, V-404]\nparent: R-1\n---\n",
		"R/R-1.md": "---\nid: R-1\ntype: Req\nverified_by: [V-1]\n---\n",
		"R/R-3.md": "---\nid: R-3\ntype: Req\nparent: R-1\n---\n",
		"V/V-1.md": "---\nid: V-1\ntype: Ver\nsteps:\n  - checks: R-1\n  - checks: R-1\n---\n",
	})
	byID := map[string]*Entity{}
	for _, e := range es {
		byID[e.ID] = e
	}
	ids := func(in *Incoming) string {
		var out []string
		for _, e := range in.From {
			out = append(out, e.ID)
		}
		return strings.Join(out, ",")
	}

	vb := byID["R-2"].Field("verified_by")
	if vb.Targets[0] != byID["V-1"] || vb.Targets[1] != nil {
		t.Errorf("R-2 verified_by targets = %v (want V-1 resolved, V-404 unresolved)", vb.Targets)
	}
	if got := ids(byID["V-1"].Facet("verifies")); got != "R-1,R-2" {
		t.Errorf("V-1 verifies = %s, want R-1,R-2 (sorted, from the Req side only)", got)
	}
	if got := ids(byID["R-1"].Facet("children")); got != "R-2,R-3" {
		t.Errorf("R-1 children = %s", got)
	}
	if got := ids(byID["R-1"].Facet("checked_in")); got != "V-1" {
		t.Errorf("R-1 checked_in = %s, want V-1 once (from two list rows)", got)
	}
	if got := byID["R-3"].Facet("children"); got == nil || len(got.From) != 0 {
		t.Errorf("R-3 should have an empty children facet, got %+v", got)
	}
}

func TestListItemsAndRecordRows(t *testing.T) {
	es := buildRepo(t, map[string]string{
		"schema/enums/Status.yaml":  "enum: Status\nvalues: [draft, approved]\n",
		"schema/records/Probe.yaml": "record: Probe\nfields:\n  - {name: serial, type: string}\n  - {name: due, type: date}\n",
		"schema/Ev.yaml": `type: Ev
fields:
  - {name: probes, type: list, of: Probe}
  - {name: standards, type: list, of: string}
  - {name: readings, type: list, of: number}
  - {name: reviews, type: list, of: Status}
  - {name: empty, type: list, of: string}
  - {name: scalar, type: list, of: string}
`,
		"E/E-1.md": `---
id: E-1
type: Ev
probes:
  - {serial: P-1, due: 2026-11-01}
standards: [IEC 60601-1, ISO 14971]
readings: [1, 2.5, high]
reviews: [draft, approved]
empty: []
scalar: IEC 62304
---
`,
	})
	e := es[0]
	if rows := e.Field("probes").Rows; len(rows) != 1 || rows[0][0].Str != "P-1" || rows[0][1].Str != "2026-11-01" {
		t.Errorf("probes rows = %+v", rows)
	}
	items := func(name string) []*Value {
		v := e.Field(name)
		if !v.Present || v.Invalid || v.Rows != nil {
			t.Errorf("%s = %+v, want a valid item list", name, v)
		}
		return v.Items
	}
	if it := items("standards"); len(it) != 2 || it[0].Str != "IEC 60601-1" || it[1].Str != "ISO 14971" {
		t.Errorf("standards = %+v", it)
	}
	if it := items("reviews"); len(it) != 2 || it[1].Str != "approved" || it[1].Field.Kind != schema.Enum {
		t.Errorf("reviews = %+v", it)
	}
	// A bad item is marked on the item; the list stays valid.
	if it := items("readings"); len(it) != 3 || it[1].Num != 2.5 || it[1].Invalid || !it[2].Invalid || it[2].Raw != "high" {
		t.Errorf("readings = %+v", it)
	}
	if it := items("empty"); len(it) != 0 {
		t.Errorf("empty = %+v", it)
	}
	if v := e.Field("scalar"); !v.Invalid || v.Problem != "not a list" || v.Raw != "IEC 62304" {
		t.Errorf("scalar = %+v, want invalid: not a list", v)
	}
}
