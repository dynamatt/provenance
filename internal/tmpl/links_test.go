package tmpl

import (
	"testing"

	"github.com/dynamatt/provenance/internal/schema"
)

// A report links a row whose file's rows have no pages to its row on the
// file's table page (DES-0053), knowing it only by its data.
func TestLinkerNoPages(t *testing.T) {
	summary := &schema.Field{Name: "summary", Kind: schema.String, Title: true}
	s := &schema.Schema{Types: map[string]*schema.Type{"Bug": {Name: "Bug", TitleField: summary, Fields: []*schema.Field{summary}}}}
	l := Linker{Base: "entities/", NoPages: map[string]bool{"Bug": true}, Schema: s}
	bug := Data{"ID": "BUG-7", "Type": "Bug", "summary": "Crash", "Resolved": true}
	req := Data{"ID": "REQ-0001", "Type": "Requirement", "Resolved": true}
	for _, c := range []struct {
		ref  Data
		want string
	}{{bug, "entities/Bug.html#BUG-7"}, {req, "entities/REQ-0001.html"}} {
		if got, err := l.Href(c.ref); err != nil || got != c.want {
			t.Errorf("href %v: %q %v, want %q", c.ref["ID"], got, err, c.want)
		}
	}
	if got, _ := l.Link(bug); got != `<a class="ref" href="entities/Bug.html#BUG-7" title="Crash">BUG-7</a>` {
		t.Errorf("link: %s", got)
	}
	// The title function reads the title: true field; a type without one
	// has no title.
	if got, _ := l.Title(bug); got != "Crash" {
		t.Errorf("title: %q", got)
	}
	if got, _ := l.Title(req); got != "" {
		t.Errorf("title of a type without a title field: %q", got)
	}
}

// Every entity's .File has the same keys, whatever its source.
func TestFileData(t *testing.T) {
	b := Baseline("X-1")["File"].(Data)
	md := FileData("REQ/REQ-0001.md", 2)
	for k := range md {
		if _, ok := b[k]; !ok {
			t.Errorf("baseline .File lacks %s", k)
		}
	}
	if len(b) != len(md) || md["Path"] != "REQ/REQ-0001.md" || md["Line"] != 2 {
		t.Errorf("file data: %v, baseline %v", md, b)
	}
}
