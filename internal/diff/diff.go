// Package diff compares the design history file at two refs (DES-0055):
// entities matched by ID and compared by their typed values, so a change
// of formatting alone is none; derived values, calculated fields and
// query-field results, compared too and marked; and the files that are not
// entities listed by path. The rendered diff of the pages (DES-0056) is in
// pages.go; the outputs (DES-0057) in output.go.
package diff

import (
	"cmp"
	"errors"
	htmltemplate "html/template"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/problem"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
)

// Retyped is an entity whose ID the two sides give different types.
const Retyped Op = "retyped"

// Diff is what changed from the old side to the new.
type Diff struct {
	Old, New *Side
	// Entities are the entities that differ, by type name, then ID.
	Entities []*Entity
	// Files are the files that are not entities and differ, by path.
	Files []*File
	// Pages are the rendered pages that differ, when they were compared
	// (Render); nil otherwise.
	Pages []*Page
	// NotRendered says why the pages were not compared, when Render was
	// asked to and could not.
	NotRendered string
	// Rendered is set once Render has compared the pages.
	Rendered bool
	// Version is the binary's, for the html output's header.
	Version string
	// WorkingTree is the working tree's files, where a template the new
	// side lacks is read from; nil for none.
	WorkingTree fs.FS
	// Style is the new side's site stylesheet, once Render has exported
	// it, so the html output's pages look as the site's do.
	Style htmltemplate.CSS

	scope map[string]bool
}

// Side is one side of the comparison.
type Side struct {
	// Ref is the revision as given; "" for the working tree.
	Ref string
	// Commit is its full commit hash; "" for the working tree.
	Commit string
	// ContentHash is the DHF content hash (DES-0022) of the commit, or of
	// HEAD suffixed -dirty for a working tree that differs from it; ""
	// outside a git repository.
	ContentHash string
	// Problems are the problems loading the side: it is compared as far as
	// it loaded (DES-0055).
	Problems []problem.Problem

	res *load.Result
}

// Name is how the side is named: its ref and short commit, or the working
// tree.
func (s *Side) Name() string {
	switch {
	case s.Commit == "":
		return "working tree"
	case s.Ref == "" || strings.HasPrefix(s.Commit, s.Ref):
		return history.Short(s.Commit)
	default:
		return s.Ref + " (" + history.Short(s.Commit) + ")"
	}
}

// Files is what the side's files are read from.
func (s *Side) Files() fs.FS { return s.res.Repo.Files() }

// Repo is the side's repository.
func (s *Side) Repo() *repo.Repo { return s.res.Repo }

// NewSide is a loaded side.
func NewSide(ref, commit, contentHash string, res *load.Result) *Side {
	return &Side{Ref: ref, Commit: commit, ContentHash: contentHash, Problems: res.Problems, res: res}
}

// Entity is an entity that differs.
type Entity struct {
	ID string
	// Type is the entity's type on the new side, or on the old side for a
	// removed entity; OldType is its type on the old side, when retyped.
	Type, OldType string
	Op            Op
	// Title is the entity's title, as the new side has it when it has the
	// entity.
	Title string
	// Path is the file holding it on the new side, OldPath on the old;
	// moving the file is no change.
	Path, OldPath string
	// Fields are the fields that differ, in the new side's schema order,
	// then those only the old side's schema has; the body, when it is no
	// field, is last, as "body". Empty for an added or removed entity, and
	// when a side's schema couldn't be read.
	Fields []*Field
	// Derived is set when every difference is a derived value's: the
	// entity changed only because something it is derived from did.
	Derived bool
}

// File is a file that is not an entity: .component, a file under schema/,
// rules/, templates/, assets/ or .signatures/, or an image an entity
// shows. Images are compared by their git object hash, never held.
type File struct {
	Path string
	Op   Op
}

// Changed reports whether anything differs.
func (d *Diff) Changed() bool {
	return len(d.Entities) > 0 || len(d.Files) > 0 || len(d.Pages) > 0
}

// Count is how many entities differ by op: added, removed, retyped,
// changed, and derived for those changed only in derived values.
func (d *Diff) Count(op string) int {
	n := 0
	for _, e := range d.Entities {
		switch {
		case op == "derived" && e.Op == Changed && e.Derived,
			op == string(Changed) && e.Op == Changed && !e.Derived,
			op != "derived" && op != string(Changed) && string(e.Op) == op:
			n++
		}
	}
	return n
}

// Compare compares old with new. scope, when not nil, limits the
// entities compared to its IDs (DES-0033).
func Compare(old, new *Side, scope map[string]bool) (*Diff, error) {
	d := &Diff{Old: old, New: new, scope: scope}
	a, b := entities(old.res), entities(new.res)
	ids := map[string]bool{}
	for id := range a {
		ids[id] = true
	}
	for id := range b {
		ids[id] = true
	}
	for _, id := range slices.Sorted(func(yield func(string) bool) {
		for id := range ids {
			if !yield(id) {
				return
			}
		}
	}) {
		if scope != nil && !scope[id] {
			continue
		}
		e, err := compareEntity(old, new, a[id], b[id])
		if err != nil {
			return nil, err
		}
		if e != nil {
			d.Entities = append(d.Entities, e)
		}
	}
	slices.SortStableFunc(d.Entities, func(x, y *Entity) int {
		return cmp.Or(strings.Compare(x.Type, y.Type), strings.Compare(x.ID, y.ID))
	})
	files, err := compareFiles(old, new)
	if err != nil {
		return nil, err
	}
	d.Files = files
	return d, nil
}

