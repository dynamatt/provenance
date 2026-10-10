// Package history reads the repository's git history (DES-0022, DES-0023):
// the DHF content hash, the working tree's dirty state, and which commit
// last changed each file, for last_changed_sha and revision history.
//
// Git is read with go-git, never the git executable, so the host's git
// version is not part of the validated configuration (DES-0026).
package history

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"

	"github.com/dynamatt/provenance/internal/entity"
)

// Repo is a component repository's git history. The component may sit in a
// subfolder of the git working tree; paths are relative to the component
// root, slash-separated.
type Repo struct {
	r  *git.Repository
	st *filesystem.Storage
	// root is the component root; prefix its path inside the git tree
	// ("" when they are the same folder).
	root, prefix string
	head         *object.Commit
}

// ErrNotRepository is returned by Open for a folder outside any git
// repository.
var ErrNotRepository = errors.New("not in a git repository")

// Open opens the git repository containing the component root. A repository
// without commits is an error: there is no HEAD to describe. Close it when
// done.
func Open(root string) (*Repo, error) {
	work, gitDir, err := findGit(root)
	if err != nil {
		return nil, err
	}
	dotGit, err := gitFilesystem(gitDir)
	if err != nil {
		return nil, err
	}
	// Keeping pack files open avoids reopening one per object read, which
	// is slow on network and VM-shared filesystems.
	st := filesystem.NewStorageWithOptions(dotGit, cache.NewObjectLRUDefault(), filesystem.Options{KeepDescriptors: true})
	r, err := git.Open(st, osfs.New(work))
	if err != nil {
		st.Close()
		return nil, err
	}
	prefix, err := filepath.Rel(work, root)
	if err != nil {
		st.Close()
		return nil, err
	}
	prefix = filepath.ToSlash(prefix)
	if prefix == "." {
		prefix = ""
	}
	ref, err := r.Head()
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("reading HEAD: %w", err)
	}
	head, err := r.CommitObject(ref.Hash())
	if err != nil {
		st.Close()
		return nil, err
	}
	return &Repo{r: r, st: st, root: root, prefix: prefix, head: head}, nil
}

// Close releases the open pack files.
func (h *Repo) Close() error { return h.st.Close() }

// findGit walks up from dir to the working tree holding it, returning that
// folder and its git directory: .git, or where a .git file (a submodule or
// linked checkout) points.
func findGit(dir string) (work, gitDir string, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	for {
		dot := filepath.Join(dir, ".git")
		info, err := os.Stat(dot)
		switch {
		case err == nil && info.IsDir():
			return dir, dot, nil
		case err == nil:
			b, err := os.ReadFile(dot)
			if err != nil {
				return "", "", err
			}
			target, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
			if !ok {
				return "", "", fmt.Errorf("%s: not a gitdir file", dot)
			}
			target = strings.TrimSpace(target)
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			return dir, target, nil
		case !errors.Is(err, os.ErrNotExist):
			return "", "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", ErrNotRepository
		}
		dir = parent
	}
}

// gitFilesystem is the git directory gitDir. A linked worktree's (git
// worktree add) holds only its own HEAD and index, and names the main
// repository's, which holds the objects and refs, in its commondir file.
func gitFilesystem(gitDir string) (billy.Filesystem, error) {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if errors.Is(err, os.ErrNotExist) {
		return osfs.New(gitDir), nil
	}
	if err != nil {
		return nil, err
	}
	common := strings.TrimSpace(string(b))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return dotgit.NewRepositoryFilesystem(osfs.New(gitDir), osfs.New(common)), nil
}

// HEAD is the full hash of the commit checked out.
func (h *Repo) HEAD() string { return h.head.Hash.String() }

// Resolve finds a commit by hash, abbreviated hash, branch or tag.
func (h *Repo) Resolve(rev string) (string, error) {
	hash, err := h.r.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return "", fmt.Errorf("unknown commit %q", rev)
	}
	return hash.String(), nil
}

