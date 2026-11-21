package history

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Change is one change to an entity's values (DES-0058): a row of the
// entity's changes.
type Change struct {
	// Commit is the commit on the first-parent history that made the
	// change; "" for a change not yet committed.
	Commit string
	// Date, Author and Subject are those of the commit that made the
	// change itself: Commit, or, when Commit merged a branch, the newest
	// commit on that branch whose values for the entity differ from those
	// at each of its parents. Date is its author date, YYYY-MM-DD in the
	// author's time zone. All "" for a change not yet committed.
	Date, Author, Subject string
	// Kind is Added when the change added the entity, otherwise
	// ChangedKind.
	Kind string
	// Fields are the fields whose own values the change set or changed,
	// in the entity type's order, "body" for a body that is no field;
	// every field with a value when it added the entity.
	Fields []string
}

// The kinds of change.
const (
	Added       = "added"
	ChangedKind = "changed"
	Removed     = "removed"
)

// Version is a component root's files at one version, for Values to read
// and compare.
type Version struct {
	// Commit is the version's commit; "" for the working tree.
	Commit string
	// FS holds every file of the version, nil for a version before the
	// first commit.
	FS fs.FS
	// Paths are the files compared that are at this version.
	Paths []string
}

// EntityChange is an entity whose values differ between two versions.
type EntityChange struct {
	ID, Type string
	// Kind is Added, ChangedKind or Removed.
	Kind string
	// Fields are as Change's; none for a removed entity.
	Fields []string
}

// Values compares the entities two versions of some files hold by their
// values, as diff compares them (DES-0055): those only one version holds,
// and those whose own values differ, with the fields that do. An entity
// whose file moved is matched by ID across the files. load provides it,
// from the sources and each version's schema.
type Values interface {
	Compare(before, after Version) ([]EntityChange, error)
}

// EntityHistory is every entity's changes, from HEAD's first-parent
// history and the working tree.
type EntityHistory struct {
	// Changes are each entity's changes by ID, newest first. Changes made
	// before the commit that last added the entity are left out: they are
	// another entity's that had its ID.
	Changes map[string][]*Change
	// Order holds the first-parent commits, oldest first: a commit's index
	// orders it among the others (DES-0058 compares commits by ancestry).
	Order []string
}

// Changes walks HEAD's first-parent history, comparing each commit's
// entities with its parent's by value through v, then the working tree's
// with HEAD's. It needs every commit: a shallow clone that cut some off
// is a *ShallowError.
func (h *Repo) Changes(content Content, v Values) (*EntityHistory, error) {
	shallow, err := h.st.Shallow()
	if err != nil {
		return nil, err
	}
	var chain []*object.Commit
	first := map[plumbing.Hash]bool{}
	for c := h.head; c != nil; {
		chain = append(chain, c)
		first[c.Hash] = true
		if c.NumParents() == 0 {
			break
		}
		if slices.Contains(shallow, c.Hash) {
			return nil, &ShallowError{Commit: c.Hash.String()}
		}
		if c, err = c.Parent(0); err != nil {
			return nil, err
		}
	}
	w := &walk{h: h, v: v, first: first, trees: map[plumbing.Hash]fs.FS{}}
	out := &EntityHistory{Changes: map[string][]*Change{}}
	closed := map[string]bool{}
	add := func(ec EntityChange, c *Change) {
		if closed[ec.ID] {
			return
		}
		if ec.Kind == Removed {
			closed[ec.ID] = true
			return
		}
		c.Kind, c.Fields = ec.Kind, ec.Fields
		out.Changes[ec.ID] = append(out.Changes[ec.ID], c)
		if ec.Kind == Added {
			closed[ec.ID] = true
		}
	}

	work, err := w.working(content)
	if err != nil {
		return nil, err
	}
	for _, ec := range work {
		add(ec, &Change{})
	}
	for i, c := range chain {
		var parent *object.Commit
		if i+1 < len(chain) {
			parent = chain[i+1]
		}
		changes, err := w.compare(parent, c)
		if err != nil {
			return nil, err
		}
		var makers map[string]*object.Commit
		if c.NumParents() > 1 && len(changes) > 0 {
			if makers, err = w.makers(c, changes); err != nil {
				return nil, err
			}
		}
		for _, ec := range changes {
			maker := c
			if m := makers[ec.ID]; m != nil {
				maker = m
			}
			add(ec, &Change{Commit: c.Hash.String(), Date: maker.Author.When.Format("2006-01-02"), Author: maker.Author.Name, Subject: subject(maker)})
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		out.Order = append(out.Order, chain[i].Hash.String())
	}
	return out, nil
}

func subject(c *object.Commit) string {
	s, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
	return s
}

// walk compares commits through Values, keeping each commit's tree.
type walk struct {
	h     *Repo
	v     Values
	first map[plumbing.Hash]bool
	trees map[plumbing.Hash]fs.FS
}

func (w *walk) tree(c *object.Commit) (fs.FS, error) {
	if t, ok := w.trees[c.Hash]; ok {
		return t, nil
	}
	t, err := w.h.Tree(c.Hash.String())
	if err != nil {
		return nil, err
	}
	// A walk reads each commit as a child and as a parent, one after the
	// other: two trees are all it needs at once.
	if len(w.trees) >= 4 {
		clear(w.trees)
	}
	w.trees[c.Hash] = t
	return t, nil
}

// compare lists the entities c changed against parent (nil for a root
// commit).
func (w *walk) compare(parent, c *object.Commit) ([]EntityChange, error) {
	to, err := w.h.componentTree(c)
	if err != nil {
		return nil, err
	}
	var from *object.Tree
	if parent != nil {
		if from, err = w.h.componentTree(parent); err != nil {
			return nil, err
		}
	}
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, err
	}
	var before, after []string
	for _, ch := range changes {
		if ch.From.Name != "" && !Excluded(ch.From.Name) && ch.From.TreeEntry.Mode != filemode.Submodule {
			before = append(before, ch.From.Name)
		}
		if ch.To.Name != "" && !Excluded(ch.To.Name) && ch.To.TreeEntry.Mode != filemode.Submodule {
			after = append(after, ch.To.Name)
		}
	}
	if len(before)+len(after) == 0 {
		return nil, nil
	}
	b := Version{Paths: before}
	if parent != nil {
		b.Commit = parent.Hash.String()
		if b.FS, err = w.tree(parent); err != nil {
			return nil, err
		}
	}
	a := Version{Commit: c.Hash.String(), Paths: after}
	if a.FS, err = w.tree(c); err != nil {
		return nil, err
	}
	return w.v.Compare(b, a)
}

