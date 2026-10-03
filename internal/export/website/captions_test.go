package website

import (
	"maps"
	"strings"
	"testing"
)

var captionRepo = map[string]string{
	"schema/Note.yaml":         noteSchema,
	"schema/Fig.yaml":          "type: Fig\nfields:\n  - {name: title, type: string}\n",
	"schema/Tab.yaml":          "type: Tab\nfields:\n  - {name: title, type: string}\n",
	"templates/Fig.tmpl":       `<figure>{{markdown .Body}}<figcaption>{{.CaptionNumber}}: {{.Title}}</figcaption></figure>`,
	"templates/_captions.yaml": "Figure: [Fig]\nTable: [Tab]\n",
	"F/F-1.md":                 "---\nid: F-1\ntype: Fig\ntitle: Loop\n---\nLoop drawing.\n",
	"F/F-2.md":                 "---\nid: F-2\ntype: Fig\ntitle: Ceiling\n---\nCeiling drawing.\n",
	"F/F-3.md":                 "---\nid: F-3\ntype: Fig\ntitle: Unused\n---\nNot embedded.\n",
	"T/T-1.md":                 "---\nid: T-1\ntype: Tab\ntitle: Limits\n---\nA table.\n",
	// References come before, between and after the figures; F-3 is only
	// referenced.
	"N/N-1.md": note("N-1", "title: Doc\n", "See [[F-2]], [[F-1|the loop]], [[F-1#title]] and [[F-3]].\n\n![[F-2]]\n\n![[T-1]]\n\n![[F-1]]\n\n![[F-2]]\n\nAgain [[F-2]] and [[T-1]].\n"),
}

func TestCaptionNumbering(t *testing.T) {
	files, err := exportRepo(t, maps.Clone(captionRepo))
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/N-1.html"])
	for _, want := range []string{
		// Numbered in order of first embed, each sequence on its own.
		`<section class="embed" data-entity="F-2" id="caption-F-2"><figure><p>Ceiling drawing.</p>
<figcaption>Figure 1: Ceiling</figcaption></figure></section>`,
		`<section class="embed" data-entity="F-1" id="caption-F-1"><figure><p>Loop drawing.</p>
<figcaption>Figure 2: Loop</figcaption></figure></section>`,
		`<p class="caption"><span class="caption-number">Table 1</span> Limits</p>`, // the built-in template
		// References resolve to the numbers, even before the figure.
		`See <a class="ref xref" href="#caption-F-2" title="Ceiling">Figure 1</a>, <a class="ref xref" href="#caption-F-1" title="Loop">the loop</a>, <a class="ref" href="F-1.html" title="Loop">Loop</a> and <a class="ref" href="F-3.html" title="Unused">F-3</a>.`,
		`Again <a class="ref xref" href="#caption-F-2" title="Ceiling">Figure 1</a> and <a class="ref xref" href="#caption-T-1" title="Limits">Table 1</a>.`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("N-1 page missing %q\n%s", want, page)
		}
	}
	// The second embed of F-2 is not an anchor too.
	if strings.Count(page, `id="caption-F-2"`) != 1 {
		t.Error("F-2 anchored more than once")
	}
	// A figure's own page numbers it as the page's first figure.
	if own := string(files["entities/F-3.html"]); !strings.Contains(own, "<figcaption>Figure 1: Unused</figcaption>") {
		t.Errorf("F-3's own page:\n%s", own)
	}
	for name, content := range files {
		if strings.ContainsAny(string(content), tokOpen+tokSep+tokBody+tokClose) {
			t.Errorf("%s has an unresolved placeholder", name)
		}
	}
}

func TestCaptionsWithoutConfig(t *testing.T) {
	repo := maps.Clone(captionRepo)
	delete(repo, "templates/_captions.yaml")
	files, err := exportRepo(t, repo)
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/N-1.html"])
	if strings.Contains(page, "Figure 1") || !strings.Contains(page, `See <a class="ref" href="F-2.html" title="Ceiling">F-2</a>`) {
		t.Errorf("nothing should be numbered:\n%s", page)
	}
}

func TestCaptionConfigErrors(t *testing.T) {
	for config, want := range map[string]string{
		"Figure: [Fgi]\n":               `templates/_captions.yaml:1: Figure: unknown type "Fgi"`,
		"Figure: [Fig]\nPlate: [Fig]\n": "templates/_captions.yaml:2: Fig is already numbered as Figure",
		"Figure: Fig\n":                 "templates/_captions.yaml:1: Figure: expected a list of entity types",
		"- Fig\n":                       "templates/_captions.yaml:1: expected a mapping of sequence label to entity types",
	} {
		repo := maps.Clone(captionRepo)
		repo["templates/_captions.yaml"] = config
		_, err := exportRepo(t, repo)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: got %v, want %s", config, err, want)
		}
	}
}
