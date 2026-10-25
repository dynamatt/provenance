package load

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
)

func entityFile(id, typ string) string {
	return "---\nid: " + id + "\ntype: " + typ + "\n---\n"
}

// The Markdown source reads every .md file (any case) outside .git, the
// configuration folders at the root and the skipped folders; a file
// without frontmatter is not an entity. Its files are the entity files.
func TestMarkdownWalk(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"REQ/REQ-0002.md":           entityFile("REQ-0002", "Requirement"),
		"REQ/REQ-0001.md":           entityFile("REQ-0001", "Requirement"),
		"USR/deep/USR-0001.MD":      entityFile("USR-0001", "UserNeed"),
		"README.md":                 "# not an entity\n",
		"REQ/notes.txt":             entityFile("TXT-1", "Ignored"),
		"DOC/templates/DOC-0001.md": entityFile("DOC-0001", "Document"), // only skipped at the root
		".git/x.md":                 entityFile("GIT-1", "Ignored"),
		"sub/.git/x.md":             entityFile("GIT-2", "Ignored"),
		"_site/copy.md":             entityFile("REQ-0001", "Requirement"),
	}
	for _, d := range entity.ConfigDirs {
		files[d+"/x.md"] = entityFile("CFG-"+d, "Ignored")
	}
	write(t, root, files)
	l, err := markdown{}.Load(root, []string{filepath.Join(root, "_site")})
	if err != nil || len(l.Problems) != 0 {
		t.Fatal(err, l.Problems)
	}
	var ids []string
	for _, e := range l.Entities {
		ids = append(ids, e.ID+"="+e.Path)
	}
	want := "DOC-0001=DOC/templates/DOC-0001.md REQ-0001=REQ/REQ-0001.md REQ-0002=REQ/REQ-0002.md USR-0001=USR/deep/USR-0001.MD"
	if got := strings.Join(slices.Sorted(slices.Values(ids)), " "); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := strings.Join(newContent(l.Files, nil).files, " "); got != "DOC/templates/DOC-0001.md REQ/REQ-0001.md REQ/REQ-0002.md USR/deep/USR-0001.MD" {
		t.Errorf("files %s", got)
	}
}

// Every problem is collected in walk order; every copy of a duplicated ID
// is reported where the walk first found the duplicate, and only the
// first copy is an entity. Files with problems are content files too.
func TestMarkdownProblems(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"A/bad.md":        "---\nid: 1\ntype: Requirement\n---\n",
		"REQ/REQ-0001.md": entityFile("REQ-0001", "Requirement"),
		"REQ/copy1.md":    entityFile("REQ-0001", "Requirement"),
		"REQ/copy2.md":    entityFile("REQ-0001", "Requirement"),
		"REQ/half.md":     "---\ntitle: x\ntype: Requirement\n---\n",
		"Z/bad.md":        "---\nid: [\n---\n",
	})
	l, err := markdown{}.Load(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range l.Problems {
		line := p.Check + " " + p.Path + ": " + p.Msg
		if p.Err != nil {
			line += " | " + p.Err.Error()
		}
		got = append(got, line)
	}
	want := []string{
		"content/frontmatter A/bad.md: id must be a non-empty string | A/bad.md:2: id must be a non-empty string",
		"content/duplicate-id REQ/REQ-0001.md: duplicate entity ID REQ-0001, also in REQ/copy1.md, REQ/copy2.md | duplicate entity ID REQ-0001 in REQ/REQ-0001.md and REQ/copy1.md",
		"content/duplicate-id REQ/copy1.md: duplicate entity ID REQ-0001, also in REQ/REQ-0001.md, REQ/copy2.md",
		"content/duplicate-id REQ/copy2.md: duplicate entity ID REQ-0001, also in REQ/REQ-0001.md, REQ/copy1.md",
		"content/frontmatter Z/bad.md: invalid frontmatter: did not find expected node content | Z/bad.md:2: invalid frontmatter: did not find expected node content",
		"content/frontmatter REQ/half.md: frontmatter has a type but no id, so the file is not read as an entity",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(l.Entities) != 1 || l.Entities[0].Path != "REQ/REQ-0001.md" {
		t.Errorf("entities = %v", l.Entities)
	}
	if len(l.Files) != 6 {
		t.Errorf("files = %v", l.Files)
	}
}

// Types reads a .md file's bytes only, and names its entity's type.
func TestMarkdownTypes(t *testing.T) {
	read := func(s string) func() ([]byte, error) { return func() ([]byte, error) { return []byte(s), nil } }
	if ts, ok, err := (markdown{}).Types("R/r.md", read(entityFile("R-1", "Requirement"))); !ok || err != nil || strings.Join(ts, ",") != "Requirement" {
		t.Errorf("entity file: %v %v %v", ts, ok, err)
	}
	if ts, ok, _ := (markdown{}).Types("README.md", read("# hi\n")); !ok || ts != nil {
		t.Errorf("not an entity: %v %v", ts, ok)
	}
	called := false
	if _, ok, _ := (markdown{}).Types("schema/x.yaml", func() ([]byte, error) { called = true; return nil, nil }); ok || called {
		t.Errorf("other file: ok %v, read %v", ok, called)
	}
}
