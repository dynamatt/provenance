package history_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/load/loadtest"
	"github.com/dynamatt/provenance/internal/repo"
)

// A commit records the entities it changed, not just its files (G5): of
// two test-source entities sharing a file, a commit changing one moves
// only that entity's last-changed commit, and so does a working-tree edit.
func TestEntitiesSharingAFile(t *testing.T) {
	load.UseTestSource(t, loadtest.Rows{})
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	day := 0
	commit := func(files map[string]string) string {
		t.Helper()
		write(t, dir, files)
		wt, err := r.Worktree()
		if err != nil {
			t.Fatal(err)
		}
		if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
			t.Fatal(err)
		}
		day++
		sig := &object.Signature{Name: "Ann", Email: "ann@example.com", When: time.Date(2026, 9, day, 12, 0, 0, 0, time.UTC)}
		h, err := wt.Commit("change", &git.CommitOptions{Author: sig})
		if err != nil {
			t.Fatal(err)
		}
		return h.String()
	}
	first := commit(map[string]string{
		"schema/Part.yaml": "type: Part\nid_prefix: PRT\nfields:\n  - {name: title, type: string}\n",
		"data/parts.rows":  "id | type | title\nPRT-0001 | Part | Bolt\nPRT-0002 | Part | Nut\n",
	})
	second := commit(map[string]string{"data/parts.rows": "id | type | title\nPRT-0001 | Part | Bolt\nPRT-0002 | Part | Wing nut\n"})

	res, err := load.Load(&repo.Repo{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	h, err := res.History()
	if err != nil {
		t.Fatal(err)
	}
	log, err := h.Log()
	if err != nil {
		t.Fatal(err)
	}
	if got := log.Commits[0].Entities; !slices.Equal(got, []string{"PRT-0002"}) {
		t.Errorf("the second commit changed %v, want PRT-0002", got)
	}
	for id, want := range map[string]string{"PRT-0001": first, "PRT-0002": second} {
		if got := log.LastChanged(history.Inputs{Entities: []string{id}}); got != want {
			t.Errorf("%s last changed in %s, want %s", id, history.Short(got), history.Short(want))
		}
	}

	write(t, dir, map[string]string{"data/parts.rows": "id | type | title\nPRT-0001 | Part | Hex bolt\nPRT-0002 | Part | Wing nut\n"})
	res, err = load.Load(&repo.Repo{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if h, err = res.History(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.Dirty.Entities, []string{"PRT-0001"}) || !h.Dirty.Touches(history.Inputs{Entities: []string{"PRT-0001"}}) || h.Dirty.Touches(history.Inputs{Entities: []string{"PRT-0002"}}) {
		t.Errorf("dirty entities %v", h.Dirty.Entities)
	}
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
