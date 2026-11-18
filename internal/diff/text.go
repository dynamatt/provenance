package diff

import (
	"regexp"
	"strings"
)

// Op says how a block, a word or a row differs between the two sides.
type Op string

const (
	Same    Op = "same"
	Added   Op = "added"
	Removed Op = "removed"
	// Changed is a block or row on both sides whose words or cells differ.
	Changed Op = "changed"
)

// Text is a text value, a body or a text field, compared by Markdown block
// and a changed block word by word (DES-0055). Blocks are every block of
// both sides, in order, so an output can show the unchanged ones around a
// change or fold them.
type Text struct {
	Blocks []*Block
}

// Changed counts the blocks that differ.
func (t *Text) Changed() int {
	n := 0
	for _, b := range t.Blocks {
		if b.Op != Same {
			n++
		}
	}
	return n
}

// Block is one Markdown block: a heading, a paragraph, a list item, a
// table row or a fenced block.
type Block struct {
	Op Op
	// Old and New are the block as each side writes it, its lines joined
	// by single spaces (a fenced block as written); Old is "" for an
	// added block, New for a removed one.
	Old, New string
	// Words are a changed block's words, each kept, removed or added, in
	// reading order: the old side's words removed before the new side's
	// that replace them. Adjacent words of one op are one Word.
	Words []Word
	// Code marks a fenced block, compared line by line: Lines are a
	// changed one's lines, each kept, removed or added, and it has no
	// Words.
	Code  bool
	Lines []Word
}

// Word is a run of words, kept, removed or added.
type Word struct {
	Op   Op
	Text string
}

// compareText compares two texts block by block, nil when they read the
// same: whitespace inside a block, such as a paragraph rewrapped, is no
// change, being none when rendered.
func compareText(old, new string) *Text {
	a, b := blocks(old), blocks(new)
	ka, kb := keys(a), keys(b)
	t := &Text{}
	es := script(ka, kb)
	changed := false
	for i := 0; i < len(es); {
		e := es[i]
		if e.kept() {
			t.Blocks = append(t.Blocks, &Block{Op: Same, Old: ka[e.A], New: kb[e.B], Code: isCode(kb[e.B])})
			i++
			continue
		}
		changed = true
		j := i
		for j < len(es) && !es[j].kept() {
			j++
		}
		var dels, ins []string
		for _, e := range es[i:j] {
			if e.deleted() {
				dels = append(dels, ka[e.A])
			} else {
				ins = append(ins, kb[e.B])
			}
		}
		t.Blocks = append(t.Blocks, pair(dels, ins)...)
		i = j
	}
	if !changed {
		return nil
	}
	return t
}

// pair turns a run of removed and added blocks into the blocks shown: a
// removed block and an added one are one changed block when both are
// fenced blocks, or when they share at least half their words, so an
// edited paragraph reads as edited, not as replaced.
func pair(dels, ins []string) []*Block {
	matched := pairs(len(dels), len(ins), func(i, j int) bool {
		if isCode(dels[i]) || isCode(ins[j]) {
			return isCode(dels[i]) && isCode(ins[j])
		}
		return similar(dels[i], ins[j])
	})
	var out []*Block
	j := 0
	added := func(upto int) {
		for ; j < upto; j++ {
			out = append(out, &Block{Op: Added, New: ins[j], Code: isCode(ins[j])})
		}
	}
	for i, mj := range matched {
		if mj < 0 {
			out = append(out, &Block{Op: Removed, Old: dels[i], Code: isCode(dels[i])})
			continue
		}
		added(mj)
		if isCode(dels[i]) {
			out = append(out, &Block{Op: Changed, Old: dels[i], New: ins[mj], Code: true, Lines: lines(dels[i], ins[mj])})
		} else {
			out = append(out, &Block{Op: Changed, Old: dels[i], New: ins[mj], Words: words(dels[i], ins[mj])})
		}
		j = mj + 1
	}
	added(len(ins))
	return out
}

