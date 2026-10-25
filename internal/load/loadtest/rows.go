// Package loadtest is a second source of entities for tests (G6): files
// holding several entities each, so a test can show that a layer handles
// entities that are not one Markdown file each.
package loadtest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Rows reads every .rows file: a YAML list of entities, each a mapping with
// its id, its type and its fields. An entity is placed at its row.
type Rows struct{}

var _ load.Source = Rows{}

const ext = ".rows"

func (Rows) Load(root string, skip []string) (*load.Loaded, error) {
	out := &load.Loaded{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			for _, s := range skip {
				if p == s {
					return filepath.SkipDir
				}
			}
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ext) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out.Files = append(out.Files, rel)
		entities, err := Parse(rel, data)
		var fe *entity.Error
		if errors.As(err, &fe) {
			out.Problems = append(out.Problems, load.Problem{Check: "content/frontmatter", Path: rel, Line: fe.Line, Msg: fe.Msg, Err: fe})
			return nil
		}
		out.Entities = append(out.Entities, entities...)
		return err
	})
	return out, err
}

func (Rows) Types(path string, data func() ([]byte, error)) ([]string, bool, error) {
	if !strings.HasSuffix(path, ext) {
		return nil, false, nil
	}
	b, err := data()
	if err != nil {
		return nil, true, err
	}
	entities, err := Parse(path, b)
	var types []string
	for _, e := range entities {
		types = append(types, e.Type)
	}
	return types, true, err
}

// Parse reads the entities of a .rows file.
func Parse(path string, data []byte) ([]*entity.Entity, error) {
	var doc yaml.Node
	if err := yamlnode.Unmarshal(data, 1, &doc); err != nil {
		line, msg := yamlnode.ErrorLine(err)
		return nil, &entity.Error{Path: path, Line: line, Msg: msg}
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	list := doc.Content[0]
	if list.Kind != yaml.SequenceNode {
		return nil, &entity.Error{Path: path, Line: list.Line, Msg: "a .rows file is a list of entities"}
	}
	var out []*entity.Entity
	for _, row := range list.Content {
		id, typ := yamlnode.Lookup(row, "id"), yamlnode.Lookup(row, "type")
		if row.Kind != yaml.MappingNode || id == nil || typ == nil {
			return nil, &entity.Error{Path: path, Line: row.Line, Msg: "a row needs an id and a type"}
		}
		out = append(out, &entity.Entity{ID: id.Value, Type: typ.Value, Path: path, Raw: data, Front: row})
	}
	return out, nil
}
