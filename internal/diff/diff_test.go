package diff

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/repo"
)

var schemaFiles = map[string]string{
	".component": "code: T\nname: Test\n",
	"schema/Part.yaml": `type: Part
id_prefix: PRT
fields:
  - name: title
    type: string
    title: true
  - name: notes
    type: text
  - name: status
    type: string
  - name: weight
    type: number
  - name: uses
    type: link
    target: [Part]
    cardinality: many
    reverse_name: used_by
  - name: steps
    type: list
    fields:
      - name: action
        type: string
      - name: check
        type: string
  - name: use_count
    type: calculated
    formula: COUNT(uses)
`,
	"schema/Report.yaml": `type: Report
id_prefix: REP
fields:
  - name: title
    type: string
    title: true
  - name: scope
    type: query
    of: [Part]
`,
	"schema/Other.yaml": "type: Other\nid_prefix: OTH\nfields:\n  - name: title\n    type: string\n    title: true\n",
}

// testRepo is a git repository whose commits are the given files.
type testRepo struct {
	t   *testing.T
	dir string
	r   *git.Repository
	n   int
}

func newRepo(t *testing.T) *testRepo {
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return &testRepo{t: t, dir: dir, r: r}
}

func (tr *testRepo) commit(files map[string]string) string {
	tr.t.Helper()
	for rel, content := range files {
		p := filepath.Join(tr.dir, filepath.FromSlash(rel))
		if content == "" {
			os.Remove(p)
			continue
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			tr.t.Fatal(err)
		}
	}
	wt, _ := tr.r.Worktree()
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		tr.t.Fatal(err)
	}
	tr.n++
	h, err := wt.Commit("c", &git.CommitOptions{Author: &object.Signature{Name: "A", Email: "a@example.com", When: time.Date(2026, 11, tr.n, 0, 0, 0, 0, time.UTC)}})
	if err != nil {
		tr.t.Fatal(err)
	}
	return h.String()
}

func (tr *testRepo) diff(old, new string) *Diff {
	tr.t.Helper()
	rp, err := repo.Open(tr.dir)
	if err != nil {
		tr.t.Fatal(err)
	}
	h, err := history.Open(rp.Root)
	if err != nil {
		tr.t.Fatal(err)
	}
	defer h.Close()
	side := func(commit string) *Side {
		res, err := load.At(rp, h, commit)
		if err != nil {
			tr.t.Fatal(err)
		}
		return NewSide(commit, commit, "", res)
	}
	d, err := Compare(side(old), side(new), nil)
	if err != nil {
		tr.t.Fatal(err)
	}
	return d
}

func with(base map[string]string, more map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}

func TestCompare(t *testing.T) {
	tr := newRepo(t)
	old := tr.commit(with(schemaFiles, map[string]string{
		"P/PRT-0001.md":       "---\nid: PRT-0001\ntype: Part\ntitle: Housing\nstatus: draft\nweight: 2.50\nuses: [PRT-0002]\nsteps:\n  - action: Weigh\n    check: 2.5 kg\n  - action: Drop\n    check: no crack\n---\nThe housing holds\nthe board.\n\nIt is 8 mm thick.\n",
		"P/PRT-0002.md":       "---\nid: PRT-0002\ntype: Part\ntitle: Board\n---\n",
		"P/PRT-0003.md":       "---\nid: PRT-0003\ntype: Part\ntitle: Screw\n---\n",
		"P/PRT-0004.md":       "---\nid: PRT-0004\ntype: Part\ntitle: Washer\n---\n",
		"O/OTH-0001.md":       "---\nid: OTH-0001\ntype: Other\ntitle: Gone\n---\n",
		"R/REP-0001.md":       "---\nid: REP-0001\ntype: Report\ntitle: Parts used\nscope:\n  from: Part\n  where:\n    field: used_by\n    operator: exists\n---\n",
		"assets/a.png":        "png1",
		"templates/Part.tmpl": "x",
	}))
	new := tr.commit(map[string]string{
		// Reformatted and moved, with three values changed: no change
		// but those.
		"P/PRT-0001.md":       "",
		"Parts/housing.md":    "---\nid: PRT-0001\ntype: Part\ntitle: \"Housing\"\nweight: 2.5\nstatus: approved\nuses:\n  - PRT-0002\n  - PRT-0003\nsteps:\n  - action: Weigh\n    check: 2.5 kg\n  - action: Drop\n    check: no cracks\n---\nThe housing holds the board.\n\nIt is 6 mm thick.\n",
		"O/OTH-0001.md":       "",
		"O/OTH-0002.md":       "---\nid: OTH-0002\ntype: Other\ntitle: New\n---\n",
		"P/PRT-0004.md":       "---\nid: PRT-0004\ntype: Other\ntitle: Washer\n---\n",
		"assets/a.png":        "png2",
		"templates/Part.tmpl": "x",
	})
	d := tr.diff(old, new)
	if len(d.Old.Problems)+len(d.New.Problems) > 0 {
		t.Fatalf("problems loading: %v %v", d.Old.Problems, d.New.Problems)
	}
	got, err := d.Text()
	if err != nil {
		t.Fatal(err)
	}
	want := `diff ` + history.Short(old) + ` → ` + history.Short(new) + `
assets/a.png  changed
OTH-0001  Other  removed  Gone
OTH-0002  Other  added  New
PRT-0004  Other  retyped from Part  Washer
  use_count (derived): 0 → (none)
PRT-0001  Part  changed
  status: draft → approved
  uses: + PRT-0003
  steps:
    row 2 changed
      check: no crack → no cracks
  use_count (derived): 1 → 2
  body:
    ~ It is [-8-] {+6+} mm thick.
REP-0001  Report  changed (derived)
  scope (derived): + PRT-0003
5 entities: 1 changed, 1 changed by derivation, 1 added, 1 removed, 1 retyped; 1 other file changed
`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if d2 := tr.diff(new, new); d2.Changed() {
		t.Errorf("a commit differs from itself: %+v", d2)
	}
}

// A side whose schema doesn't load is compared as far as it loads: its
// problems first, and its entities by the text their files write.
func TestCompareBrokenSide(t *testing.T) {
	tr := newRepo(t)
	old := tr.commit(with(schemaFiles, map[string]string{
		"P/PRT-0001.md": "---\nid: PRT-0001\ntype: Part\ntitle: Housing\n---\n",
		"P/PRT-0002.md": "---\nid: PRT-0002\ntype: Part\ntitle: Board\n---\n",
	}))
	new := tr.commit(map[string]string{
		"schema/Part.yaml": "type: Part\nid_prefix: PRT\nfields: [\n",
		"P/PRT-0002.md":    "---\nid: PRT-0002\ntype: Part\ntitle: Board v2\n---\n",
	})
	got, err := tr.diff(old, new).Text()
	if err != nil {
		t.Fatal(err)
	}
	want := `diff ` + history.Short(old) + ` → ` + history.Short(new) + `
problems loading ` + history.Short(new) + `, compared as far as it loaded:
  schema/Part.yaml:3: [schema/file] did not find expected node content
schema/Part.yaml  changed
PRT-0002  Part  changed
1 entity: 1 changed; 1 other file changed
`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
