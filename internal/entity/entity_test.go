package entity

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/schema"
)

const req = "---\nid: REQ-0001\ntype: Requirement\ntitle: Closed-loop\n---\nThe system shall.\n"

func TestParseEntity(t *testing.T) {
	e, err := Parse("REQ/REQ-0001.md", []byte(req))
	if err != nil || e == nil {
		t.Fatalf("Parse: %v, %v", e, err)
	}
	if e.ID != "REQ-0001" || e.Type != "Requirement" || e.Path != "REQ/REQ-0001.md" {
		t.Errorf("got %+v", e)
	}
	if body, line := e.Record.Body(); body != "The system shall.\n" || line != 6 {
		t.Errorf("Body = %q at line %d", body, line)
	}
	if w := e.Record.Value(&schema.Field{Name: "title"}); w == nil || w.Kind != Text || w.Text != "Closed-loop" || w.Line != 4 {
		t.Errorf("title = %+v, want text at file line 4", w)
	}
	if e.IDLine() != 2 || e.Record.TypeLine() != 3 {
		t.Errorf("id line %d, type line %d", e.IDLine(), e.Record.TypeLine())
	}
}

// A YAML value is read as its tag says, with its position: a block
// scalar's text starts on the line after its | or >.
func TestMarkdownValues(t *testing.T) {
	e, err := Parse("a.md", []byte("---\nid: A\ntype: T\nn: 1_000\nb: True\nd: 2026-01-02\nnone: null\nl: [x, 2]\ntext: |\n  one\n  two\nrows:\n  - {a: 1}\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	value := func(name string) *Written { return e.Record.Value(&schema.Field{Name: name}) }
	for name, want := range map[string]Written{
		"n":    {Kind: Number, Text: "1000", Line: 4, TextLine: 4},
		"b":    {Kind: Boolean, Text: "true", Line: 5, TextLine: 5},
		"d":    {Kind: Scalar, Text: "2026-01-02", Line: 6, TextLine: 6},
		"none": {Kind: Null, Text: "null", Line: 7, TextLine: 7},
		"text": {Kind: Text, Text: "one\ntwo\n", Line: 9, TextLine: 10},
	} {
		got := value(name)
		if got == nil {
			t.Errorf("%s not written", name)
			continue
		}
		if src := got.SourceText(); src != map[string]string{"n": "1_000", "b": "True"}[name] && src != want.Text {
			t.Errorf("%s source %q", name, src)
		}
		got.Source = nil
		if !reflect.DeepEqual(*got, want) {
			t.Errorf("%s = %+v, want %+v", name, got, want)
		}
	}
	if l := value("l"); l.Kind != List || l.SourceText() != "[x, 2]" || len(l.Items) != 2 || l.Items[1].Kind != Number {
		t.Errorf("l = %+v", l)
	}
	rows := value("rows")
	if row := rows.Items[0]; row.Kind != Mapping || row.Row.Value(&schema.Field{Name: "a"}).Line != 13 {
		t.Errorf("rows = %+v", rows)
	}
	if value("missing") != nil {
		t.Error("a missing value is written")
	}
	var keys []string
	for _, k := range e.Record.Keys() {
		keys = append(keys, fmt.Sprintf("%s@%d/%v", k.Name, k.Line, k.Set))
	}
	if got := strings.Join(keys, " "); got != "id@2/true type@3/true n@4/true b@5/true d@6/true none@7/false l@8/true text@9/true rows@12/true" {
		t.Errorf("keys %s", got)
	}
}

// A text value's references: embeds, citations, query blocks, images and
// fenced blocks, at lines of the text.
func TestMarkdownRefs(t *testing.T) {
	e, _ := Parse("REQ/a.md", []byte(req))
	refs, err := e.Record.Refs("![[B-1]]\n\nSee [[C-1]] and [[#fig]].\n\n![x](img/a.png)\n\n```query\nfrom: T\n```\n\n```\ncode\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%v %v %v %+v %+v", refs.Embeds, refs.Cited, refs.Assets, refs.Queries, refs.CodeBlocks)
	if want := "[B-1] [C-1] [REQ/img/a.png] [{Lang:query Source:from: T\n Line:8}] [{Lang:query Source: Line:7} {Lang: Source: Line:11}]"; got != want {
		t.Errorf("refs\n%s\nwant\n%s", got, want)
	}
	refs, err = e.Record.Refs("```query\nfrom: T\n```\n\n![x](https://example.com/a.png)\n")
	var te *TextError
	if !errors.As(err, &te) || te.Line != 5 || len(refs.Queries) != 0 || len(refs.CodeBlocks) != 1 {
		t.Errorf("refs %+v, err %v", refs, err)
	}
}

func TestParseToleratesCRLFAndBOM(t *testing.T) {
	crlf := strings.ReplaceAll(req, "\n", "\r\n")
	for name, raw := range map[string][]byte{
		"crlf": []byte(crlf),
		"bom":  append(append([]byte{}, utf8BOM...), req...),
	} {
		e, err := Parse("x.md", raw)
		if err != nil || e == nil || e.ID != "REQ-0001" {
			t.Errorf("%s: %v, %v", name, e, err)
		}
	}
}

func TestParseNonEntities(t *testing.T) {
	for name, content := range map[string]string{
		"no frontmatter":     "# README\n\nJust prose.\n",
		"thematic break":     "text\n---\nmore\n",
		"unclosed":           "---\nid: X\ntype: Y\n",
		"no id":              "---\ntype: Requirement\n---\n",
		"no type":            "---\nid: REQ-0001\n---\n",
		"empty frontmatter":  "---\n---\nbody\n",
		"list frontmatter":   "---\n- a\n- b\n---\n",
		"delimiter not line": "---x\nid: A\ntype: B\n---\n",
	} {
		e, err := Parse("x.md", []byte(content))
		if e != nil || err != nil {
			t.Errorf("%s: got %v, %v; want not an entity", name, e, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for content, want := range map[string]string{
		"---\nid: REQ-0001\ntype: [unclosed\n---\n": "REQ/bad.md:",
		"---\nid: 42\ntype: Requirement\n---\n":     "REQ/bad.md:2: id must be a non-empty string",
		"---\nid: A\ntype: [x]\n---\n":              "REQ/bad.md:3: type must be a non-empty string",
		"---\nid: \"\"\ntype: T\n---\n":             "REQ/bad.md:2: id must be a non-empty string",
	} {
		_, err := Parse("REQ/bad.md", []byte(content))
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: err = %v, want prefix %q", content, err, want)
		}
	}
}

func TestParseYAMLErrorUsesFileLines(t *testing.T) {
	_, err := Parse("a.md", []byte("---\nid: A\ntype: B\ntitle: x: y\n---\n"))
	var fe *Error
	if !errors.As(err, &fe) || fe.Line != 4 || !strings.Contains(fe.Msg, "invalid frontmatter") {
		t.Errorf("err = %v, want line 4 invalid frontmatter", err)
	}
}
