// Package repo finds the repository a command runs in and loads its
// repo-level configuration (Detailed Design §1).
package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
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
}

// ErrNotFound is returned when no .component exists in the start directory or
// any of its parents.
var ErrNotFound = errors.New("not inside a provenance repository: no " + ComponentFile +
	" file in the current directory or any parent")

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
	var c Component
	data, err := os.ReadFile(filepath.Join(root, ComponentFile))
	if err != nil {
		return c, err
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("%s: %w", ComponentFile, err)
	}
	if c.Code == "" {
		return c, fmt.Errorf("%s: missing code", ComponentFile)
	}
	if c.Name == "" {
		return c, fmt.Errorf("%s: missing name", ComponentFile)
	}
	return c, nil
}
