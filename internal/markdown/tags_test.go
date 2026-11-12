package markdown

import (
	"strings"
	"testing"

	"github.com/yuin/goldmark/util"
)

func TestTagsOnHeadingsAndOrderedItems(t *testing.T) {
	var found []Tag
	src := "## Device under test {#dut}\n\n1. Connect the probe. {#connect}\n2. Record\n   one pulse. {#one-pulse}\n   1. Nested {#nested}\n\n## Plain_name {#a_b}\n"
	out, errs := ConvertAll(src, fake{}, Options{Owner: "VER-0001", Tag: func(tg Tag) { found = append(found, tg) }})
	for _, want := range []string{
		`<h2 data-tag="dut" data-owner="VER-0001">Device under test</h2>`,
		`<li data-tag="connect" data-owner="VER-0001">Connect the probe.</li>`,
		"<li data-tag=\"one-pulse\" data-owner=\"VER-0001\">Record\none pulse.",
		`<li data-tag="nested" data-owner="VER-0001">Nested</li>`,
		// A tag split across text nodes ('_') is found, and checked.
		`<h2>Plain_name {#a_b}</h2>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s\n%s", want, out)
		}
	}
	got := []string{}
	for _, tg := range found {
		got = append(got, tg.Name+"@"+string(rune('0'+tg.Line)))
	}
	if strings.Join(got, " ") != "dut@1 connect@3 one-pulse@5 nested@6" {
		t.Errorf("tags found = %v", got)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "line 8: tag {#a_b}: use lower-case") {
		t.Errorf("errs = %v", errs)
	}
}

func TestMisplacedTags(t *testing.T) {
	for src, want := range map[string]string{
		"- item {#x}\n":          "line 1: {#x}: a tag on an unordered list item",
		"Text. {#x}\n":           "line 1: {#x}: a tag goes at the end of a heading",
		"1. a\n\n   more {#x}\n": "line 3: {#x}: a tag goes at the end of a heading",
		"## Bad {#Bad}\n":        "line 1: tag {#Bad}: use lower-case",
	} {
		out, errs := ConvertAll(src, fake{}, Options{})
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), want) {
			t.Errorf("%q: errs = %v, want %s", src, errs, want)
		}
		if !strings.Contains(out, "{#") || strings.Contains(out, "data-tag") {
			t.Errorf("%q: a bad tag is left as written: %s", src, out)
		}
	}
}

func TestReferenceModes(t *testing.T) {
	var got []Link
	r := recorder{links: &got}
	if _, err := Convert("[[#dut]] [[#dut:number]] [[VER-0001#step:long|the step]] [[REQ-0001#status]]", r, Options{}); err != nil {
		t.Fatal(err)
	}
	want := []Link{
		{Local: "dut", Line: 1},
		{Local: "dut", Mode: "number", Line: 1},
		{ID: "VER-0001", Field: "step", Mode: "long", Label: "the step", Line: 1},
		{ID: "REQ-0001", Field: "status", Line: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

type recorder struct {
	fake
	links *[]Link
}

func (r recorder) Reference(_ util.BufWriter, l Link) error {
	*r.links = append(*r.links, l)
	return nil
}
