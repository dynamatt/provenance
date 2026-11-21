package validate

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dynamatt/provenance/internal/history/historytest"
	"github.com/dynamatt/provenance/internal/repo"
)

// The scenarios of DES-0058: rules on what changed when, over histories
// laid out commit by commit as git and a host write them. The rules are
// the example's: evidence-current, approval-current,
// evidence-after-approval and execution-not-in-future.

var historyFiles = map[string]string{
	".component": "name: Test\ncode: TEST\n",
	"schema/Protocol.yaml": `type: Protocol
id_prefix: VER
fields:
  - {name: title, type: string, title: true}
  - {name: status, type: string}
  - name: approvals
    type: list
    fields:
      - {name: date, type: date}
  - {name: approved_on, type: calculated, formula: "MAX(approvals[].date)"}
  - {name: procedure, type: text, body: true}
`,
	"schema/Evidence.yaml": `type: Evidence
id_prefix: EVD
fields:
  - {name: title, type: string, title: true}
  - {name: execution_date, type: date}
  - {name: tests, type: link, target: [Protocol], cardinality: one, reverse_name: tested_by}
  - name: justifications
    type: list
    fields:
      - {name: date, type: date}
  - {name: current_as_of, type: calculated, formula: "MAX(execution_date, justifications[].date)"}
  - {name: notes, type: text, body: true}
`,
	"rules/evidence-current.yaml": `severity: warning
message: stale
from: Evidence
where:
  field: tests
  where:
    field: changes
    where:
      - {field: kind, operator: equals, value: changed}
      - {field: fields, operator: equals, value: procedure}
      - {field: date, operator: greater_than, value_of: ^^current_as_of}
`,
	"rules/approval-current.yaml": `severity: error
message: changed after approval
from: Protocol
where:
  field: changes
  where:
    - {field: kind, operator: equals, value: changed}
    - {field: fields, operator: equals, value: procedure}
    - {field: date, operator: greater_than, value_of: ^approved_on}
`,
	"rules/evidence-after-approval.yaml": `severity: warning
message: before approval
from: Evidence
where:
  field: tests
  where: {field: approved_on, operator: greater_than, value_of: ^current_as_of}
`,
	"rules/execution-not-in-future.yaml": `severity: error
message: in the future
from: Evidence
where:
  - {field: execution_date, operator: exists}
  - field: changes
    where:
      - {field: fields, operator: equals, value: execution_date}
      - {field: date, operator: greater_or_equal, value_of: ^execution_date}
    aggregation: {type: count}
    operator: equals
    value: 0
`,
}

// protocol is VER-0001 with its procedure and approval dates.
func protocol(procedure string, approvals ...string) string {
	s := "---\nid: VER-0001\ntype: Protocol\ntitle: Pump\nstatus: approved\n"
	if len(approvals) > 0 {
		s += "approvals:\n"
		for _, d := range approvals {
			s += "  - date: " + d + "\n"
		}
	}
	return s + "---\n" + procedure + "\n"
}

// evidence is EVD-0001, run on executed, with its justification dates.
func evidence(executed string, justified ...string) string {
	s := "---\nid: EVD-0001\ntype: Evidence\ntitle: Pump run\nexecution_date: " + executed + "\ntests: VER-0001\n"
	if len(justified) > 0 {
		s += "justifications:\n"
		for _, d := range justified {
			s += "  - date: " + d + "\n"
		}
	}
	return s + "---\n"
}

// tree is the files of a commit: the schema and rules, then the
// protocol's and the evidence's files, "" for none.
func tree(ver, evd string, extra ...string) map[string]string {
	out := map[string]string{}
	for k, v := range historyFiles {
		out[k] = v
	}
	if ver != "" {
		out["VER/VER-0001.md"] = ver
	}
	if evd != "" {
		out["VER/EVD-0001.md"] = evd
	}
	for i := 0; i+1 < len(extra); i += 2 {
		out[extra[i]] = extra[i+1]
	}
	return out
}

func by(name string, day int) historytest.Signature {
	return historytest.Signature{Name: name, When: historytest.Day(day)}
}

// verdicts are the history rules' violations, "rule ENTITY" each, sorted.
func verdicts(t *testing.T, dir string) string {
	t.Helper()
	r, err := Run(&repo.Repo{Root: dir}, exporters())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, v := range r.Violations {
		if _, ok := historyFiles["rules/"+v.Rule+".yaml"]; ok {
			out = append(out, v.Rule+" "+v.Entity)
		} else if v.Severity == Error {
			t.Errorf("unexpected: %s:%d %s %s", v.Path, v.Line, v.Rule, v.Message)
		}
	}
	slices.Sort(out)
	return strings.Join(out, ", ")
}

