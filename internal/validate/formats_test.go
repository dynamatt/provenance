package validate

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"

	"github.com/dynamatt/provenance/internal/repo"
)

// goldenFormats are the machine-readable formats and their golden files'
// extensions in testdata/validate. SARIF names the binary's version; the
// golden files say "dev", as make golden-update writes them.
var goldenFormats = map[string]string{"json": ".json", "junit": ".junit.xml", "sarif": ".sarif"}

func report(t *testing.T, root, format string) []byte {
	t.Helper()
	r, err := Run(&repo.Repo{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := r.Write(&b, format, "dev"); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The violations repositories in every format match their golden files,
// which scripts/acceptance.sh also checks against the binary.
func TestFormatsGolden(t *testing.T) {
	for _, name := range violationRepos {
		for format, ext := range goldenFormats {
			t.Run(name+"/"+format, func(t *testing.T) {
				want, err := os.ReadFile(filepath.Join("../../testdata/validate", "violations-"+name+ext))
				if err != nil {
					t.Fatal(err)
				}
				if got := report(t, filepath.Join(violationsDir, name), format); !bytes.Equal(got, want) {
					t.Errorf("got:\n%s\nwant:\n%s", got, want)
				}
			})
		}
	}
}

// Every SARIF report conforms to the SARIF 2.1.0 schema, checked in so the
// test needs no network.
func TestSARIFSchema(t *testing.T) {
	abs, err := filepath.Abs("../../testdata/schemas/sarif-schema-2.1.0.json")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := gojsonschema.NewSchema(gojsonschema.NewReferenceLoader("file://" + filepath.ToSlash(abs)))
	if err != nil {
		t.Fatal(err)
	}
	roots := map[string]string{"empty": t.TempDir(), "rules skipped": skippedRules(t)}
	for _, name := range violationRepos {
		roots[name] = filepath.Join(violationsDir, name)
	}
	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			res, err := sch.Validate(gojsonschema.NewBytesLoader(report(t, root, "sarif")))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range res.Errors() {
				t.Error(e)
			}
		})
	}
}

// skippedRules is a repository whose rules are skipped: one by a broken
// schema, which skips them all, beside one that would be.
func skippedRules(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "schema/Requirement.yaml", "type: [\n")
	write(t, root, "rules/needs-title.yaml", "severity: warning\nmessage: needs a title\nfrom: Requirement\nwhere: {field: title, operator: missing}\n")
	write(t, root, "rules/odd.yaml", "- not a rule\n")
	return root
}

type junitCase struct {
	Name      string `xml:"name,attr"`
	Classname string `xml:"classname,attr"`
	Skipped   *struct {
		Message string `xml:"message,attr"`
	} `xml:"skipped"`
	Failures []struct {
		Message string `xml:"message,attr"`
		Text    string `xml:",chardata"`
	} `xml:"failure"`
	SystemOut string `xml:"system-out"`
}

type junitDoc struct {
	Tests    int `xml:"tests,attr"`
	Failures int `xml:"failures,attr"`
	Suites   []struct {
		Name     string      `xml:"name,attr"`
		Tests    int         `xml:"tests,attr"`
		Failures int         `xml:"failures,attr"`
		Skipped  int         `xml:"skipped,attr"`
		Cases    []junitCase `xml:"testcase"`
	} `xml:"testsuite"`
}

