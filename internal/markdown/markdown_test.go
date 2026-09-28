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
	fmt.Fprintf(w, "{ref %s|%s#%s}", l.ID, l.Label, l.Field)
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

func convert(t *testing.T, src string) string {
	t.Helper()
	out, err := Convert(src, fake{})
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
	_, err := Convert("![[DOC-1]]", fake{err: boom})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}
