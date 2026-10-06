package query

import (
	"strings"
	"testing"
)

func TestDependencies(t *testing.T) {
	g := graph(t, map[string]string{
		// DOC-1 embeds REQ-0003, whose body embeds USR-0001 in turn; its
		// query renders approved requirements in full and user needs as IDs;
		// it only references SEV-0003.
		"DOC-1":    fm("DOC-1", "Document", "summary: \"See ![[DOC-2]] below.\"\n") + "![[REQ-0003]]\n\n```query\nfrom: Requirement\nwhere: {field: status, operator: equals, value: approved}\n```\n\n```query\nfrom: UserNeed\nrender: id\n```\n\n[[SEV-0003]]\n",
		"DOC-2":    fm("DOC-2", "Document", "") + "Nothing pulled in.\n",
		"REQ-0003": fm("REQ-0003", "Requirement", "status: in_review\n") + "![[USR-0001]]\n",
		// USR-0002 is pulled in as an ID only: its embed is not followed.
		"USR-0002": fm("USR-0002", "UserNeed", "status: deprecated\n") + "![[RSK-0001]]\n",
	})
	d, err := g.Dependencies(g.Entity("DOC-1"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(d.IDs, " "), "DOC-1 REQ-0001 REQ-0002 REQ-0003 USR-0001 USR-0002"; got != want {
		t.Errorf("IDs: %s\nwant %s", got, want)
	}
	if got := strings.Join(d.Types, " "); got != "Requirement UserNeed" {
		t.Errorf("Types: %s", got)
	}
	// Citations: DOC-1's own reference and its misplaced embed, which
	// renders as one; nothing it embeds cites anything.
	if got := strings.Join(d.Cited, " "); got != "DOC-2 SEV-0003" {
		t.Errorf("Cited: %s", got)
	}
	// An embed inside running text (a misplaced embed) is not pulled in.
	if strings.Contains(strings.Join(d.IDs, " "), "DOC-2") {
		t.Error("a misplaced embed was followed")
	}

	d, err = g.Dependencies(g.Entity("REQ-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(d.IDs, " ") != "REQ-0001" || len(d.Types) != 0 {
		t.Errorf("a plain entity depends on itself alone, got %+v", d)
	}
}

func TestDependenciesQueryError(t *testing.T) {
	g := graph(t, map[string]string{
		"DOC-1": fm("DOC-1", "Document", "") + "```query\nfrom: Nope\n```\n",
	})
	_, err := g.Dependencies(g.Entity("DOC-1"))
	if err == nil || !strings.HasPrefix(err.Error(), `DOC-1:6: query block: from: unknown type "Nope"`) {
		t.Errorf("err = %v", err)
	}
}

func TestFormulaInputs(t *testing.T) {
	g := graph(t, nil)
	if got := strings.Join(g.FormulaInputs(g.Entity("RSK-0001")), " "); got != "OCC-0001 OCC-0002 SEV-0003" {
		t.Errorf("RSK-0001 reads %q", got)
	}
	if got := g.FormulaInputs(g.Entity("REQ-0001")); len(got) != 0 {
		t.Errorf("REQ-0001 has no calculated fields but reads %v", got)
	}
}

func TestDependencyDetails(t *testing.T) {
	g := graph(t, map[string]string{
		"DOC-1": fm("DOC-1", "Document", "") + "![[RSK-0001]]\n\n![chart](assets/chart.png)\n\n" +
			"```query\nfrom: Requirement\nwhere: {field: {via: implements, field: status}, operator: equals, value: approved}\nrender: id\n```\n\n" +
			"```query\nfrom: Requirement\ntemplate: compact-row\n```\n",
	})
	d, err := g.Dependencies(g.Entity("DOC-1"))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]string{"Full": d.Full, "Read": d.Read, "Types": d.Types, "Templates": d.Templates, "Assets": d.Assets} {
		want := map[string]string{
			// Requirements are rendered in full by the second query.
			"Full": "DOC-1 REQ-0001 REQ-0002 REQ-0003 RSK-0001",
			// The embedded risk's ratings read these; they get no page.
			"Read": "OCC-0001 OCC-0002 SEV-0003",
			// The via condition reads user needs' status.
			"Types":     "Requirement UserNeed",
			"Templates": "compact-row",
			"Assets":    "assets/chart.png", // relative to DOC-1's file, at the root
		}[name]
		if strings.Join(got, " ") != want {
			t.Errorf("%s: %q, want %q", name, strings.Join(got, " "), want)
		}
	}
}
