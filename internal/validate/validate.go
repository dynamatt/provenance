// Package validate runs the checks of provenance validate and reports their
// violations (DES-0048, DES-0049). A problem with the repository's content,
// a file that cannot be loaded included, is a violation; only a problem with
// the tool or its invocation is an error.
package validate

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/export/website"
	"github.com/dynamatt/provenance/internal/load"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/schema"
)

// Severity is how a violation affects the gate: errors fail it, warnings
// are reported without failing it (REQ-0055).
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Violation is one problem, placed at a file and line (DES-0048).
type Violation struct {
	// Rule is a rule's ID, or a built-in check's, e.g. "content/frontmatter".
	Rule     string
	Severity Severity
	Message  string
	// Entity is the ID of the entity the violation is about, or "".
	Entity string
	// Path is slash-separated and relative to the repository root.
	Path string
	// Line is 0 when the problem has no line, such as an empty file.
	Line int
}

// Report is the outcome of validating a repository.
type Report struct {
	// Violations are sorted by path, line, rule and message.
	Violations []Violation
	// RulesSkipped counts the rules not run because the schema they are
	// checked against has errors.
	RulesSkipped int
	// Rules are every rule file, in file name order, run or skipped.
	Rules []RuleInfo

	// For report templates: the repository's root and component, and
	// every entity's template data by ID (nil when the schema has errors).
	root      string
	component repo.Component
	entities  map[string]map[string]any
}

// Errors counts the error-severity violations.
func (r *Report) Errors() int { return r.count(Error) }

// Warnings counts the warning-severity violations.
func (r *Report) Warnings() int { return r.count(Warning) }

func (r *Report) count(s Severity) int {
	n := 0
	for _, v := range r.Violations {
		if v.Severity == s {
			n++
		}
	}
	return n
}

func (r *Report) add(v Violation) { r.Violations = append(r.Violations, v) }

func (r *Report) sort() {
	slices.SortFunc(r.Violations, func(a, b Violation) int {
		return cmp.Or(
			strings.Compare(a.Path, b.Path),
			cmp.Compare(a.Line, b.Line),
			strings.Compare(a.Rule, b.Rule),
			strings.Compare(a.Message, b.Message),
		)
	})
}

// RulesDir is the folder of rule files at the repository root (DES-0016).
const RulesDir = "rules"

// Run validates the repository rp. Every problem is reported, not just the
// first (REQ-0038). err is an I/O error: the repository could not be read.
func Run(rp *repo.Repo) (*Report, error) {
	root := rp.Root
	r := &Report{root: root, component: rp.Component}
	res, err := load.Load(rp)
	if err != nil {
		return nil, err
	}
	r.loadProblems(res)
	if res.SchemaBroken() {
		if r.RulesSkipped, err = r.skipRules(root, "the schema has errors"); err != nil {
			return nil, err
		}
	} else {
		r.schemaChecks(res.Schema, res.Parsed)
		r.conformance(res.Schema, res.Entities)
		if err := r.content(res.Input()); err != nil {
			return nil, err
		}
		if err := r.rules(root, res.Schema, res.Graph); err != nil {
			return nil, err
		}
		r.entities = website.EntityData(res.Schema, res.Entities)
	}
	r.sort()
	// Report templates are checked against everything else found.
	if err := r.checkReports(); err != nil {
		return nil, err
	}
	r.sort()
	return r, nil
}

// loadProblems reports what loading found, each kind as its built-in
// check.
func (r *Report) loadProblems(res *load.Result) {
	type place struct {
		path string
		line int
	}
	dups := map[string][]place{}
	var dupIDs []string
	for _, p := range res.Problems {
		switch p.Kind {
		case load.Frontmatter, load.Partial:
			r.add(Violation{Rule: "content/frontmatter", Severity: Error, Message: p.Err.(*entity.Error).Msg, Path: p.Path, Line: p.Line})
		case load.DuplicateID:
			de := p.Err.(*entity.DuplicateIDError)
			if dups[de.ID] == nil {
				dupIDs = append(dupIDs, de.ID)
				dups[de.ID] = []place{{de.First, de.FirstLine}}
			}
			dups[de.ID] = append(dups[de.ID], place{de.Then, de.ThenLine})
		case load.SchemaFile:
			r.add(Violation{Rule: "schema/file", Severity: Error, Message: p.Err.(*schema.Error).Msg, Path: p.Path, Line: p.Line})
		case load.SchemaKey:
			r.add(Violation{Rule: "schema/unknown-key", Severity: Error, Message: p.Err.(*schema.Error).Msg, Path: p.Path, Line: p.Line})
		case load.UnknownType:
			msg := fmt.Sprintf("type %q is not declared in schema/", p.Err.(*model.UnknownTypeError).Type)
			if names := res.Schema.TypeNames(); len(names) > 0 {
				msg += " (declared: " + strings.Join(names, ", ") + ")"
			}
			r.add(Violation{Rule: "schema/unknown-type", Severity: Error, Message: msg, Entity: p.Entity, Path: p.Path, Line: p.Line})
		}
	}
	// Every file declaring a duplicated ID is reported, naming the others.
	for _, id := range dupIDs {
		copies := dups[id]
		for i, c := range copies {
			var others []string
			for j, o := range copies {
				if j != i {
					others = append(others, o.path)
				}
			}
			r.add(Violation{
				Rule: "content/duplicate-id", Severity: Error, Entity: id, Path: c.path, Line: c.line,
				Message: fmt.Sprintf("duplicate entity ID %s, also in %s", id, strings.Join(others, ", ")),
			})
		}
	}
}

// content reports what would stop the website export, and what it would
// show as a gap, found by rendering the site as an export does
// (website.Exporter.Check).
func (r *Report) content(in *export.Input) error {
	problems, err := website.New().Check(in)
	if err != nil {
		return err
	}
	byPath := map[string]string{}
	for _, e := range in.Entities {
		byPath[e.Path] = e.ID
	}
	for _, p := range problems {
		sev := Error
		if p.Warning {
			sev = Warning
		}
		r.add(Violation{Rule: p.Rule, Severity: sev, Message: p.Msg, Entity: byPath[p.Path], Path: p.Path, Line: p.Line})
	}
	return nil
}
