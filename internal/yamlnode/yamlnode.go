// Package yamlnode reads parsed YAML nodes, which every file Provenance
// loads (entities, schema, query blocks, captions) is read through, so
// errors can name the line.
package yamlnode

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
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
// file: node lines are file lines, and so are the lines ErrorLine finds in a
// syntax error. yaml places an error found at the end of the text, such as
// an unclosed [, on the line after it; that is put on the text's last line.
func Unmarshal(data []byte, first int, doc *yaml.Node) error {
	if err := yaml.Unmarshal(data, doc); err != nil {
		last := max(bytes.Count(bytes.TrimRight(data, "\n"), []byte("\n"))+1, 1)
		return &fileError{err: err, shift: first - 1, last: last}
	}
	shiftLines(doc, first-1)
	return nil
}

type fileError struct {
	err         error
	shift, last int
}

func (e *fileError) Error() string { return e.err.Error() }
func (e *fileError) Unwrap() error { return e.err }

func shiftLines(n *yaml.Node, by int) {
	n.Line += by
	for _, c := range n.Content {
		shiftLines(c, by)
	}
}

// ErrorLine splits a YAML parse or decode error into the line where the
// problem was found (0 when it names none) and its message. When the
// construct at fault began on an earlier line, such as an unclosed [, the
// message says so with that line. Lines count from the start of the parsed
// text, or are file lines for an error from Unmarshal. Of several decode
// errors, it keeps the first.
func ErrorLine(err error) (line int, msg string) {
	var le *yaml.LoadError
	if !errors.As(err, &le) {
		return 0, strings.TrimPrefix(err.Error(), "yaml: ")
	}
	var fe *fileError
	errors.As(err, &fe)
	at := func(l int) int {
		switch {
		case l <= 0:
			return 0
		case fe == nil:
			return l
		}
		return min(l, fe.last) + fe.shift
	}
	line, msg = at(le.Mark.Line), le.Message
	if ctx := at(le.ContextMark.Line); le.ContextMsg != "" && ctx > 0 && ctx != line {
		msg += fmt.Sprintf(" (%s at line %d)", le.ContextMsg, ctx)
	}
	return line, msg
}