// pairs pairs n removed blocks with m added ones, in order, the most pairs
// that match: matched[i] is the added block removed block i pairs with, or
// -1. A run longer than maxPair is paired by position.
func pairs(n, m int, match func(i, j int) bool) []int {
	matched := make([]int, n)
	for i := range matched {
		matched[i] = -1
	}
	if n*m > maxPair {
		for i := range min(n, m) {
			if match(i, i) {
				matched[i] = i
			}
		}
		return matched
	}
	// best[i][j] is the most pairs among removed i.. and added j...
	best := make([][]int, n+1)
	for i := range best {
		best[i] = make([]int, m+1)
	}
	ok := make([][]bool, n)
	for i := n - 1; i >= 0; i-- {
		ok[i] = make([]bool, m)
		for j := m - 1; j >= 0; j-- {
			ok[i][j] = match(i, j)
			best[i][j] = max(best[i+1][j], best[i][j+1])
			if ok[i][j] {
				best[i][j] = max(best[i][j], best[i+1][j+1]+1)
			}
		}
	}
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case ok[i][j] && best[i][j] == best[i+1][j+1]+1:
			matched[i] = j
			i, j = i+1, j+1
		case best[i+1][j] >= best[i][j+1]:
			i++
		default:
			j++
		}
	}
	return matched
}

// maxPair bounds the blocks of one run paired by their content.
const maxPair = 40000

// isCode reports a fenced block's key: the only key with a line break.
func isCode(key string) bool { return strings.Contains(key, "\n") }

// lines compares two fenced blocks line by line.
func lines(old, new string) []Word {
	la, lb := strings.Split(old, "\n"), strings.Split(new, "\n")
	var out []Word
	for _, e := range script(la, lb) {
		switch {
		case e.kept():
			out = append(out, Word{Op: Same, Text: lb[e.B]})
		case e.deleted():
			out = append(out, Word{Op: Removed, Text: la[e.A]})
		default:
			out = append(out, Word{Op: Added, Text: lb[e.B]})
		}
	}
	return out
}

func similar(a, b string) bool {
	if isCode(a) || isCode(b) {
		return false
	}
	wa, wb := strings.Fields(a), strings.Fields(b)
	kept := 0
	for _, e := range script(wa, wb) {
		if e.kept() {
			kept++
		}
	}
	return 2*kept >= max(len(wa), len(wb))
}

// words compares two blocks word by word.
func words(old, new string) []Word {
	wa, wb := strings.Fields(old), strings.Fields(new)
	var out []Word
	add := func(op Op, w string) {
		if n := len(out); n > 0 && out[n-1].Op == op {
			out[n-1].Text += " " + w
			return
		}
		out = append(out, Word{Op: op, Text: w})
	}
	for _, e := range script(wa, wb) {
		switch {
		case e.kept():
			add(Same, wb[e.B])
		case e.deleted():
			add(Removed, wa[e.A])
		default:
			add(Added, wb[e.B])
		}
	}
	return out
}

var (
	fence    = regexp.MustCompile("^\\s{0,3}(```|~~~)")
	heading  = regexp.MustCompile(`^\s{0,3}#{1,6}(\s|$)`)
	listItem = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s`)
	tableRow = regexp.MustCompile(`^\s*\|`)
)

// rawBlock is a block as its lines are written.
type rawBlock struct {
	lines []string
	code  bool
}

// blocks splits Markdown into its blocks: a fenced block whole, a heading
// or a table row by itself, a list item with its continuation lines, and
// otherwise paragraphs separated by blank lines.
func blocks(md string) []rawBlock {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	var out []rawBlock
	var cur *rawBlock
	end := func() {
		if cur != nil && len(cur.lines) > 0 {
			out = append(out, *cur)
		}
		cur = nil
	}
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case fence.MatchString(line):
			end()
			marker := fence.FindStringSubmatch(line)[1]
			b := rawBlock{lines: []string{line}, code: true}
			for i++; i < len(lines); i++ {
				b.lines = append(b.lines, lines[i])
				if strings.HasPrefix(strings.TrimSpace(lines[i]), marker) {
					break
				}
			}
			out = append(out, b)
		case strings.TrimSpace(line) == "":
			end()
		case heading.MatchString(line), tableRow.MatchString(line):
			end()
			out = append(out, rawBlock{lines: []string{line}})
		case listItem.MatchString(line):
			end()
			cur = &rawBlock{lines: []string{line}}
		default:
			if cur == nil {
				cur = &rawBlock{}
			}
			cur.lines = append(cur.lines, line)
		}
	}
	end()
	return out
}

// keys are the blocks as compared: a fenced block as written, any other
// with its whitespace runs as single spaces.
func keys(bs []rawBlock) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		if b.code {
			out[i] = strings.Join(b.lines, "\n")
		} else {
			out[i] = strings.Join(strings.Fields(strings.Join(b.lines, " ")), " ")
		}
	}
	return out
}
