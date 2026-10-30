package validate

import (
	"strings"
	"testing"
)

// ruleRepo is a repository of parts and the checks made on them, for rule
// placement (D8): fields written and absent, a list, a link both ways, a
// body field.
func ruleRepo(t *testing.T, rules map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "schema/Part.yaml", `type: Part
id_prefix: PRT
fields:
  - {name: title, type: string}
  - {name: status, type: string}
  - {name: owner, type: string}
  - {name: parent, type: link, target: [Part], cardinality: one, reverse_name: children}
  - name: checks
    type: list
    fields:
      - {name: result, type: string}
  - {name: notes, type: text, body: true}
`)
	write(t, root, "PRT/PRT-0001.md", "---\nid: PRT-0001\ntype: Part\ntitle: Frame\nstatus: draft\nchecks:\n  - result: pass\n  - result: fail\n---\nNotes.\n")
	write(t, root, "PRT/PRT-0002.md", "---\nid: PRT-0002\ntype: Part\ntitle: Frame\nstatus: approved\nparent: PRT-0001\n---\n")
	for name, src := range rules {
		write(t, root, "rules/"+name, src)
	}
	return root
}

func TestRules(t *testing.T) {
	for _, tc := range []struct{ name, rule, want string }{
		{"a value test at its field",
			"from: Part\nwhere: {field: status, operator: equals, value: draft}\n",
			"PRT/PRT-0001.md:5: error [r] broken (PRT-0001)"},
		{"the last condition decides",
			"from: Part\nwhere:\n  - {field: status, operator: equals, value: draft}\n  - {field: title, operator: exists}\n",
			"PRT/PRT-0001.md:4: error [r] broken (PRT-0001)"},
		{"missing at the id",
			"from: Part\nwhere: [{field: status, operator: equals, value: approved}, {field: owner, operator: missing}]\n",
			"PRT/PRT-0002.md:2: error [r] broken (PRT-0002)"},
		{"an absent field at the id",
			"from: Part\nwhere: {field: owner, operator: not_equals, value: me}\n",
			"PRT/PRT-0001.md:2: error [r] broken (PRT-0001)\nPRT/PRT-0002.md:2: error [r] broken (PRT-0002)"},
		{"the body field at the body",
			"from: Part\nwhere: {field: notes, operator: exists}\n",
			"PRT/PRT-0001.md:10: error [r] broken (PRT-0001)"},
		{"each matching row",
			"from: Part\nwhere: [{field: status, operator: exists}, {field: checks, where: {field: result, operator: exists}}]\n",
			"PRT/PRT-0001.md:7: error [r] broken (PRT-0001)\nPRT/PRT-0001.md:8: error [r] broken (PRT-0001)"},
		{"a sub-query decides over a later value test",
			"from: Part\nwhere: [{field: checks, where: {field: result, operator: equals, value: fail}}, {field: status, operator: exists}]\n",
			"PRT/PRT-0001.md:8: error [r] broken (PRT-0001)"},
		{"a link where it is written",
			"from: Part\nwhere: {field: parent, where: {field: status, operator: equals, value: draft}}\n",
			"PRT/PRT-0002.md:6: error [r] broken (PRT-0002)"},
		{"a facet where the linking entity writes it",
			"from: Part\nwhere: {field: children, where: {field: status, operator: exists}}\n",
			"PRT/PRT-0002.md:6: error [r] broken (PRT-0001)"},
		{"an aggregation at its field, or the id when it's absent",
			"from: Part\nwhere: {field: checks, aggregation: {type: count}, operator: less_than, value: 3}\n",
			"PRT/PRT-0001.md:6: error [r] broken (PRT-0001)\nPRT/PRT-0002.md:2: error [r] broken (PRT-0002)"},
		{"from at the id, naming what it found",
			"from: Part\nwhere:\n  from: Part\n  where: [{field: title, operator: equals, value_of: ^title}, {field: id, operator: not_equals, value_of: ^id}]\n",
			"PRT/PRT-0001.md:2: error [r] broken: PRT-0002 (PRT-0001)\nPRT/PRT-0002.md:2: error [r] broken: PRT-0001 (PRT-0002)"},
		{"any_of at its first alternative that holds",
			"from: Part\nwhere:\n  any_of:\n    - {field: owner, operator: exists}\n    - {field: parent, operator: exists}\n    - {field: status, operator: exists}\n",
			"PRT/PRT-0001.md:5: error [r] broken (PRT-0001)\nPRT/PRT-0002.md:6: error [r] broken (PRT-0002)"},
		{"any_of placing a sub-query by its rows",
			"from: Part\nwhere:\n  any_of:\n    - {field: checks, where: {field: result, operator: equals, value: fail}}\n    - {field: parent, operator: exists}\n",
			"PRT/PRT-0001.md:8: error [r] broken (PRT-0001)\nPRT/PRT-0002.md:6: error [r] broken (PRT-0002)"},
		{"no where: every entity, at its id",
			"from: Part\n",
			"PRT/PRT-0001.md:2: error [r] broken (PRT-0001)\nPRT/PRT-0002.md:2: error [r] broken (PRT-0002)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := ruleRepo(t, map[string]string{"r.yaml": "severity: error\nmessage: broken\n" + tc.rule})
			want := tc.want + "\n"
			got := text(t, root)
			got = got[:strings.LastIndex(strings.TrimSuffix(got, "\n"), "\n")+1] // drop the summary
			if got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestRuleFileProblems(t *testing.T) {
	ok := "severity: warning\nmessage: m\nfrom: Part\nwhere: {field: owner, operator: equals, value: nobody}\n"
	root := ruleRepo(t, map[string]string{
		"fine.yaml":         ok,
		"Bad_Name.yaml":     ok,
		"upper.YAML":        ok,
		"broken-yaml.yaml":  "severity: [\n",
		"not-mapping.yaml":  "- a\n",
		"unknown-keys.yaml": "id: unknown-keys\n" + ok + "render: id\n",
		"no-severity.yaml":  "message: m\nfrom: Part\n",
		"bad-severity.yaml": "severity: fatal\nmessage: m\nfrom: Part\n",
		"no-message.yaml":   "severity: error\nmessage: \"\"\nfrom: Part\n",
		"no-from.yaml":      "severity: error\nmessage: m\nwhere: {field: status, operator: exists}\n",
		"bad-field.yaml":    "severity: error\nmessage: m\nfrom: Part\nwhere:\n  field: state\n  operator: exists\n",
		"two-problems.yaml": "severity: fatal\nmessage: m\nfrom: Prat\n",
	})
	want := `rules/Bad_Name.yaml: error [rules/name] a rule file is named for its ID, in lower-case kebab-case, with .yaml: Bad_Name.yaml is not
rules/bad-field.yaml:5: error [rules/query] Part has no field "state" (fields: id, type, title, status, owner, parent, checks, notes, children, code_blocks)
rules/bad-severity.yaml:1: error [rules/severity] severity is error or warning, not "fatal"
rules/broken-yaml.yaml:1: error [rules/file] invalid YAML: did not find expected node content
rules/no-from.yaml:1: error [rules/query] rule has no from: <type>, from: [<type>, …] or from: any
rules/no-message.yaml:2: error [rules/message] message is the text each violation is reported with
rules/no-severity.yaml:1: error [rules/severity] no severity: error or warning
rules/not-mapping.yaml:1: error [rules/file] a rule file is a mapping with severity, message, from, where
rules/two-problems.yaml:1: error [rules/severity] severity is error or warning, not "fatal"
rules/two-problems.yaml:3: error [rules/query] from: unknown type "Prat" (types: Part)
rules/unknown-keys.yaml:1: error [rules/unknown-key] unknown key "id": a rule's ID is its file name, unknown-keys
rules/unknown-keys.yaml:6: error [rules/unknown-key] unknown key "render" (expected severity, message, from, where)
rules/upper.YAML: error [rules/name] a rule file is named for its ID, in lower-case kebab-case, with .yaml: upper.YAML is not
13 errors, 0 warnings
`
	if got := text(t, root); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
