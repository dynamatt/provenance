package validate

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/repo"
)

// testdata/violations is the catalogue of what validate catches: an
// ErrorClass entity for every check, each naming files that break it and
// files that don't (testdata/violations/README.md). These tests hold the
// catalogue to the report and to the checks in the code.

const violationsDir = "../../testdata/violations"

// violationRepos are the catalogue's repositories. broken-schema is apart
// because schema/file stops every check made against the schema.
var violationRepos = []string{"core", "broken-schema"}

// The reports are also compared by scripts/acceptance.sh; make golden-update
// rewrites them.
func TestViolationsReports(t *testing.T) {
	for _, name := range violationRepos {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("../../testdata/validate", "violations-"+name+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			if got := text(t, filepath.Join(violationsDir, name)); got != string(want) {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

type errorClass struct {
	ID       string `yaml:"id"`
	Rule     string `yaml:"rule"`
	Severity string `yaml:"severity"`
	Examples []struct {
		File   string `yaml:"file"`
		Expect string `yaml:"expect"`
	} `yaml:"examples"`
}

func errorClasses(t *testing.T) []errorClass {
	t.Helper()
	sc, err := entity.Scan(filepath.Join(violationsDir, "core"))
	if err != nil {
		t.Fatal(err)
	}
	var out []errorClass
	for _, e := range sc.Entities {
		if e.Type != "ErrorClass" {
			continue
		}
		var c errorClass
		if err := e.Front.Decode(&c); err != nil {
			t.Fatalf("%s: %v", e.Path, err)
		}
		out = append(out, c)
	}
	return out
}

// Every violation example is reported for its class's rule, at the class's
// severity; every clean example is not reported at all.
func TestViolationsExamples(t *testing.T) {
	reported := map[string][]Violation{} // by file, relative to violationsDir
	for _, name := range violationRepos {
		r, err := Run(&repo.Repo{Root: filepath.Join(violationsDir, name)})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range r.Violations {
			f := name + "/" + v.Path
			reported[f] = append(reported[f], v)
		}
	}
	for _, c := range errorClasses(t) {
		var violation, clean int
		for _, ex := range c.Examples {
			if _, err := os.Stat(filepath.Join(violationsDir, ex.File)); err != nil {
				t.Errorf("%s: example %s: %v", c.ID, ex.File, err)
				continue
			}
			got := reported[ex.File]
			switch ex.Expect {
			case "violation":
				violation++
				i := slices.IndexFunc(got, func(v Violation) bool { return v.Rule == c.Rule })
				switch {
				case i < 0:
					t.Errorf("%s: %s is not reported for %s (reported: %v)", c.ID, ex.File, c.Rule, got)
				case string(got[i].Severity) != c.Severity:
					t.Errorf("%s: %s is reported for %s as %s, want %s", c.ID, ex.File, c.Rule, got[i].Severity, c.Severity)
				}
			case "clean":
				clean++
				if len(got) > 0 {
					t.Errorf("%s: clean example %s is reported: %v", c.ID, ex.File, got)
				}
			}
		}
		if violation == 0 || clean == 0 {
			t.Errorf("%s (%s): has %d violation and %d clean examples, want at least one of each", c.ID, c.Rule, violation, clean)
		}
	}
}

// ruleID matches a built-in check's ID written in the code.
var ruleID = regexp.MustCompile(`"((?:content|links|schema|rules)/[a-z][a-z-]*)"`)

// Every check in the code has an ErrorClass, and every ErrorClass is a
// check in the code: a new check is not done until the catalogue shows it.
func TestViolationsCoverEveryRule(t *testing.T) {
	inCode := map[string]string{}
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		// checks.go lists every check by ID, so it would show them all
		// as in the code; TestChecksMatchTheCatalogue holds it to the rest.
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || p == "../validate/checks.go" {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range ruleID.FindAllSubmatch(src, -1) {
			inCode[string(m[1])] = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	classes := map[string]string{}
	for _, c := range errorClasses(t) {
		if prev, dup := classes[c.Rule]; dup {
			t.Errorf("%s and %s are both %s", prev, c.ID, c.Rule)
		}
		classes[c.Rule] = c.ID
		if inCode[c.Rule] == "" {
			t.Errorf("%s: no check in internal/ reports %s", c.ID, c.Rule)
		}
	}
	for rule, p := range inCode {
		if classes[rule] == "" {
			t.Errorf("%s (%s) has no ErrorClass in testdata/violations/core/cases", rule, p)
		}
	}
}

// Checks, which the junit and sarif reports list, has every class of the
// catalogue, with its severity and its title as the description.
func TestChecksMatchTheCatalogue(t *testing.T) {
	classes := map[string]errorClass{}
	titles := map[string]string{}
	sc, err := entity.Scan(filepath.Join(violationsDir, "core"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range sc.Entities {
		if e.Type != "ErrorClass" {
			continue
		}
		var c errorClass
		var title struct {
			Title string `yaml:"title"`
		}
		if err := e.Front.Decode(&c); err != nil {
			t.Fatal(err)
		}
		if err := e.Front.Decode(&title); err != nil {
			t.Fatal(err)
		}
		classes[c.Rule], titles[c.Rule] = c, title.Title
	}
	seen := map[string]bool{}
	for i, ch := range Checks {
		if i > 0 && Checks[i-1].ID >= ch.ID {
			t.Errorf("Checks: %s is not after %s", ch.ID, Checks[i-1].ID)
		}
		seen[ch.ID] = true
		c, ok := classes[ch.ID]
		switch {
		case !ok:
			t.Errorf("Checks: %s has no ErrorClass", ch.ID)
		case string(ch.Severity) != c.Severity:
			t.Errorf("Checks: %s is %s, its ErrorClass %s", ch.ID, ch.Severity, c.Severity)
		case ch.Description != titles[ch.ID]:
			t.Errorf("Checks: %s is described %q, its ErrorClass %q", ch.ID, ch.Description, titles[ch.ID])
		}
	}
	for rule, c := range classes {
		if !seen[rule] {
			t.Errorf("%s (%s) is not in Checks", rule, c.ID)
		}
	}
}
