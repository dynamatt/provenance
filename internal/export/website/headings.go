package website

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Relative heading depth (DES-0035): templates and Markdown write
// ordinary <h1>–<h6>, where <h1> is the top of the entity being rendered.
// After an entity renders, embeds are spliced in shifted by the level of the
// heading they follow, and a whole embedded entity is shifted by its own
// depth, so nesting composes.
//
// The rewrite scans tokens and changes only the level digit in heading tags;
// every other byte of the rendered HTML passes through untouched.

// embedMarker is the placeholder an embed leaves in its host's HTML until
// the host has rendered and the embed's depth is known.
const embedMarker = "provenance-embed "

func embedPlaceholder(i int) string { return "<!--" + embedMarker + strconv.Itoa(i) + "-->" }

// splice replaces embed placeholders in host with the matching fragment,
// shifted by the level of the last heading before it (0 when none), and
// returns the result. Placeholders it did not issue are left alone.
func splice(host string, fragments []string) (string, error) {
	var out strings.Builder
	level := 0
	err := scan(host, func(tt html.TokenType, raw []byte, z *html.Tokenizer) {
		switch tt {
		case html.StartTagToken:
			if n := headingLevel(z); n > 0 {
				level = n
			}
		case html.CommentToken:
			data := string(z.Text())
			if i, err := strconv.Atoi(strings.TrimPrefix(data, embedMarker)); err == nil &&
				strings.HasPrefix(data, embedMarker) && i >= 0 && i < len(fragments) {
				out.WriteString(shift(fragments[i], level))
				return
			}
		}
		out.Write(raw)
	})
	return out.String(), err
}

// shift moves every heading in fragment down by levels, clamping at <h6>.
func shift(fragment string, levels int) string {
	if levels == 0 {
		return fragment
	}
	var out strings.Builder
	_ = scan(fragment, func(tt html.TokenType, raw []byte, z *html.Tokenizer) {
		if tt == html.StartTagToken || tt == html.EndTagToken || tt == html.SelfClosingTagToken {
			if n := headingLevel(z); n > 0 {
				digit := 2 // "<h1"
				if tt == html.EndTagToken {
					digit = 3 // "</h1"
				}
				raw[digit] = byte('0' + min(n+levels, 6))
			}
		}
		out.Write(raw)
	})
	return out.String()
}

// scan tokenizes s and calls fn with a private copy of each token's raw bytes.
func scan(s string, fn func(html.TokenType, []byte, *html.Tokenizer)) error {
	z := html.NewTokenizer(strings.NewReader(s))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if errors.Is(z.Err(), io.EOF) {
				return nil
			}
			return z.Err()
		}
		raw := append([]byte(nil), z.Raw()...)
		fn(tt, raw, z)
	}
}

// headingLevel returns 1–6 when the current tag is h1–h6, else 0. It must be
// called after the token's raw bytes were copied.
func headingLevel(z *html.Tokenizer) int {
	name, _ := z.TagName()
	if len(name) == 2 && (name[0] == 'h' || name[0] == 'H') && name[1] >= '1' && name[1] <= '6' {
		return int(name[1] - '0')
	}
	return 0
}
