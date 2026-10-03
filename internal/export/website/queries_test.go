package website

import (
	"errors"
	"strings"
	"testing"
)

const rankedNoteSchema = noteSchema + "  - {name: rank, type: number}\n"

func queryRepo(t *testing.T, hostFront, hostBody string) (map[string]string, error) {
	t.Helper()
	files, err := exportRepo(t, map[string]string{
		"schema/Note.yaml": rankedNoteSchema,
		"N/N-1.md":         note("N-1", "title: Third\nrank: 3\n", "# One\n\nFirst body."),
		"N/N-2.md":         note("N-2", "title: First\nrank: 1\nsee: [N-1]\n", "Second body."),
		"N/N-3.md":         note("N-3", "title: Unranked\n", "Third body."),
		"H/H-1.md":         note("H-1", hostFront, hostBody),
	})
	pages := map[string]string{}
	for k, v := range files {
		pages[k] = string(v)
	}
	return pages, err
}

func TestQueryBlockRendering(t *testing.T) {
	block := func(lines ...string) string { return "```query\n" + strings.Join(lines, "\n") + "\n```\n" }
	pages, err := queryRepo(t, "title: Host\n", "## Full\n\n"+
		block("from: Note", "where: {field: rank, operator: exists}", "order_by: rank")+
		"\n## IDs\n\n"+block("from: Note", "where: {field: id, operator: not_equals, value: H-1}", "render: id")+
		"\n## Titles\n\n"+block("from: Note", "where: {field: seen_by, operator: exists}", "render: field:title")+
		"\n## None\n\n"+block("from: Note", "where: {field: rank, operator: greater_than, value: 10}"))
	if err != nil {
		t.Fatal(err)
	}
	page := pages["entities/H-1.html"]
	for _, want := range []string{
		// Full: embedded in rank order, headings shifted under the section.
		"<h2>Full</h2>\n<div class=\"query\">\n<section class=\"embed\" data-entity=\"N-2\">",
		"</section>\n<section class=\"embed\" data-entity=\"N-1\">",
		"<h3>One</h3>",
		// IDs: every note but the host, by ID.
		"<ul class=\"query\">\n<li><a class=\"ref\" href=\"N-1.html\" title=\"Third\">N-1</a></li>\n<li><a class=\"ref\" href=\"N-2.html\"",
		// A field's value, linked.
		"<li><a class=\"ref\" href=\"N-1.html\" title=\"Third\">Third</a></li>",
		"<p class=\"query-empty\">No Note matches this query.</p>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("H-1 page missing %q\n%s", want, page)
		}
	}
	if strings.Contains(page, `data-entity="N-3"`) {
		t.Error("N-3 has no rank but was embedded")
	}
}

func TestQueryBlockErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		front, body string
		want        string
	}{
		"in the body": {"title: Host\n", "Intro.\n\n```query\nfrom: Note\nwhere:\n  field: rank\n  operator: \"=\"\n  value: 1\n```\n",
			`H/H-1.md:12: query block: unknown operator "="`},
		"after blank lines": {"title: Host\n", "\n\n```query\nfrom: Nope\n```\n",
			`H/H-1.md:9: query block: from: unknown type "Nope"`},
		"in a text field": {"title: Host\nsummary: |\n  Text.\n\n  ```query\n  from: Note\n  order_by: see\n  ```\n", "",
			`H/H-1.md:10: query block: order_by: "see" has several values`},
		"cycle through a query": {"title: Host\n", "```query\nfrom: Note\nwhere: {field: id, operator: equals, value: H-1}\n```\n",
			"H/H-1.md: embed cycle H-1 → H-1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := queryRepo(t, tc.front, tc.body)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("got  %v\nwant %s…", err, tc.want)
			}
			var qe *QueryError
			var cycle *EmbedCycleError
			if !errors.As(err, &qe) && !errors.As(err, &cycle) {
				t.Errorf("error is a %T", err)
			}
		})
	}
}