func junit(t *testing.T, root string) junitDoc {
	t.Helper()
	var doc junitDoc
	if err := xml.Unmarshal(report(t, root, "junit"), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// A clean run lists every check and rule as a passed test case; an error
// fails its case, one failure each, and a warning goes to its output.
func TestJUnit(t *testing.T) {
	root := ruleRepo(t, map[string]string{
		"draft.yaml":    "severity: error\nmessage: still a draft\nfrom: Part\nwhere: {field: status, operator: equals, value: draft}\n",
		"titled.yaml":   "severity: warning\nmessage: has a title\nfrom: Part\nwhere: {field: title, operator: exists}\n",
		"nothing.yaml":  "severity: error\nmessage: no owner is nobody\nfrom: Part\nwhere: {field: owner, operator: equals, value: nobody}\n",
		"Bad_Name.yaml": "severity: error\nmessage: m\nfrom: Part\n",
	})
	doc := junit(t, root)
	if len(doc.Suites) != 2 || doc.Suites[0].Name != "built-in" || doc.Suites[1].Name != "rules" {
		t.Fatalf("suites = %+v", doc.Suites)
	}
	if got := len(doc.Suites[0].Cases); got != len(Checks) {
		t.Errorf("built-in cases = %d, want %d", got, len(Checks))
	}
	cases := map[string]junitCase{}
	for _, c := range doc.Suites[1].Cases {
		cases[c.Name] = c
	}
	if c := cases["draft"]; len(c.Failures) != 1 || c.Failures[0].Message != "still a draft" || c.Failures[0].Text != "PRT/PRT-0001.md:5: error [draft] still a draft (PRT-0001)" {
		t.Errorf("draft = %+v", c)
	}
	if c := cases["titled"]; len(c.Failures) != 0 || c.SystemOut != "PRT/PRT-0001.md:4: warning [titled] has a title (PRT-0001)\nPRT/PRT-0002.md:4: warning [titled] has a title (PRT-0002)\n" {
		t.Errorf("titled = %+v", c)
	}
	if c := cases["nothing"]; len(c.Failures) != 0 || c.SystemOut != "" || c.Skipped != nil {
		t.Errorf("nothing = %+v", c)
	}
	if c := cases["Bad_Name"]; c.Skipped == nil || c.Skipped.Message != "the rule file has errors" {
		t.Errorf("Bad_Name = %+v", c)
	}
	// rules/name fails among the built-in checks, and draft among the rules.
	if doc.Tests != len(Checks)+4 || doc.Failures != 2 || doc.Suites[1].Failures != 1 || doc.Suites[1].Skipped != 1 {
		t.Errorf("tests %d failures %d; rules failures %d skipped %d", doc.Tests, doc.Failures, doc.Suites[1].Failures, doc.Suites[1].Skipped)
	}
}

func TestJUnitSkippedBySchema(t *testing.T) {
	doc := junit(t, skippedRules(t))
	var names []string
	for _, c := range doc.Suites[1].Cases {
		if c.Skipped == nil || c.Skipped.Message != "the schema has errors" {
			t.Errorf("%s: skipped = %+v", c.Name, c.Skipped)
		}
		names = append(names, c.Name)
	}
	if strings.Join(names, " ") != "needs-title odd" {
		t.Errorf("rules = %v", names)
	}
}

// The json report has every field of every violation, and an empty list
// rather than null when there are none.
func TestJSON(t *testing.T) {
	if got := string(report(t, t.TempDir(), "json")); got != "{\n  \"version\": 1,\n  \"violations\": [],\n  \"summary\": {\n    \"errors\": 0,\n    \"warnings\": 0,\n    \"rules_skipped\": 0\n  }\n}\n" {
		t.Errorf("empty: %s", got)
	}
	var out struct {
		Violations []map[string]any `json:"violations"`
		Summary    map[string]int   `json:"summary"`
	}
	if err := json.Unmarshal(report(t, skippedRules(t), "json"), &out); err != nil {
		t.Fatal(err)
	}
	v := out.Violations[0]
	if v["rule"] != "schema/file" || v["entity"] != "" || v["path"] != "schema/Requirement.yaml" || v["line"] != float64(1) || len(v) != 6 {
		t.Errorf("violation = %v", v)
	}
	if out.Summary["errors"] != 1 || out.Summary["rules_skipped"] != 2 {
		t.Errorf("summary = %v", out.Summary)
	}
}

func TestUnknownFormat(t *testing.T) {
	r := &Report{}
	if err := r.Write(&bytes.Buffer{}, "xml", "dev"); err == nil || !strings.Contains(err.Error(), `unknown report format "xml"`) {
		t.Errorf("err = %v", err)
	}
}