const (
	run      = "Prime the pump."
	changed  = "Prime the pump twice."
	reverted = run
)

// base is the history every scenario starts from: VER-0001 added and
// approved on the 1st, EVD-0001 run and recorded on the 2nd.
func base(t *testing.T) (*historytest.Repo, string) {
	r := historytest.New(t)
	c1 := r.Commit(historytest.Commit{Message: "protocol", Author: by("Ann", 1), Files: tree(protocol(run, "2026-11-01"), "")})
	c2 := r.Commit(historytest.Commit{Message: "evidence", Author: by("Cy", 2), Parents: []string{c1}, Files: tree(protocol(run, "2026-11-01"), evidence("2026-11-02"))})
	return r, c2
}

func TestHistoryScenarios(t *testing.T) {
	commit := func(r *historytest.Repo, msg string, author historytest.Signature, parent string, files map[string]string, more ...string) string {
		return r.Commit(historytest.Commit{Message: msg, Author: author, Parents: append([]string{parent}, more...), Files: files})
	}
	for _, sc := range []struct {
		id, name string
		build    func(t *testing.T) string // the repository's folder
		want     string
	}{
		{"none", "the base history", func(t *testing.T) string {
			r, _ := base(t)
			return r.Dir
		}, ""},
		{"S1", "the procedure changed and approved again; the evidence neither run again nor justified", func(t *testing.T) string {
			r, c2 := base(t)
			commit(r, "change", by("Ann", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			return r.Dir
		}, "evidence-after-approval EVD-0001, evidence-current EVD-0001"},
		{"S2", "then run again", func(t *testing.T) string {
			r, c2 := base(t)
			c3 := commit(r, "change", by("Ann", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			commit(r, "rerun", by("Cy", 6), c3, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-06")))
			return r.Dir
		}, ""},
		{"S3", "then justified after the change", func(t *testing.T) string {
			r, c2 := base(t)
			c3 := commit(r, "change", by("Ann", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			commit(r, "justify", by("Cy", 6), c3, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06")))
			return r.Dir
		}, ""},
		{"S4", "a second change after the justification", func(t *testing.T) string {
			r, c2 := base(t)
			c3 := commit(r, "change", by("Ann", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			c4 := commit(r, "justify", by("Cy", 6), c3, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06")))
			commit(r, "again", by("Ann", 8), c4, tree(protocol("Prime the pump three times.", "2026-11-01", "2026-11-05", "2026-11-08"), evidence("2026-11-02", "2026-11-06")))
			return r.Dir
		}, "evidence-after-approval EVD-0001, evidence-current EVD-0001"},
		{"S5", "the change and its justification in one pull request, merged with a merge commit", func(t *testing.T) string {
			r, c2 := base(t)
			m3 := commit(r, "other", by("Ann", 7), c2, tree(protocol(run, "2026-11-01"), evidence("2026-11-02"), "VER/VER-0002.md", "---\nid: VER-0002\ntype: Protocol\ntitle: Valve\n---\nOpen it.\n"))
			b1 := commit(r, "change", by("Bea", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			b2 := commit(r, "justify", by("Bea", 6), b1, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06")))
			m := commit(r, "Merge pull request #1", by("Ann", 10), m3, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06"), "VER/VER-0002.md", "---\nid: VER-0002\ntype: Protocol\ntitle: Valve\n---\nOpen it.\n"), b2)
			r.Head(m)
			return r.Dir
		}, ""},
		{"S6", "the same pull request squash-merged: its change is dated the merge day", func(t *testing.T) string {
			r, c2 := base(t)
			commit(r, "Pull request (#1)", by("Ann", 10), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06")))
			return r.Dir
		}, "approval-current VER-0001, evidence-current EVD-0001"},
		{"S7", "the same pull request rebase-merged: author dates kept", func(t *testing.T) string {
			r, c2 := base(t)
			r1 := r.Commit(historytest.Commit{Message: "change", Author: by("Bea", 5), Committer: by("Ann", 10), Parents: []string{c2}, Files: tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02"))})
			r.Commit(historytest.Commit{Message: "justify", Author: by("Bea", 6), Committer: by("Ann", 10), Parents: []string{r1}, Files: tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06"))})
			return r.Dir
		}, ""},
		{"S8", "a pull request that merged its base into itself, then merged", func(t *testing.T) string {
			r, c2 := base(t)
			other := []string{"VER/VER-0002.md", "---\nid: VER-0002\ntype: Protocol\ntitle: Valve\n---\nOpen it.\n"}
			m3 := commit(r, "other", by("Ann", 7), c2, tree(protocol(run, "2026-11-01"), evidence("2026-11-02"), other...))
			b1 := commit(r, "change", by("Bea", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			b2 := commit(r, "justify", by("Bea", 6), b1, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06")))
			b3 := commit(r, "merge main", by("Bea", 9), b2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06"), other...), m3)
			m := commit(r, "Merge pull request #1", by("Ann", 10), m3, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-06"), other...), b3)
			r.Head(m)
			return r.Dir
		}, ""},
		{"S9", "the protocol reformatted and its file moved", func(t *testing.T) string {
			r, c2 := base(t)
			moved := tree("", evidence("2026-11-02"), "protocols/VER-0001.md", "---\ntype: Protocol\nid: VER-0001\nstatus: \"approved\"\ntitle: Pump\napprovals:\n  - date: 2026-11-01\n---\nPrime\nthe pump.\n")
			commit(r, "tidy", by("Ann", 5), c2, moved)
			return r.Dir
		}, ""},
		{"S10", "only the protocol's status changed", func(t *testing.T) string {
			r, c2 := base(t)
			commit(r, "status", by("Ann", 5), c2, tree(strings.Replace(protocol(run, "2026-11-01"), "status: approved", "status: released", 1), evidence("2026-11-02")))
			return r.Dir
		}, ""},
		{"S11", "the evidence edited for another reason", func(t *testing.T) string {
			r, c2 := base(t)
			c3 := commit(r, "change", by("Ann", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			commit(r, "typo", by("Cy", 6), c3, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")+"A typo fixed.\n"))
			return r.Dir
		}, "evidence-after-approval EVD-0001, evidence-current EVD-0001"},
		{"S12", "the procedure changed after its latest approval", func(t *testing.T) string {
			r, c2 := base(t)
			commit(r, "change", by("Ann", 5), c2, tree(protocol(changed, "2026-11-01"), evidence("2026-11-02")))
			return r.Dir
		}, "approval-current VER-0001, evidence-current EVD-0001"},
		{"S13", "the evidence run before its protocol's approval", func(t *testing.T) string {
			r := historytest.New(t)
			c1 := r.Commit(historytest.Commit{Message: "protocol", Author: by("Ann", 1), Files: tree(protocol(run, "2026-11-03"), "")})
			r.Commit(historytest.Commit{Message: "evidence", Author: by("Cy", 4), Parents: []string{c1}, Files: tree(protocol(run, "2026-11-03"), evidence("2026-11-02"))})
			return r.Dir
		}, "evidence-after-approval EVD-0001"},
		{"S14", "a change reverted: the revert is a change too", func(t *testing.T) string {
			r, c2 := base(t)
			c3 := commit(r, "change", by("Ann", 5), c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			commit(r, "revert", by("Ann", 6), c3, tree(protocol(reverted, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			return r.Dir
		}, "approval-current VER-0001, evidence-after-approval EVD-0001, evidence-current EVD-0001"},
		{"S15", "the procedure edited but not committed: no date yet", func(t *testing.T) string {
			r, _ := base(t)
			r.Write(tree(protocol(changed, "2026-11-01"), evidence("2026-11-02")))
			return r.Dir
		}, ""},
		{"S16", "a commit backdated before the run is taken at its date", func(t *testing.T) string {
			r, c2 := base(t)
			back := historytest.Signature{Name: "Ann", When: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
			commit(r, "backdated", back, c2, tree(protocol(changed, "2026-11-01"), evidence("2026-11-02")))
			return r.Dir
		}, ""},
		{"S17", "the change and its justification on one day, in the author's time zone", func(t *testing.T) string {
			r, c2 := base(t)
			// 23:00 on the 5th at UTC−8 is the 6th in UTC: the day is the 5th.
			late := historytest.Signature{Name: "Bea", When: time.Date(2026, 11, 5, 23, 0, 0, 0, time.FixedZone("", -8*3600))}
			c3 := commit(r, "change", late, c2, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02")))
			commit(r, "justify", by("Cy", 5), c3, tree(protocol(changed, "2026-11-01", "2026-11-05"), evidence("2026-11-02", "2026-11-05")))
			return r.Dir
		}, ""},
		{"S19", "an execution date after the commit recording it", func(t *testing.T) string {
			r := historytest.New(t)
			c1 := r.Commit(historytest.Commit{Message: "protocol", Author: by("Ann", 1), Files: tree(protocol(run, "2026-11-01"), "")})
			r.Commit(historytest.Commit{Message: "evidence", Author: by("Cy", 2), Parents: []string{c1}, Files: tree(protocol(run, "2026-11-01"), evidence("2026-11-09"))})
			return r.Dir
		}, "execution-not-in-future EVD-0001"},
	} {
		t.Run(sc.id, func(t *testing.T) {
			if got := verdicts(t, sc.build(t)); got != sc.want {
				t.Errorf("%s: %s\ngot  %q\nwant %q", sc.id, sc.name, got, sc.want)
			}
		})
	}
}
