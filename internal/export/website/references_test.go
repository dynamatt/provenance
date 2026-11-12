package website

import (
	"strings"
	"testing"
)

// procedure is a protocol's body: tagged sections and steps, nested two
// deep, and references to them in each mode.
const procedure = "## Setup {#setup}\n\n### Equipment {#equipment}\n\n## Procedure {#procedure}\n\n" +
	"1. Connect the probe. {#connect}\n" +
	"2. Record one pulse.\n" +
	"   1. Set the amplitude.\n" +
	"   2. Trigger the *capture*. {#capture}\n" +
	"      1. Wait.\n" +
	"      2. Save the trace. {#save}\n\n" +
	"## Limits\n\n3. Ceiling {#ceiling}\n4. Floor\n\n" +
	"Own: [[#setup]], [[#equipment:number]], [[#connect:title]], [[#capture]], [[#save:long]], " +
	"[[#procedure|the procedure]], [[#ceiling]].\n"

func referenceRepo() map[string]string {
	return map[string]string{
		"schema/Note.yaml": noteSchema,
		"N/N-1.md":         note("N-1", "title: Protocol\n", procedure),
		// N-2 embeds N-1 under its second section.
		"N/N-2.md": note("N-2", "title: Report\n", "## Intro\n\nAs in [[N-1#setup]] and [[N-1#capture:long]].\n\n## Verification\n\n![[N-1]]\n"),
		// N-3 refers to N-1 without embedding it.
		"N/N-3.md": note("N-3", "title: Plan\n", "See [[N-1#procedure]], [[N-1#capture:number]], [[N-1#setup:title]] and [[N-1#equipment|its kit]].\n"),
	}
}

func TestSectionAndItemReferences(t *testing.T) {
	files, err := exportRepo(t, referenceRepo())
	if err != nil {
		t.Fatal(err)
	}
	for page, wants := range map[string][]string{
		"entities/N-1.html": {
			// Headings numbered below the title; tagged ones get their tag as id.
			`<h2 id="setup"><span class="section-number">1</span> Setup</h2>`,
			`<h3 id="equipment"><span class="section-number">1.1</span> Equipment</h3>`,
			`<h2 id="procedure"><span class="section-number">2</span> Procedure</h2>`,
			`<li id="connect">Connect the probe.</li>`,
			`<li id="capture">Trigger the <em>capture</em>.`,
			`<li id="save">Save the trace.</li>`,
			// Each mode, and the default.
			`Own: <a class="ref section-ref" href="#setup">Section 1</a>, <a class="ref section-ref" href="#equipment">1.1</a>, ` +
				`<a class="ref item-ref" href="#connect">Connect the probe.</a>, <a class="ref item-ref" href="#capture">item 2(b)</a>, ` +
				`<a class="ref item-ref" href="#save">item 2(b)(ii) Save the trace.</a>, <a class="ref section-ref" href="#procedure">the procedure</a>, ` +
				// An ordered list starting at 3 counts from 3.
				`<a class="ref item-ref" href="#ceiling">item 3</a>.`,
		},
		"entities/N-2.html": {
			// Embedded under section 2: N-1's title is 2.1, its sections 2.1.1 …
			`<h4 id="N-1-setup"><span class="section-number">2.1.1</span> Setup</h4>`,
			// … which its references and the host's show, with no "of".
			`As in <a class="ref section-ref" href="#N-1-setup">Section 2.1.1</a> and <a class="ref item-ref" href="#N-1-capture">item 2(b) Trigger the capture.</a>.`,
			`Own: <a class="ref section-ref" href="#N-1-setup">Section 2.1.1</a>`,
		},
		"entities/N-3.html": {
			// Not embedded: the number on N-1's own page, of N-1.
			`See <a class="ref section-ref" href="N-1.html#procedure">Section 2 of N-1</a>, <a class="ref item-ref" href="N-1.html#capture">2(b) of N-1</a>, ` +
				`<a class="ref section-ref" href="N-1.html#setup">Setup of N-1</a> and <a class="ref section-ref" href="N-1.html#equipment">its kit</a>.`,
		},
	} {
		got := string(files[page])
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s missing %s\n%s", page, want, got)
			}
		}
		if strings.Contains(got, refElement) || strings.Contains(got, "data-tag") {
			t.Errorf("%s has a placeholder or tag attribute left", page)
		}
	}
}

// A reference to an entity outside the export's scope shows its number
// on its own page, unlinked.
func TestReferenceOutOfScope(t *testing.T) {
	files, err := exportScoped(t, referenceRepo(), "N/N-3.md")
	if err != nil {
		t.Fatal(err)
	}
	got := string(files["index.html"])
	if want := `See <span class="ref section-ref out-of-scope">Section 2 of N-1</span>`; !strings.Contains(got, want) {
		t.Errorf("missing %s\n%s", want, got)
	}
}

func TestReferenceAndTagProblems(t *testing.T) {
	files := map[string]string{
		"schema/Note.yaml": noteSchema,
		"N/N-1.md": note("N-1", "title: One\n", "## A {#a}\n\n## B {#a}\n\n## C {#Bad}\n\n- loose {#loose}\n\nA paragraph. {#para}\n\n"+
			"## Summary {#summary}\n\n![x](../a.svg)\n\n```caption\nkind: figure\nid: a\n```\n\n"+
			"[[#a:short]] [[#gone]] [[N-2#title:number]] [[N-2#missing]]\n"),
		"N/N-2.md": note("N-2", "title: Two\n", "Two."),
		"a.svg":    "<svg/>",
	}
	want := strings.Join([]string{
		`N/N-1.md:10 content/tag tag {#Bad}: use lower-case letters, digits and '-'`,
		`N/N-1.md:12 content/tag {#loose}: a tag on an unordered list item: it has no number to show`,
		`N/N-1.md:14 content/tag {#para}: a tag goes at the end of a heading or of an ordered list item's first paragraph`,
		`N/N-1.md:16 content/tag tag {#summary} is named like a field of Note, so [[N-1#summary]] would show the field`,
		`N/N-1.md:21 content/tag #a is used twice in N-1 (first at N/N-1.md:6): tags and caption ids must be unique within an entity`,
		`N/N-1.md:25 links/reference [[#a:short]]: unknown mode "short" (full, number, title or long)`,
		`N/N-1.md:25 links/reference [[#gone]]: N-1 has no field, tag or caption "gone"`,
		`N/N-1.md:25 links/reference [[N-2#missing]]: N-2 has no field, tag or caption "missing"`,
		`N/N-1.md:25 links/reference [[N-2#title:number]]: "title" is a field of Note, which takes no mode: a mode is for a tag or caption`,
		`N/N-1.md:8 content/tag #a is used twice in N-1 (first at N/N-1.md:6): tags and caption ids must be unique within an entity`,
	}, "\n")
	if got := check(t, files); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestItemLabels(t *testing.T) {
	for _, tc := range []struct {
		n, depth int
		want     string
	}{{4, 1, "4"}, {2, 2, "b"}, {27, 2, "aa"}, {3, 3, "iii"}, {14, 3, "xiv"}, {5, 4, "5"}} {
		if got := itemLabel(tc.n, tc.depth); got != tc.want {
			t.Errorf("itemLabel(%d, %d) = %q, want %q", tc.n, tc.depth, got, tc.want)
		}
	}
}
