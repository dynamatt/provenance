package cli

import (
	"os"
	"path/filepath"
	"regexp"
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
	code, out, _ := run("validate", "--path", root, "--output", "text=reports/v.txt")
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
		{[]string{"validate", "--output", "xml"}, ""},
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

// Every format writes its report, to standard output or a file, and the
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
		code, out, _ := run("validate", "--output", format)
		if code != 1 || !strings.Contains(out, want) {
			t.Errorf("%s: exit %d, stdout %q; want 1 and %s", format, code, out, want)
		}
	}
}

// One run writes every --output: formats and report templates, each to
// its file or one to standard output.
func TestValidateOutputs(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "templates/reports/summary.md.tmpl", "{{.Summary.Errors}} errors\n")
	writeFile(t, root, "REQ/a.md", "---\nid: REQ-0001\n---\n")
	code, out, errOut := run("validate", "--output", "summary.md", "--output", "json=r/v.json", "--output", "junit=r/v.xml", "--output", "text=r/v.txt")
	if code != 1 || out != "1 errors\n" || errOut != "validate: 1 error, 0 warnings\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	for file, want := range map[string]string{"v.json": `"errors": 1,`, "v.xml": `<failure type="error"`, "v.txt": "1 error, 0 warnings\n"} {
		if got, _ := os.ReadFile(filepath.Join(root, "r", file)); !strings.Contains(string(got), want) {
			t.Errorf("%s = %q, want %q in it", file, got, want)
		}
	}
	if code, out, _ := run("validate", "--output", "json=r/only.json"); code != 1 || out != "" {
		t.Errorf("file only: exit %d, stdout %q; want nothing on standard output", code, out)
	}
}

// A report template renders with the project's data; its exit code is
// the one the violations give.
func TestValidateReport(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "templates/reports/summary.md.tmpl", "{{.Component.Name}}: {{.Summary.Errors}} errors{{range .Violations}}\n- {{.Path}}: {{.Rule}}{{end}}\n")
	if code, out, errOut := run("validate", "--output", "summary.md"); code != 0 || out != "Test System: 0 errors\n" || errOut != "" {
		t.Errorf("clean: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	writeFile(t, root, "REQ/a.md", "---\nid: REQ-0001\n---\n")
	code, out, _ := run("validate", "--output", "summary.md=_site/summary.md")
	if code != 1 || out != "" {
		t.Errorf("violations: exit %d, stdout %q", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "_site", "summary.md")); string(got) != "Test System: 1 errors\n- REQ/a.md: content/frontmatter\n" {
		t.Errorf("report = %q", got)
	}
}

// A report template that fails is a violation: exit 1, and no output is
// written, not even the ones that rendered. An unknown name, a second
// output to standard output or two to one file is a usage error.
func TestValidateReportErrors(t *testing.T) {
	root := newRepo(t)
	writeFile(t, root, "templates/reports/broken.tmpl", "{{.Nope}}\n")
	writeFile(t, root, "templates/reports/sarif.tmpl", "shadowed\n")
	code, out, errOut := run("validate", "--output", "json=v.json", "--output", "broken=r.txt", "--output", "text")
	if code != 1 || out != "" || errOut != `validate: report broken: templates/reports/broken.tmpl:1: executing "templates/reports/broken.tmpl" at <.Nope>: map has no entry for key "Nope" (2 errors, 0 warnings)`+"\n" {
		t.Errorf("broken: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	for _, f := range []string{"r.txt", "v.json"} {
		if _, err := os.Stat(filepath.Join(root, f)); !os.IsNotExist(err) {
			t.Errorf("%s written: %v", f, err)
		}
	}
	if _, out, _ := run("validate", "--output", "sarif"); !strings.Contains(out, `"version": "2.1.0"`) {
		t.Errorf("--output sarif rendered the template: %q", out)
	}
	usage := "\nRun 'provenance validate --help' for usage.\n"
	for _, tc := range []struct {
		args   []string
		stderr string
	}{
		{[]string{"validate", "--output", "nope"}, `validate: unknown report "nope": not a format (text, json, junit, sarif) and there is no templates/reports/nope.tmpl (reports: broken)` + usage},
		{[]string{"validate", "--output", "text", "--output", "json"}, "validate: --output text and --output json both write to standard output; give one a file with json=<file>" + usage},
		{[]string{"validate", "--output", "text=r.txt", "--output", "json=./r.txt"}, "validate: --output text and --output json both write ./r.txt" + usage},
		{[]string{"validate", "--output", "=r.txt"}, `validate: --output "=r.txt": missing report name` + usage},
		{[]string{"validate", "--output", "json="}, `validate: --output "json=": missing file after =` + usage},
	} {
		if code, out, errOut := run(tc.args...); code != 2 || out != "" || errOut != tc.stderr {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", tc.args, code, out, errOut)
		}
	}
}

// In a git repository a report has the stamps: HEAD, the content hash and
// whether the working tree differs, as it does here with the template
// not yet committed.
func TestValidateReportStamps(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, root, "templates/reports/stamps.tmpl", "{{.GitSHA}} {{.ContentHash}} {{.Dirty}}\n")
	_, out, _ := run("validate", "--output", "stamps")
	if !regexp.MustCompile(`^[0-9a-f]{40} sha256:[0-9a-f]{64}-dirty true\n$`).MatchString(out) {
		t.Errorf("stdout = %q", out)
	}
}
