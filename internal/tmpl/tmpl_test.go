package tmpl

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.tmpl")
	if err := os.WriteFile(p, []byte("a\r\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(p); err != nil || got != "a\nb\n" {
		t.Errorf("Read = %q, %v", got, err)
	}
}

// Parse and execution errors, from either package, are located at the
// file, line and column Go names, with Go's prefix gone; a misspelt field
// is one of them.
func TestErrors(t *testing.T) {
	data := map[string]any{"Title": "x"}
	_, parseErr := Text("t/parse.tmpl", "one\n{{.Title}\n", nil)
	h, err := HTML("t/field.html.tmpl", "<p>\n{{.Nope}}</p>\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	execErr := NewError(h.Execute(&strings.Builder{}, data), "t/field.html.tmpl")
	for _, tc := range []struct {
		err  error
		want Error
		text string
	}{
		{parseErr, Error{Path: "t/parse.tmpl", Line: 2, Msg: "bad character U+007D '}'"}, "t/parse.tmpl:2: bad character U+007D '}'"},
		{execErr, Error{Path: "t/field.html.tmpl", Line: 2, Col: 2, Msg: `executing "t/field.html.tmpl" at <.Nope>: map has no entry for key "Nope"`},
			`t/field.html.tmpl:2:2: executing "t/field.html.tmpl" at <.Nope>: map has no entry for key "Nope"`},
		{NewError(errors.New("html/template: no files named"), "t/x.tmpl"), Error{Path: "t/x.tmpl", Msg: "no files named"}, "t/x.tmpl: no files named"},
		{NewError(errors.New("no location"), ""), Error{Msg: "no location"}, "no location"},
	} {
		var e *Error
		if !errors.As(tc.err, &e) || *e != tc.want || e.Error() != tc.text {
			t.Errorf("got %#v (%v), want %#v (%s)", e, tc.err, tc.want, tc.text)
		}
	}
}
