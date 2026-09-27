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
