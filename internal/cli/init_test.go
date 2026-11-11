package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/version"
)

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// A fresh folder gets the starter, a git repository with no commits, and
// validates clean.
func TestInitFresh(t *testing.T) {
	t.Chdir(t.TempDir())
	code, out, errOut := run("init", "Therm-o 2", "--code", "THERMO2")
	if code != 0 || !strings.HasPrefix(out, "Wrote the medical-device starter for Therm-o 2 (THERMO2) to Therm-o 2: ") ||
		!strings.Contains(out, "Created a git repository there, on branch main, with no commits.") || errOut != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if got := read(t, "Therm-o 2", ".component"); !strings.Contains(got, "code: THERMO2\nname: Therm-o 2\n") {
		t.Errorf(".component:\n%s", got)
	}
	abs, _ := filepath.Abs("Therm-o 2")
	if _, err := history.Open(abs); !errors.Is(err, history.ErrNoCommits) {
		t.Errorf("history: %v, want no commits", err)
	}
	if exists(t, "Therm-o 2/.github/workflows/validate.yml") {
		t.Error("a development build wrote a CI workflow")
	}
	t.Chdir("Therm-o 2")
	if code, out, _ := run("validate"); code != 0 || out != "0 errors, 0 warnings\n" {
		t.Errorf("validate: exit %d, %q", code, out)
	}
	if code, out, _ := run("fmt", "--check"); code != 0 || out != "" {
		t.Errorf("fmt --check, every file new: exit %d, %q", code, out)
	}
}

// In a folder already in a git repository, alongside unrelated files,
// init writes the starter and leaves git alone.
func TestInitInGitRepository(t *testing.T) {
	root := t.TempDir()
	if _, err := git.PlainInit(root, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "dhf/notes.txt", "mine\n")
	t.Chdir(root)
	code, out, _ := run("init", "dhf", "--code", "DHF1", "--name", "Design file")
	if code != 0 || strings.Contains(out, "Created a git repository") {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if exists(t, "dhf/.git") {
		t.Error("init ran git init inside a repository")
	}
	if got := read(t, "dhf", ".component"); !strings.Contains(got, "code: DHF1\nname: Design file\n") {
		t.Errorf(".component:\n%s", got)
	}
	if read(t, "dhf", "notes.txt") != "mine\n" {
		t.Error("an unrelated file changed")
	}
}

// Nothing is written over a file, inside a Provenance repository, for an
// unknown template or an invalid code: exit 2.
func TestInitRefuses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "taken/README.md", "mine\n")
	t.Chdir(root)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"init", "taken", "--code", "TAKEN"}, "init: nothing was written: taken already has README.md\n"},
		{[]string{"init", "new", "--code", "NEW", "--template", "nope"}, `init: no starter "nope" (starters: medical-device)` + "\n"},
		{[]string{"init", "new"}, "init: --code is required: the component code, 1 to 8 upper-case letters and digits, such as --code NEURO\n"},
		{[]string{"init", "new", "--code", "lower"}, `init: component code "lower" is not 1 to 8 upper-case letters and digits; give one with --code` + "\n"},
	} {
		if code, _, errOut := run(c.args...); code != 2 || errOut != c.want {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, errOut)
		}
	}
	if exists(t, "taken/USR") || exists(t, "new") {
		t.Error("a refused init wrote files")
	}
	newRepo(t)
	if code, _, errOut := run("init", "sub"); code != 2 || errOut != "init: sub is inside the Provenance repository whose root is . (it holds a .component file, which is hidden: ls -a shows it); run init outside it\n" {
		t.Errorf("inside a repository: exit %d, stderr %q", code, errOut)
	}
}

// A release writes the CI workflow, pinned to itself.
func TestInitWorkflow(t *testing.T) {
	prev := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = prev })
	t.Chdir(t.TempDir())
	if code, out, _ := run("init", "p", "--code", "P"); code != 0 || !strings.Contains(out, "set PROVENANCE_SHA256") {
		t.Fatalf("exit %d, stdout %q", code, out)
	}
	if got := read(t, "p", ".github/workflows/validate.yml"); !strings.Contains(got, "PROVENANCE_VERSION: v1.2.3\n") {
		t.Errorf("workflow:\n%s", got)
	}
}
