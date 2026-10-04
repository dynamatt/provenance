package website

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

var scopeRepo = map[string]string{
	"schema/Note.yaml": rankedNoteSchema,
	"templates/Note.tmpl": `<article>{{.Title}}{{range .See}} {{link .}} [{{link . "named"}}|{{href .}}]{{end}}
{{markdown .Body}}</article>`,
	"N/N-1.md":           note("N-1", "title: Doc\nsee: [N-4]\n", "Intro [[N-4]], [[N-4|the fourth]], [[N-4#title]].\n\n![[N-2]]\n\n```query\nfrom: Note\nwhere: {field: rank, operator: exists}\nrender: id\n```\n"),
	"N/N-2.md":           note("N-2", "title: Embedded\n", "Embedded body."),
	"N/N-3.md":           note("N-3", "title: Ranked\nrank: 1\n", "Ranked."),
	"N/N-4.md":           note("N-4", "title: Elsewhere\n", "Not pulled in."),
	"scopes/ranked.yaml": "from: Note\nwhere: {field: rank, operator: exists}\n",
}

func pages(files map[string][]byte) []string {
	return slices.Sorted(maps.Keys(files))
}

func TestEntityScope(t *testing.T) {
	files, err := exportScoped(t, maps.Clone(scopeRepo), "N/N-1.md")
	if err != nil {
		t.Fatal(err)
	}
	// N-1 plus what it pulls in: the embed and the query's result.
	if got := strings.Join(pages(files), " "); got != "entities/N-1.html entities/N-2.html entities/N-3.html index.html style.css" {
		t.Errorf("pages: %s", got)
	}
	index := string(files["index.html"])
	for _, want := range []string{
		"<title>N-1 Doc", // the scoped entity is the main page
		`<section class="embed" data-entity="N-2">`,
		`<li><a class="ref" href="entities/N-3.html" title="Ranked">N-3</a></li>`, // links from the main page reach entities/
		// Outside the scope: the same text, unlinked.
		`Intro <span class="ref out-of-scope">N-4</span>, <span class="ref out-of-scope">the fourth</span>, <span class="ref out-of-scope">Elsewhere</span>.`,
		`<span class="ref out-of-scope">N-4</span> [<span class="ref out-of-scope">named</span>|]`, // link and href in a project template
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index missing %q\n%s", want, index)
		}
	}
	// The entity's own page links relative to entities/.
	if !strings.Contains(string(files["entities/N-1.html"]), `href="N-3.html"`) {
		t.Error("N-1's page does not link N-3 relative to entities/")
	}
}

func TestQueryScope(t *testing.T) {
	files, err := exportScoped(t, maps.Clone(scopeRepo), "scopes/ranked.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(pages(files), " "); got != "entities/N-3.html index.html style.css" {
		t.Errorf("pages: %s", got)
	}
	if index := string(files["index.html"]); !strings.Contains(index, `entities/N-3.html`) || strings.Contains(index, "N-1") {
		t.Errorf("index should list only N-3:\n%s", index)
	}
}

func TestBuiltInTemplateOutOfScope(t *testing.T) {
	repo := maps.Clone(scopeRepo)
	delete(repo, "templates/Note.tmpl")
	files, err := exportScoped(t, repo, "N/N-1.md")
	if err != nil {
		t.Fatal(err)
	}
	if index := string(files["index.html"]); !strings.Contains(index, `<tr><th>See</th><td><span class="id out-of-scope">N-4</span></td></tr>`) {
		t.Errorf("built-in link field not plain out of scope:\n%s", index)
	}
}

func TestScopeErrors(t *testing.T) {
	for scope, want := range map[string]string{
		"nope.yaml":        "--scope nope.yaml: no such file",
		"N/../../x":        "--scope N/../../x: is outside the repository",
		"schema/Note.yaml": "--scope schema/Note.yaml:1: unknown key \"type\" in a scope query file (expected from, where)",
	} {
		_, err := exportScoped(t, maps.Clone(scopeRepo), scope)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%s: got %v, want %s…", scope, err, want)
		}
	}
}
