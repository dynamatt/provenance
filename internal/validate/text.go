package validate

import (
	"fmt"
	"io"
)

// WriteText writes the text report (DES-0049): one line per violation,
//
//	path:line: severity [rule] message (ENTITY-ID)
//
// then a line when rules were skipped, then the summary.
func (r *Report) WriteText(w io.Writer) error {
	for _, v := range r.Violations {
		loc := v.Path
		if v.Line > 0 {
			loc = fmt.Sprintf("%s:%d", v.Path, v.Line)
		}
		line := fmt.Sprintf("%s: %s [%s] %s", loc, v.Severity, v.Rule, v.Message)
		if v.Entity != "" {
			line += " (" + v.Entity + ")"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	if r.RulesSkipped > 0 {
		if _, err := fmt.Fprintf(w, "%s not run: the schema has errors\n", plural(r.RulesSkipped, "rule")); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, r.Summary())
	return err
}

// Summary is the report's last line, e.g. "2 errors, 1 warning".
func (r *Report) Summary() string {
	return plural(r.Errors(), "error") + ", " + plural(r.Warnings(), "warning")
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
