package load

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/repo"
)

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

const partSchema = "type: Part\nid_prefix: PRT\nfields:\n  - {name: title, type: string}\n  - {name: weight, type: number}\n  - {name: total, type: calculated, formula: \"weight * 2\"}\n"

func part(id, extra string) string {
	return "---\nid: " + id + "\ntype: Part\ntitle: T\n" + extra + "---\n"
}

func loadRepo(t *testing.T, files map[string]string, skip ...string) *Result {
	t.Helper()
	root := t.TempDir()
	write(t, root, files)
	res, err := Load(&repo.Repo{Root: root}, skip...)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

type found struct {
	check, path string
	line        int
	stops       bool
}

func problems(res *Result) []found {
	var out []found
	for _, p := range res.Problems {
		out = append(out, found{p.Check, p.Path, p.Line, p.Err != nil})
	}
	return out
}

// A clean repository loads typed, with its calculated values evaluated.
func TestLoad(t *testing.T) {
	res := loadRepo(t, map[string]string{
		"schema/Part.yaml": partSchema,
		"P/b.md":           part("PRT-0002", "weight: 3\n"),
		"P/a.md":           part("PRT-0001", "weight: 1\n"),
		"notes.md":         "no frontmatter\n",
	})
	if len(res.Problems) != 0 || res.SchemaBroken || res.Err() != nil {
		t.Fatalf("problems %v", problems(res))
	}
	var ids []string
	for _, e := range res.Entities {
		ids = append(ids, e.ID)
	}
	if !slices.Equal(ids, []string{"PRT-0001", "PRT-0002"}) || len(res.Parsed) != 2 {
		t.Errorf("entities %v, parsed %d", ids, len(res.Parsed))
	}
	if v := res.Graph.Entity("PRT-0002").Field("total"); v == nil || v.Num != 6 {
		t.Errorf("total = %+v", v)
	}
	if in := res.Input(); in.Graph != res.Graph || len(in.Entities) != 2 {
		t.Errorf("input %+v", in)
	}
}

// Every problem is collected under its built-in check, in the order export
// meets them: entity files as the walk finds them (every copy of a
// duplicated ID, naming the others), the schema's key problems, then
// unknown types. Export stops at the first it can't carry on past; a file
// that is not an entity and an unknown schema key are not such problems.
func TestProblems(t *testing.T) {
	res := loadRepo(t, map[string]string{
		"schema/Part.yaml": partSchema + "colour: red\n",
		"A/half.md":        "---\nid: PRT-0009\n---\n",
		"B/bad.md":         "---\nid: [\n---\n",
		"C/one.md":         part("PRT-0001", ""),
		"D/two.md":         part("PRT-0001", ""),
		"E/w.md":           "---\nid: WID-0001\ntype: Widget\n---\n",
	})
	want := []found{
		{"content/frontmatter", "B/bad.md", 2, true},
		{"content/duplicate-id", "C/one.md", 2, true},
		{"content/duplicate-id", "D/two.md", 2, false},
		{"content/frontmatter", "A/half.md", 2, false},
		{"schema/unknown-key", "schema/Part.yaml", 7, false},
		{"schema/unknown-type", "E/w.md", 3, true},
	}
	if got := problems(res); !slices.Equal(got, want) {
		t.Errorf("problems\n got %v\nwant %v", got, want)
	}
	if err := res.Err(); err == nil || err.Error() != "B/bad.md:2: invalid frontmatter: did not find expected node content" {
		t.Errorf("Err = %v", err)
	}
	if p := res.Problems[2]; p.Entity != "PRT-0001" || p.Msg != "duplicate entity ID PRT-0001, also in C/one.md" {
		t.Errorf("duplicate %+v", p)
	}
	var ut *model.UnknownTypeError
	if p := res.Problems[5]; !errors.As(p.Err, &ut) || p.Entity != "WID-0001" ||
		ut.Error() != `E/w.md:3: type "Widget" is not declared in schema/` ||
		p.Msg != `type "Widget" is not declared in schema/ (declared: Part)` {
		t.Errorf("unknown type %+v", p)
	}
	if len(res.Entities) != 1 || res.Entities[0].ID != "PRT-0001" {
		t.Errorf("entities %v: want the declared ones", res.Entities)
	}
}

// A schema that cannot load leaves the entities untyped, and export stops
// at it; the entity files are still read and their problems reported.
func TestBrokenSchema(t *testing.T) {
	res := loadRepo(t, map[string]string{
		"schema/Part.yaml": "type: Part\nid_prefix: PRT\nfields:\n  - {name: weight, type: Weight}\n",
		"A/a.md":           part("PRT-0001", ""),
		"A/half.md":        "---\ntype: Part\n---\n",
	})
	if !res.SchemaBroken || res.Entities != nil || res.Graph != nil || len(res.Parsed) != 1 {
		t.Fatalf("broken %v, entities %v, parsed %d", res.SchemaBroken, res.Entities, len(res.Parsed))
	}
	if got := problems(res); len(got) != 2 || got[0].check != "content/frontmatter" || got[1].check != "schema/file" {
		t.Errorf("problems %v", got)
	}
	if err := res.Err(); err == nil || err.Error() != `schema/Part.yaml:4: field "weight": unknown type "Weight" (expected string, text, number, date, boolean, link, list, calculated)` {
		t.Errorf("Err = %v", err)
	}
}

// A skipped folder, such as the export's output, is not read.
func TestSkip(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"schema/Part.yaml": partSchema,
		"P/a.md":           part("PRT-0001", ""),
		"_site/copy.md":    part("PRT-0001", ""),
	})
	res, err := Load(&repo.Repo{Root: root}, filepath.Join(root, "_site"))
	if err != nil || len(res.Problems) != 0 || len(res.Entities) != 1 {
		t.Errorf("err %v, problems %v, entities %d", err, problems(res), len(res.Entities))
	}
}
