package website

import (
	"maps"
	"strings"
	"testing"
)

const captionDoc = "See [[#loop]], [[#loop|the loop]] and [[#limits]]; [[#nothing]] is not here.\n\n" +
	"![Loop](../assets/loop.svg)\n\n```caption\nkind: figure\nid: loop\ntext: The *control* loop.\n```\n\n" +
	"| Limit | mA |\n| - | - |\n| Ceiling | 8 |\n\n```caption\nkind: table\nid: limits\ntext: Limits.\n```\n\n" +
	"![[N-2]]\n\n```caption\nkind: figure\n```\n\n" +
	"<p class=\"math\">E = IR</p>\n\n```caption\nkind: equation\nid: ohm\n```\n\n" +
	"After: [[#loop]] and [[#ohm]].\n"

var captionRepo = map[string]string{
	"schema/Note.yaml": noteSchema,
	"assets/loop.svg":  "<svg/>",
	"N/N-1.md":         note("N-1", "title: Doc\n", captionDoc),
	"N/N-2.md":         note("N-2", "title: Inner\n", "Inner body."),
}

func TestCaptionBlocks(t *testing.T) {
	files, err := exportRepo(t, maps.Clone(captionRepo))
	if err != nil {
		t.Fatal(err)
	}
	page := string(files["entities/N-1.html"])
	for _, want := range []string{
		// References resolve to numbers, even before the caption.
		`See <a class="ref xref" href="#caption-loop">Figure 1</a>, <a class="ref xref" href="#caption-loop">the loop</a> and <a class="ref xref" href="#caption-limits">Table 1</a>;`,
		`<span class="id unresolved-id">#nothing</span> <span class="unresolved">no caption #nothing</span> is not here.`,
		`After: <a class="ref xref" href="#caption-loop">Figure 1</a> and <a class="ref xref" href="#caption-ohm">Equation 1</a>.`,
		// An equation (raw HTML here): its own sequence, caption below.
		"<figure class=\"captioned captioned-equation\" id=\"caption-ohm\">\n<p class=\"math\">E = IR</p>\n<figcaption><span class=\"caption-number\">Equation 1</span></figcaption>\n</figure>",
		// A figure: the image copied into the site, the caption below.
		"<figure class=\"captioned captioned-figure\" id=\"caption-loop\">\n<p><img src=\"../assets/loop.svg\" alt=\"Loop\"></p>\n<figcaption><span class=\"caption-number\">Figure 1</span> The <em>control</em> loop.</figcaption>\n</figure>",
		// A table: caption above.
		"<figure class=\"captioned captioned-table\" id=\"caption-limits\">\n<figcaption><span class=\"caption-number\">Table 1</span> Limits.</figcaption>\n<table>",
		// A figure captioning an embed.
		"<figure class=\"captioned captioned-figure\">\n<section class=\"embed\" data-entity=\"N-2\">",
		`<figcaption><span class="caption-number">Figure 2</span></figcaption>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("N-1 page missing %q\n%s", want, page)
		}
	}
	if string(files["assets/loop.svg"]) != "<svg/>" {
		t.Error("the image was not copied into the site")
	}
	for name, content := range files {
		if strings.ContainsAny(string(content), tokOpen+tokSep+tokBody+tokClose) {
			t.Errorf("%s has an unresolved placeholder", name)
		}
	}
}

func TestCaptionsInEmbeddedEntities(t *testing.T) {
	// Captions from embedded entities number in the host's sequence, in
	// document order.
	files, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": noteSchema,
		"assets/a.png":     "png",
		"N/N-1.md":         note("N-1", "title: Host\n", "![a](../assets/a.png)\n\n```caption\nkind: figure\n```\n\n![[N-2]]\n"),
		"N/N-2.md":         note("N-2", "title: Inner\n", "![a](../assets/a.png)\n\n```caption\nkind: figure\nid: inner\n```\n\nThat is [[#inner]].\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	host := string(files["entities/N-1.html"])
	if !strings.Contains(host, "Figure 2</span>") || !strings.Contains(host, `That is <a class="ref xref" href="#caption-inner">Figure 2</a>.`) {
		t.Errorf("embedded caption not numbered after the host's:\n%s", host)
	}
	if inner := string(files["entities/N-2.html"]); !strings.Contains(inner, `That is <a class="ref xref" href="#caption-inner">Figure 1</a>.`) {
		t.Errorf("N-2's own page:\n%s", inner)
	}
}

func TestCaptionAndImageErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  string
	}{
		"unknown kind": {map[string]string{"N/N-1.md": note("N-1", "", "![a](../assets/loop.svg)\n\n```caption\nkind: chart\n```\n")},
			`N/N-1.md:8: unknown caption kind "chart" (kinds: equation, figure, table)`},
		"nothing to caption": {map[string]string{"N/N-1.md": note("N-1", "", "Just text.\n\n```caption\nkind: figure\n```\n")},
			"N/N-1.md:7: a caption must come right after the figure, table, equation or embed it captions"},
		"missing image": {map[string]string{"N/N-1.md": note("N-1", "", "Intro.\n\n![a](../assets/gone.png)\n")},
			"N/N-1.md:7: image assets/gone.png does not exist"},
		"unsupported image": {map[string]string{"assets/notes.pdf": "pdf", "N/N-1.md": note("N-1", "", "Intro.\n\n![a](../assets/notes.pdf)\n")},
			"N/N-1.md:7: image assets/notes.pdf: the website export does not support this format (supported: .gif, .jpeg, .jpg, .png, .svg, .webp)"},
		"remote image": {map[string]string{"N/N-1.md": note("N-1", "", "![a](https://example.com/a.png)\n")},
			"N/N-1.md:5: image https://example.com/a.png: images must be files in the repository"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := maps.Clone(captionRepo)
			maps.Copy(repo, tc.files)
			_, err := exportRepo(t, repo)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
		})
	}
}

func TestImagesOnTheMainPage(t *testing.T) {
	// With an entity scope the entity is the main page, at the site root:
	// its images are linked without ../.
	files, err := exportScoped(t, maps.Clone(captionRepo), "N/N-1.md")
	if err != nil {
		t.Fatal(err)
	}
	if index := string(files["index.html"]); !strings.Contains(index, `<img src="assets/loop.svg" alt="Loop">`) {
		t.Errorf("main page image:\n%s", index)
	}
}