// side is an entity as one side has it: typed when the side's schema
// loaded.
type side struct {
	parsed *entity.Entity
	typed  *model.Entity
}

func entities(r *load.Result) map[string]side {
	out := map[string]side{}
	for _, e := range r.Parsed {
		out[e.ID] = side{parsed: e}
	}
	for _, e := range r.Entities {
		out[e.ID] = side{parsed: e.Entity, typed: e}
	}
	return out
}

func compareEntity(old, new *Side, a, b side) (*Entity, error) {
	switch {
	case a.parsed == nil:
		return &Entity{ID: b.parsed.ID, Type: b.parsed.Type, Op: Added, Title: title(b), Path: b.parsed.Path}, nil
	case b.parsed == nil:
		return &Entity{ID: a.parsed.ID, Type: a.parsed.Type, Op: Removed, Title: title(a), OldPath: a.parsed.Path}, nil
	}
	e := &Entity{ID: b.parsed.ID, Type: b.parsed.Type, Op: Changed, Title: title(b), Path: b.parsed.Path, OldPath: a.parsed.Path}
	if a.parsed.Type != b.parsed.Type {
		e.Op, e.OldType = Retyped, a.parsed.Type
	}
	if a.typed == nil || b.typed == nil {
		// A side whose schema didn't load has no typed values: the entity
		// differs when the text its source writes it as does.
		same, err := sameText(old, new, a.parsed, b.parsed)
		if err != nil || (same && e.Op == Changed) {
			return nil, err
		}
		return e, nil
	}
	e.Fields = compareValues(a.typed, b.typed)
	if len(e.Fields) == 0 && e.Op == Changed {
		return nil, nil
	}
	e.Derived = e.Op == Changed
	for _, f := range e.Fields {
		e.Derived = e.Derived && f.Derived
	}
	return e, nil
}

func title(s side) string {
	if s.typed != nil {
		return s.typed.Title()
	}
	return ""
}

// sameText compares two entities as their sources write them.
func sameText(old, new *Side, a, b *entity.Entity) (bool, error) {
	text := func(s *Side, e *entity.Entity) (string, error) {
		fes, err := s.res.Content().Entities(e.Path, func() ([]byte, error) { return fs.ReadFile(s.Files(), e.Path) })
		if err != nil {
			return "", err
		}
		for _, fe := range fes {
			if fe.ID == e.ID {
				return strings.ReplaceAll(fe.Text, "\r\n", "\n"), nil
			}
		}
		return "", nil
	}
	ta, err := text(old, a)
	if err != nil {
		return false, err
	}
	tb, err := text(new, b)
	return ta == tb, err
}

// compareFiles compares the files that are not entities: .component,
// every file under the configuration folders, and the images entities
// show. A text file is compared with its line endings as LF, an image by
// its git object hash.
func compareFiles(old, new *Side) ([]*File, error) {
	pa, err := otherFiles(old)
	if err != nil {
		return nil, err
	}
	pb, err := otherFiles(new)
	if err != nil {
		return nil, err
	}
	var out []*File
	for _, p := range slices.Sorted(func(yield func(string) bool) {
		for p := range pa {
			if !yield(p) {
				return
			}
		}
		for p := range pb {
			if !pa[p] && !yield(p) {
				return
			}
		}
	}) {
		switch {
		case !pa[p]:
			out = append(out, &File{Path: p, Op: Added})
		case !pb[p]:
			out = append(out, &File{Path: p, Op: Removed})
		default:
			ka, err := fileKey(old.Files(), p)
			if err != nil {
				return nil, err
			}
			kb, err := fileKey(new.Files(), p)
			if err != nil {
				return nil, err
			}
			if ka != kb {
				out = append(out, &File{Path: p, Op: Changed})
			}
		}
	}
	return out, nil
}

// otherFiles lists a side's files that are not entities.
func otherFiles(s *Side) (map[string]bool, error) {
	fsys := s.Files()
	content, err := s.res.Content().Files()
	if err != nil {
		return nil, err
	}
	isContent := map[string]bool{}
	for _, p := range content {
		isContent[p] = true
	}
	out := map[string]bool{}
	if _, err := fs.Stat(fsys, repo.ComponentFile); err == nil {
		out[repo.ComponentFile] = true
	}
	for _, dir := range entity.ConfigDirs {
		err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == dir && errors.Is(err, fs.ErrNotExist) {
					return fs.SkipDir
				}
				return err
			}
			if !d.IsDir() && !isContent[p] {
				out[p] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, e := range s.res.Entities {
		for _, text := range texts(e) {
			refs, err := e.Record.Refs(text)
			if err != nil || refs == nil {
				continue
			}
			for _, a := range refs.Assets {
				if _, err := fs.Stat(fsys, a); err == nil && !isContent[a] {
					out[a] = true
				}
			}
		}
	}
	return out, nil
}

// texts are an entity's Markdown: its body and text fields.
func texts(e *model.Entity) []string {
	out := []string{e.BodyText()}
	for _, v := range e.Fields {
		if v.Present && !v.Invalid && v.Field.Kind == schema.Text && v.Field != e.Schema.BodyField {
			out = append(out, v.Str)
		}
	}
	return out
}

// images are compared by their git object hash: read from the commit's
// tree without reading the file.
var images = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".ico": true, ".pdf": true}

func fileKey(fsys fs.FS, p string) (string, error) {
	if images[strings.ToLower(path.Ext(p))] {
		return history.BlobHash(fsys, p)
	}
	b, err := fs.ReadFile(fsys, p)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n"), nil
}

// Graph is the side's evaluated graph; nil when its schema didn't load.
func (s *Side) Graph() *query.Graph { return s.res.Graph }
