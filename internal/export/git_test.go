package export

import (
	"testing"

	"github.com/dynamatt/provenance/internal/history"
)

func TestGitPage(t *testing.T) {
	a := "1111111111111111111111111111111111111111"
	b := "2222222222222222222222222222222222222222"
	log := &history.Log{Commits: []*history.Commit{
		{SHA: b, Paths: []string{"DOC/DOC-1.md"}},
		{SHA: a, Paths: []string{"DOC/DOC-1.md", "REQ/REQ-1.md"}, Types: []string{"Requirement"}},
	}}
	clean := &Git{Log: log}
	dirty := &Git{Log: log, Dirty: &history.Dirty{Paths: []string{"REQ/REQ-1.md", "REQ/REQ-2.md"}, Types: []string{"Requirement"}}}
	for _, tc := range []struct {
		name string
		git  *Git
		in   history.Inputs
		last string
		revs int
	}{
		{"outside git", nil, history.Inputs{Paths: []string{"DOC/DOC-1.md"}}, "", 0},
		{"newest change", clean, history.Inputs{Paths: []string{"DOC/DOC-1.md"}}, b, 2},
		{"older change", clean, history.Inputs{Paths: []string{"REQ/REQ-1.md"}}, a, 1},
		{"by type", clean, history.Inputs{Types: []string{"Requirement"}}, a, 1},
		{"never committed", clean, history.Inputs{Paths: []string{"REQ/REQ-2.md"}}, "", 0},
		{"changed since", dirty, history.Inputs{Paths: []string{"REQ/REQ-1.md"}}, a + "-dirty", 1},
		{"new and uncommitted", dirty, history.Inputs{Paths: []string{"REQ/REQ-2.md"}}, "uncommitted", 0},
		{"unaffected by the working tree", dirty, history.Inputs{Paths: []string{"DOC/DOC-1.md"}}, b, 2},
	} {
		last, revs := tc.git.Page(tc.in)
		if last != tc.last || len(revs) != tc.revs {
			t.Errorf("%s: got %q with %d revisions, want %q with %d", tc.name, last, len(revs), tc.last, tc.revs)
		}
	}
}
