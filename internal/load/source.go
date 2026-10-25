package load

import (
	"slices"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/schema"
)

// Source turns files into entities (G3). The loader runs every source
// after the schema, which a source backed by files the schema declares
// needs; a source that doesn't need it, such as Markdown, also runs when
// the schema has problems.
type Source interface {
	// Load reads the source's files under root, leaving out the folders in
	// skip (absolute paths).
	Load(root string, skip []string) (*Loaded, error)
	// Entities lists the entities path holds, each with the text it is
	// written as, for history to compare two versions of the file (G5).
	// ok is false when path is not one of the source's files, and data,
	// which reads its bytes, is then not called. A file that does not
	// parse holds no entities.
	Entities(path string, data func() ([]byte, error)) (entities []history.FileEntity, ok bool, err error)
}

// Loaded is what a source read.
type Loaded struct {
	// Entities are every entity the source read, one per ID: a second
	// with the same ID is a problem.
	Entities []*entity.Entity
	// Problems are in the order the source found them.
	Problems []Problem
	// Files are the files it read that hold entities, or would but for a
	// problem: the content files a change to which marks an export dirty.
	Files []string
}

// sources are the sources of a repository with schema s, Markdown first.
// testSources are added by tests (UseTestSource).
func sources(s *schema.Schema) []Source {
	return append([]Source{markdown{}}, testSources...)
}

var testSources []Source

// content is history.Content from the sources and the files they read.
type content struct {
	files   []string
	sources []Source
}

func (c *content) Files() ([]string, error) { return c.files, nil }

func (c *content) Entities(path string, data func() ([]byte, error)) ([]history.FileEntity, error) {
	for _, src := range c.sources {
		if entities, ok, err := src.Entities(path, data); ok || err != nil {
			return entities, err
		}
	}
	return nil, nil
}

var _ history.Content = (*content)(nil)

// newContent sorts and de-duplicates files.
func newContent(files []string, srcs []Source) *content {
	files = slices.Clone(files)
	slices.Sort(files)
	return &content{files: slices.Compact(files), sources: srcs}
}

// UseTestSource adds src to the sources of every load until the test ends,
// so a test can show that a layer handles entities that are not Markdown
// (G6). Tests that use it must not run in parallel.
func UseTestSource(tb testing.TB, src Source) {
	tb.Helper()
	prev := testSources
	testSources = append(slices.Clone(prev), src)
	tb.Cleanup(func() { testSources = prev })
}
