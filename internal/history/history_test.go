package history

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// mdContent is the Content of the tests: every .md file under root whose
// frontmatter names a type holds an entity of that type. Folders in skip
// are not read.
type mdContent struct {
	root string
	skip []string
}

var (
	idLine   = regexp.MustCompile(`(?m)^id: (\S+)$`)
	typeLine = regexp.MustCompile(`(?m)^type: (\S+)$`)
)

func (c mdContent) Files() ([]string, error) {
	var out []string
	err := filepath.WalkDir(c.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || slices.Contains(c.skip, p)) {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(c.root, p)
		rel = filepath.ToSlash(rel)
		if !d.IsDir() && strings.HasSuffix(rel, ".md") {
			es, err := c.Entities(rel, func() ([]byte, error) { return os.ReadFile(p) })
			if err != nil {
				return err
			}
			if len(es) > 0 {
				out = append(out, rel)
			}
		}
		return nil
	})
	return out, err
}

func (mdContent) Entities(path string, data func() ([]byte, error)) ([]FileEntity, error) {
	if !strings.HasSuffix(path, ".md") {
		return nil, nil
	}
	b, err := data()
	if err != nil {
		return nil, err
	}
	id, typ := idLine.FindSubmatch(b), typeLine.FindSubmatch(b)
	if id != nil && typ != nil {
		return []FileEntity{{ID: string(id[1]), Type: string(typ[1]), Text: string(b)}}, nil
	}
	return nil, nil
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
// the documented definition (DES-0022).
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

func TestLinkedWorktree(t *testing.T) {
	// The layout git worktree add writes: the checkout's .git file names a
	// git directory holding its own HEAD, whose commondir names the main
	// repository's, which holds the objects.
	tr := newRepo(t)
	sha := tr.commit("first", base)
	gitDir := filepath.Join(tr.dir, ".git", "worktrees", "wt")
	work := filepath.Join(t.TempDir(), "wt")
	files := map[string]string{
		filepath.Join(gitDir, "HEAD"):      sha + "\n",
		filepath.Join(gitDir, "commondir"): "../..\n",
		filepath.Join(work, ".git"):        "gitdir: " + gitDir + "\n",
	}
	for rel, content := range base {
		files[filepath.Join(work, filepath.FromSlash(rel))] = content
	}
	for p, content := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h, err := Open(work)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	got, dirty, err := h.WorkingContentHash(mdContent{root: work})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := tr.open("").ContentHash(sha); h.HEAD() != sha || got != want || dirty.Any() {
		t.Errorf("worktree: HEAD %s, hash %s, dirty %v; want %s, %s, clean", h.HEAD(), got, dirty.Paths, sha, want)
	}
}

func TestStatus(t *testing.T) {
	tr := newRepo(t)
	tr.commit("first", base)
	h := tr.open("")
	content := mdContent{root: tr.dir, skip: []string{filepath.Join(tr.dir, "_site")}}
	if d, err := h.Status(content); err != nil || d.Any() {
		t.Fatalf("clean checkout: %+v, %v", d, err)
	}

	// CRLF line endings alone, as core.autocrlf writes them, are not a
	// change.
	tr.write(map[string]string{"DOC/DOC-1.md": strings.ReplaceAll(base["DOC/DOC-1.md"], "\n", "\r\n")})
	if d, _ := h.Status(content); d.Any() {
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
	d, err := h.Status(content)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.Paths, " "); got != "REQ/REQ-1.md USR/USR-1.md schema/Req.yaml" {
		t.Errorf("dirty paths: %s", got)
	}
	if got := strings.Join(d.Entities, " ") + " / " + strings.Join(d.Types, " "); got != "REQ-1 USR-1 / Requirement UserNeed" {
		t.Errorf("dirty entities and types: %s", got)
	}
	if !d.Touches(Inputs{Paths: []string{"schema/Req.yaml"}}) || !d.Touches(Inputs{Types: []string{"UserNeed"}}) || !d.Touches(Inputs{Entities: []string{"REQ-1"}}) || d.Touches(Inputs{Entities: []string{"DOC-1"}}) {
		t.Error("Touches does not match the dirty entities, paths and types")
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
	l, err := tr.open("").Log(mdContent{root: tr.dir})
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
	if got := strings.Join(l.Commits[1].Entities, " "); got != "DOC-1" {
		t.Errorf("the second commit changed %s, want DOC-1", got)
	}
	doc := Inputs{Entities: []string{"DOC-1"}}
	if got := l.LastChanged(doc); got != l.Commits[1].SHA {
		t.Errorf("DOC-1 last changed in %s, want the second commit", Short(got))
	}
	// A page querying requirements changes with any requirement.
	if got := l.LastChanged(Inputs{Entities: []string{"DOC-1"}, Types: []string{"Requirement"}}); got != third {
		t.Errorf("with Requirement inputs: %s, want the third commit", Short(got))
	}
	if got := l.LastChanged(Inputs{Paths: []string{"schema/Req.yaml"}}); got != first {
		t.Errorf("schema/Req.yaml last changed in %s, want the first commit", Short(got))
	}
}

// A shallow clone has HEAD, so its content hash and dirty state, but not
// the history it cut off, which the log needs.
func TestShallowClone(t *testing.T) {
	tr := newRepo(t)
	tr.commit("first", base)
	second := tr.commit("second", map[string]string{"DOC/DOC-1.md": "---\nid: DOC-1\ntype: Document\n---\nText 2.\n"})
	if err := os.WriteFile(filepath.Join(tr.dir, ".git", "shallow"), []byte(second+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := tr.open("")
	if _, dirty, err := h.WorkingContentHash(mdContent{root: tr.dir}); err != nil || dirty.Any() {
		t.Errorf("content hash: dirty %v, %v", dirty, err)
	}
	_, err := h.Log(mdContent{root: tr.dir})
	var se *ShallowError
	if !errors.As(err, &se) || se.Commit != second || !strings.Contains(err.Error(), "fetch-depth: 0") {
		t.Errorf("err = %v, want a ShallowError at the second commit naming fetch-depth: 0", err)
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
