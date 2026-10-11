// Package yamlnode reads parsed YAML nodes, which every file Provenance
// loads (entities, schema, query blocks, captions) is read through, so
// errors can name the line.
package yamlnode

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Lookup returns the value node for key in a mapping node, or nil when
// mapping is nil, is not a mapping, or has no such key.
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

// Unmarshal is yaml.Unmarshal for text that starts on line first of its
// file: node lines are file lines, and so is the line ErrorLine finds in a
// syntax error. The text is read after two blank lines; an error yaml
// places before the text is put on its first line.
//
// This works around a bug in go.yaml.in/yaml/v3 (v3.0.5), in parser.fail()
// in decode.go, which formats an error's line from the parser's 0-based
// marks:
//   - it treats line 0 as "no line", so an error on the first line it reads
//     (e.g. `id: a: b`) has no line at all; the blank lines keep every line
//     above 0;
//   - it adds 1 to scanner errors' lines but not to parser errors', and
//     prefers the line where the construct began to the one where the
//     problem was found, so an unclosed `[` is reported a line or more
//     before the problem. This is not worked around.
//
// v4 (go.yaml.in/yaml/v4, at v4.0.0-rc.6 on 2026-10-11) fixes both with a
// structured LoadError that carries the line and column of the problem
// and of its context. Upgrade to v4 once it is released, drop this
// workaround, and use the columns in reports (SARIF can show them).
func Unmarshal(data []byte, first int, doc *yaml.Node) error {
	if err := yaml.Unmarshal(append([]byte("\n\n"), data...), doc); err != nil {
		return &fileError{err: err, first: first}
	}
	shiftLines(doc, first-3)
	return nil
}

type fileError struct {
	err   error
	first int
}

func (e *fileError) Error() string { return e.err.Error() }
func (e *fileError) Unwrap() error { return e.err }

func shiftLines(n *yaml.Node, by int) {
	n.Line += by
	for _, c := range n.Content {
		shiftLines(c, by)
	}
}

// ErrorLine splits a yaml.v3 parse or decode error into the line it names
// (0 when it names none) and its message. The line counts from the start
// of the parsed text, or is a file line for an error from Unmarshal. Of
// several decode errors, it keeps the first.
func ErrorLine(err error) (line int, msg string) {
	defer func() {
		var fe *fileError
		if line > 0 && errors.As(err, &fe) {
			line = max(line+fe.first-3, fe.first)
		}
	}()
	msg = strings.TrimPrefix(err.Error(), "yaml: ")
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "unmarshal errors:"))
	msg, _, _ = strings.Cut(msg, "\n")
	if n, _ := fmt.Sscanf(msg, "line %d:", &line); n == 1 {
		return line, strings.TrimSpace(strings.TrimPrefix(msg, fmt.Sprintf("line %d:", line)))
	}
	return 0, msg
}
