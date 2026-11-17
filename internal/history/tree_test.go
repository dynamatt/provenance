package history

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

// A commit's tree reads as the commit's files, under the component root,
// whatever the working tree holds now; fstest checks it is a proper fs.FS.
func TestTree(t *testing.T) {
	tr := newRepo(t)
	first := tr.commit("first", map[string]string{
		"comp/.component":      "code: X\nname: X\n",
		"comp/REQ/REQ-0001.md": "one\r\n",
		"comp/schema/a.yaml":   "type: A\n",
		"comp/z.md":            "z",
		"outside.md":           "not in the component",
	})
	for link, target := range map[string]string{
		"comp/REQ/link.md": "REQ-0001.md",
		"comp/chain.md":    "REQ/link.md",
		"comp/escape.md":   "../outside.md",
		"comp/dir-link":    "REQ",
		"comp/dangling.md": "nothing.md",
		"comp/absolute.md": "/etc/passwd",
	} {
		if err := os.Symlink(target, filepath.Join(tr.dir, filepath.FromSlash(link))); err != nil {
			t.Skip("no symbolic links here:", err)
		}
	}
	second := tr.commit("second", map[string]string{"comp/REQ/REQ-0001.md": "changed", "comp/REQ/REQ-0002.md": "two"})
	tr.write(map[string]string{"comp/REQ/REQ-0003.md": "not committed"})

	h, err := Open(filepath.Join(tr.dir, "comp"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	old, err := h.Tree(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(old, ".component", "REQ/REQ-0001.md", "schema/a.yaml", "z.md"); err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.ReadFile(old, "REQ/REQ-0001.md"); string(b) != "one\r\n" {
		t.Errorf("REQ-0001 at the first commit = %q, as committed", b)
	}
	if _, err := fs.Stat(old, "REQ/REQ-0002.md"); !os.IsNotExist(err) {
		t.Errorf("REQ-0002 is not in the first commit: %v", err)
	}

	now, err := h.Tree(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(now, "REQ/REQ-0002.md", "REQ/link.md", "chain.md"); err != nil {
		t.Fatal(err)
	}
	var files []string
	fs.WalkDir(now, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	want := []string{".component", "REQ/REQ-0001.md", "REQ/REQ-0002.md", "REQ/link.md", "chain.md", "schema/a.yaml", "z.md"}
	if !slices.Equal(files, want) {
		t.Errorf("files = %q\nwant    %q (links to files followed; links out of the tree, to folders and to nothing left out; the uncommitted file absent)", files, want)
	}
	for _, p := range []string{"REQ/link.md", "chain.md"} {
		if b, _ := fs.ReadFile(now, p); string(b) != "changed" {
			t.Errorf("%s = %q, want REQ-0001's text", p, b)
		}
	}
	if _, err := fs.ReadFile(now, "../outside.md"); err == nil {
		t.Error("a path out of the tree was read")
	}
}
