// Package website is the static DHF website exporter (Requirements Spec §7).
// Every link in the output is relative, so the site works from any host path
// or straight from disk, with no network access.
package website

import (
	"bytes"
	"embed"
	"html/template"

	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/repo"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

//go:embed assets/style.css
var styleCSS []byte

// Exporter renders the website format.
type Exporter struct{}

func New() *Exporter { return &Exporter{} }

// page is the data every page template receives.
type page struct {
	// Root is the relative path from the page to the site root ("" or "../").
	Root      string
	Title     string
	Component repo.Component
	// Data is the page-specific content.
	Data any
}

func (e *Exporter) Export(in *export.Input) (export.Files, error) {
	s := &site{component: in.Repo.Component, files: export.Files{"style.css": styleCSS}}
	if err := s.render("index.html", "index.tmpl", "", in.Repo.Component.Name, nil); err != nil {
		return nil, err
	}
	return s.files, nil
}

type site struct {
	component repo.Component
	files     export.Files
}

// render executes templates/<tmpl> inside the base layout and stores the
// result at path.
func (s *site) render(path, tmpl, root, title string, data any) error {
	t, err := template.ParseFS(templateFS, "templates/base.tmpl", "templates/"+tmpl)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	p := page{Root: root, Title: title, Component: s.component, Data: data}
	if err := t.ExecuteTemplate(&b, "base", p); err != nil {
		return err
	}
	s.files[path] = b.Bytes()
	return nil
}
