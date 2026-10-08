package website

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/dynamatt/provenance/internal/query"
	"github.com/dynamatt/provenance/internal/schema"
)

// Project templates (DES-0031) live in the repository's templates/
// folder. Each replaces its built-in counterpart independently:
//
//	templates/<TypeName>.tmpl   one entity type's rendering
//	templates/<name>.tmpl       a named presentation template, used where a
//	                            query block chooses it; <name> is lower-case
//	                            kebab-case (query.IsTemplateName)
//	templates/_layout.tmpl      the layout wrapping every page
//	templates/_index.tmpl       the site's main page
//	templates/_cite.tmpl        an inline citation (DES-0046)
//	templates/style.css         the stylesheet
//
// Other files there (a README, drafts) are ignored, as is a type template
// naming no declared type; validate reports those.
const templatesDir = "templates"

// source is one template's text, named as errors should show it.
type source struct {
	name string // e.g. "templates/_layout.tmpl"; built-ins are "built-in <file>"
	text string
}

type templateSet struct {
	layout, index source
	types         map[string]source // project type templates by type name
	named         map[string]source // named presentation templates by name
	// cite renders inline citations; nil without a project _cite.tmpl,
	// which has no built-in counterpart.
	cite  *source
	style []byte
	// Project reports whether any project template or stylesheet was found.
	project bool
}

func builtin(file string) source {
	b, err := templateFS.ReadFile("templates/" + file)
	if err != nil {
		panic(err) // embedded at build time
	}
	return source{name: "built-in " + file, text: string(b)}
}

// loadTemplates reads the project's templates/ over the built-in defaults
// and parses every one, so a broken template fails the export even when no
// page would use it.
func loadTemplates(root string, s *schema.Schema) (*templateSet, error) {
	ts := &templateSet{
		layout: builtin("layout.tmpl"),
		index:  builtin("index.tmpl"),
		types:  map[string]source{},
		named:  map[string]source{},
		style:  styleCSS,
	}
	dir := filepath.Join(root, templatesDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return ts, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		rel := templatesDir + "/" + name
		// Line endings are normalized to LF, like entity bodies, so a
		// Windows core.autocrlf checkout exports the same bytes.
		read := func() (string, error) {
			b, err := os.ReadFile(filepath.Join(dir, name))
			return strings.ReplaceAll(string(b), "\r\n", "\n"), err
		}
		switch {
		case name == "style.css":
			text, err := read()
			if err != nil {
				return nil, err
			}
			ts.style, ts.project = []byte(text), true
		case name == "_cite.tmpl":
			text, err := read()
			if err != nil {
				return nil, err
			}
			// A citation is inline: the file's final line break is not
			// part of it, so it does not put a space before punctuation.
			ts.cite, ts.project = &source{rel, strings.TrimSuffix(text, "\n")}, true
		case name == "_layout.tmpl" || name == "_index.tmpl":
			text, err := read()
			if err != nil {
				return nil, err
			}
			if name == "_layout.tmpl" {
				ts.layout = source{rel, text}
			} else {
				ts.index = source{rel, text}
			}
			ts.project = true
		case strings.HasSuffix(name, ".tmpl") && s.Types[strings.TrimSuffix(name, ".tmpl")] != nil:
			text, err := read()
			if err != nil {
				return nil, err
			}
			ts.types[strings.TrimSuffix(name, ".tmpl")] = source{rel, text}
			ts.project = true
		case strings.HasSuffix(name, ".tmpl") && query.IsTemplateName(strings.TrimSuffix(name, ".tmpl")):
			text, err := read()
			if err != nil {
				return nil, err
			}
			ts.named[strings.TrimSuffix(name, ".tmpl")] = source{rel, text}
			ts.project = true
		}
	}
	// Parse everything now for early, file-named errors.
	check := []source{ts.layout, ts.index}
	if ts.cite != nil {
		check = append(check, *ts.cite)
	}
	for _, name := range slices.Sorted(maps.Keys(ts.types)) {
		check = append(check, ts.types[name])
	}
	for _, name := range slices.Sorted(maps.Keys(ts.named)) {
		check = append(check, ts.named[name])
	}
	for _, src := range check {
		if _, err := parse(src, placeholderFuncs); err != nil {
			return nil, err
		}
	}
	return ts, nil
}

// unknownNamed explains a named template that does not exist.
func (ts *templateSet) unknownNamed(name string) string {
	msg := fmt.Sprintf("unknown template %q: there is no %s/%s.tmpl", name, templatesDir, name)
	if names := slices.Sorted(maps.Keys(ts.named)); len(names) > 0 {
		msg += " (named templates: " + strings.Join(names, ", ") + ")"
	}
	return msg
}

// Functions available to project templates. They are bound per render (links
// are relative to the page, markdown knows the embed chain); the
// placeholders only let templates parse ahead of rendering.
var placeholderFuncs = template.FuncMap{
	"markdown": func(any) (template.HTML, error) { return "", nil },
	"link":     func(any, ...any) (template.HTML, error) { return "", nil },
	"href":     func(any) (string, error) { return "", nil },
	"short":    func(string) string { return "" },
}

// parse parses src as a template set whose main template has src's name.
// Missing map keys are errors, so a misspelt field fails the export instead
// of rendering as empty.
func parse(src source, fm template.FuncMap) (*template.Template, error) {
	t, err := template.New(src.name).Option("missingkey=error").Funcs(fm).Parse(src.text)
	if err != nil {
		return nil, templateError(err)
	}
	return t, nil
}

// TemplateError is a template that failed to parse or render, reported at
// its own location: templates/Requirement.tmpl:5:12: …
type TemplateError struct{ Msg string }

func (e *TemplateError) Error() string { return e.Msg }

// templateError reports the innermost failure. An error raised while
// rendering an embed passes up through every enclosing template; the one the
// author must fix is where it started, not where it surfaced.
func templateError(err error) error {
	var cycle *EmbedCycleError
	if errors.As(err, &cycle) {
		return cycle
	}
	var qe *QueryError
	if errors.As(err, &qe) {
		return qe
	}
	var ce *ContentError
	if errors.As(err, &ce) {
		return ce
	}
	var inner *TemplateError
	if errors.As(err, &inner) {
		return inner
	}
	msg := err.Error()
	if rest, ok := strings.CutPrefix(msg, "template: "); ok {
		msg = rest
	}
	return &TemplateError{Msg: msg}
}
