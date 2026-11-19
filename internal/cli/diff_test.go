package cli

import "testing"

// The refs are read as git diff's.
func TestDiffRefs(t *testing.T) {
	cases := []struct {
		args          []string
		old, new      string
		mergeBase, ok bool
	}{
		{nil, "HEAD", "", false, true},
		{[]string{"main"}, "main", "", false, true},
		{[]string{"v1", "v2"}, "v1", "v2", false, true},
		{[]string{"v1..v2"}, "v1", "v2", false, true},
		{[]string{"main..."}, "main", "HEAD", true, true},
		{[]string{"main...HEAD"}, "main", "HEAD", true, true},
		{[]string{"..v2"}, "HEAD", "v2", false, true},
		{[]string{""}, "", "", false, false},
	}
	for _, c := range cases {
		old, new, mb, err := refs(c.args)
		if (err == nil) != c.ok || old != c.old || new != c.new || mb != c.mergeBase {
			t.Errorf("%q: %q %q %v %v", c.args, old, new, mb, err)
		}
	}
}
