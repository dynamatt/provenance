package website

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
)

const noteSchema = `type: Note
fields:
  - {name: title, type: string}
  - {name: summary, type: text}
  - {name: see, type: link, target: [Note], cardinality: many, reverse_name: seen_by}
`

// export builds a synthetic repository (edge cases only; the example repo is
// the main fixture) and exports it.
func exportRepo(t *testing.T, files map[string]string) (export.Files, error) {
	t.Helper()
	root := t.TempDir()
	files[".component"] = "code: T\nname: Test\n"
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := repo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := schema.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := entity.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	built, err := model.Build(s, parsed)
	if err != nil {
		t.Fatal(err)
	}
	return New().Export(&export.Input{Repo: r, Schema: s, Entities: built})
}

func note(id, front, body string) string {
	return "---\nid: " + id + "\ntype: Note\n" + front + "---\n" + body
}

func TestWikilinkReferences(t *testing.T) {
	files, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": noteSchema,
		"N/N-1.md":         note("N-1", "title: First <one>\n", "Body."),
		"N/N-2.md": note("N-2", "title: Second\nsee: [N-1]\nsummary: Points at [[N-1]].\n",
			"[[N-1]] | [[N-1|custom]] | [[N-1#title]] | [[N-1#seen_by]] | [[N-1#summary]] | [[N-1#nope]] | [[N-404]]\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/N-2.html"])
	for _, want := range []string{
		`<a class="ref" href="N-1.html" title="First &lt;one&gt;">N-1</a> |`,
		`>custom</a>`,
		`>First &lt;one&gt;</a>`, // #field uses the live value, escaped
		`>N-2</a>`,               // #facet lists the incoming IDs
		`>N-1#summary</a> <span class="unresolved">empty</span>`,
		`>N-1#nope</a> <span class="unresolved">no field &#34;nope&#34;</span>`,
		`<span class="id unresolved-id">N-404</span> <span class="unresolved">unresolved</span>`,
		`<div class="text"><p>Points at <a class="ref" href="N-1.html"`, // Markdown in text fields
	} {
		if !strings.Contains(page, want) {
			t.Errorf("N-2 page missing %s", want)
		}
	}
}

func TestEmbeds(t *testing.T) {
	files, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": noteSchema,
		"N/N-1.md":         note("N-1", "title: Inner\n", "Inner body with [[N-2]]."),
		"N/N-2.md":         note("N-2", "title: Outer\n", "Before.\n\n![[N-1]]\n\nInline ![[N-1]] here.\n\n![[N-404]]\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/N-2.html"])
	for _, want := range []string{
		`<section class="embed" data-entity="N-1">`,
		`<article class="entity" id="N-1">`,
		`Inner body with <a class="ref" href="N-2.html"`,
		`>N-1</a> <span class="unresolved">embed must be on its own line</span> here.`,
		`<p class="embed-missing"><span class="id unresolved-id">N-404</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("N-2 page missing %s", want)
		}
	}
}

func TestEmbedCycles(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"self": {map[string]string{
			"N/N-1.md": note("N-1", "", "![[N-1]]\n"),
		}, "N/N-1.md: embed cycle N-1 → N-1"},
		"indirect": {map[string]string{
			"N/N-1.md": note("N-1", "", "![[N-2]]\n"),
			"N/N-2.md": note("N-2", "", "![[N-3]]\n"),
			"N/N-3.md": note("N-3", "", "![[N-1]]\n"),
		}, "N/N-1.md: embed cycle N-1 → N-2 → N-3 → N-1"},
	} {
		t.Run(name, func(t *testing.T) {
			tc.files["schema/Note.yaml"] = noteSchema
			_, err := exportRepo(t, tc.files)
			var cycle *EmbedCycleError
			if !errors.As(err, &cycle) || err.Error() != tc.want {
				t.Errorf("err = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestSameEntityEmbeddedTwiceIsNotACycle(t *testing.T) {
	_, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": noteSchema,
		"N/N-1.md":         note("N-1", "", "leaf"),
		"N/N-2.md":         note("N-2", "", "![[N-1]]\n\n![[N-1]]\n"),
	})
	if err != nil {
		t.Errorf("err = %v", err)
	}
}
