package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/schema"
	"github.com/dynamatt/provenance/internal/yamlnode"
)

// Rule files (DES-0016): rules/<id>.yaml, a query with a severity and a
// message. Every entity the query matches is a violation of the rule,
// placed as D8 says (place). A rule file with a problem is reported at its
// line under rules/… and skipped; the other rules still run.

// rule is a rule file that passed its checks.
type rule struct {
	id, path string
	severity Severity
	message  string
	q        *query.Block
}

// ruleKeys are the keys a rule file may have.
var ruleKeys = []string{"severity", "message", "from", "where"}

// ruleName is a rule ID: lower-case kebab-case.
var ruleName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// rules runs every rule file in rules/ against the graph.
func (r *Report) rules(root string, s *schema.Schema, g *query.Graph) error {
	rules, err := r.loadRules(root, s)
	if err != nil {
		return err
	}
	for _, ru := range rules {
		if err := r.runRule(ru, g); err != nil {
			return err
		}
	}
	return nil
}

// ruleFiles lists the YAML files in root/rules, in name order, as
// repository paths.
func ruleFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, RulesDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".yaml") {
			out = append(out, RulesDir+"/"+e.Name())
		}
	}
	return out, nil
}

// loadRules reads and checks every rule file, reporting the problems of
// each, and returns the ones without problems.
func (r *Report) loadRules(root string, s *schema.Schema) ([]*rule, error) {
	paths, err := ruleFiles(root)
	if err != nil {
		return nil, err
	}
	var out []*rule
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		info := peekRule(path, data)
		if ru := r.checkRule(path, data, s); ru != nil {
			out = append(out, ru)
		} else {
			info.Skipped = "the rule file has errors"
		}
		r.Rules = append(r.Rules, info)
	}
	return out, nil
}

// skipRules lists every rule file as skipped for why, without checking it,
// and returns how many there are.
func (r *Report) skipRules(root, why string) (int, error) {
	paths, err := ruleFiles(root)
	if err != nil {
		return 0, err
	}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return 0, err
		}
		info := peekRule(path, data)
		info.Skipped = why
		r.Rules = append(r.Rules, info)
	}
	return len(paths), nil
}

// peekRule is what a report says about a rule file, read without checking
// it: its ID and text, and its severity and message where they can be
// read.
func peekRule(path string, data []byte) RuleInfo {
	base := filepath.Base(path)
	info := RuleInfo{ID: strings.TrimSuffix(base, filepath.Ext(base)), Path: path,
		Source: strings.ReplaceAll(string(data), "\r\n", "\n")}
	var doc yaml.Node
	if yamlnode.Unmarshal(data, 1, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return info
	}
	m := doc.Content[0]
	if sev := yamlnode.Lookup(m, "severity"); sev != nil && (sev.Value == string(Error) || sev.Value == string(Warning)) {
		info.Severity = Severity(sev.Value)
	}
	if msg := yamlnode.Lookup(m, "message"); msg != nil && msg.Kind == yaml.ScalarNode {
		info.Message = msg.Value
	}
	return info
}

// checkRule checks one rule file, reporting every problem it finds; nil
// when it has any.
func (r *Report) checkRule(path string, data []byte, s *schema.Schema) *rule {
	ok := true
	fail := func(id string, line int, format string, args ...any) {
		ok = false
		r.add(Violation{Rule: id, Severity: Error, Path: path, Line: line, Message: fmt.Sprintf(format, args...)})
	}
	base := filepath.Base(path)
	id, ext := strings.TrimSuffix(base, filepath.Ext(base)), filepath.Ext(base)
	if ext != ".yaml" || !ruleName.MatchString(id) {
		fail("rules/name", 0, "a rule file is named for its ID, in lower-case kebab-case, with .yaml: %s is not", base)
	}

	var doc yaml.Node
	if err := yamlnode.Unmarshal(data, 1, &doc); err != nil {
		line, msg := yamlnode.ErrorLine(err)
		fail("rules/file", line, "invalid YAML: %s", msg)
		return nil
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		fail("rules/file", 1, "a rule file is a mapping with %s", strings.Join(ruleKeys, ", "))
		return nil
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		switch {
		case slices.Contains(ruleKeys, k.Value):
		case k.Value == "id":
			fail("rules/unknown-key", k.Line, "unknown key \"id\": a rule's ID is its file name, %s", id)
		default:
			fail("rules/unknown-key", k.Line, "unknown key %q (expected %s)", k.Value, strings.Join(ruleKeys, ", "))
		}
	}

	ru := &rule{id: id, path: path}
	switch sev := yamlnode.Lookup(m, "severity"); {
	case sev == nil:
		fail("rules/severity", m.Line, "no severity: error or warning")
	case sev.Kind != yaml.ScalarNode || (sev.Value != string(Error) && sev.Value != string(Warning)):
		fail("rules/severity", sev.Line, "severity is error or warning, not %s", nodeText(sev))
	default:
		ru.severity = Severity(sev.Value)
	}
	switch msg := yamlnode.Lookup(m, "message"); {
	case msg == nil:
		fail("rules/message", m.Line, "no message: the text each violation is reported with")
	case msg.Kind != yaml.ScalarNode || msg.Tag != "!!str" || strings.TrimSpace(msg.Value) == "":
		fail("rules/message", msg.Line, "message is the text each violation is reported with")
	default:
		ru.message = msg.Value
	}
	q, err := query.ParseQuery(m, s, "rule")
	if err != nil {
		line, msg := m.Line, err.Error()
		var qe *query.Error
		if errors.As(err, &qe) {
			line, msg = qe.Line, qe.Msg
		}
		fail("rules/query", line, "%s", msg)
	}
	ru.q = q
	if !ok {
		return nil
	}
	return ru
}

