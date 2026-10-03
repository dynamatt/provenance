package history

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// testRepo is a git repository in a temporary folder, committed to with
// go-git so the tests need no git executable.
type testRepo struct {
	t    *testing.T
	dir  string
	r    *git.Repository
	tick int
}

func newRepo(t *testing.T) *testRepo {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return &testRepo{t: t, dir: dir, r: r}
}

func (tr *testRepo) write(files map[string]string) {
	tr.t.Helper()
	for rel, content := range files {
		p := filepath.Join(tr.dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			tr.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			tr.t.Fatal(err)
		}
	}
}

// commit writes files, stages everything and commits; dates advance a day
// per commit.
func (tr *testRepo) commit(msg string, files map[string]string) string {
	tr.t.Helper()
	tr.write(files)
	wt, err := tr.r.Worktree()
	if err != nil {
		tr.t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		tr.t.Fatal(err)
	}
	tr.tick++
	when := time.Date(2026, 9, tr.tick, 12, 0, 0, 0, time.UTC)
	h, err := wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "Ann", Email: "ann@example.com", When: when}})
	if err != nil {
		tr.t.Fatal(err)
	}
	return h.String()
}

func (tr *testRepo) open(sub string) *Repo {
	tr.t.Helper()
	h, err := Open(filepath.Join(tr.dir, sub))
	if err != nil {
		tr.t.Fatal(err)
	}
	tr.t.Cleanup(func() { h.Close() })
	return h
}

