// Package entity discovers and parses entity files: Markdown files whose YAML
// frontmatter declares an id and a type (DES-0004, DES-0006).
package entity

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Entity is one parsed entity file. Raw keeps the file's exact bytes: the
// content hash is computed over source as committed, never over a
// re-serialized form (DES-0022).
type Entity struct {
	ID   string
	Type string
	// Path is slash-separated and relative to the repository root.
	Path string
	Raw  []byte
	// Front is the frontmatter's top-level mapping node. Node line numbers
	// are file line numbers.
	Front *yaml.Node
	// Body is everything after the closing frontmatter delimiter.
	Body string
	// BodyLine is the file line number where Body starts.
	BodyLine int
}

// Scalar returns the string form of a top-level scalar frontmatter value.
func (e *Entity) Scalar(key string) (string, bool) {
	if v := yamlnode.Lookup(e.Front, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value, true
	}
	return "", false
}

// Error is a problem with one file, reported as path:line: message.
type Error struct {
	Path string
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return e.Path + ": " + e.Msg
}

// Parse parses one Markdown file. It returns nil, nil when the file is not an
// entity: no frontmatter, or frontmatter without both id and type.
// Frontmatter that is not valid YAML is an error — the file might be an
// entity, and silently leaving it out of an export would lose it.
func Parse(path string, raw []byte) (*Entity, error) {
	e, _, err := parse(path, raw)
	return e, err
}

// parse is Parse, also returning partial: frontmatter that declares one of
// id and type but not the other, which Parse leaves out as not an entity.
func parse(path string, raw []byte) (e *Entity, partial *Error, err error) {
	front, body, bodyLine, ok := split(raw)
	if !ok {
		return nil, nil, nil
	}
	var doc yaml.Node
	if err := yamlnode.Unmarshal(front, 2, &doc); err != nil { // after the opening ---
		return nil, nil, yamlError(path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, nil
	}
	m := doc.Content[0]
	idNode, typeNode := yamlnode.Lookup(m, "id"), yamlnode.Lookup(m, "type")
	switch {
	case idNode == nil && typeNode == nil:
		return nil, nil, nil
	case typeNode == nil:
		return nil, &Error{Path: path, Line: idNode.Line, Msg: "frontmatter has an id but no type, so the file is not read as an entity"}, nil
	case idNode == nil:
		return nil, &Error{Path: path, Line: typeNode.Line, Msg: "frontmatter has a type but no id, so the file is not read as an entity"}, nil
	}
	for _, f := range []struct {
		key  string
		node *yaml.Node
	}{{"id", idNode}, {"type", typeNode}} {
		if f.node.Kind != yaml.ScalarNode || f.node.Tag != "!!str" || strings.TrimSpace(f.node.Value) == "" {
			return nil, nil, &Error{Path: path, Line: f.node.Line, Msg: f.key + " must be a non-empty string"}
		}
	}
	return &Entity{
		ID:       idNode.Value,
		Type:     typeNode.Value,
		Path:     path,
		Raw:      raw,
		Front:    m,
		Body:     body,
		BodyLine: bodyLine,
	}, nil, nil
}

// split separates frontmatter from body. Frontmatter is a first line of
// exactly "---" and runs to the next line of exactly "---"; a UTF-8 BOM and
// CRLF line endings are tolerated.
func split(raw []byte) (front []byte, body string, bodyLine int, ok bool) {
	text := bytes.TrimPrefix(raw, utf8BOM)
	lines := bytes.SplitAfter(text, []byte("\n"))
	if len(lines) == 0 || !isDelimiter(lines[0]) {
		return nil, "", 0, false
	}
	for i := 1; i < len(lines); i++ {
		if isDelimiter(lines[i]) {
			return bytes.Join(lines[1:i], nil), string(bytes.Join(lines[i+1:], nil)), i + 2, true
		}
	}
	return nil, "", 0, false
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func isDelimiter(line []byte) bool {
	return string(bytes.TrimRight(line, "\r\n")) == "---"
}

func yamlError(path string, err error) error {
	line, msg := yamlnode.ErrorLine(err)
	return &Error{Path: path, Line: line, Msg: "invalid frontmatter: " + msg}
}

// ConfigDirs are the repository-root folders that hold configuration rather
// than entities (DES-0005).
var ConfigDirs = []string{"schema", "rules", "templates", ".signatures", "assets"}

// DuplicateIDError reports two files declaring the same ID, with the line
// of the id in each.
type DuplicateIDError struct {
	ID                  string
	First, Then         string
	FirstLine, ThenLine int
}

func (e *DuplicateIDError) Error() string {
	return fmt.Sprintf("duplicate entity ID %s in %s and %s", e.ID, e.First, e.Then)
}

// Discover walks root and returns every entity, sorted by ID, failing with
// the first problem it finds. It skips the configuration folders at the
// repository root, .git anywhere, and any folder in skip (absolute paths,
// e.g. the export output folder).
func Discover(root string, skip ...string) ([]*Entity, error) {
	sc, err := Scan(root, skip...)
	if sc != nil && len(sc.Problems) > 0 {
		return nil, sc.Problems[0]
	}
	if err != nil {
		return nil, err
	}
	return sc.Entities, nil
}

// Scanned is everything Scan found.
type Scanned struct {
	// Entities are sorted by ID. Of several files declaring one ID, only
	// the first found is here.
	Entities []*Entity
	// Problems are an *Error for a file whose frontmatter does not parse,
	// or a *DuplicateIDError, in the order the walk found them.
	Problems []error
	// Partial are files whose frontmatter declares only one of id and type.
	// Discover leaves them out as not entities; validate reports them.
	Partial []*Error
}

// Scan walks root like Discover but collects every problem instead of
// stopping at the first. err is an I/O error, which stops the walk; what was
// found before it is still returned.
func Scan(root string, skip ...string) (*Scanned, error) {
	skipped := map[string]bool{}
	for _, s := range skip {
		if abs, err := filepath.Abs(s); err == nil {
			skipped[abs] = true
		}
	}
	for _, d := range ConfigDirs {
		skipped[filepath.Join(root, d)] = true
	}

	sc := &Scanned{}
	byID := map[string]*Entity{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || skipped[p]) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		e, partial, err := parse(rel, raw)
		switch {
		case err != nil:
			sc.Problems = append(sc.Problems, err)
		case partial != nil:
			sc.Partial = append(sc.Partial, partial)
		case e == nil:
		case byID[e.ID] != nil:
			prev := byID[e.ID]
			sc.Problems = append(sc.Problems, &DuplicateIDError{ID: e.ID, First: prev.Path, Then: e.Path, FirstLine: prev.IDLine(), ThenLine: e.IDLine()})
		default:
			byID[e.ID] = e
			sc.Entities = append(sc.Entities, e)
		}
		return nil
	})
	sort.Slice(sc.Entities, func(i, j int) bool { return sc.Entities[i].ID < sc.Entities[j].ID })
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			if rel, relErr := filepath.Rel(root, pathErr.Path); relErr == nil {
				pathErr.Path = filepath.ToSlash(rel)
			}
		}
		return sc, err
	}
	return sc, nil
}

// IDLine is the file line of the entity's id.
func (e *Entity) IDLine() int { return yamlnode.Lookup(e.Front, "id").Line }
