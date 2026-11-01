package starters

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/dynamatt/provenance/internal/canon"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/export/website"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/validate"
)

// write writes starter name to a new folder and opens it as a repository.
func write(t *testing.T, name string, v Values) *repo.Repo {
	t.Helper()
	root := t.TempDir()
	files, err := Files(name, v)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rp, err := repo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return rp
}

var values = Values{Code: "THERM", Name: "Thermometer", Version: "v1.2.3"}

// Every starter validates clean, exports, and is in canonical form.
func TestStarterIsClean(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			rp := write(t, name, values)
			reg := export.NewRegistry()
			reg.Register("website", website.New())
			report, err := validate.Run(rp, reg)
			if err != nil {
				t.Fatal(err)
			}
			if got := report.Summary(); got != "0 errors, 0 warnings" {
				t.Errorf("validate: %s: %+v", got, report.Violations)
			}
			res, err := load.Load(rp)
			if err != nil || res.Err() != nil {
				t.Fatalf("load: %v, %v", err, res.Err())
			}
			if _, err := website.New().Export(res.Input()); err != nil {
				t.Errorf("export: %v", err)
			}
			for _, e := range res.Parsed {
				if _, line, _, err := canon.Format(e, res.Schema); line != 0 || err != nil {
					t.Errorf("%s: not in canonical form at line %d (%v)", e.Path, line, err)
				}
			}
		})
	}
}

// clause is how a field's help ends: the clause it cites (REQ-0005).
var clause = regexp.MustCompile(`\((ISO \d+:\d{4} §\d+(\.\d+)*|21 CFR \d+(\.\d+)*)(; [^)]+)?\)$`)

// Every field of every starter type cites its clause in its help.
func TestEveryFieldCitesItsClause(t *testing.T) {
	for _, name := range Names() {
		s, err := schema.Load(write(t, name, values).Root)
		if err != nil {
			t.Fatal(err)
		}
		var check func(where string, fields []*schema.Field)
		check = func(where string, fields []*schema.Field) {
			for _, f := range fields {
				if !clause.MatchString(f.Help) {
					t.Errorf("%s: %s.%s's help cites no clause: %q", name, where, f.Name, f.Help)
				}
				check(where+"."+f.Name, f.Fields)
			}
		}
		for _, n := range s.TypeNames() {
			check(n, s.Types[n].Fields)
		}
	}
}

// The medical-device starter declares REQ-0004's types.
func TestMedicalDeviceTypes(t *testing.T) {
	s, err := schema.Load(write(t, "medical-device", values).Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"UserNeed", "Requirement", "Design", "Risk", "VerificationProtocol", "VerificationEvidence", "ValidationEvidence", "ChangeRequest"} {
		if s.Types[n] == nil {
			t.Errorf("no type %s", n)
		}
	}
}

// Values fill only the files with blanks; a build that isn't a release
// writes no CI workflow.
func TestFiles(t *testing.T) {
	files, err := Files(Default, values)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	var paths []string
	for _, f := range files {
		byPath[f.Path] = string(f.Data)
		paths = append(paths, f.Path)
	}
	if !slices.IsSorted(paths) {
		t.Error("files not sorted by path")
	}
	if !regexp.MustCompile(`(?m)^code: THERM\nname: Thermometer$`).MatchString(byPath[".component"]) {
		t.Errorf(".component:\n%s", byPath[".component"])
	}
	if !regexp.MustCompile(`PROVENANCE_VERSION: v1\.2\.3\n`).MatchString(byPath[workflow]) {
		t.Errorf("workflow:\n%s", byPath[workflow])
	}
	if !regexp.MustCompile(`\{\{\.Summary\.Errors\}\}`).MatchString(byPath["templates/reports/pr-summary.md.tmpl"]) {
		t.Error("the report template's own {{ }} were changed")
	}
	for _, v := range []string{"dev", "v1.2.3-4-gabcdef0", "v1.2.3-dirty", "abc1234"} {
		files, _ := Files(Default, Values{Code: "X", Name: "X", Version: v})
		for _, f := range files {
			if f.Path == workflow {
				t.Errorf("version %s wrote a workflow", v)
			}
		}
	}
	if _, err := Files("nope", values); err == nil {
		t.Error("an unknown starter is not an error")
	}
}
