// Package historytest builds git repositories commit by commit for tests:
// each commit's whole tree, its parents and its author and committer
// dates, so a test can lay out merges, squashes and rebases as git and a
// host write them (DES-0058).
package historytest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Repo is a git repository in a temporary folder.
type Repo struct {
	Dir string
	t   testing.TB
	r   *git.Repository
}

// New makes an empty repository.
func New(t testing.TB) *Repo {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return &Repo{Dir: dir, t: t, r: r}
}

// Commit is one commit to make.
type Commit struct {
	Message string
	// Author is who wrote it and when, Committer who committed it and
	// when; Committer defaults to Author.
	Author, Committer Signature
	// Parents are the commits it follows, first parent first; none for a
	// root commit.
	Parents []string
	// Files are its whole tree, by slash-separated path.
	Files map[string]string
}

// Signature is a name and a time, with its time zone.
type Signature struct {
	Name string
	When time.Time
}

// Commit writes c and returns its hash. The working tree is left holding
// c's files, and HEAD at c.
func (r *Repo) Commit(c Commit) string {
	r.t.Helper()
	r.write(c.Files)
	wt, err := r.r.Worktree()
	if err != nil {
		r.t.Fatal(err)
	}
	st, err := wt.Status()
	if err != nil {
		r.t.Fatal(err)
	}
	for p, s := range st {
		if s.Worktree == git.Deleted {
			if _, err := wt.Remove(p); err != nil {
				r.t.Fatal(err)
			}
		} else if _, err := wt.Add(p); err != nil {
			r.t.Fatal(err)
		}
	}
	committer := c.Committer
	if committer.Name == "" {
		committer = c.Author
	}
	parents := make([]plumbing.Hash, len(c.Parents))
	for i, p := range c.Parents {
		parents[i] = plumbing.NewHash(p)
	}
	opts := &git.CommitOptions{
		Author:            &object.Signature{Name: c.Author.Name, Email: "test@example.com", When: c.Author.When},
		Committer:         &object.Signature{Name: committer.Name, Email: "test@example.com", When: committer.When},
		Parents:           parents,
		AllowEmptyCommits: true,
	}
	if len(parents) == 0 {
		// go-git takes HEAD as the parent when given none: a root commit
		// after the first starts from no branch.
		if _, err := r.r.Head(); err == nil {
			if err := r.r.Storer.RemoveReference(plumbing.Master); err != nil {
				r.t.Fatal(err)
			}
		}
	}
	h, err := wt.Commit(c.Message, opts)
	if err != nil {
		r.t.Fatal(err)
	}
	return h.String()
}

// Head points HEAD at commit and checks its files out.
func (r *Repo) Head(commit string) {
	r.t.Helper()
	ref := plumbing.NewHashReference(plumbing.Master, plumbing.NewHash(commit))
	if err := r.r.Storer.SetReference(ref); err != nil {
		r.t.Fatal(err)
	}
	wt, err := r.r.Worktree()
	if err != nil {
		r.t.Fatal(err)
	}
	if err := wt.Reset(&git.ResetOptions{Commit: plumbing.NewHash(commit), Mode: git.HardReset}); err != nil {
		r.t.Fatal(err)
	}
}

// Write replaces the working tree's files with files, leaving .git, and
// commits nothing: an uncommitted change.
func (r *Repo) Write(files map[string]string) {
	r.t.Helper()
	r.write(files)
}

func (r *Repo) write(files map[string]string) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(r.Dir, e.Name())); err != nil {
			r.t.Fatal(err)
		}
	}
	for rel, content := range files {
		p := filepath.Join(r.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

// Day is noon UTC on a day of November 2026, for author dates.
func Day(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.UTC) }
