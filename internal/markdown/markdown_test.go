package markdown

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yuin/goldmark/util"
)

// fake records every wikilink as a readable token.
type fake struct{ err error }

func (f fake) Reference(w util.BufWriter, l Link) error {
	if l.Local != "" {
		fmt.Fprintf(w, "{local %s|%s}", l.Local, l.Label)
		return nil
	}
	fmt.Fprintf(w, "{ref %s|%s#%s}", l.ID, l.Label, l.Field)
	return nil
}

func (f fake) CaptionStart(w util.BufWriter, c Caption) error {
	fmt.Fprintf(w, "{caption %s %s line %d}\n", c.Kind, c.ID, c.Line)
	return nil
}

func (f fake) CaptionEnd(w util.BufWriter, c Caption) error {
	fmt.Fprintf(w, "{/caption %q}\n", c.Text)
	return nil
}

func (f fake) Embed(w util.BufWriter, l Link) error {
	fmt.Fprintf(w, "{embed %s}\n", l.ID)
	return f.err
}

func (f fake) MisplacedEmbed(w util.BufWriter, l Link) error {
	fmt.Fprintf(w, "{misplaced %s}", l.ID)
	return nil
}

// fences records each handled block as a readable token.
var fences = map[string]FenceFunc{
	"query": func(w util.BufWriter, f Fence) error {
		fmt.Fprintf(w, "{%s line %d: %q}\n", f.Lang, f.Line, f.Source)
		return nil
	},
}

func convert(t *testing.T, src string) string {
	t.Helper()
	out, err := Convert(src, fake{}, Options{Fences: fences})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWikilinkForms(t *testing.T) {
	got := convert(t, "See [[REQ-0001]], [[REQ-0002|the ceiling]] and [[REQ-0002#title]].")
	want := "<p>See {ref REQ-0001|#}, {ref REQ-0002|the ceiling#} and {ref REQ-0002|#title}.</p>\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEmbedOnItsOwnLineIsABlock(t *testing.T) {
	got := convert(t, "Intro:\n\n![[REQ-0003]]\n\nAfter.")
	want := "<p>Intro:</p>\n{embed REQ-0003}\n<p>After.</p>\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEmbedInsideTextIsMisplaced(t *testing.T) {
	got := convert(t, "Inline ![[REQ-0003]] here.")
	if got != "<p>Inline {misplaced REQ-0003} here.</p>\n" {
		t.Errorf("got %q", got)
	}
}

func TestNotWikilinks(t *testing.T) {
	for src, want := range map[string]string{
		"`[[REQ-0001]]` in code":          "<p><code>[[REQ-0001]]</code> in code</p>\n",
		"```\n[[REQ-0001]]\n```":          "<pre><code>[[REQ-0001]]\n</code></pre>\n",
		"[[]] empty":                      "<p>[[]] empty</p>\n",
		"[[unclosed":                      "<p>[[unclosed</p>\n",
		"[a normal](link.html)":           "<p><a href=\"link.html\">a normal</a></p>\n",
		"![an image](x.png)":              "<p><img src=\"x.png\" alt=\"an image\"></p>\n",
		"<div class=\"raw\">html</div>\n": "<div class=\"raw\">html</div>\n",
	} {
		if got := convert(t, src); got != want {
			t.Errorf("%q:\n got  %q\n want %q", src, got, want)
		}
	}
}

func TestTables(t *testing.T) {
	got := convert(t, "| a | b |\n|---|---|\n| 1 | [[X-1]] |\n")
	if !strings.Contains(got, "<table>") || !strings.Contains(got, "<td>{ref X-1|#}</td>") {
		t.Errorf("got %q", got)
	}
}

func TestEmbedErrorStopsConversion(t *testing.T) {
	boom := errors.New("cycle")
	_, err := Convert("![[DOC-1]]", fake{err: boom}, Options{})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestFencesGoToTheirHandler(t *testing.T) {
	src := "# Requirements\n\nAll approved:\n\n```query\nfrom: Requirement\norder_by: order\n```\n\n```yaml\nfrom: not a query\n```\n\n- item\n\n  ```query\n  from: Design\n  ```\n"
	got := convert(t, src)
	for _, want := range []string{
		"{query line 6: \"from: Requirement\\norder_by: order\\n\"}",
		"<pre><code class=\"language-yaml\">from: not a query\n</code></pre>",
		"{query line 17: \"from: Design\\n\"}", // indentation inside the list item is removed
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
}

func TestUnhandledFencesAreCode(t *testing.T) {
	got := convert(t, "```mermaid\ngraph TD\n```\n\n```\nplain\n```\n")
	want := "<pre><code class=\"language-mermaid\">graph TD\n</code></pre>\n<pre><code>plain\n</code></pre>\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestCaptions(t *testing.T) {
	src := "![Loop](loop.svg)\n\n```caption\nkind: figure\nid: loop\ntext: The *loop*.\n```\n\n| a |\n| - |\n| 1 |\n\n```caption\nkind: table\n```\n\nSee [[#loop]] and [[#loop|it]].\n"
	got := convert(t, src)
	for _, want := range []string{
		"{caption figure loop line 4}\n<p><img src=\"loop.svg\" alt=\"Loop\"></p>\n{/caption \"The *loop*.\"}",
		"{caption table  line 14}\n<table>",
		"</table>\n{/caption \"\"}",
		"See {local loop|} and {local loop|it}.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestCaptionErrors(t *testing.T) {
	for src, want := range map[string]string{
		"Text.\n\n```caption\nkind: figure\n```\n":                      "line 3: a caption must come right after the figure, table, equation or embed it captions",
		"```caption\nkind: figure\n```\n":                               "line 1: a caption must come right after",
		"![x](x.png)\n\n```caption\nid: x\n```\n":                       "line 4: a caption needs a kind",
		"![x](x.png)\n\n```caption\nkind: figure\nnumber: 2\n```\n":     `line 5: unknown key "number" in a caption`,
		"![x](x.png)\n\n```caption\nkind: figure\nid: has space\n```\n": `line 5: caption id "has space"`,
		"![x](x.png)\n\n```caption\nkind: figure\nid: x: y\n```\n":      "line 5: caption: mapping values are not allowed in this context",
		"![x](x.png)\n\n```caption\n- kind\n```\n":                      "line 4: a caption is a mapping",
	} {
		_, err := Convert(src, fake{}, Options{})
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: got %v, want %s…", src, err, want)
		}
	}
}

func TestImageRewrite(t *testing.T) {
	opts := Options{Image: func(dest string) (string, error) {
		if dest == "bad.png" {
			return "", errors.New("no such image")
		}
		return "site/" + dest, nil
	}}
	got, err := Convert("Text ![a](a.png) and [![b](b.png)](x).\n", fake{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `src="site/a.png"`) || !strings.Contains(got, `src="site/b.png"`) {
		t.Errorf("not rewritten: %s", got)
	}
	_, err = Convert("Intro.\n\nSee ![x](bad.png).\n", fake{}, opts)
	if err == nil || err.Error() != "line 3: no such image" {
		t.Errorf("err = %v", err)
	}
}
