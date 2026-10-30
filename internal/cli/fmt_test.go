package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const widgetSchema = "type: Widget\nid_prefix: WID\nfields:\n  - name: title\n    type: string\n  - name: uses\n    type: link\n    target: [Widget]\n    cardinality: many\n"

// messy and tidy are a Widget out of canonical form and in it.
const (
	messy = false
	tidy  = true
)

func widget(canonical bool, id string) string {
	if canonical {
		return "---\nid: " + id + "\ntype: Widget\ntitle: " + id + "\nuses:\n  - WID-1\n---\n"
	}
	return "---\ntitle: '" + id + "'\nuses: [WID-1]\ntype: Widget\nid: " + id + "\n---\n"
}

// fmtRepo is a committed repository of two Widgets out of canonical form,
// WID/WID-1.md and DOC/WID-2.md, with the working directory at its root.
// withGit is false for a folder that is not a git repository.
func fmtRepo(t *testing.T, withGit bool) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, map[string]string{
		".component":         "code: T\nname: Test\n",
		"schema/Widget.yaml": widgetSchema,
		"WID/WID-1.md":       widget(messy, "WID-1"),
		"DOC/WID-2.md":       widget(messy, "WID-2"),
	})
	if withGit {
		r, err := git.PlainInit(root, false)
		if err != nil {
			t.Fatal(err)
		}
		wt, err := r.Worktree()
		if err != nil {
			t.Fatal(err)
		}
		if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
			t.Fatal(err)
		}
		sig := &object.Signature{Name: "Ann", Email: "ann@example.com", When: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
		if _, err := wt.Commit("first", &git.CommitOptions{Author: sig}); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
	root := fmtRepo(t, true)
	if code, out, errOut := run("fmt", "--check"); code != 0 || out != "" {
		t.Fatalf("nothing changed: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	write(t, root, map[string]string{
		"WID/WID-1.md": widget(messy, "WID-1") + "Edited.\n",
		"WID/WID-3.md": widget(messy, "WID-3"),
	})
	code, out, errOut := run("fmt", "--check")
	if code != 1 || out != "WID/WID-1.md\nWID/WID-3.md\n" || errOut != "fmt: 2 files not in canonical form\n" {
		t.Errorf("--check: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, out, _ := run("fmt"); code != 0 || out != "WID/WID-1.md\nWID/WID-3.md\n" {
		t.Errorf("fmt: exit %d, stdout %q", code, out)
	}
	if got, want := read(t, root, "WID/WID-1.md"), widget(tidy, "WID-1")+"Edited.\n"; got != want {
		t.Errorf("WID-1:\n%s\nwant:\n%s", got, want)
	}
	if got := read(t, root, "DOC/WID-2.md"); got != widget(messy, "WID-2") {
		t.Errorf("WID-2, not changed, was rewritten:\n%s", got)
	}
	if code, out, _ := run("fmt", "--check"); code != 0 || out != "" {
		t.Errorf("--check after fmt: exit %d, stdout %q", code, out)
	}
}

// --all takes every entity file, within the paths given.
func TestFmtAll(t *testing.T) {
	root := fmtRepo(t, true)
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
	root := fmtRepo(t, true)
	write(t, root, map[string]string{
		"WID/WID-1.md": widget(messy, "WID-1") + "Edited.\n",
		"WID/WID-4.md": "---\nid: WID-4\ntype: Gadget\n---\n",
	})
	code, out, errOut := run("fmt")
	if code != 1 || out != "WID/WID-1.md\n" ||
		errOut != "fmt: WID/WID-4.md:3: type \"Gadget\" is not declared in schema/; skipped\nfmt: 1 file could not be read\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// Outside git, no file has changed since HEAD: fmt needs --all.
func TestFmtWithoutGit(t *testing.T) {
	fmtRepo(t, false)
	if code, _, errOut := run("fmt"); code != 2 || errOut == "" {
		t.Errorf("fmt: exit %d, stderr %q", code, errOut)
	}
	if code, out, _ := run("fmt", "--all"); code != 0 || out != "DOC/WID-2.md\nWID/WID-1.md\n" {
		t.Errorf("--all: exit %d, stdout %q", code, out)
	}
}

// A broken schema can't order any file's keys.
func TestFmtBrokenSchema(t *testing.T) {
	root := fmtRepo(t, true)
	write(t, root, map[string]string{"schema/Widget.yaml": "type: [\n"})
	if code, _, errOut := run("fmt", "--all"); code != 2 || errOut == "" {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}
