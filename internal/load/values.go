package load

import (
	"errors"
	"io/fs"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/compare"
	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/schema"
)

// values is history.Values from the sources: each version's files read
// by the sources its own schema gives, typed by that schema and compared
// as diff compares them (DES-0058). A schema is read once per version of
// the schema folder.
type values struct {
	schemas map[string]*schema.Schema
}

func newValues() *values { return &values{schemas: map[string]*schema.Schema{}} }

var _ history.Values = (*values)(nil)

// version is one version's entities by ID: typed when its schema reads
// them, otherwise as parsed.
type version struct {
	typed  map[string]*model.Entity
	parsed map[string]*entity.Entity
	texts  map[string]string
}

func (v *values) Compare(before, after history.Version) ([]history.EntityChange, error) {
	a, err := v.read(before)
	if err != nil {
		return nil, err
	}
	b, err := v.read(after)
	if err != nil {
		return nil, err
	}
	var ids []string
	for id := range a.parsed {
		ids = append(ids, id)
	}
	for id := range b.parsed {
		if a.parsed[id] == nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	var out []history.EntityChange
	for _, id := range ids {
		pa, pb := a.parsed[id], b.parsed[id]
		switch {
		case pa == nil:
			fields := set(b.typed[id])
			if b.typed[id] == nil {
				fields = written(pb)
			}
			out = append(out, history.EntityChange{ID: id, Type: pb.Type, Kind: history.Added, Fields: fields})
		case pb == nil:
			out = append(out, history.EntityChange{ID: id, Type: pa.Type, Kind: history.Removed})
		default:
			fields, same := differ(a, b, id)
			if same {
				continue
			}
			out = append(out, history.EntityChange{ID: id, Type: pb.Type, Kind: history.ChangedKind, Fields: fields})
		}
	}
	return out, nil
}

// differ compares an entity both versions hold: the own fields whose
// values differ, and whether none does. When either version's schema
// can't type it, such as an old commit's that today's Provenance doesn't
// read, it differs when its source writes it differently, and the fields
// are the names whose values are written differently.
func differ(a, b *version, id string) ([]string, bool) {
	ta, tb := a.typed[id], b.typed[id]
	if ta == nil || tb == nil {
		if a.texts[id] == b.texts[id] && a.parsed[id].Type == b.parsed[id].Type {
			return nil, true
		}
		return writtenDiffer(a.parsed[id], b.parsed[id]), false
	}
	var fields []string
	if ta.Type != tb.Type {
		fields = append(fields, "type")
	}
	for _, f := range compare.Values(ta, tb) {
		if !f.Derived && !slices.Contains(fields, f.Name) {
			fields = append(fields, f.Name)
		}
	}
	return fields, len(fields) == 0
}

// set lists the own fields an added entity gives a value, and its body.
func set(e *model.Entity) []string {
	if e == nil {
		return nil
	}
	var out []string
	for _, v := range e.Fields {
		if v.Field.Kind == schema.Calculated || v.Blank() {
			continue
		}
		out = append(out, v.Field.Name)
	}
	if e.Body != "" {
		out = append(out, "body")
	}
	return out
}

// written lists the names an untyped entity writes values for, and its
// body.
func written(e *entity.Entity) []string {
	var out []string
	for _, k := range e.Record.Keys() {
		if k.Set && k.Name != "id" && k.Name != "type" {
			out = append(out, k.Name)
		}
	}
	if body, _ := e.Record.Body(); strings.TrimSpace(body) != "" {
		out = append(out, "body")
	}
	return out
}

// writtenDiffer lists the names whose values two untyped versions write
// differently, whitespace aside, and the body.
func writtenDiffer(a, b *entity.Entity) []string {
	text := func(e *entity.Entity, name string) string {
		w := e.Record.Value(&schema.Field{Name: name})
		if w == nil {
			return ""
		}
		return strings.Join(strings.Fields(w.SourceText()), " ")
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range []*entity.Entity{b, a} {
		for _, k := range e.Record.Keys() {
			if seen[k.Name] || k.Name == "id" || k.Name == "type" {
				continue
			}
			seen[k.Name] = true
			if text(a, k.Name) != text(b, k.Name) {
				out = append(out, k.Name)
			}
		}
	}
	if a.Type != b.Type {
		out = append([]string{"type"}, out...)
	}
	ba, _ := a.Record.Body()
	bb, _ := b.Record.Body()
	if strings.Join(strings.Fields(ba), " ") != strings.Join(strings.Fields(bb), " ") {
		out = append(out, "body")
	}
	return out
}

// read parses and types the entities of a version's paths.
func (v *values) read(ver history.Version) (*version, error) {
	out := &version{typed: map[string]*model.Entity{}, parsed: map[string]*entity.Entity{}, texts: map[string]string{}}
	if ver.FS == nil || len(ver.Paths) == 0 {
		return out, nil
	}
	s, err := v.schema(ver.FS)
	if err != nil {
		return nil, err
	}
	srcs := []Source{markdown{}}
	if s != nil {
		srcs = sources(s)
	}
	for _, p := range ver.Paths {
		data := func() ([]byte, error) { return fs.ReadFile(ver.FS, p) }
		for _, src := range srcs {
			ents, ok, err := src.Parse(p, data)
			if errors.Is(err, fs.ErrNotExist) {
				break
			}
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			texts, _, err := src.Entities(p, data)
			if err != nil {
				return nil, err
			}
			for _, fe := range texts {
				out.texts[fe.ID] = fe.Text
			}
			for _, e := range ents {
				if out.parsed[e.ID] != nil {
					continue // a duplicate ID: the first is the entity
				}
				out.parsed[e.ID] = e
				if s == nil || s.Types[e.Type] == nil {
					continue
				}
				typed, err := model.Build(s, []*entity.Entity{e})
				if err != nil {
					return nil, err
				}
				out.typed[e.ID] = typed[0]
			}
			break
		}
	}
	return out, nil
}

// schema is the schema a version's files declare; nil when it has none or
// it has problems, whose entities are then compared as written.
func (v *values) schema(fsys fs.FS) (*schema.Schema, error) {
	key, err := history.DirKey(fsys, "schema")
	if err != nil {
		return nil, err
	}
	if s, ok := v.schemas[key]; ok {
		return s, nil
	}
	s, problems, err := schema.ReadFS(fsys)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		s = nil
	}
	v.schemas[key] = s
	return s, nil
}
