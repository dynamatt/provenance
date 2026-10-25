package load_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/load/loadtest"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
)

const rows = `id       | type | title | weight
PRT-0002 | Part | Bolt  | 3

PRT-0003 | Part | Nut   | 4
`

func rowsRepo(t *testing.T, extra map[string]string) *load.Result {
	t.Helper()
	load.UseTestSource(t, loadtest.Rows{})
	root := t.TempDir()
	files := map[string]string{
		"schema/Part.yaml": "type: Part\nid_prefix: PRT\nfields:\n  - {name: title, type: string}\n  - {name: weight, type: number}\n  - {name: total, type: calculated, formula: \"weight * 2\"}\n",
		"P/a.md":           "---\nid: PRT-0001\ntype: Part\ntitle: Frame\nweight: 1\n---\n",
		"data/parts.rows":  rows,
	}
	for k, v := range extra {
		files[k] = v
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := load.Load(&repo.Repo{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// A second source's entities load beside Markdown ones: typed, with their
// calculated values, at their rows, selected by queries, and their file
// is content whose entity types history can read.
func TestSourceEntities(t *testing.T) {
	res := rowsRepo(t, nil)
	if len(res.Problems) != 0 {
		t.Fatalf("problems %+v", res.Problems)
	}
	var ids []string
	for _, e := range res.Entities {
		ids = append(ids, e.ID+"@"+e.Path)
	}
	if want := "PRT-0001@P/a.md PRT-0002@data/parts.rows PRT-0003@data/parts.rows"; strings.Join(ids, " ") != want {
		t.Errorf("entities %v", ids)
	}
	nut := res.Graph.Entity("PRT-0003")
	if v := nut.Field("total"); v.Num != 8 || nut.IDLine() != 4 {
		t.Errorf("PRT-0003: total %v, id line %d", v.Num, nut.IDLine())
	}
	b, err := query.ParseBlock("from: Part\nwhere: {field: weight, operator: greater_than, value: 2}\n", res.Schema)
	if err != nil {
		t.Fatal(err)
	}
	found, err := res.Graph.Run(b)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range found {
		got = append(got, e.ID)
	}
	if !slices.Equal(got, []string{"PRT-0002", "PRT-0003"}) {
		t.Errorf("query found %v", got)
	}
	c := res.Content()
	if files, _ := c.Files(); !slices.Equal(files, []string{"P/a.md", "data/parts.rows"}) {
		t.Errorf("content files %v", files)
	}
	types, err := c.Types("data/parts.rows", func() ([]byte, error) { return []byte(rows), nil })
	if err != nil || !slices.Equal(types, []string{"Part", "Part"}) {
		t.Errorf("types %v, %v", types, err)
	}
}

// An ID two sources both hold is a duplicate, as within one source; the
// first source's entity is kept.
func TestSourceDuplicates(t *testing.T) {
	res := rowsRepo(t, map[string]string{"data/more.rows": "id | type\nPRT-0001 | Part\n"})
	var got []string
	for _, p := range res.Problems {
		got = append(got, p.Check+" "+p.Path+":"+strconv.Itoa(p.Line)+" "+p.Msg)
	}
	want := []string{
		"content/duplicate-id P/a.md:2 duplicate entity ID PRT-0001, also in data/more.rows",
		"content/duplicate-id data/more.rows:2 duplicate entity ID PRT-0001, also in P/a.md",
	}
	if !slices.Equal(got, want) {
		t.Errorf("problems\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if res.Err() == nil || res.Graph.Entity("PRT-0001").Path != "P/a.md" {
		t.Errorf("err %v, kept %s", res.Err(), res.Graph.Entity("PRT-0001").Path)
	}
}
