package validate

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	texttemplate "text/template"

	"github.com/dynamatt/provenance/internal/tmpl"
)

// Report templates (DES-0049): validate --output <name> renders
// templates/reports/<name>.tmpl with the report, through html/template
// when the name ends .html and text/template otherwise. Validate renders
// every one, so a template that does not parse, or names a missing field,
// is a content/template violation; so is one named like a format, which
// --output could never reach.

// ReportsDir is the folder of report templates.
const ReportsDir = "templates/reports"

// Stamps are the git stamps a report template receives, empty outside a
// git repository.
type Stamps struct {
	GitSHA, ContentHash string
	Dirty               bool
}

// UnknownReportError is an output name that is neither a format nor a
// report template.
type UnknownReportError struct {
	Name      string
	Available []string
}

func (e *UnknownReportError) Error() string {
	msg := "unknown report " + strconv.Quote(e.Name) + ": not a format (" + strings.Join(Formats, ", ") +
		") and there is no " + ReportsDir + "/" + e.Name + ".tmpl"
	if len(e.Available) > 0 {
		msg += " (reports: " + strings.Join(e.Available, ", ") + ")"
	}
	return msg
}

// Stamps are the report's git stamps: HEAD, and the content hash with
// whether the working tree differs from it. They need only HEAD, so a
// shallow clone has them too.
func (r *Report) Stamps() (Stamps, error) {
	h, err := r.history()
	if err != nil || h == nil {
		return Stamps{}, err
	}
	return Stamps{GitSHA: h.SHA, ContentHash: h.ContentHash, Dirty: h.Dirty.Any()}, nil
}

// IsFormat reports whether name is one of Formats rather than a report
// template.
func IsFormat(name string) bool { return slices.Contains(Formats, name) }

// CheckOutput returns an *UnknownReportError unless name is a format or a
// report template.
func (r *Report) CheckOutput(name string) error {
	if IsFormat(name) {
		return nil
	}
	names, err := reportNames(r.root)
	if err != nil {
		return err
	}
	if !slices.Contains(names, name) {
		return &UnknownReportError{Name: name, Available: slices.DeleteFunc(names, IsFormat)}
	}
	return nil
}

// WriteOutput writes the output name: a format, or the report template of
// that name rendered with the stamps. toolVersion is the binary's, for
// SARIF. An unknown name is an *UnknownReportError; a template that fails,
// a *tmpl.Error.
func (r *Report) WriteOutput(w io.Writer, name, toolVersion string, st Stamps) error {
	if IsFormat(name) {
		return r.Write(w, name, toolVersion)
	}
	return r.WriteTemplate(w, name, st)
}

// WriteTemplate renders the report template name with the report and
// stamps. An unknown name is an *UnknownReportError; a template that
// fails, a *tmpl.Error.
func (r *Report) WriteTemplate(w io.Writer, name string, st Stamps) error {
	if err := r.CheckOutput(name); err != nil {
		return err
	}
	text, err := readReport(r.root, name)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := r.render(&b, name, text, st); err != nil {
		return err
	}
	_, err = w.Write(b.Bytes())
	return err
}

// checkReports renders every report template, without stamps, and reports
// each that fails or has a format's name.
func (r *Report) checkReports() error {
	names, err := reportNames(r.root)
	if err != nil {
		return err
	}
	for _, name := range names {
		if IsFormat(name) {
			r.add(Violation{Rule: "content/template", Severity: Error, Path: ReportsDir + "/" + name + ".tmpl",
				Message: "a report template cannot be named " + name + ", which --output takes as the " + name + " format"})
			continue
		}
		text, err := readReport(r.root, name)
		if err != nil {
			return err
		}
		var te *tmpl.Error
		if err := r.render(io.Discard, name, text, Stamps{}); errors.As(err, &te) {
			r.add(Violation{Rule: "content/template", Severity: Error, Path: te.Path, Line: te.Line, Message: te.Msg})
		} else if err != nil {
			return err
		}
	}
	return nil
}

// reportNames lists the report templates' names, sorted: each file
// <name>.tmpl in ReportsDir.
func reportNames(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(ReportsDir)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".tmpl"); ok && name != "" && !e.IsDir() {
			names = append(names, name)
		}
	}
	return names, nil
}

func readReport(root, name string) (string, error) {
	return tmpl.Read(filepath.Join(root, filepath.FromSlash(ReportsDir), name+".tmpl"))
}

func (r *Report) render(w io.Writer, name, text string, st Stamps) error {
	path := ReportsDir + "/" + name + ".tmpl"
	funcs := tmpl.ReportFuncs()
	var t interface {
		Execute(io.Writer, any) error
	}
	var err error
	if strings.HasSuffix(name, ".html") {
		t, err = tmpl.HTML(path, text, funcs)
	} else {
		t, err = tmpl.Text(path, text, texttemplate.FuncMap(funcs))
	}
	if err != nil {
		return err
	}
	if err := t.Execute(w, r.templateData(st)); err != nil {
		return tmpl.NewError(err, path)
	}
	return nil
}

// templateData is what a report template receives as ".". It is maps, like
// entity data, so that a misspelt name is an error rather than empty
// output.
//
//	.Component                     .Code, .Name
//	.Violations                    every violation, in the report's order
//	.Rules                         every built-in check, then every rule file
//	.Summary                       .Errors, .Warnings, .RulesSkipped
//	.GitSHA .ContentHash .Dirty    the stamps
func (r *Report) templateData(st Stamps) map[string]any {
	violation := func(v Violation) map[string]any {
		var ent any
		if v.Entity != "" {
			if e, ok := r.entities[v.Entity]; ok {
				ent = e
			} else {
				ent = tmpl.Baseline(v.Entity)
			}
		}
		return map[string]any{"Rule": v.Rule, "Severity": string(v.Severity), "Message": v.Message,
			"Path": v.Path, "Line": v.Line, "Entity": ent}
	}
	all := []map[string]any{}
	by := map[string][]map[string]any{}
	for _, v := range r.Violations {
		m := violation(v)
		all = append(all, m)
		by[v.Rule] = append(by[v.Rule], m)
	}
	rule := func(id string, sev Severity, msg string, builtin bool, path, source, skipped string) map[string]any {
		vs := by[id]
		if vs == nil {
			vs = []map[string]any{}
		}
		return map[string]any{"ID": id, "Severity": string(sev), "Message": msg, "Builtin": builtin,
			"Path": path, "Query": source, "Skipped": skipped, "Violations": vs}
	}
	rules := []map[string]any{}
	for _, c := range Checks {
		rules = append(rules, rule(c.ID, c.Severity, c.Description, true, "", "", ""))
	}
	for _, ru := range r.Rules {
		rules = append(rules, rule(ru.ID, ru.Severity, ru.Message, false, ru.Path, ru.Source, ru.Skipped))
	}
	return map[string]any{
		"Component":  map[string]any{"Code": r.component.Code, "Name": r.component.Name},
		"Violations": all,
		"Rules":      rules,
		"Summary":    map[string]any{"Errors": r.Errors(), "Warnings": r.Warnings(), "RulesSkipped": r.RulesSkipped},
		"GitSHA":     st.GitSHA, "ContentHash": st.ContentHash, "Dirty": st.Dirty,
	}
}
