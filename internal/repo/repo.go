// Package repo finds the repository a command runs in and loads its
// repo-level configuration (DES-0005).
package repo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/yamlnode"
)

// ComponentFile marks a repository root and holds its component metadata.
const ComponentFile = ".component"

// Component is the metadata in .component: the code used for cross-repo
// addressing and the human-readable name.
type Component struct {
	Code string `yaml:"code"`
	Name string `yaml:"name"`
}

// Repo is a provenance repository.
type Repo struct {
	// Root is the absolute path of the directory holding .component.
	Root      string
	Component Component
	// FS is what the repository's files are read from, slash-separated
	// paths relative to Root: the working tree, or a commit's tree
	// (DES-0055). Nil is the working tree at Root.
	FS fs.FS
}

// Files is what the repository's files are read from: FS, or the working
// tree at Root.
func (r *Repo) Files() fs.FS {
	if r.FS != nil {
		return r.FS
	}
	return os.DirFS(r.Root)
}

// FromFS is the repository at root whose files are read from fsys, such
// as a commit's tree; its .component is fsys's.
func FromFS(root string, fsys fs.FS) (*Repo, error) {
	data, err := fs.ReadFile(fsys, ComponentFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no %s file: not a Provenance repository there", ComponentFile)
	}
	if err != nil {
		return nil, err
	}
	c, err := parseComponent(data)
	if err != nil {
		return nil, err
	}
	return &Repo{Root: root, Component: c, FS: fsys}, nil
}

// ErrNotFound is returned when no .component exists in the start directory or
// any of its parents. .component is hidden, so the message says so: a
// repository moved without its hidden files is the likely cause.
var ErrNotFound = errors.New("not in a Provenance repository: no " + ComponentFile +
	" file here or in any folder above. It is a hidden file at the repository's root: " +
	"when moving a repository, move its hidden files (" + ComponentFile + ", .git) too; " +
	"to start one, run provenance init")

// Find returns the nearest directory at or above start that contains
// .component.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(filepath.Join(dir, ComponentFile))
		if err == nil && !info.IsDir() {
			return dir, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// Open finds the repository containing start and loads its .component.
func Open(start string) (*Repo, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	c, err := loadComponent(root)
	if err != nil {
		return nil, err
	}
	return &Repo{Root: root, Component: c}, nil
}

func loadComponent(root string) (Component, error) {
	data, err := os.ReadFile(filepath.Join(root, ComponentFile))
	if err != nil {
		return Component{}, err
	}
	return parseComponent(data)
}

func parseComponent(data []byte) (Component, error) {
	var c Component
	var doc yaml.Node
	err := yamlnode.Unmarshal(data, 1, &doc)
	if err == nil && len(doc.Content) > 0 {
		err = doc.Content[0].Decode(&c)
	}
	if err != nil {
		if line, msg := yamlnode.ErrorLine(err); line > 0 {
			return c, fmt.Errorf("%s:%d: %s", ComponentFile, line, msg)
		}
		return c, fmt.Errorf("%s: %w", ComponentFile, err)
	}
	if c.Code == "" {
		return c, fmt.Errorf("%s: no code: give the component's code, such as code: NEURO", ComponentFile)
	}
	if c.Name == "" {
		return c, fmt.Errorf("%s: no name: give the component's name, such as name: Neurostimulator", ComponentFile)
	}
	return c, nil
}
