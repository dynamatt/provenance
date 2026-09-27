package entity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const req = "---\nid: REQ-0001\ntype: Requirement\ntitle: Closed-loop\n---\nThe system shall.\n"

func TestParseEntity(t *testing.T) {
	e, err := Parse("REQ/REQ-0001.md", []byte(req))
	if err != nil || e == nil {
		t.Fatalf("Parse: %v, %v", e, err)
	}
	if e.ID != "REQ-0001" || e.Type != "Requirement" || e.Path != "REQ/REQ-0001.md" {
		t.Errorf("got %+v", e)
	}
	if e.Body != "The system shall.\n" || e.BodyLine != 6 {
		t.Errorf("Body = %q at line %d", e.Body, e.BodyLine)
	}
	if string(e.Raw) != req {
		t.Error("Raw is not the exact file bytes")
	}
	if title, ok := e.Scalar("title"); !ok || title != "Closed-loop" {
		t.Errorf("title = %q, %v", title, ok)
	}
	if n := Lookup(e.Front, "title"); n.Line != 4 {
		t.Errorf("title node line = %d, want file line 4", n.Line)
	}
}

func TestParseToleratesCRLFAndBOM(t *testing.T) {
	crlf := strings.ReplaceAll(req, "\n", "\r\n")
	for name, raw := range map[string][]byte{
		"crlf": []byte(crlf),
		"bom":  append(append([]byte{}, utf8BOM...), req...),
	} {
		e, err := Parse("x.md", raw)
		if err != nil || e == nil || e.ID != "REQ-0001" {
			t.Errorf("%s: %v, %v", name, e, err)
		}
	}
}

func TestParseNonEntities(t *testing.T) {
	for name, content := range map[string]string{
		"no frontmatter":     "# README\n\nJust prose.\n",
		"thematic break":     "text\n---\nmore\n",
		"unclosed":           "---\nid: X\ntype: Y\n",
		"no id":              "---\ntype: Requirement\n---\n",
		"no type":            "---\nid: REQ-0001\n---\n",
		"empty frontmatter":  "---\n---\nbody\n",
		"list frontmatter":   "---\n- a\n- b\n---\n",
		"delimiter not line": "---x\nid: A\ntype: B\n---\n",
	} {
		e, err := Parse("x.md", []byte(content))
		if e != nil || err != nil {
			t.Errorf("%s: got %v, %v; want not an entity", name, e, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for content, want := range map[string]string{
		"---\nid: REQ-0001\ntype: [unclosed\n---\n": "REQ/bad.md:",
		"---\nid: 42\ntype: Requirement\n---\n":     "REQ/bad.md:2: id must be a non-empty string",
		"---\nid: A\ntype: [x]\n---\n":              "REQ/bad.md:3: type must be a non-empty string",
		"---\nid: \"\"\ntype: T\n---\n":             "REQ/bad.md:2: id must be a non-empty string",
	} {
		_, err := Parse("REQ/bad.md", []byte(content))
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: err = %v, want prefix %q", content, err, want)
		}
	}
}

func TestParseYAMLErrorUsesFileLines(t *testing.T) {
	_, err := Parse("a.md", []byte("---\nid: A\ntype: B\ntitle: x: y\n---\n"))
	var fe *Error
	if !errors.As(err, &fe) || fe.Line != 4 || !strings.Contains(fe.Msg, "invalid frontmatter") {
		t.Errorf("err = %v, want line 4 invalid frontmatter", err)
	}
}

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

func entityFile(id, typ string) string {
	return "---\nid: " + id + "\ntype: " + typ + "\n---\n"
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	write(t, root, "REQ/REQ-0002.md", entityFile("REQ-0002", "Requirement"))
	write(t, root, "REQ/REQ-0001.md", entityFile("REQ-0001", "Requirement"))
	write(t, root, "USR/deep/USR-0001.MD", entityFile("USR-0001", "UserNeed"))
	write(t, root, "README.md", "# not an entity\n")
	write(t, root, "REQ/notes.txt", entityFile("TXT-1", "Ignored"))
	// Configuration folders at the root are skipped...
	for _, d := range ConfigDirs {
		write(t, root, d+"/x.md", entityFile("CFG-"+d, "Ignored"))
	}
	// ...but only at the root.
	write(t, root, "DOC/templates/DOC-0001.md", entityFile("DOC-0001", "Document"))
	write(t, root, ".git/x.md", entityFile("GIT-1", "Ignored"))
	write(t, root, "sub/.git/x.md", entityFile("GIT-2", "Ignored"))
	write(t, root, "_site/copy.md", entityFile("REQ-0001", "Requirement"))

	got, err := Discover(root, filepath.Join(root, "_site"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range got {
		ids = append(ids, e.ID+"="+e.Path)
	}
	want := "DOC-0001=DOC/templates/DOC-0001.md REQ-0001=REQ/REQ-0001.md REQ-0002=REQ/REQ-0002.md USR-0001=USR/deep/USR-0001.MD"
	if strings.Join(ids, " ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(ids, " "), want)
	}
}

func TestDiscoverDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "REQ/REQ-0001.md", entityFile("REQ-0001", "Requirement"))
	write(t, root, "REQ/copy.md", entityFile("REQ-0001", "Requirement"))
	_, err := Discover(root)
	var dup *DuplicateIDError
	if !errors.As(err, &dup) || err.Error() != "duplicate entity ID REQ-0001 in REQ/REQ-0001.md and REQ/copy.md" {
		t.Errorf("err = %v", err)
	}
}

func TestDiscoverReportsParseErrorsWithRelativePath(t *testing.T) {
	root := t.TempDir()
	write(t, root, "REQ/bad.md", "---\nid: 1\ntype: Requirement\n---\n")
	_, err := Discover(root)
	if err == nil || !strings.HasPrefix(err.Error(), "REQ/bad.md:2:") {
		t.Errorf("err = %v", err)
	}
}
