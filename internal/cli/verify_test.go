package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// gitRepo makes a committed component repository and changes into it.
func gitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	r, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		".component":   "code: T\nname: Test\n",
		"REQ/REQ-1.md": "---\nid: REQ-1\ntype: Requirement\n---\nShall.\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "Ann", Email: "ann@example.com", When: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	if _, err := wt.Commit("first", &git.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

var contentHash = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestVerifyContent(t *testing.T) {
	root := gitRepo(t)
	code, out, errOut := run("verify", "content")
	hash := strings.TrimSpace(out)
	if code != 0 || !contentHash.MatchString(hash) || errOut != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, _, _ := run("verify", "content", "--expected", hash); code != 0 {
		t.Errorf("--expected the same hash: exit %d", code)
	}
	if code, _, errOut := run("verify", "content", "--expected", "sha256:00"); code != 1 || !strings.Contains(errOut, "does not match the expected sha256:00") {
		t.Errorf("--expected another hash: exit %d, stderr %q", code, errOut)
	}
	if code, out, _ := run("verify", "content", "--commit", "HEAD"); code != 0 || strings.TrimSpace(out) != hash {
		t.Errorf("--commit HEAD: exit %d, %q", code, out)
	}
	if code, _, errOut := run("verify", "content", "--commit", "nope"); code != 2 || !strings.Contains(errOut, `unknown commit "nope"`) {
		t.Errorf("--commit nope: exit %d, stderr %q", code, errOut)
	}

	// An uncommitted edit marks the hash, but not a historical commit's.
	if err := os.WriteFile(filepath.Join(root, "REQ", "REQ-1.md"), []byte("---\nid: REQ-1\ntype: Requirement\n---\nEdited.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := run("verify", "content"); strings.TrimSpace(out) != hash+"-dirty" {
		t.Errorf("after an edit: %q, want %s-dirty", out, hash)
	}
	if _, out, _ := run("verify", "content", "--commit", "HEAD"); strings.TrimSpace(out) != hash {
		t.Errorf("--commit HEAD after an edit: %q, want %s", out, hash)
	}
}

func TestVerifyContentOutsideGit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".component"), []byte("code: T\nname: Test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if code, _, errOut := run("verify", "content"); code != 2 || !strings.Contains(errOut, "not in a git repository") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}