// Excluded reports whether a path is outside the content hash: the
// signature ledger, whose records are made about the content and so cannot
// be part of it.
func Excluded(p string) bool {
	return p == ".signatures" || strings.HasPrefix(p, ".signatures/")
}

// file is one entry of a commit's tree under the component root.
type file struct {
	path string
	mode filemode.FileMode
	hash plumbing.Hash
}

// files lists the tree of commit under the component root, sorted by path
// bytes, without excluded paths.
func (h *Repo) files(commit string) ([]file, error) {
	c, err := h.r.CommitObject(plumbing.NewHash(commit))
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	if h.prefix != "" {
		if tree, err = tree.Tree(h.prefix); err != nil {
			return nil, fmt.Errorf("%s is not in commit %s", h.prefix, commit[:7])
		}
	}
	var out []file
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if entry.Mode == filemode.Dir || Excluded(name) {
			continue
		}
		out = append(out, file{path: name, mode: entry.Mode, hash: entry.Hash})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

// ContentHash computes the DHF content hash of commit (DES-0022,
// algorithm provenance-content-v1):
//
//	SHA-256 over "provenance-content-v1\n", then for every file of the
//	commit's tree under the component root except .signatures/, in
//	ascending byte order of its slash-separated path relative to the
//	component root:
//	    kind NUL path NUL length NUL bytes
//	where kind is "blob" for a file or symbolic link (bytes: the blob as git
//	stores it, so line endings as committed whatever the checkout does) or
//	"commit" for a submodule (bytes: its commit hash in lower-case hex), and
//	length is the byte count in decimal.
//
// The result is "sha256:" and the digest in lower-case hex.
func (h *Repo) ContentHash(commit string) (string, error) {
	files, err := h.files(commit)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	io.WriteString(sum, "provenance-content-v1\n")
	for _, f := range files {
		kind, data := "blob", []byte(nil)
		if f.mode == filemode.Submodule {
			kind, data = "commit", []byte(f.hash.String())
		} else {
			if data, err = h.blob(f.hash); err != nil {
				return "", fmt.Errorf("%s: %w", f.path, err)
			}
		}
		fmt.Fprintf(sum, "%s\x00%s\x00%d\x00", kind, f.path, len(data))
		sum.Write(data)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}

func (h *Repo) blob(hash plumbing.Hash) ([]byte, error) {
	b, err := h.r.BlobObject(hash)
	if err != nil {
		return nil, err
	}
	rd, err := b.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	return io.ReadAll(rd)
}

// Dirty is how the working tree differs from HEAD.
type Dirty struct {
	// Paths are the content paths that differ, sorted.
	Paths []string
	// Types are the entity types of the differing entity files, as
	// committed or as now, sorted.
	Types []string
}

// Any reports whether anything differs.
func (d *Dirty) Any() bool { return d != nil && len(d.Paths) > 0 }

// Touches reports whether any input of a page differs.
func (d *Dirty) Touches(in Inputs) bool {
	if d == nil {
		return false
	}
	return in.sets().touches(&Commit{Paths: d.Paths, Types: d.Types})
}

// WorkingContentHash is HEAD's content hash, suffixed -dirty when the
// working tree differs from it (Status, with skip), and how it differs.
func (h *Repo) WorkingContentHash(skip ...string) (string, *Dirty, error) {
	hash, err := h.ContentHash(h.HEAD())
	if err != nil {
		return "", nil, err
	}
	dirty, err := h.Status(skip...)
	if err != nil {
		return "", nil, err
	}
	if dirty.Any() {
		hash += "-dirty"
	}
	return hash, dirty, nil
}

// Status compares the working tree with HEAD: the content paths that
// differ, a committed file changed or deleted, or a file Provenance
// reads that is not committed (an entity file, or anything in schema/,
// templates/, rules/ or assets/). A file that differs only by CRLF line
// endings, as a core.autocrlf checkout writes it, is not a difference.
// skip are folders never read (the export output folder).
func (h *Repo) Status(skip ...string) (*Dirty, error) {
	files, err := h.files(h.HEAD())
	if err != nil {
		return nil, err
	}
	tracked := make(map[string]bool, len(files))
	var dirty []string
	for _, f := range files {
		tracked[f.path] = true
		if f.mode == filemode.Submodule {
			continue
		}
		local := filepath.Join(h.root, filepath.FromSlash(f.path))
		var work []byte
		if f.mode == filemode.Symlink {
			target, err := os.Readlink(local)
			if err != nil {
				dirty = append(dirty, f.path)
				continue
			}
			work = []byte(filepath.ToSlash(target))
		} else if work, err = os.ReadFile(local); err != nil {
			dirty = append(dirty, f.path)
			continue
		}
		committed, err := h.blob(f.hash)
		if err != nil {
			return nil, err
		}
		if !sameContent(work, committed) {
			dirty = append(dirty, f.path)
		}
	}
	untracked, err := contentFiles(h.root, skip)
	if err != nil {
		return nil, err
	}
	for _, p := range untracked {
		if !tracked[p] {
			dirty = append(dirty, p)
		}
	}
	slices.Sort(dirty)
	d := &Dirty{Paths: slices.Compact(dirty)}
	byPath := make(map[string]file, len(files))
	for _, f := range files {
		byPath[f.path] = f
	}
	types := map[string]bool{}
	for _, p := range d.Paths {
		if !strings.EqualFold(path.Ext(p), ".md") {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(h.root, filepath.FromSlash(p))); err == nil {
			if e, _ := entity.Parse(p, data); e != nil {
				types[e.Type] = true
			}
		}
		if f, ok := byPath[p]; ok && f.mode != filemode.Submodule {
			data, err := h.blob(f.hash)
			if err != nil {
				return nil, err
			}
			if e, _ := entity.Parse(p, data); e != nil {
				types[e.Type] = true
			}
		}
	}
	d.Types = slices.Sorted(maps.Keys(types))
	return d, nil
}

func sameContent(work, committed []byte) bool {
	if bytes.Equal(work, committed) {
		return true
	}
	return !bytes.Contains(committed, []byte("\r\n")) && bytes.Equal(bytes.ReplaceAll(work, []byte("\r\n"), []byte("\n")), committed)
}

// contentFiles lists the working-tree files Provenance reads as content:
// entity files, .component, and every file under the configuration folders.
func contentFiles(root string, skip []string) ([]string, error) {
	entities, err := entity.Discover(root, skip...)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entities {
		out = append(out, e.Path)
	}
	if _, err := os.Stat(filepath.Join(root, ".component")); err == nil {
		out = append(out, ".component")
	}
	for _, dir := range []string{"schema", "templates", "rules", "assets"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipDir
			}
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Commit is one commit of the first-parent history.
type Commit struct {
	SHA     string
	Date    string // author date, YYYY-MM-DD in the author's time zone
	Author  string
	Subject string
	Tags    []string // tags pointing at the commit, sorted
	// Paths are the content paths the commit changed against its first
	// parent (every path, for the root commit); Types the entity types of
	// the entity files among them, before or after the change.
	Paths []string
	Types []string
}

// Log is the first-parent history from HEAD, newest first.
type Log struct {
	Commits []*Commit
}

// Log walks the first-parent history from HEAD once, diffing each commit's
// tree against its parent's.
func (h *Repo) Log() (*Log, error) {
	tags, err := h.tagsByCommit()
	if err != nil {
		return nil, err
	}
	l := &Log{}
	for c := h.head; c != nil; {
		var parent *object.Commit
		if c.NumParents() > 0 {
			if parent, err = c.Parent(0); err != nil {
				return nil, err
			}
		}
		cm, err := h.changes(c, parent)
		if err != nil {
			return nil, err
		}
		cm.SHA = c.Hash.String()
		cm.Date = c.Author.When.Format("2006-01-02")
		cm.Author = c.Author.Name
		cm.Subject, _, _ = strings.Cut(strings.TrimSpace(c.Message), "\n")
		cm.Tags = tags[c.Hash]
		l.Commits = append(l.Commits, cm)
		c = parent
	}
	return l, nil
}

// changes lists what c changed against parent (nil for a root commit),
// under the component root.
func (h *Repo) changes(c, parent *object.Commit) (*Commit, error) {
	tree := func(c *object.Commit) (*object.Tree, error) {
		if c == nil {
			return nil, nil
		}
		t, err := c.Tree()
		if err != nil || h.prefix == "" {
			return t, err
		}
		sub, err := t.Tree(h.prefix)
		if errors.Is(err, object.ErrDirectoryNotFound) {
			return nil, nil
		}
		return sub, err
	}
	to, err := tree(c)
	if err != nil {
		return nil, err
	}
	from, err := tree(parent)
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, err
	}
	cm := &Commit{}
	types := map[string]bool{}
	for _, ch := range changes {
		for _, side := range []object.ChangeEntry{ch.From, ch.To} {
			if side.Name == "" || Excluded(side.Name) {
				continue
			}
			cm.Paths = append(cm.Paths, side.Name)
			if strings.EqualFold(path.Ext(side.Name), ".md") && side.TreeEntry.Mode != filemode.Submodule {
				data, err := h.blob(side.TreeEntry.Hash)
				if err != nil {
					return nil, err
				}
				if e, _ := entity.Parse(side.Name, data); e != nil {
					types[e.Type] = true
				}
			}
		}
	}
	slices.Sort(cm.Paths)
	cm.Paths = slices.Compact(cm.Paths)
	cm.Types = slices.Sorted(maps.Keys(types))
	return cm, nil
}

func (h *Repo) tagsByCommit() (map[plumbing.Hash][]string, error) {
	iter, err := h.r.Tags()
	if err != nil {
		return nil, err
	}
	out := map[plumbing.Hash][]string{}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		hash := ref.Hash()
		if tag, err := h.r.TagObject(hash); err == nil {
			c, err := tag.Commit()
			if err != nil {
				return nil // a tag of something other than a commit
			}
			hash = c.Hash
		}
		out[hash] = append(out[hash], ref.Name().Short())
		return nil
	})
	for _, names := range out {
		slices.Sort(names)
	}
	return out, err
}

