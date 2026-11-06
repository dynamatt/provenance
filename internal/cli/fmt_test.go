package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// widgets are two Widgets out of canonical form, WID/WID-1.md and
// DOC/WID-2.md, and their schema.
var widgets = map[string]string{
	"schema/Widget.yaml": "type: Widget\nid_prefix: WID\nfields:\n  - name: title\n    type: string\n    title: true\n  - name: uses\n    type: link\n    target: [Widget]\n    cardinality: many\n",
	"WID/WID-1.md":       messy("WID-1"),
	"DOC/WID-2.md":       messy("WID-2"),
}

// messy is a Widget out of canonical form; tidy is the same in it.
func messy(id string) string {
	return "---\ntitle: '" + id + "'\nuses: [WID-1]\ntype: Widget\nid: " + id + "\n---\n"
}

func tidy(id string) string {
	return "---\nid: " + id + "\ntype: Widget\ntitle: " + id + "\nuses:\n  - WID-1\n---\n"
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fmt formats only the files changed since HEAD, an untracked one
// included; a committed file nobody touched is left alone.
func TestFmtChangedFiles(t *testing.T) {
	root := gitRepo(t, widgets)
	if code, out, errOut := run("fmt", "--check"); code != 0 || out != "" {
		t.Fatalf("nothing changed: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	writeFile(t, root, "WID/WID-1.md", messy("WID-1")+"Edited.\n")
	writeFile(t, root, "WID/WID-3.md", messy("WID-3"))
	code, out, errOut := run("fmt", "--check")
	if code != 1 || out != "WID/WID-1.md\nWID/WID-3.md\n" || errOut != "fmt: the files listed are not in canonical form\n" {
		t.Errorf("--check: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, out, _ := run("fmt"); code != 0 || out != "WID/WID-1.md\nWID/WID-3.md\n" {
		t.Errorf("fmt: exit %d, stdout %q", code, out)
	}
	if got, want := read(t, root, "WID/WID-1.md"), tidy("WID-1")+"Edited.\n"; got != want {
		t.Errorf("WID-1:\n%s\nwant:\n%s", got, want)
	}
	if got := read(t, root, "DOC/WID-2.md"); got != messy("WID-2") {
		t.Errorf("WID-2, not changed, was rewritten:\n%s", got)
	}
	if code, out, _ := run("fmt", "--check"); code != 0 || out != "" {
		t.Errorf("--check after fmt: exit %d, stdout %q", code, out)
	}
}

// --all takes every entity file, within the paths given.
func TestFmtAll(t *testing.T) {
	root := gitRepo(t, widgets)
	if code, out, _ := run("fmt", "--all", "--check"); code != 1 || out != "DOC/WID-2.md\nWID/WID-1.md\n" {
		t.Errorf("--all --check: exit %d, stdout %q", code, out)
	}
	t.Chdir(filepath.Join(root, "WID"))
	if code, out, _ := run("fmt", "--all", "."); code != 0 || out != "WID/WID-1.md\n" {
		t.Errorf("--all .: exit %d, stdout %q", code, out)
	}
	if code, out, _ := run("fmt", "--all", "--check", "../DOC/WID-2.md"); code != 1 || out != "DOC/WID-2.md\n" {
		t.Errorf("--all --check a file: exit %d, stdout %q", code, out)
	}
	if code, _, errOut := run("fmt", "--all", "../.."); code != 2 || errOut == "" {
		t.Errorf("a path outside: exit %d, stderr %q", code, errOut)
	}
}

// A file fmt can't read is skipped and reported, exit 1, and the rest
// are formatted.
func TestFmtSkipsUnreadable(t *testing.T) {
	root := gitRepo(t, widgets)
	writeFile(t, root, "WID/WID-1.md", messy("WID-1")+"Edited.\n")
	writeFile(t, root, "WID/WID-4.md", "---\nid: WID-4\ntype: Gadget\n---\n")
	code, out, errOut := run("fmt")
	if code != 1 || out != "WID/WID-1.md\n" ||
		!strings.HasPrefix(errOut, "fmt: WID/WID-4.md:3: type \"Gadget\" is not declared in schema/ (declared: Widget); skipped\n") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// Outside git, no file has changed since HEAD: fmt needs --all.
func TestFmtWithoutGit(t *testing.T) {
	root := newRepo(t)
	for rel, content := range widgets {
		writeFile(t, root, rel, content)
	}
	if code, _, errOut := run("fmt"); code != 2 || errOut == "" {
		t.Errorf("fmt: exit %d, stderr %q", code, errOut)
	}
	if code, out, _ := run("fmt", "--all"); code != 0 || out != "DOC/WID-2.md\nWID/WID-1.md\n" {
		t.Errorf("--all: exit %d, stdout %q", code, out)
	}
}

// A broken schema can't order any file's keys.
func TestFmtBrokenSchema(t *testing.T) {
	root := gitRepo(t, widgets)
	writeFile(t, root, "schema/Widget.yaml", "type: [\n")
	if code, _, errOut := run("fmt", "--all"); code != 2 || errOut == "" {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}
