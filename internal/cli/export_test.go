package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo creates a minimal repository and makes it the working directory.
// The example repository is the main fixture (scripts/acceptance.sh); these
// tests cover edge cases only.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".component"), []byte("code: TST\nname: Test System\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

func TestExportWebsiteWritesIndex(t *testing.T) {
	root := newRepo(t)
	code, out, errOut := run("export", "website")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if out != "exported website to _site\n" {
		t.Errorf("stdout = %q", out)
	}
	index, err := os.ReadFile(filepath.Join(root, "_site", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "<h1>Test System (TST)</h1>") {
		t.Errorf("index.html missing component heading:\n%s", index)
	}
}

func TestExportIsDeterministic(t *testing.T) {
	root := newRepo(t)
	read := func(dir string) map[string]string {
		got := map[string]string{}
		entries, _ := os.ReadDir(filepath.Join(root, dir))
		for _, e := range entries {
			b, _ := os.ReadFile(filepath.Join(root, dir, e.Name()))
			got[e.Name()] = string(b)
		}
		return got
	}
	for _, dir := range []string{"a", "b"} {
		if code, _, errOut := run("export", "website", "--out", dir); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	}
	a, b := read("a"), read("b")
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("a=%d files, b=%d files", len(a), len(b))
	}
	for name := range a {
		if a[name] != b[name] {
			t.Errorf("%s differs between exports", name)
		}
	}
}

func TestExportErrorsExitTwo(t *testing.T) {
	root := newRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "notmine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notmine", "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"export", "pdf"}, "export: pdf is not available in this release"},
		{[]string{"export", "docx"}, "export: docx is not available in this release"},
		{[]string{"export", "latex"}, `export: unknown format "latex" (available: website)`},
		{[]string{"export", "website", "--out", "notmine"}, "export: output folder notmine is not empty"},
		{[]string{"export", "website", "--scope", "DOC/DOC-0001.md"}, "export: --scope is not implemented yet"},
	}
	for _, tc := range cases {
		code, _, errOut := run(tc.args...)
		if code != 2 || !strings.HasPrefix(errOut, tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want 2 and %q", tc.args, code, errOut, tc.want)
		}
	}
}

func TestExportOutsideRepositoryExitsTwo(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, errOut := run("export", "website")
	if code != 2 || !strings.Contains(errOut, "not inside a provenance repository") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}
