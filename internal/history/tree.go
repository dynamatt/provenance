package history

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Tree is the component root's files at commit, read from git's objects
// without checking anything out (DES-0055): what the loader reads in
// place of the working tree to load the repository as it was. Paths are
// slash-separated and relative to the component root, as fs.FS's are.
//
// It holds what a checkout of the commit would: a file as git stores it,
// so line endings as committed, which every reader normalizes; a
// symbolic link to a file in the tree as that file, as the working tree
// follows it. A submodule, and a link to a folder or outside the tree,
// are left out: the loader never reads into a submodule, and a link out
// of the repository is no part of its content.
func (h *Repo) Tree(commit string) (fs.FS, error) {
	c, err := h.r.CommitObject(plumbing.NewHash(commit))
	if err != nil {
		return nil, fmt.Errorf("reading commit %s: %w", Short(commit), err)
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	if h.prefix != "" {
		if tree, err = tree.Tree(h.prefix); err != nil {
			return nil, fmt.Errorf("%s is not in commit %s", h.prefix, Short(commit))
		}
	}
	t := &treeFS{h: h, files: map[string]plumbing.Hash{}, dirs: map[string][]string{".": nil}}
	links := map[string]plumbing.Hash{}
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
		switch entry.Mode {
		case filemode.Dir:
			t.dirs[name] = nil
		case filemode.Submodule:
			continue
		case filemode.Symlink:
			links[name] = entry.Hash
		default:
			t.files[name] = entry.Hash
		}
	}
	// A link resolves to what its target is in the tree, through other
	// links, as the working tree resolves it.
	for name := range links {
		if hash, ok := t.resolve(name, links, 0); ok {
			t.files[name] = hash
		}
	}
	for name := range t.files {
		t.dirs[path.Dir(name)] = append(t.dirs[path.Dir(name)], name)
	}
	for name := range t.dirs {
		if name != "." {
			t.dirs[path.Dir(name)] = append(t.dirs[path.Dir(name)], name)
		}
	}
	for name, entries := range t.dirs {
		slices.Sort(entries)
		t.dirs[name] = slices.Compact(entries)
	}
	return t, nil
}

// resolve follows the link at name to a file of the tree.
func (t *treeFS) resolve(name string, links map[string]plumbing.Hash, depth int) (plumbing.Hash, bool) {
	if depth > 8 {
		return plumbing.ZeroHash, false
	}
	target, err := t.h.blob(links[name])
	if err != nil || len(target) == 0 || target[0] == '/' {
		return plumbing.ZeroHash, false
	}
	p := path.Join(path.Dir(name), string(target))
	if p == ".." || strings.HasPrefix(p, "../") {
		return plumbing.ZeroHash, false
	}
	if hash, ok := t.files[p]; ok {
		return hash, true
	}
	if _, ok := links[p]; ok {
		return t.resolve(p, links, depth+1)
	}
	return plumbing.ZeroHash, false
}

// treeFS is a commit's tree as an fs.FS. Blobs are read when a file is
// opened.
type treeFS struct {
	h     *Repo
	files map[string]plumbing.Hash
	// dirs are every folder's entries, as paths, sorted.
	dirs map[string][]string
}

var (
	_ fs.ReadFileFS = (*treeFS)(nil)
	_ fs.ReadDirFS  = (*treeFS)(nil)
	_ fs.StatFS     = (*treeFS)(nil)
)

func (t *treeFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if _, ok := t.dirs[name]; ok {
		entries, _ := t.ReadDir(name)
		return &treeDir{info: dirInfo(name), entries: entries}, nil
	}
	b, err := t.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &treeFile{info: fileInfo{name: path.Base(name), size: int64(len(b))}, Reader: bytes.NewReader(b)}, nil
}

func (t *treeFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	hash, ok := t.files[name]
	if !ok {
		err := fs.ErrNotExist
		if _, dir := t.dirs[name]; dir {
			err = errors.New("is a directory")
		}
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	b, err := t.h.blob(hash)
	if err != nil {
		return nil, &fs.PathError{Op: "read", Path: name, Err: err}
	}
	return b, nil
}

func (t *treeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	entries, ok := t.dirs[name]
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	out := make([]fs.DirEntry, 0, len(entries))
	for _, p := range entries {
		info, err := t.Stat(p)
		if err != nil {
			return nil, err
		}
		out = append(out, fs.FileInfoToDirEntry(info))
	}
	return out, nil
}

func (t *treeFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if _, ok := t.dirs[name]; ok {
		return dirInfo(name), nil
	}
	hash, ok := t.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	size, err := t.h.st.EncodedObjectSize(hash)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	return fileInfo{name: path.Base(name), size: size}, nil
}

func dirInfo(name string) fileInfo {
	return fileInfo{name: path.Base(name), dir: true}
}

// fileInfo describes a tree entry. A commit records no modification time,
// so ModTime is the zero time.
type fileInfo struct {
	name string
	size int64
	dir  bool
}

func (fi fileInfo) Name() string { return fi.name }
func (fi fileInfo) Size() int64  { return fi.size }
func (fi fileInfo) Mode() fs.FileMode {
	if fi.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (fi fileInfo) ModTime() time.Time { return time.Time{} }
func (fi fileInfo) IsDir() bool        { return fi.dir }
func (fi fileInfo) Sys() any           { return nil }

type treeFile struct {
	info fileInfo
	*bytes.Reader
}

func (f *treeFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *treeFile) Close() error               { return nil }

type treeDir struct {
	info    fileInfo
	entries []fs.DirEntry
	read    int
}

func (d *treeDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *treeDir) Close() error               { return nil }
func (d *treeDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: errors.New("is a directory")}
}

func (d *treeDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.entries[d.read:]
	if n <= 0 {
		d.read = len(d.entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	d.read += len(rest)
	return rest, nil
}
