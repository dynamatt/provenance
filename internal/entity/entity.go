// Package entity discovers and parses entity files: Markdown files whose YAML
// frontmatter declares an id and a type (High-Level Design §4.3).
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

	"go.yaml.in/yaml/v3"
)

// Entity is one parsed entity file. Raw keeps the file's exact bytes: the
// content hash is computed over source as committed, never over a
// re-serialized form (Detailed Design §4).
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
	if v := Lookup(e.Front, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value, true
	}
	return "", false
}

// Lookup returns the value node for key in a mapping node, or nil.
func Lookup(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
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
// entity, and silently leaving it out of an export would drop a record.
func Parse(path string, raw []byte) (*Entity, error) {
	front, body, bodyLine, ok := split(raw)
	if !ok {
		return nil, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(front, &doc); err != nil {
		return nil, yamlError(path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	m := doc.Content[0]
	shiftLines(m, 1) // frontmatter starts on file line 2
	idNode, typeNode := Lookup(m, "id"), Lookup(m, "type")
	if idNode == nil || typeNode == nil {
		return nil, nil
	}
	for _, f := range []struct {
		key  string
		node *yaml.Node
	}{{"id", idNode}, {"type", typeNode}} {
		if f.node.Kind != yaml.ScalarNode || f.node.Tag != "!!str" || strings.TrimSpace(f.node.Value) == "" {
			return nil, &Error{Path: path, Line: f.node.Line, Msg: f.key + " must be a non-empty string"}
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
	}, nil
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

func shiftLines(n *yaml.Node, by int) {
	n.Line += by
	for _, c := range n.Content {
		shiftLines(c, by)
	}
}

// yamlError converts a YAML syntax error, whose line numbers count from the
// start of the frontmatter, to file line numbers.
func yamlError(path string, err error) error {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "unmarshal errors:"))
	msg, _, _ = strings.Cut(msg, "\n")
	var line int
	if n, _ := fmt.Sscanf(msg, "line %d:", &line); n == 1 {
		msg = strings.TrimSpace(strings.TrimPrefix(msg, fmt.Sprintf("line %d:", line)))
		return &Error{Path: path, Line: line + 1, Msg: "invalid frontmatter: " + msg}
	}
	return &Error{Path: path, Msg: "invalid frontmatter: " + msg}
}

// ConfigDirs are the repository-root folders that hold configuration rather
// than entities (Detailed Design §1).
var ConfigDirs = []string{"schema", "rules", "templates", ".signatures", "assets"}

// DuplicateIDError reports two files declaring the same ID.
type DuplicateIDError struct {
	ID          string
	First, Then string
}

func (e *DuplicateIDError) Error() string {
	return fmt.Sprintf("duplicate entity ID %s in %s and %s", e.ID, e.First, e.Then)
}

// Discover walks root and returns every entity, sorted by ID. It skips the
// configuration folders at the repository root, .git anywhere, and any
// folder in skip (absolute paths, e.g. the export output folder).
func Discover(root string, skip ...string) ([]*Entity, error) {
	skipped := map[string]bool{}
	for _, s := range skip {
		if abs, err := filepath.Abs(s); err == nil {
			skipped[abs] = true
		}
	}
	for _, d := range ConfigDirs {
		skipped[filepath.Join(root, d)] = true
	}

	var entities []*Entity
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
		e, err := Parse(rel, raw)
		if err != nil || e == nil {
			return err
		}
		if prev, dup := byID[e.ID]; dup {
			return &DuplicateIDError{ID: e.ID, First: prev.Path, Then: e.Path}
		}
		byID[e.ID] = e
		entities = append(entities, e)
		return nil
	})
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			if rel, relErr := filepath.Rel(root, pathErr.Path); relErr == nil {
				pathErr.Path = filepath.ToSlash(rel)
			}
		}
		return nil, err
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].ID < entities[j].ID })
	return entities, nil
}
