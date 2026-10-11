package validate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The example repository is the main fixture (scripts/acceptance.sh); these
// tests cover edge cases only.

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func text(t *testing.T, root string) string {
	t.Helper()
	r, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := r.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

const reqType = "type: Requirement\nid_prefix: REQ\nfields:\n  - name: title\n    type: string\n"

func TestRunEmptyRepository(t *testing.T) {
	if got := text(t, t.TempDir()); got != "0 errors, 0 warnings\n" {
		t.Errorf("got %q", got)
	}
}

func TestRunReportsEveryLoadProblemSorted(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/Requirement.yaml", reqType)
	write(t, root, "REQ/REQ-0001.md", "---\nid: REQ-0001\ntype: Requirement\n---\n")
	write(t, root, "REQ/b.md", "---\ntitle: x\nid: REQ-0001\ntype: Requirement\n---\n")
	write(t, root, "REQ/a.md", "---\nid: REQ-0001\ntype: Requirement\n---\n")
	write(t, root, "DES/DES-0001.md", "---\nid: DES-0001\ntype: Design\n---\n")
	write(t, root, "DOC/bad.md", "---\nid: [\n---\n")
	write(t, root, "DOC/half.md", "---\nid: DOC-0002\n---\n")

	want := `DES/DES-0001.md:3: error [schema/unknown-type] type "Design" is not declared in schema/ (declared: Requirement) (DES-0001)
DOC/bad.md:2: error [content/frontmatter] invalid frontmatter: did not find expected node content
DOC/half.md:2: error [content/frontmatter] frontmatter has an id but no type, so the file is not read as an entity
REQ/REQ-0001.md:2: error [content/duplicate-id] duplicate entity ID REQ-0001, also in REQ/a.md, REQ/b.md (REQ-0001)
REQ/a.md:2: error [content/duplicate-id] duplicate entity ID REQ-0001, also in REQ/REQ-0001.md, REQ/b.md (REQ-0001)
REQ/b.md:3: error [content/duplicate-id] duplicate entity ID REQ-0001, also in REQ/REQ-0001.md, REQ/a.md (REQ-0001)
6 errors, 0 warnings
`
	if got := text(t, root); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A broken schema skips the rules and the checks made against the schema,
// which would otherwise report its missing part over and over.
func TestRunBrokenSchemaSkipsRules(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/Requirement.yaml", "type: [\n")
	write(t, root, "REQ/REQ-0001.md", "---\nid: REQ-0001\ntype: Requirement\n---\n")
	write(t, root, "rules/a.yaml", "severity: error\n")
	write(t, root, "rules/b.yaml", "severity: error\n")
	write(t, root, "rules/notes.txt", "not a rule\n")

	want := `schema/Requirement.yaml:1: error [schema/file] did not find expected node content
2 rules not run: the schema has errors
1 error, 0 warnings
`
	if got := text(t, root); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSummary(t *testing.T) {
	r := &Report{Violations: []Violation{{Severity: Error}, {Severity: Warning}, {Severity: Warning}}}
	if got := r.Summary(); got != "1 error, 2 warnings" {
		t.Errorf("got %q", got)
	}
	if r.Errors() != 1 || r.Warnings() != 2 {
		t.Errorf("errors %d, warnings %d", r.Errors(), r.Warnings())
	}
}

func TestWriteTextWithoutLineOrEntity(t *testing.T) {
	r := &Report{Violations: []Violation{{Rule: "x/y", Severity: Warning, Message: "m", Path: "p.md"}}}
	var b bytes.Buffer
	if err := r.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "p.md: warning [x/y] m\n0 errors, 1 warning\n" {
		t.Errorf("got %q", got)
	}
}

// One repository breaking every schema conformance check, beside one
// conformant entity; the report is compared whole.
func TestRunSchemaConformance(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/enums/Status.yaml", "enum: Status\nvalues: [draft, approved]\n")
	write(t, root, "schema/enums/Spare.yaml", "enum: Spare\nvalues: [x]\ncolour: red\n")
	write(t, root, "schema/records/Unused.yaml", "record: Unused\nfields:\n  - name: a\n    type: string\n")
	write(t, root, "schema/Requirement.yaml", `type: Requirement
id_prefix: REQ
fields:
  - name: title
    type: string
    required: true
  - name: statement
    type: text
    body: true
  - name: status
    type: Status
    default: draft
  - name: due
    type: date
    target: [Requirement]
  - name: tags
    type: list
    of: Status
  - name: steps
    type: list
    fields:
      - name: n
        type: number
        required: true
      - name: double
        type: calculated
        formula: "n * 2"
  - name: score
    type: calculated
    formula: "nope + 1"
`)
	write(t, root, "schema/Test.yaml", `type: Test
id_prefix: REQ
fields:
  - name: verifies
    type: link
    target: [Requirement, Nothing]
    cardinality: many
    reverse_name: verified_by
`)
	write(t, root, "schema/Note.yaml", "type: Note\nfields:\n  - name: text\n    type: text\n")
	write(t, root, "REQ/REQ-0001.md", "---\nid: REQ-0001\ntype: Requirement\ntitle: Good\nstatus: approved\ndue: 2026-01-02\ntags: [draft]\nsteps:\n  - n: 1\n---\nBody\n")
	write(t, root, "REQ/REQ-0002.md", `---
id: REQ-2
type: Requirement
status: aproved
statement: in the frontmatter
verified_by: [TST-0001]
colour: red
score: 3
due: tomorrow
tags: [draft, final]
steps:
  - n: one
  - double: 2
    size: 1
---
`)

	want := `REQ/REQ-0002.md:2: error [schema/id-format] ID REQ-2 is not REQ-NNNN, the form of a Requirement ID (REQ-2)
REQ/REQ-0002.md:2: error [schema/required] required field title is not set (REQ-2)
REQ/REQ-0002.md:4: error [schema/enum-value] status: "aproved" is not one of Status: draft, approved (REQ-2)
REQ/REQ-0002.md:5: error [schema/unknown-field] statement is the Markdown body below the frontmatter, not a frontmatter field (REQ-2)
REQ/REQ-0002.md:6: error [schema/unknown-field] verified_by is derived from Test.verifies, set on the linking entity; a Requirement does not store it (REQ-2)
REQ/REQ-0002.md:7: error [schema/unknown-field] Requirement has no field "colour" (fields: title, status, due, tags, steps) (REQ-2)
REQ/REQ-0002.md:8: error [schema/calculated-set] score is calculated (nope + 1); it cannot be written in the file (REQ-2)
REQ/REQ-0002.md:9: error [schema/invalid-value] due: not a date (YYYY-MM-DD), got tomorrow (REQ-2)
REQ/REQ-0002.md:10: error [schema/enum-value] tags item 2: "final" is not one of Status: draft, approved (REQ-2)
REQ/REQ-0002.md:12: error [schema/invalid-value] steps row 1: n: not a number, got one (REQ-2)
REQ/REQ-0002.md:13: error [schema/calculated-set] steps row 2: double is calculated (n * 2); it cannot be written in the file (REQ-2)
REQ/REQ-0002.md:13: error [schema/required] steps row 2: required sub-field n is not set (REQ-2)
REQ/REQ-0002.md:14: error [schema/list-row] steps row 2: no sub-field "size" (sub-fields: n) (REQ-2)
schema/Note.yaml:1: error [schema/id-format] type Note has no id_prefix, so its IDs cannot be checked
schema/Note.yaml:1: warning [schema/unused-type] type Note has no entities and no links to or from it
schema/Requirement.yaml:1: error [schema/id-format] type Requirement has the id_prefix REQ, which Test (schema/Test.yaml) also has
schema/Requirement.yaml:12: error [schema/unknown-key] field "status": unknown key "default": fields have no defaults; a field that must have a value says required: true
schema/Requirement.yaml:15: error [schema/unknown-key] field "due": target applies only to a link field
schema/Requirement.yaml:28: error [schema/formula] field "score": formula: column 1: Requirement has no field "nope" (fields: title, statement, status, due, tags, steps, score, verified_by)
schema/Test.yaml:1: error [schema/id-format] type Test has the id_prefix REQ, which Requirement (schema/Requirement.yaml) also has
schema/Test.yaml:4: error [schema/link-target] field "verifies": target "Nothing" is not a declared type (declared: Note, Requirement, Test)
schema/enums/Spare.yaml:1: warning [schema/unused-enum] enum Spare is not used by any field
schema/enums/Spare.yaml:3: error [schema/unknown-key] enum Spare: unknown key "colour" (expected enum, values)
schema/records/Unused.yaml:1: warning [schema/unused-record] record Unused is not used by any list
21 errors, 3 warnings
`
	if got := text(t, root); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