// nodeText is a node's value as written, for messages.
func nodeText(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return fmt.Sprintf("%q", n.Value)
	}
	b, err := yaml.Marshal(n)
	if err != nil {
		return "a " + map[yaml.Kind]string{yaml.SequenceNode: "list", yaml.MappingNode: "mapping"}[n.Kind]
	}
	return strings.TrimSpace(string(b))
}

// runRule reports a violation of ru for every entity its query matches,
// at each place place finds.
func (r *Report) runRule(ru *rule, g *query.Graph) error {
	run := &ruleRun{g: g, types: ru.q.From, alts: map[*query.Condition]map[string]query.Match{}}
	ms, err := g.Matches(ru.q.From, ru.q.Where)
	if err != nil {
		return fmt.Errorf("%s: %w", ru.path, err)
	}
	for _, m := range ms {
		spots, err := run.place(m, ru.q.Where)
		if err != nil {
			return fmt.Errorf("%s: %w", ru.path, err)
		}
		seen := map[string]bool{}
		for _, sp := range spots {
			key := fmt.Sprintf("%s:%d", sp.path, sp.line)
			if seen[key] {
				continue
			}
			seen[key] = true
			msg := ru.message
			if len(sp.found) > 0 {
				msg += ": " + strings.Join(sp.found, ", ")
			}
			r.add(Violation{Rule: ru.id, Severity: ru.severity, Message: msg, Entity: m.Entity.ID, Path: sp.path, Line: sp.line})
		}
	}
	return nil
}

// spot is where a violation is placed; found names the entities a
// sub-query on from found, which the message lists.
type spot struct {
	path  string
	line  int
	found []string
}

// ruleRun places one rule's matches. alts caches, for each any_of
// alternative, the entities it matches on its own.
type ruleRun struct {
	g     *query.Graph
	types []*schema.Type
	alts  map[*query.Condition]map[string]query.Match
}

// place says where a match of cond is reported (D8, DES-0048). A match
// through a sub-query is reported once per row or link it matched, at its
// line; a sub-query on from once, at the entity's id, naming the entities
// it found. Otherwise the last condition decides, the one a rule writes
// after the conditions that select what it applies to: a test at its
// field, or at the id when the field is absent or written elsewhere (a
// facet, a missing value); an aggregation at the field it aggregates; an
// any_of at its first alternative that holds, placed by the same rules.
func (run *ruleRun) place(m query.Match, cond *query.Condition) ([]spot, error) {
	e := m.Entity
	at := func(line int) []spot { return []spot{{path: e.Path, line: line}} }
	items := conditions(cond)
	if len(items) == 0 {
		return at(e.IDLine()), nil
	}
	var sub *query.Test
	for _, c := range items {
		if c.Test != nil && c.Test.SubQuery() && c.Test.Aggregation == nil {
			sub = c.Test
		}
	}
	switch {
	case sub != nil && sub.From != nil:
		sp := spot{path: e.Path, line: e.IDLine()}
		for _, it := range m.Items {
			if it.Field == "" && !slices.Contains(sp.found, it.ID) {
				sp.found = append(sp.found, it.ID)
			}
		}
		slices.Sort(sp.found)
		return []spot{sp}, nil
	case sub != nil:
		var out []spot
		for _, it := range m.Items {
			if it.Field == sub.Field.Field {
				line := it.Line
				if line == 0 && it.Path == e.Path {
					line = e.IDLine()
				}
				out = append(out, spot{path: it.Path, line: line})
			}
		}
		if len(out) == 0 {
			return at(e.IDLine()), nil
		}
		return out, nil
	}

	last := items[len(items)-1]
	if last.AnyOf != nil {
		for _, alt := range last.AnyOf {
			am, err := run.alt(alt, e.ID)
			if err != nil {
				return nil, err
			}
			if am != nil {
				return run.place(*am, alt)
			}
		}
		return at(e.IDLine()), nil
	}
	t := last.Test
	if t.From != nil || t.Operator == query.Missing {
		return at(e.IDLine()), nil
	}
	return at(fieldLine(e, t.Field.Field)), nil
}

// alt is the match of alternative cond for the entity id, with the items
// its own sub-queries matched; nil when it does not hold for id.
func (run *ruleRun) alt(cond *query.Condition, id string) (*query.Match, error) {
	byID, ok := run.alts[cond]
	if !ok {
		ms, err := run.g.Matches(run.types, cond)
		if err != nil {
			return nil, err
		}
		byID = map[string]query.Match{}
		for _, m := range ms {
			byID[m.Entity.ID] = m
		}
		run.alts[cond] = byID
	}
	if m, ok := byID[id]; ok {
		return &m, nil
	}
	return nil, nil
}

// conditions lists cond's top-level conditions: a list inside a list is the
// same all-of group.
func conditions(cond *query.Condition) []*query.Condition {
	if cond == nil {
		return nil
	}
	if cond.AllOf == nil {
		return []*query.Condition{cond}
	}
	var out []*query.Condition
	for _, c := range cond.AllOf {
		out = append(out, conditions(c)...)
	}
	return out
}

// fieldLine is the line where e writes the field name: its key in the
// record, or the body's first line for the body field; e's id line when it
// doesn't write it (absent, a facet, a calculated field, id).
func fieldLine(e *model.Entity, name string) int {
	if f := e.Schema.BodyField; f != nil && f.Name == name {
		if v := e.Field(name); v != nil && v.Present {
			return e.BodyFileLine(1)
		}
		return e.IDLine()
	}
	if name != "id" {
		for _, k := range e.Record.Keys() {
			if k.Name == name && k.Set {
				return k.Line
			}
		}
	}
	return e.IDLine()
}