// referenceHash is provenance-content-v1 written out independently, from
// the documented definition (Detailed Design §4).
func referenceHash(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	sum := sha256.New()
	sum.Write([]byte("provenance-content-v1\n"))
	for _, p := range paths {
		fmt.Fprintf(sum, "blob\x00%s\x00%d\x00%s", p, len(files[p]), files[p])
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

var base = map[string]string{
	".component":         "code: T\nname: Test\n",
	"REQ/REQ-1.md":       "---\nid: REQ-1\ntype: Requirement\n---\nShall.\n",
	"DOC/DOC-1.md":       "---\nid: DOC-1\ntype: Document\n---\nText.\n",
	"schema/Req.yaml":    "type: Requirement\n",
	".signatures/s.yaml": "signed: REQ-1\n",
}

func TestContentHash(t *testing.T) {
	tr := newRepo(t)
	sha := tr.commit("first", base)
	h := tr.open("")
	got, err := h.ContentHash(sha)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for p, c := range base {
		if !strings.HasPrefix(p, ".signatures/") {
			want[p] = c
		}
	}
	if got != referenceHash(want) {
		t.Errorf("hash %s, want %s (the signature ledger is excluded)", got, referenceHash(want))
	}

	// A signature record does not change the hash; content does.
	sha2 := tr.commit("sign", map[string]string{".signatures/t.yaml": "signed: DOC-1\n"})
	if h2, _ := tr.open("").ContentHash(sha2); h2 != got {
		t.Errorf("a signature record changed the hash")
	}
	sha3 := tr.commit("edit", map[string]string{"REQ/REQ-1.md": "---\nid: REQ-1\ntype: Requirement\n---\nShall not.\n"})
	if h3, _ := tr.open("").ContentHash(sha3); h3 == got {
		t.Errorf("a content edit did not change the hash")
	}
}

func TestComponentInASubfolder(t *testing.T) {
	tr := newRepo(t)
	files := map[string]string{"README.md": "outside\n"}
	for p, c := range base {
		files["comp/"+p] = c
	}
	sha := tr.commit("first", files)
	h := tr.open("comp")
	got, err := h.ContentHash(sha)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := tr.open("").ContentHash(sha); got == want {
		t.Error("the subfolder's hash covers the whole repository")
	}
	// Same files, relative to the component root, as a top-level component.
	tr2 := newRepo(t)
	sha2 := tr2.commit("first", base)
	if top, _ := tr2.open("").ContentHash(sha2); got != top {
		t.Errorf("subfolder component hash %s, want the top-level hash %s", got, top)
	}
}

func TestStatus(t *testing.T) {
	tr := newRepo(t)
	tr.commit("first", base)
	h := tr.open("")
	if d, err := h.Status(); err != nil || d.Any() {
		t.Fatalf("clean checkout: %+v, %v", d, err)
	}

	// CRLF line endings alone, as core.autocrlf writes them, are not a
	// change.
	tr.write(map[string]string{"DOC/DOC-1.md": strings.ReplaceAll(base["DOC/DOC-1.md"], "\n", "\r\n")})
	if d, _ := h.Status(); d.Any() {
		t.Errorf("CRLF-only difference reported dirty: %v", d.Paths)
	}

	tr.write(map[string]string{
		"REQ/REQ-1.md":   "---\nid: REQ-1\ntype: Requirement\n---\nChanged.\n",
		"USR/USR-1.md":   "---\nid: USR-1\ntype: UserNeed\n---\nNew.\n",
		"_site/index.md": "---\nid: X-1\ntype: Generated\n---\n", // the output folder
		"notes.txt":      "not content\n",
	})
	if err := os.Remove(filepath.Join(tr.dir, "schema", "Req.yaml")); err != nil {
		t.Fatal(err)
	}
	d, err := h.Status(filepath.Join(tr.dir, "_site"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.Paths, " "); got != "REQ/REQ-1.md USR/USR-1.md schema/Req.yaml" {
		t.Errorf("dirty paths: %s", got)
	}
	if got := strings.Join(d.Types, " "); got != "Requirement UserNeed" {
		t.Errorf("dirty types: %s", got)
	}
	if !d.Touches(Inputs{Prefixes: []string{"schema/"}}) || !d.Touches(Inputs{Types: []string{"UserNeed"}}) || d.Touches(Inputs{Paths: []string{"DOC/DOC-1.md"}}) {
		t.Error("Touches does not match the dirty paths and types")
	}
}

func TestLog(t *testing.T) {
	tr := newRepo(t)
	first := tr.commit("first", base)
	tr.commit("touch the document", map[string]string{"DOC/DOC-1.md": "---\nid: DOC-1\ntype: Document\n---\nText 2.\n"})
	third := tr.commit("touch the requirement\n\nWith a body.", map[string]string{"REQ/REQ-1.md": "---\nid: REQ-1\ntype: Requirement\n---\nShall 2.\n"})
	if _, err := tr.r.CreateTag("v1", plumbingHash(t, tr, first), nil); err != nil {
		t.Fatal(err)
	}
	l, err := tr.open("").Log()
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Commits) != 3 {
		t.Fatalf("%d commits, want 3", len(l.Commits))
	}
	c := l.Commits[0]
	if c.SHA != third || c.Subject != "touch the requirement" || c.Date != "2026-09-03" || c.Author != "Ann" {
		t.Errorf("newest commit: %+v", c)
	}
	if got := l.Commits[2].Tags; !slices.Equal(got, []string{"v1"}) {
		t.Errorf("tags of the first commit: %v", got)
	}
	doc := Inputs{Paths: []string{"DOC/DOC-1.md"}}
	if got := l.LastChanged(doc); got != l.Commits[1].SHA {
		t.Errorf("DOC-1 last changed in %s, want the second commit", Short(got))
	}
	// A page querying requirements changes with any requirement.
	if got := l.LastChanged(Inputs{Paths: []string{"DOC/DOC-1.md"}, Types: []string{"Requirement"}}); got != third {
		t.Errorf("with Requirement inputs: %s, want the third commit", Short(got))
	}
	if got := len(l.Revisions(Inputs{Prefixes: []string{"schema/"}})); got != 1 {
		t.Errorf("schema revisions: %d, want 1 (the first commit)", got)
	}
}

func plumbingHash(t *testing.T, tr *testRepo, sha string) (h [20]byte) {
	t.Helper()
	b, err := hex.DecodeString(sha)
	if err != nil {
		t.Fatal(err)
	}
	copy(h[:], b)
	return h
}

func TestNotARepository(t *testing.T) {
	if _, err := Open(t.TempDir()); err != ErrNotRepository {
		t.Errorf("err = %v, want ErrNotRepository", err)
	}
}

func TestShort(t *testing.T) {
	for in, want := range map[string]string{
		strings.Repeat("ab", 20): "abababa", strings.Repeat("ab", 20) + "-dirty": "abababa-dirty", "uncommitted": "uncommitted", "": "",
	} {
		if got := Short(in); got != want {
			t.Errorf("Short(%q) = %q, want %q", in, got, want)
		}
	}
}
