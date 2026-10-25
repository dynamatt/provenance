// Package tmpl is what project templates share, whether they render the
// website (DES-0030) or a validation report (DES-0049): reading them,
// parsing them so a misspelt field is an error, and locating their errors.
package tmpl

import (
	htmltemplate "html/template"
	"os"
	"regexp"
	"strconv"
	"strings"
	texttemplate "text/template"
)

// Read reads a template file with LF line endings, so a Windows
// core.autocrlf checkout renders the same bytes.
func Read(path string) (string, error) {
	b, err := os.ReadFile(path)
	return strings.ReplaceAll(string(b), "\r\n", "\n"), err
}

// HTML parses text as an html/template named name. Missing map keys are
// errors, so a misspelt field fails instead of rendering as empty.
func HTML(name, text string, funcs htmltemplate.FuncMap) (*htmltemplate.Template, error) {
	t, err := htmltemplate.New(name).Option("missingkey=error").Funcs(funcs).Parse(text)
	if err != nil {
		return nil, NewError(err, name)
	}
	return t, nil
}

// Text parses text as a text/template named name, which escapes nothing.
// Missing map keys are errors, as for HTML.
func Text(name, text string, funcs texttemplate.FuncMap) (*texttemplate.Template, error) {
	t, err := texttemplate.New(name).Option("missingkey=error").Funcs(funcs).Parse(text)
	if err != nil {
		return nil, NewError(err, name)
	}
	return t, nil
}

// Error is a template that failed to parse or render, at its own location:
// templates/Requirement.tmpl:5:12: …. Line and Col are 0 when Go gives no
// location.
type Error struct {
	Path      string
	Line, Col int
	Msg       string
}

func (e *Error) Error() string {
	switch {
	case e.Line > 0 && e.Col > 0:
		return e.Path + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Col) + ": " + e.Msg
	case e.Line > 0:
		return e.Path + ":" + strconv.Itoa(e.Line) + ": " + e.Msg
	case e.Path != "":
		return e.Path + ": " + e.Msg
	}
	return e.Msg
}

// location splits a template error's location from its message, once Go's
// "template: " or "html/template:" prefix is gone.
var location = regexp.MustCompile(`^([^\s:]+):(\d+)(?::(\d+))?: (.*)$`)

// NewError locates err, an error from parsing or executing a template, at
// the file and line Go names; one Go gives no location for is at path.
func NewError(err error, path string) *Error {
	msg := err.Error()
	for _, prefix := range []string{"template: ", "html/template:"} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	msg = strings.TrimLeft(msg, " ")
	m := location.FindStringSubmatch(msg)
	if m == nil {
		return &Error{Path: path, Msg: msg}
	}
	line, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])
	return &Error{Path: m[1], Line: line, Col: col, Msg: m[4]}
}
