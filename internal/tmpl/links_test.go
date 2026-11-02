package tmpl

import (
	"testing"
)

// A report links a row whose file's rows have no pages to its row on the
// file's table page (DES-0053), knowing it only by its data.
func TestLinkerNoPages(t *testing.T) {
	l := Linker{Base: "entities/", NoPages: map[string]bool{"Bug": true}}
	bug := Data{"ID": "BUG-7", "Type": "Bug", "Title": "Crash", "Resolved": true}
	req := Data{"ID": "REQ-0001", "Type": "Requirement", "Title": "R", "Resolved": true}
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
}

// Every entity's .File has the same keys, whatever its source.
func TestFileData(t *testing.T) {
	b := Baseline("X-1")["File"].(Data)
	md := FileData("REQ/REQ-0001.md", 2, nil)
	for k := range md {
		if _, ok := b[k]; !ok {
			t.Errorf("baseline .File lacks %s", k)
		}
	}
	if len(b) != len(md) || md["System"] != "" || md["Line"] != 2 {
		t.Errorf("file data: %v, baseline %v", md, b)
	}
}