// Inputs are what a rendered page depends on: files, and entity types whose
// every entity can change a query result.
type Inputs struct {
	Paths []string
	Types []string
}

// inputSets are Inputs as sets, to test many commits against.
type inputSets struct{ paths, types map[string]bool }

func (in Inputs) sets() inputSets {
	s := inputSets{paths: map[string]bool{}, types: map[string]bool{}}
	for _, p := range in.Paths {
		s.paths[p] = true
	}
	for _, t := range in.Types {
		s.types[t] = true
	}
	return s
}

// touches reports whether c changed any of the inputs.
func (s inputSets) touches(c *Commit) bool {
	return slices.ContainsFunc(c.Paths, func(p string) bool { return s.paths[p] }) ||
		slices.ContainsFunc(c.Types, func(t string) bool { return s.types[t] })
}

// LastChanged is the newest commit that changed any of in (DES-0023), or
// "" when none did.
func (l *Log) LastChanged(in Inputs) string {
	if r := l.Revisions(in); len(r) > 0 {
		return r[0].SHA
	}
	return ""
}

// Revisions are the commits that changed any of in, newest first.
func (l *Log) Revisions(in Inputs) []*Commit {
	s := in.sets()
	var out []*Commit
	for _, c := range l.Commits {
		if s.touches(c) {
			out = append(out, c)
		}
	}
	return out
}

// Short abbreviates a commit hash to 7 characters, keeping any suffix
// (-dirty). Anything else, such as "uncommitted", is returned as it is.
func Short(sha string) string {
	base, suffix, _ := strings.Cut(sha, "-")
	if _, err := hex.DecodeString(base); err != nil || len(base) != 40 {
		return sha
	}
	base = base[:7]
	if suffix != "" {
		return base + "-" + suffix
	}
	return base
}
