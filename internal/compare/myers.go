package compare

// Edit is one step of an edit script turning a into b: an element kept
// (both indexes set), deleted from a (B is -1) or inserted from b (A is
// -1).
type Edit struct{ A, B int }

func (e Edit) Kept() bool     { return e.A >= 0 && e.B >= 0 }
func (e Edit) Deleted() bool  { return e.B < 0 }
func (e Edit) Inserted() bool { return e.A < 0 }

// Script is a shortest edit script from a to b (Myers' O(ND) algorithm),
// with deletions before insertions where both are possible, so a changed
// element reads old then new. Elements are compared by key.
func Script(a, b []string) []Edit {
	// The common prefix and suffix need no search.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var out []Edit
	for i := 0; i < pre; i++ {
		out = append(out, Edit{i, i})
	}
	for _, e := range myers(a[pre:len(a)-suf], b[pre:len(b)-suf]) {
		if e.A >= 0 {
			e.A += pre
		}
		if e.B >= 0 {
			e.B += pre
		}
		out = append(out, e)
	}
	for i := suf; i > 0; i-- {
		out = append(out, Edit{len(a) - i, len(b) - i})
	}
	return out
}

// maxTrace bounds the search: a script of more than this many deletions
// and insertions is taken as a whole replacement.
const maxTrace = 4000

func myers(a, b []string) []Edit {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return replaced(n, m)
	}
	maxD := n + m
	off := maxD
	v := make([]int, 2*maxD+2)
	// trace keeps, for each d, v's diagonals -d-1..d+1 as they were before
	// step d: the D² ints backtracking needs. Past maxTrace steps the
	// inputs have little in common, and are taken as replaced whole.
	var trace [][]int
	for d := 0; d <= maxD; d++ {
		if d > maxTrace {
			return replaced(n, m)
		}
		lo, hi := max(off-d-1, 0), min(off+d+2, len(v))
		trace = append(trace, append([]int(nil), v[lo:hi]...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1] // down: an insertion
			} else {
				x = v[off+k-1] + 1 // right: a deletion
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, off, n, m, d)
			}
		}
	}
	panic("unreachable")
}

func backtrack(trace [][]int, off, n, m, d int) []Edit {
	var rev []Edit
	x, y := n, m
	for ; d > 0; d-- {
		// at reads diagonal k of v before step d.
		lo := max(off-d-1, 0)
		at := func(k int) int { return trace[d][off+k-lo] }
		k := x - y
		var pk int
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := at(pk)
		py := px - pk
		for x > px && y > py {
			x, y = x-1, y-1
			rev = append(rev, Edit{x, y})
		}
		if x == px {
			rev = append(rev, Edit{-1, y - 1})
		} else {
			rev = append(rev, Edit{x - 1, -1})
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		x, y = x-1, y-1
		rev = append(rev, Edit{x, y})
	}
	out := make([]Edit, len(rev))
	for i, e := range rev {
		out[len(rev)-1-i] = e
	}
	return orderRuns(out)
}

// orderRuns puts each run of deletions and insertions with no kept
// element between them deletions first.
func orderRuns(es []Edit) []Edit {
	for i := 0; i < len(es); {
		if es[i].Kept() {
			i++
			continue
		}
		j := i
		for j < len(es) && !es[j].Kept() {
			j++
		}
		var dels, ins []Edit
		for _, e := range es[i:j] {
			if e.Deleted() {
				dels = append(dels, e)
			} else {
				ins = append(ins, e)
			}
		}
		copy(es[i:], append(dels, ins...))
		i = j
	}
	return es
}

// replaced is the script deleting all n elements, then inserting all m.
func replaced(n, m int) []Edit {
	out := make([]Edit, 0, n+m)
	for i := range n {
		out = append(out, Edit{i, -1})
	}
	for j := range m {
		out = append(out, Edit{-1, j})
	}
	return out
}
