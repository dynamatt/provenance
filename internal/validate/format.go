package validate

import (
	"errors"

	"github.com/dynamatt/provenance/internal/canon"
	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/schema"
)

// format reports each entity file not in canonical form (content/format,
// DES-0038), at its first line fmt would change: the check fmt --check
// runs alone. A warning, so formatting blocks a merge only where the
// repository's gate also runs fmt --all --check. An entity of an
// undeclared type is reported by its own check.
func (r *Report) format(s *schema.Schema, parsed []*entity.Entity) {
	for _, e := range parsed {
		if s.Types[e.Type] == nil {
			continue
		}
		_, line, ok, err := canon.Format(e, s)
		v := Violation{Rule: "content/format", Severity: Warning, Entity: e.ID, Path: e.Path, Line: line,
			Message: "not in canonical form; provenance fmt rewrites it"}
		var unsafe *canon.UnsafeError
		switch {
		case errors.As(err, &unsafe):
			v.Line, v.Message = 1, "fmt cannot put the file in canonical form: "+unsafe.Error()
		case !ok || line == 0:
			continue
		}
		r.add(v)
	}
}
