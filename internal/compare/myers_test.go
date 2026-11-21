package compare

import (
	"math/rand"
	"strings"
	"testing"
)

// The script turns a into b, keeps a longest common subsequence's worth,
// and lists each changed run's deletions first.
func TestScript(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	gen := func() []string {
		s := make([]string, r.Intn(12))
		for i := range s {
			s[i] = string(rune('a' + r.Intn(4)))
		}
		return s
	}
	for range 2000 {
		a, b := gen(), gen()
		es := Script(a, b)
		var got []string
		kept, ai, bi := 0, 0, 0
		for i, e := range es {
			switch {
			case e.Kept():
				if a[e.A] != b[e.B] || e.A != ai || e.B != bi {
					t.Fatalf("%v → %v: bad keep %v", a, b, e)
				}
				got = append(got, b[e.B])
				kept++
				ai, bi = ai+1, bi+1
			case e.Deleted():
				if e.A != ai {
					t.Fatalf("%v → %v: bad delete %v", a, b, e)
				}
				ai++
			default:
				if e.B != bi {
					t.Fatalf("%v → %v: bad insert %v", a, b, e)
				}
				if i+1 < len(es) && es[i+1].Deleted() {
					t.Fatalf("%v → %v: insertion before deletion", a, b)
				}
				got = append(got, b[e.B])
				bi++
			}
		}
		if strings.Join(got, "") != strings.Join(b, "") || ai != len(a) {
			t.Fatalf("%v → %v: script gives %v", a, b, got)
		}
		if want := lcs(a, b); kept != want {
			t.Fatalf("%v → %v: kept %d, the longest common subsequence is %d", a, b, kept, want)
		}
	}
}

func lcs(a, b []string) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	return dp[0][0]
}
