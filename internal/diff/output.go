package diff

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"slices"
	"strings"
	texttemplate "text/template"

	"github.com/dynamatt/provenance/internal/tmpl"
)

//go:embed templates
var builtins embed.FS

// indented is a field with the indent its lines start with, for the text
// output's recursion into list rows.
type indented struct {
	Indent string
	Field  *Field
}

// textFuncs are the functions the text output and a project's diff
// templates have, besides the link functions of the html output.
func textFuncs() texttemplate.FuncMap {
	return texttemplate.FuncMap(funcs())
}

func htmlFuncs() htmltemplate.FuncMap {
	return htmltemplate.FuncMap(funcs())
}

func funcs() map[string]any {
	return map[string]any{
		"at":      func(indent string, f *Field) indented { return indented{indent, f} },
		"join":    func(s []string) string { return strings.Join(s, ", ") },
		"lines":   func(s string) []string { return strings.Split(s, "\n") },
		"sides":   func(d *Diff) []*Side { return []*Side{d.Old, d.New} },
		"summary": Summary,
		"count":   func(d *Diff, op string) int { return d.Count(op) },
		"add":     func(a, b int) int { return a + b },
		"plural":  plural,
		"pageOnly": func(d *Diff) int {
			n := 0
			for _, p := range d.Pages {
				if p.Op == PageOnly {
					n++
				}
			}
			return n
		},
		"hasPage": func(d *Diff, name string) bool {
			for _, p := range d.Pages {
				if p.Name == name {
					return true
				}
			}
			return false
		},
	}
}

// Summary is the diff in one line: how many entities differ, by how, and
// how many other files.
func Summary(d *Diff) string {
	var parts []string
	for _, c := range []struct{ op, label string }{
		{string(Changed), "changed"}, {"derived", "changed by derivation"}, {string(Added), "added"},
		{string(Removed), "removed"}, {string(Retyped), "retyped"},
	} {
		if n := d.Count(c.op); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, c.label))
		}
	}
	var out []string
	if len(parts) > 0 {
		out = append(out, plural(len(d.Entities), "entity", "entities")+": "+strings.Join(parts, ", "))
	}
	if n := len(d.Files); n > 0 {
		out = append(out, plural(n, "other file", "other files")+" changed")
	}
	pages := 0
	for _, p := range d.Pages {
		if p.Op == PageOnly {
			pages++
		}
	}
	if pages > 0 {
		out = append(out, plural(pages, "page", "pages")+" changed by another change")
	}
	if len(out) == 0 {
		return "no changes"
	}
	return strings.Join(out, "; ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// TemplatesDir holds a project's diff templates (DES-0057).
const TemplatesDir = "templates/diff"

// UnknownOutputError is an --output that is neither built in nor a
// template of the project's.
type UnknownOutputError struct {
	Name      string
	Available []string
}

func (e *UnknownOutputError) Error() string {
	return fmt.Sprintf("--output %s: no such output (%s)", e.Name, strings.Join(e.Available, ", "))
}

// Outputs are the outputs available: text and html, then the new side's
// templates/diff/<name>.tmpl, by name.
func (d *Diff) Outputs() ([]string, error) {
	out := []string{"text", "html"}
	entries, err := fs.ReadDir(d.New.Files(), TemplatesDir)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".tmpl"); ok && name != "" && !e.IsDir() && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out, nil
}

// NeedsPages reports whether output name shows rendered pages: html, and
// a template whose name ends .html, so Render must compare them first.
func NeedsPages(name string) bool { return name == "html" || strings.HasSuffix(name, ".html") }

// Output renders output name: the new side's templates/diff/<name>.tmpl,
// which replaces a built-in of the same name, or the built-in text or
// html. A template is Go's html/template when its name is html or ends
// .html, text/template otherwise, and receives the Diff as ".". A
// template that fails is a *tmpl.Error at its file and line.
func (d *Diff) Output(name string) ([]byte, error) {
	path := TemplatesDir + "/" + name + ".tmpl"
	text, err := tmpl.ReadFS(d.New.Files(), path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if name != "text" && name != "html" {
			available, err := d.Outputs()
			if err != nil {
				return nil, err
			}
			return nil, &UnknownOutputError{Name: name, Available: available}
		}
		path = "built-in " + name + ".tmpl"
		b, err := builtins.ReadFile("templates/" + name + ".tmpl")
		if err != nil {
			return nil, err
		}
		text = string(b)
	case err != nil:
		return nil, err
	}
	var t interface {
		Execute(io.Writer, any) error
	}
	if NeedsPages(name) {
		t, err = tmpl.HTML(path, text, htmlFuncs())
	} else {
		t, err = tmpl.Text(path, text, textFuncs())
	}
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, d); err != nil {
		return nil, tmpl.NewError(err, path)
	}
	return b.Bytes(), nil
}

// Text is the text output.
func (d *Diff) Text() ([]byte, error) { return d.Output("text") }
