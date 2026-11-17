package load_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dynamatt/provenance/internal/export/website"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/starters"
)

// A commit loaded from git's objects is the repository its clean checkout
// is: the same entities, problems and exported site (DES-0055). Checked on
// the violation cases, which hold every kind of problem, on each starter,
// and on the example when make example has checked it out.
func TestAtIsTheCheckout(t *testing.T) {
	cases := map[string]string{}
	for _, name := range []string{"core", "broken-schema"} {
		cases["violations/"+name] = copyTree(t, filepath.Join("..", "..", "testdata", "violations", name))
	}
	for _, name := range starters.Names() {
		files, err := starters.Files(name, starters.Values{Code: "THERM", Name: "Thermometer", Version: "v1.2.3"})
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		for _, f := range files {
			writeFile(t, filepath.Join(dir, filepath.FromSlash(f.Path)), f.Data)
		}
		cases["starter/"+name] = dir
	}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) { compareAt(t, dir, commitAll(t, dir)) })
	}
	example, err := filepath.Abs(filepath.Join("..", "..", ".cache", "example"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(example, ".git")); err == nil {
		t.Run("example", func(t *testing.T) {
			h, err := history.Open(example)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			compareAt(t, example, h.HEAD())
		})
	}
}

// compareAt loads dir's working tree and commit, which it must equal.
func compareAt(t *testing.T, dir, commit string) {
	t.Helper()
	rp, err := repo.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := history.Open(rp.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	wt, err := load.Load(rp)
	if err != nil {
		t.Fatal(err)
	}
	if d, err := h.Status(wt.Content()); err != nil || d.Any() {
		t.Skipf("the working tree is not clean: %v %v", d, err)
	}
	at, err := load.At(rp, h, commit)
	if err != nil {
		t.Fatal(err)
	}
	if at.Commit != commit || at.Repo.Component != wt.Repo.Component {
		t.Errorf("commit %q, component %+v; want %q, %+v", at.Commit, at.Repo.Component, commit, wt.Repo.Component)
	}
	equal(t, "entities", summary(wt), summary(at))
	equal(t, "problems", problems(wt), problems(at))
	if wt.SchemaBroken != at.SchemaBroken {
		t.Errorf("schema broken %v, at the commit %v", wt.SchemaBroken, at.SchemaBroken)
	}
	if wt.SchemaBroken {
		return
	}
	got, gotErr := website.New().Export(at.Input())
	want, wantErr := website.New().Export(wt.Input())
	if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
		t.Fatalf("export error %v, at the commit %v", wantErr, gotErr)
	}
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range want {
			if !yield(k) {
				return
			}
		}
		for k := range got {
			if _, ok := want[k]; !ok && !yield(k) {
				return
			}
		}
	}) {
		if string(got[name]) != string(want[name]) {
			t.Errorf("exported %s differs at the commit", name)
		}
	}
}

func summary(r *load.Result) []string {
	var out []string
	for _, e := range r.Parsed {
		out = append(out, e.ID+" "+e.Type+" "+e.Path)
	}
	for _, e := range r.Entities {
		var vals []string
		for _, v := range e.Fields {
			vals = append(vals, fmt.Sprintf("%s=%v/%q/%v/%v/%q/%d/%d/%v/%q", v.Field.Name, v.Present, v.Str, v.Num, v.Bool, v.IDs, len(v.Rows), len(v.Items), v.Invalid, v.Raw))
		}
		vals = append(vals, "body="+e.Body)
		out = append(out, e.ID+": "+strings.Join(vals, " "))
	}
	for _, tb := range r.Tables {
		out = append(out, "table "+tb.Type+" "+tb.Path)
	}
	return out
}

func problems(r *load.Result) []string {
	var out []string
	for _, p := range r.Problems {
		out = append(out, fmt.Sprintf("%s %s:%d %s %s", p.Check, p.Path, p.Line, p.Entity, p.Msg))
	}
	return out
}

func equal(t *testing.T, what string, want, got []string) {
	t.Helper()
	if !slices.Equal(want, got) {
		t.Errorf("%s differ at the commit:\n got %q\nwant %q", what, got, want)
	}
}

func copyTree(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		writeFile(t, filepath.Join(dir, rel), b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAll makes dir a git repository with everything in one commit.
func commitAll(t *testing.T, dir string) string {
	t.Helper()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	h, err := wt.Commit("all", &git.CommitOptions{Author: &object.Signature{Name: "Ann", Email: "ann@example.com", When: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	return h.String()
}
