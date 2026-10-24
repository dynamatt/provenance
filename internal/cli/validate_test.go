package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateClean(t *testing.T) {
	newRepo(t)
	code, out, errOut := run("validate")
	if code != 0 || out != "0 errors, 0 warnings\n" || errOut != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestValidateViolationsExitOne(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "REQ/a.md", "---\nid: [\n---\n")
	code, out, errOut := run("validate")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	want := "REQ/a.md:2: error [content/frontmatter] invalid frontmatter: did not find expected node content\n1 error, 0 warnings\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if errOut != "validate: 1 error, 0 warnings\n" {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestValidatePathAndOut(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "REQ/a.md", "---\nid: REQ-0001\n---\n")
	t.Chdir(t.TempDir())
	code, out, _ := run("validate", "--path", root, "--out", "reports/v.txt")
	if code != 1 || out != "" {
		t.Errorf("exit %d, stdout %q; want 1 and nothing on stdout", code, out)
	}
	got, err := os.ReadFile(filepath.Join("reports", "v.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "REQ/a.md:2: error [content/frontmatter] frontmatter has an id but no type, so the file is not read as an entity\n1 error, 0 warnings\n"; string(got) != want {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestValidateToolErrorsExitTwo(t *testing.T) {
	newRepo(t)
	for _, tc := range []struct {
		args   []string
		stderr string
	}{
		{[]string{"validate", "--format", "xml"}, ""},
		{[]string{"validate", "--path", filepath.Join(t.TempDir(), "none")}, ""},
	} {
		code, out, errOut := run(tc.args...)
		if code != 2 || out != "" {
			t.Errorf("%v: exit %d, stdout %q; want 2 and nothing on stdout", tc.args, code, out)
		}
		if tc.stderr != "" && errOut != tc.stderr {
			t.Errorf("%v: stderr = %q, want %q", tc.args, errOut, tc.stderr)
		}
	}
}

// Every format writes its report, to standard output or --out, and the
// exit code is the same whichever is chosen.
func TestValidateFormats(t *testing.T) {
	newRepo(t)
	if err := os.Mkdir("REQ", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("REQ/a.md", []byte("---\nid: REQ-0001\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for format, want := range map[string]string{
		"json":  `"rule": "content/frontmatter"`,
		"junit": `<failure type="error"`,
		"sarif": `"ruleId": "content/frontmatter"`,
	} {
		code, out, _ := run("validate", "--format", format)
		if code != 1 || !strings.Contains(out, want) {
			t.Errorf("%s: exit %d, stdout %q; want 1 and %s", format, code, out, want)
		}
	}
}
