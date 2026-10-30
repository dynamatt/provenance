package validate

import (
	"bytes"
	"errors"

	"github.com/dynamatt/provenance/internal/canon"
	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/schema"
)

// format reports each Markdown entity file not in canonical form
// (content/format, DES-0038), at its first line fmt would change: the
// check fmt --check runs alone. A warning, so formatting blocks a merge
// only where the repository's gate also runs fmt --all --check.
func (r *Report) format(s *schema.Schema, parsed []*entity.Entity) {
	for _, e := range parsed {
		m, ok := e.Record.(*entity.Markdown)
		if !ok || s.Types[e.Type] == nil {
			continue
		}
		v := Violation{Rule: "content/format", Severity: Warning, Entity: e.ID, Path: e.Path}
		out, err := canon.File(e.Path, m.Raw(), s)
		var unsafe *canon.UnsafeError
		switch {
		case errors.As(err, &unsafe):
			v.Line, v.Message = 1, "fmt cannot put the file in canonical form: "+unsafe.Error()
		case err != nil:
			continue // reported by its own check
		case bytes.Equal(out, m.Raw()):
			continue
		default:
			v.Line, v.Message = firstDiff(m.Raw(), out), "not in canonical form; provenance fmt rewrites it"
		}
		r.add(v)
	}
}

// firstDiff is the first line, from 1, on which a and b differ.
func firstDiff(a, b []byte) int {
	al, bl := bytes.SplitAfter(a, []byte("\n")), bytes.SplitAfter(b, []byte("\n"))
	for i := range min(len(al), len(bl)) {
		if !bytes.Equal(al[i], bl[i]) {
			return i + 1
		}
	}
	return min(len(al), len(bl)) + 1
}
