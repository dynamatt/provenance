package compare

import (
	"fmt"
	"strings"
	"testing"
)

func show(t *Text) string {
	if t == nil {
		return "nil"
	}
	var out []string
	for _, b := range t.Blocks {
		switch b.Op {
		case Changed:
			if b.Code {
				var ls []string
				for _, l := range b.Lines {
					ls = append(ls, map[Op]string{Same: " ", Removed: "-", Added: "+"}[l.Op]+l.Text)
				}
				out = append(out, "~ "+strings.Join(ls, "|"))
				continue
			}
			var ws []string
			for _, w := range b.Words {
				switch w.Op {
				case Removed:
					ws = append(ws, "[-"+w.Text+"-]")
				case Added:
					ws = append(ws, "{+"+w.Text+"+}")
				default:
					ws = append(ws, w.Text)
				}
			}
			out = append(out, "~ "+strings.Join(ws, " "))
		case Removed:
			out = append(out, "- "+b.Old)
		case Added:
			out = append(out, "+ "+b.New)
		default:
			out = append(out, "  "+b.New)
		}
	}
	return strings.Join(out, "\n")
}

func TestCompareText(t *testing.T) {
	cases := []struct{ name, old, new, want string }{
		{"rewrapped", "The system shall\nnot exceed 8 mA.\n", "The system shall not\r\nexceed 8 mA.", "nil"},
		{"a word", "# Title\n\nThe ceiling is 8 mA under\nany fault.\n\n- one\n- two\n", "# Title\n\nThe ceiling is 6 mA under any fault.\n\n- one\n- two\n",
			"  # Title\n~ The ceiling is [-8-] {+6+} mA under any fault.\n  - one\n  - two"},
		{"list item added and one removed", "- one\n- two\n- three\n", "- one\n- three\n- four\n", "  - one\n- - two\n  - three\n+ - four"},
		{"paragraph replaced", "Alpha beta gamma.\n", "Something else entirely here.\n", "- Alpha beta gamma.\n+ Something else entirely here."},
		{"table row", "| a | b |\n|---|---|\n| 1 | 8 mA |\n", "| a | b |\n|---|---|\n| 1 | 6 mA |\n", "  | a | b |\n  |---|---|\n~ | 1 | [-8-] {+6+} mA |"},
		{"code block by line", "```\nx = 1\n\ny = 2\n```\n", "```\nx = 1\n\ny = 3\n```\n", "~  ```| x = 1| |-y = 2|+y = 3| ```"},
	}
	for _, c := range cases {
		if got := show(CompareText(c.old, c.new)); got != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
}

// A long text compares in reasonable time, and two texts with nothing in
// common past the search bound read as one replaced by the other.
func TestCompareLongText(t *testing.T) {
	var a, b strings.Builder
	for i := range 6000 {
		fmt.Fprintf(&a, "a%d\n\n", i)
		fmt.Fprintf(&b, "b%d\n\n", i)
	}
	got := CompareText(a.String(), b.String())
	if got == nil || len(got.Blocks) != 12000 || got.Blocks[0].Op != Removed || got.Blocks[11999].Op != Added {
		t.Fatalf("got %d blocks", len(got.Blocks))
	}
}
