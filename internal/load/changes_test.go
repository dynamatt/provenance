package load

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dynamatt/provenance/internal/history/historytest"
	"github.com/dynamatt/provenance/internal/repo"
)

// An entity's changes (DES-0058): one row per first-parent commit that
// changed its values, compared as diff compares them, dated by the
// commit that made the change.

const docSchema = `type: Doc
id_prefix: DOC
fields:
  - {name: title, type: string, title: true}
  - {name: status, type: string}
  - name: steps
    type: list
    fields:
      - {name: description, type: text}
  - {name: text, type: text, body: true}
`

func doc(id, front, body string) string {
	return "---\nid: " + id + "\ntype: Doc\n" + front + "---\n" + body
}

var (
	ann = historytest.Signature{Name: "Ann", When: historytest.Day(1)}
	bea = historytest.Signature{Name: "Bea"}
)

func on(s historytest.Signature, day int) historytest.Signature {
	s.When = historytest.Day(day)
	return s
}

// rows renders an entity's changes, one line each: commit (by name),
// date, author, kind and fields.
func rows(t *testing.T, dir string, names map[string]string, id string) string {
	t.Helper()
	res, err := Load(&repo.Repo{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	h, err := res.History()
	if err != nil {
		t.Fatal(err)
	}
	eh, err := h.Changes()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range eh.Changes[id] {
		name := names[c.Commit]
		if c.Commit == "" {
			name = "work"
		}
		out = append(out, fmt.Sprintf("%s %s %s %s %v", name, c.Date, c.Author, c.Kind, c.Fields))
	}
	return strings.Join(out, "\n")
}

func with(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func TestChangesByValue(t *testing.T) {
	r := historytest.New(t)
	names := map[string]string{}
	base := map[string]string{"schema/Doc.yaml": docSchema}
	first := with(base, map[string]string{"DOC/DOC-0001.md": doc("DOC-0001", "title: Pump\nstatus: draft\nsteps:\n  - description: Prime it.\n", "The pump is primed\nbefore use.\n")})
	c1 := r.Commit(historytest.Commit{Message: "add", Author: on(ann, 1), Files: first})
	names[c1] = "c1"
	// Formatting only: the title quoted, keys reordered, the body
	// rewrapped. No change.
	c2 := r.Commit(historytest.Commit{Message: "fmt", Author: on(ann, 2), Parents: []string{c1}, Files: with(base, map[string]string{
		"DOC/DOC-0001.md": doc("DOC-0001", "status: draft\ntitle: \"Pump\"\nsteps:\n  - description: |-\n      Prime it.\n", "The pump is primed before use.\n")})})
	names[c2] = "c2"
	// The file moved: no change.
	c3 := r.Commit(historytest.Commit{Message: "move", Author: on(ann, 3), Parents: []string{c2}, Files: with(base, map[string]string{
		"docs/DOC-0001.md": doc("DOC-0001", "status: draft\ntitle: \"Pump\"\nsteps:\n  - description: |-\n      Prime it.\n", "The pump is primed before use.\n")})})
	names[c3] = "c3"
	c4 := r.Commit(historytest.Commit{Message: "approve", Author: on(bea, 4), Parents: []string{c3}, Files: with(base, map[string]string{
		"docs/DOC-0001.md": doc("DOC-0001", "status: approved\ntitle: Pump\nsteps:\n  - description: Prime it twice.\n", "The pump is primed before use.\n")})})
	names[c4] = "c4"
	// Uncommitted: the body.
	r.Write(with(base, map[string]string{
		"docs/DOC-0001.md": doc("DOC-0001", "status: approved\ntitle: Pump\nsteps:\n  - description: Prime it twice.\n", "The pump is primed and bled before use.\n")}))

	got := rows(t, r.Dir, names, "DOC-0001")
	want := strings.Join([]string{
		"work   changed [text]",
		"c4 2026-11-04 Bea changed [status steps]",
		"c1 2026-11-01 Ann added [title status steps text]",
	}, "\n")
	if got != want {
		t.Errorf("rows:\n%s\nwant:\n%s", got, want)
	}
}

// A merged branch's change is dated by the branch's commit that made it;
// a merge of the base into the branch carried the base's change and made
// none. A squash merge is one commit, dated when made; a rebased commit
// keeps its author date.
func TestChangesOfMerges(t *testing.T) {
	base := map[string]string{"schema/Doc.yaml": docSchema}
	one := func(body string) map[string]string {
		return map[string]string{"DOC/DOC-0001.md": doc("DOC-0001", "title: Pump\n", body)}
	}
	two := map[string]string{"DOC/DOC-0002.md": doc("DOC-0002", "title: Valve\n", "")}

	t.Run("merge commit", func(t *testing.T) {
		r := historytest.New(t)
		names := map[string]string{}
		c1 := r.Commit(historytest.Commit{Message: "add", Author: on(ann, 1), Files: with(base, one("Old.\n"))})
		names[c1] = "c1"
		b1 := r.Commit(historytest.Commit{Message: "edit", Author: on(bea, 3), Parents: []string{c1}, Files: with(base, one("New.\n"))})
		m2 := r.Commit(historytest.Commit{Message: "valve", Author: on(ann, 4), Parents: []string{c1}, Files: with(with(base, one("Old.\n")), two)})
		names[m2] = "m2"
		// The branch merges its base in on the 5th.
		b2 := r.Commit(historytest.Commit{Message: "merge base", Author: on(bea, 5), Parents: []string{b1, m2}, Files: with(with(base, one("New.\n")), two)})
		m := r.Commit(historytest.Commit{Message: "merge PR", Author: on(ann, 10), Parents: []string{m2, b2}, Files: with(with(base, one("New.\n")), two)})
		names[m] = "M"
		r.Head(m)
		if got, want := rows(t, r.Dir, names, "DOC-0001"), "M 2026-11-03 Bea changed [text]\nc1 2026-11-01 Ann added [title text]"; got != want {
			t.Errorf("DOC-0001:\n%s\nwant:\n%s", got, want)
		}
		if got, want := rows(t, r.Dir, names, "DOC-0002"), "m2 2026-11-04 Ann added [title]"; got != want {
			t.Errorf("DOC-0002:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("squash", func(t *testing.T) {
		r := historytest.New(t)
		names := map[string]string{}
		c1 := r.Commit(historytest.Commit{Message: "add", Author: on(ann, 1), Files: with(base, one("Old.\n"))})
		names[c1] = "c1"
		s := r.Commit(historytest.Commit{Message: "PR (#1)", Author: on(ann, 10), Parents: []string{c1}, Files: with(base, one("New.\n"))})
		names[s] = "S"
		if got, want := rows(t, r.Dir, names, "DOC-0001"), "S 2026-11-10 Ann changed [text]\nc1 2026-11-01 Ann added [title text]"; got != want {
			t.Errorf("DOC-0001:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("rebase", func(t *testing.T) {
		r := historytest.New(t)
		names := map[string]string{}
		c1 := r.Commit(historytest.Commit{Message: "add", Author: on(ann, 1), Files: with(base, one("Old.\n"))})
		names[c1] = "c1"
		rb := r.Commit(historytest.Commit{Message: "edit", Author: on(bea, 3), Committer: on(ann, 10), Parents: []string{c1}, Files: with(base, one("New.\n"))})
		names[rb] = "R"
		if got, want := rows(t, r.Dir, names, "DOC-0001"), "R 2026-11-03 Bea changed [text]\nc1 2026-11-01 Ann added [title text]"; got != want {
			t.Errorf("DOC-0001:\n%s\nwant:\n%s", got, want)
		}
	})
}

// A date is the day in the author's own time zone.
func TestChangeDateInAuthorsZone(t *testing.T) {
	r := historytest.New(t)
	late := historytest.Signature{Name: "Cy", When: historytest.Day(20).Add(11 * 3600e9).In(fixedZone(11))}
	r.Commit(historytest.Commit{Message: "add", Author: late, Files: map[string]string{"schema/Doc.yaml": docSchema, "DOC/DOC-0001.md": doc("DOC-0001", "title: Pump\n", "")}})
	if got := rows(t, r.Dir, nil, "DOC-0001"); !strings.Contains(got, "2026-11-21") {
		t.Errorf("rows: %s, want the 21st, the day in UTC+11", got)
	}
}

func fixedZone(hours int) *time.Location { return time.FixedZone("", hours*3600) }

// The example's history holds the commit that put every entity file in
// canonical form (fmt --all, Epic 4): it changed no entity's values.
func TestExampleFormatCommitIsNoChange(t *testing.T) {
	example, err := filepath.Abs(filepath.Join("..", "..", ".cache", "example"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(example, ".git")); err != nil {
		t.Skip("no example checkout: make example")
	}
	res, err := Load(&repo.Repo{Root: example})
	if err != nil {
		t.Fatal(err)
	}
	h, err := res.History()
	if err != nil {
		t.Fatal(err)
	}
	eh, err := h.Changes()
	if err != nil {
		t.Fatal(err)
	}
	const fmtMerge = "4dab2f7" // Merge pull request #22, epic/E4-fmt
	if !slices.ContainsFunc(eh.Order, func(c string) bool { return strings.HasPrefix(c, fmtMerge) }) {
		t.Fatalf("the example's history has no commit %s", fmtMerge)
	}
	n := 0
	for id, cs := range eh.Changes {
		n += len(cs)
		for _, c := range cs {
			if strings.HasPrefix(c.Commit, fmtMerge) {
				t.Errorf("%s changed at the fmt commit: %v", id, c.Fields)
			}
		}
	}
	if n == 0 {
		t.Error("no changes at all")
	}
}
