// Package yamlnode reads parsed YAML nodes, which every file Provenance
// loads (entities, schema, query blocks, captions) is read through, so
// errors can name the line.
package yamlnode

import (
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

// ErrorLine splits a yaml.v3 parse or decode error into the line it names,
// counted from the start of the parsed text (0 when it names none), and
// its message. Of several decode errors, it keeps the first.
func ErrorLine(err error) (line int, msg string) {
	msg = strings.TrimPrefix(err.Error(), "yaml: ")
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "unmarshal errors:"))
	msg, _, _ = strings.Cut(msg, "\n")
	if n, _ := fmt.Sscanf(msg, "line %d:", &line); n == 1 {
		return line, strings.TrimSpace(strings.TrimPrefix(msg, fmt.Sprintf("line %d:", line)))
	}
	return 0, msg
}