// makers finds, for each entity a merge changed, the newest commit on the
// branches it merged that made the change itself: whose values for the
// entity differ from those at each of its parents. A commit that merged
// the base into the branch only carried the base's values, so it made no
// change itself. Commits on the first-parent history are not the branch's.
func (w *walk) makers(merge *object.Commit, changes []EntityChange) (map[string]*object.Commit, error) {
	want := map[string]bool{}
	for _, ec := range changes {
		want[ec.ID] = true
	}
	out := map[string]*object.Commit{}
	seen := map[plumbing.Hash]bool{}
	// Preorder from each merged parent, following first parents first:
	// the branch's own commits, newest first.
	var stack []*object.Commit
	for i := merge.NumParents() - 1; i >= 1; i-- {
		p, err := merge.Parent(i)
		if err != nil {
			return nil, err
		}
		stack = append(stack, p)
	}
	for len(stack) > 0 && len(out) < len(want) {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[c.Hash] || w.first[c.Hash] {
			continue
		}
		seen[c.Hash] = true
		own, err := w.own(c)
		if err != nil {
			return nil, err
		}
		for id := range own {
			if want[id] && out[id] == nil {
				out[id] = c
			}
		}
		for i := c.NumParents() - 1; i >= 0; i-- {
			p, err := c.Parent(i)
			if err != nil {
				return nil, err
			}
			stack = append(stack, p)
		}
	}
	return out, nil
}

// own is the set of entities c changed against every one of its parents.
func (w *walk) own(c *object.Commit) (map[string]bool, error) {
	if c.NumParents() == 0 {
		changes, err := w.compare(nil, c)
		return ids(changes), err
	}
	var out map[string]bool
	for i := range c.NumParents() {
		p, err := c.Parent(i)
		if err != nil {
			return nil, err
		}
		changes, err := w.compare(p, c)
		if err != nil {
			return nil, err
		}
		got := ids(changes)
		if out == nil {
			out = got
			continue
		}
		for id := range out {
			if !got[id] {
				delete(out, id)
			}
		}
	}
	return out, nil
}

func ids(changes []EntityChange) map[string]bool {
	out := map[string]bool{}
	for _, ec := range changes {
		out[ec.ID] = true
	}
	return out
}

// working compares the working tree's entities with HEAD's, in the files
// that differ (Status).
func (w *walk) working(content Content) ([]EntityChange, error) {
	dirty, err := w.h.Status(content)
	if err != nil {
		return nil, err
	}
	if !dirty.Any() {
		return nil, nil
	}
	head, err := w.tree(w.h.head)
	if err != nil {
		return nil, err
	}
	work := os.DirFS(w.h.root)
	var before, after []string
	for _, p := range dirty.Paths {
		if _, err := fs.Stat(head, p); err == nil {
			before = append(before, p)
		}
		if _, err := fs.Stat(work, p); err == nil {
			after = append(after, p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return w.v.Compare(Version{Commit: w.h.HEAD(), FS: head, Paths: before}, Version{FS: work, Paths: after})
}

// componentTree is c's tree under the component root; nil when the
// commit has no such folder.
func (h *Repo) componentTree(c *object.Commit) (*object.Tree, error) {
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

// DirKey identifies what the folder dir of fsys holds: equal keys, equal
// files. A commit's tree gives it from the object hashes it records,
// without reading a file; another file system's files are read. A schema
// is read once per key.
func DirKey(fsys fs.FS, dir string) (string, error) {
	sum := sha256.New()
	if t, ok := fsys.(*treeFS); ok {
		var names []string
		for name := range t.files {
			if strings.HasPrefix(name, dir+"/") {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			fmt.Fprintf(sum, "%s\x00%s\x00", name, t.files[name])
		}
		return hex.EncodeToString(sum.Sum(nil)), nil
	}
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && p == dir {
			return fs.SkipDir
		}
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		fmt.Fprintf(sum, "%s\x00%s\x00", p, plumbing.ComputeHash(plumbing.BlobObject, b))
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
