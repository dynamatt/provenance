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
