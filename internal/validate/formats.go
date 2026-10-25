package validate

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// The machine-readable reports (DES-0049). Each lists every violation in
// the report's order, with no timestamps or absolute paths, so a commit
// always gives the same bytes. Only the exit code is guaranteed; the field
// names are kept within a format's version.

// textLine is a violation as the text report writes it.
func textLine(v Violation) string {
	loc := v.Path
	if v.Line > 0 {
		loc = fmt.Sprintf("%s:%d", v.Path, v.Line)
	}
	line := fmt.Sprintf("%s: %s [%s] %s", loc, v.Severity, v.Rule, v.Message)
	if v.Entity != "" {
		line += " (" + v.Entity + ")"
	}
	return line
}

// WriteJSON writes the json report: version 1, every violation with all of
// its fields ("" and 0 when it has no entity or line), and the summary.
func (r *Report) WriteJSON(w io.Writer) error {
	// violation is Violation with the json field names.
	type violation struct {
		Rule     string   `json:"rule"`
		Severity Severity `json:"severity"`
		Message  string   `json:"message"`
		Entity   string   `json:"entity"`
		Path     string   `json:"path"`
		Line     int      `json:"line"`
	}
	type summary struct {
		Errors       int `json:"errors"`
		Warnings     int `json:"warnings"`
		RulesSkipped int `json:"rules_skipped"`
	}
	out := struct {
		Version    int         `json:"version"`
		Violations []violation `json:"violations"`
		Summary    summary     `json:"summary"`
	}{Version: 1, Violations: []violation{}, Summary: summary{r.Errors(), r.Warnings(), r.RulesSkipped}}
	for _, v := range r.Violations {
		out.Violations = append(out.Violations, violation(v))
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// byRule groups the violations by rule ID, keeping their order.
func (r *Report) byRule() map[string][]Violation {
	out := map[string][]Violation{}
	for _, v := range r.Violations {
		out[v.Rule] = append(out[v.Rule], v)
	}
	return out
}

// WriteJUnit writes the junit report: a test suite of the built-in checks
// and one of the rules, a test case for each, passed or not. Each error is
// a failure of its case; warnings go to its system-out, so they don't fail
// it. A rule that was not run is skipped. It conforms to Jenkins'
// junit-10.xsd (testdata/schemas), which GitLab's and GitHub's JUnit
// readers accept.
func (r *Report) WriteJUnit(w io.Writer) error {
	type failure struct {
		Type    string `xml:"type,attr"`
		Message string `xml:"message,attr"`
		Text    string `xml:",chardata"`
	}
	type skipped struct {
		Message string `xml:"message,attr"`
	}
	type testcase struct {
		Name      string    `xml:"name,attr"`
		Classname string    `xml:"classname,attr"`
		Skipped   *skipped  `xml:"skipped,omitempty"`
		Failures  []failure `xml:"failure"`
		SystemOut string    `xml:"system-out,omitempty"`
	}
	type testsuite struct {
		Name     string     `xml:"name,attr"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Errors   int        `xml:"errors,attr"`
		Skipped  int        `xml:"skipped,attr"`
		Cases    []testcase `xml:"testcase"`
	}
	type testsuites struct {
		XMLName  xml.Name    `xml:"testsuites"`
		Name     string      `xml:"name,attr"`
		Tests    int         `xml:"tests,attr"`
		Failures int         `xml:"failures,attr"`
		Errors   int         `xml:"errors,attr"`
		Suites   []testsuite `xml:"testsuite"`
	}
	by := r.byRule()
	cases := func(class string, ids []string, skip map[string]string) testsuite {
		ts := testsuite{Name: class}
		for _, id := range ids {
			tc := testcase{Name: id, Classname: class}
			var out []string
			for _, v := range by[id] {
				if v.Severity == Error {
					tc.Failures = append(tc.Failures, failure{Type: string(Error), Message: v.Message, Text: textLine(v)})
				} else {
					out = append(out, textLine(v))
				}
			}
			if len(out) > 0 {
				tc.SystemOut = strings.Join(out, "\n") + "\n"
			}
			if why := skip[id]; why != "" {
				tc.Skipped = &skipped{Message: why}
				ts.Skipped++
			}
			if len(tc.Failures) > 0 {
				ts.Failures++
			}
			ts.Tests++
			ts.Cases = append(ts.Cases, tc)
		}
		return ts
	}
	var builtin []string
	for _, c := range Checks {
		builtin = append(builtin, c.ID)
	}
	var rules []string
	skip := map[string]string{}
	for _, ru := range r.Rules {
		rules = append(rules, ru.ID)
		skip[ru.ID] = ru.Skipped
	}
	doc := testsuites{Name: "provenance validate", Suites: []testsuite{cases("built-in", builtin, nil), cases("rules", rules, skip)}}
	for _, ts := range doc.Suites {
		doc.Tests += ts.Tests
		doc.Failures += ts.Failures
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// WriteSARIF writes the sarif report: SARIF 2.1.0, one run of the tool
// version given, a rules entry for every built-in check and rule file, and
// a result for every violation, located relative to the repository root
// (%SRCROOT%).
func (r *Report) WriteSARIF(w io.Writer, toolVersion string) error {
	type text struct {
		Text string `json:"text"`
	}
	type config struct {
		Level string `json:"level"`
	}
	type descriptor struct {
		ID               string  `json:"id"`
		ShortDescription text    `json:"shortDescription"`
		Default          *config `json:"defaultConfiguration,omitempty"`
	}
	type region struct {
		StartLine int `json:"startLine"`
	}
	type artifact struct {
		URI       string `json:"uri"`
		URIBaseID string `json:"uriBaseId"`
	}
	type physical struct {
		ArtifactLocation artifact `json:"artifactLocation"`
		Region           *region  `json:"region,omitempty"`
	}
	type location struct {
		PhysicalLocation physical `json:"physicalLocation"`
	}
	type result struct {
		RuleID     string            `json:"ruleId"`
		RuleIndex  int               `json:"ruleIndex"`
		Level      string            `json:"level"`
		Message    text              `json:"message"`
		Locations  []location        `json:"locations"`
		Properties map[string]string `json:"properties,omitempty"`
	}
	type driver struct {
		Name           string       `json:"name"`
		Version        string       `json:"version"`
		InformationURI string       `json:"informationUri"`
		Rules          []descriptor `json:"rules"`
	}
	type baseID struct {
		Description text `json:"description"`
	}
	type run struct {
		Tool struct {
			Driver driver `json:"driver"`
		} `json:"tool"`
		OriginalURIBaseIDs map[string]baseID `json:"originalUriBaseIds"`
		Results            []result          `json:"results"`
	}

	d := driver{Name: "provenance", Version: toolVersion, InformationURI: "https://github.com/dynamatt/provenance"}
	index := map[string]int{}
	add := func(id, desc string, sev Severity) {
		index[id] = len(d.Rules)
		desc = strings.TrimSpace(desc)
		if desc == "" {
			desc = id
		}
		dc := descriptor{ID: id, ShortDescription: text{desc}}
		if sev != "" {
			dc.Default = &config{Level: string(sev)}
		}
		d.Rules = append(d.Rules, dc)
	}
	for _, c := range Checks {
		add(c.ID, c.Description, c.Severity)
	}
	for _, ru := range r.Rules {
		add(ru.ID, ru.Message, ru.Severity)
	}
	ru := run{OriginalURIBaseIDs: map[string]baseID{"%SRCROOT%": {text{"The repository root."}}}, Results: []result{}}
	ru.Tool.Driver = d
	for _, v := range r.Violations {
		i, ok := index[v.Rule]
		if !ok {
			return fmt.Errorf("violation of %s, which is no check or rule", v.Rule)
		}
		loc := physical{ArtifactLocation: artifact{URI: (&url.URL{Path: v.Path}).EscapedPath(), URIBaseID: "%SRCROOT%"}}
		if v.Line > 0 {
			loc.Region = &region{StartLine: v.Line}
		}
		res := result{RuleID: v.Rule, RuleIndex: i, Level: string(v.Severity), Message: text{v.Message}, Locations: []location{{loc}}}
		if v.Entity != "" {
			res.Properties = map[string]string{"entity": v.Entity}
		}
		ru.Results = append(ru.Results, res)
	}
	out := struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []run  `json:"runs"`
	}{"https://json.schemastore.org/sarif-2.1.0.json", "2.1.0", []run{ru}}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Formats are the built-in report formats --output takes.
var Formats = []string{"text", "json", "junit", "sarif"}

// Write writes the report in format, one of Formats; toolVersion is the
// binary's, for SARIF.
func (r *Report) Write(w io.Writer, format, toolVersion string) error {
	switch format {
	case "text":
		return r.WriteText(w)
	case "json":
		return r.WriteJSON(w)
	case "junit":
		return r.WriteJUnit(w)
	case "sarif":
		return r.WriteSARIF(w, toolVersion)
	}
	return fmt.Errorf("unknown report format %q (formats: %s)", format, strings.Join(Formats, ", "))
}
