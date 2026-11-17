package tmpl

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// minorWords stay lower-case in a title unless first or last.
var minorWords = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "but": true, "by": true,
	"for": true, "in": true, "nor": true, "of": true, "on": true, "or": true, "the": true, "to": true,
}

// TitleCase writes a name as a title (DES-0030): words split at
// underscores, hyphens, spaces and CamelCase humps, each capitalised but
// minor words (of, by, the…) inside the title, an acronym kept whole.
// VerificationSummaryReport is "Verification Summary Report",
// bill_of_materials "Bill of Materials", SBOMEntry "SBOM Entry". It changes how a name reads, not
// what it says: no word is respelt.
func TitleCase(s string) string {
	words := splitWords(s)
	for i, w := range words {
		lower := strings.ToLower(w)
		if i > 0 && i < len(words)-1 && minorWords[lower] {
			words[i] = lower
			continue
		}
		r, n := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + w[n:]
	}
	return strings.Join(words, " ")
}

// splitWords splits s at separators and at CamelCase boundaries: before
// an upper-case letter that follows a lower-case letter or a digit, and
// before the last upper-case letter of a run followed by a lower-case one.
func splitWords(s string) []string {
	var words []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || unicode.IsSpace(r) }) {
		rs := []rune(part)
		start := 0
		for i := 1; i < len(rs); i++ {
			prev, cur := rs[i-1], rs[i]
			next := rune(0)
			if i+1 < len(rs) {
				next = rs[i+1]
			}
			hump := unicode.IsUpper(cur) && (unicode.IsLower(prev) || unicode.IsDigit(prev))
			acronymEnd := unicode.IsUpper(cur) && unicode.IsUpper(prev) && unicode.IsLower(next)
			if hump || acronymEnd {
				words = append(words, string(rs[start:i]))
				start = i
			}
		}
		words = append(words, string(rs[start:]))
	}
	return words
}
