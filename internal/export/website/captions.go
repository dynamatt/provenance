package website

import (
	"fmt"
	"html/template"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/util"

	"github.com/dynamatt/provenance/internal/markdown"
)

// Captions (Requirements Spec §7, Detailed Design §7) are written where a
// figure, table or equation is used, as a ```caption block after it
// (markdown/captions.go). Each page numbers its captions in document order,
// one sequence per kind.
//
// A number is only known once the whole page is rendered, so rendering
// leaves placeholders that finalize resolves (a two-pass render): the
// number in each caption, and every [[#id]] reference, which becomes a link
// to the caption showing "Figure 1" (or its label), even when it comes
// before the caption. Placeholders are delimited by Unicode private-use
// characters, which neither Markdown, html/template nor the heading shift
// changes.

// captionKind is how one kind of caption is numbered and placed.
type captionKind struct {
	Label string // the sequence's label, shown before the number
	Above bool   // the caption goes above the captioned block (tables)
}

// captionKinds are fixed (decided 2026-10-04): how a caption looks is the
// stylesheet's concern, through the captioned-<kind> class.
var captionKinds = map[string]captionKind{
	"figure":   {Label: "Figure"},
	"table":    {Label: "Table", Above: true},
	"equation": {Label: "Equation"},
}

const (
	tokOpen  = ""
	tokSep   = ""
	tokBody  = ""
	tokClose = ""
)

// numberToken stands for the number of caption serial in sequence label;
// id is the caption's id, or "".
func numberToken(label string, serial int, id string) string {
	return tokOpen + "num" + tokSep + label + tokSep + strconv.Itoa(serial) + tokSep + id + tokClose
}

// xrefToken stands for a [[#id]] reference; ordinary is what it renders as
// when the page has no such caption.
func xrefToken(id, label, ordinary string) string {
	return tokOpen + "xref" + tokSep + id + tokSep + label + tokBody + ordinary + tokClose
}

var (
	numberTok = regexp.MustCompile(tokOpen + "num" + tokSep + "([^" + tokSep + "]*)" + tokSep + "([0-9]+)" + tokSep + "([^" + tokClose + "]*)" + tokClose)
	xrefTok   = regexp.MustCompile(tokOpen + "xref" + tokSep + "([^" + tokSep + "]*)" + tokSep + "([^" + tokBody + "]*)" + tokBody + "([^" + tokClose + "]*)" + tokClose)
)

func anchorID(id string) string { return "caption-" + id }

func (r *resolver) kind(c markdown.Caption) (captionKind, error) {
	k, ok := captionKinds[c.Kind]
	if !ok {
		return k, &markdown.Error{Line: c.Line, Msg: fmt.Sprintf("unknown caption kind %q (kinds: %s)", c.Kind, strings.Join(slices.Sorted(maps.Keys(captionKinds)), ", "))}
	}
	return k, nil
}

func (r *resolver) CaptionStart(w util.BufWriter, c markdown.Caption) error {
	k, err := r.kind(c)
	if err != nil {
		return err
	}
	id := ""
	if c.ID != "" {
		id = fmt.Sprintf(` id="%s"`, template.HTMLEscapeString(anchorID(c.ID)))
	}
	fmt.Fprintf(w, "<figure class=\"captioned captioned-%s\"%s>\n", template.HTMLEscapeString(c.Kind), id)
	if k.Above {
		return r.figcaption(w, c, k)
	}
	return nil
}

func (r *resolver) CaptionEnd(w util.BufWriter, c markdown.Caption) error {
	k, err := r.kind(c)
	if err != nil {
		return err
	}
	if !k.Above {
		if err := r.figcaption(w, c, k); err != nil {
			return err
		}
	}
	_, _ = w.WriteString("</figure>\n")
	return nil
}

// figcaption writes the caption: its number placeholder, then its text as
// inline Markdown.
func (r *resolver) figcaption(w util.BufWriter, c markdown.Caption, k captionKind) error {
	r.ctx.site.serial++
	fmt.Fprintf(w, `<figcaption><span class="caption-number">%s</span>`, numberToken(k.Label, r.ctx.site.serial, c.ID))
	if c.Text != "" {
		text, err := markdown.Convert(c.Text, r, r.options())
		if err != nil {
			return err
		}
		text = strings.TrimSpace(text)
		if inner, ok := strings.CutPrefix(text, "<p>"); ok && strings.Count(text, "<p>") == 1 {
			text = strings.TrimSuffix(inner, "</p>")
		}
		_, _ = w.WriteString(" " + text)
	}
	_, _ = w.WriteString("</figcaption>\n")
	return nil
}

// finalize numbers one page's captions in document order and resolves its
// placeholders.
func (s *site) finalize(content string) string {
	bySerial := map[string]string{}
	byID := map[string]string{}
	next := map[string]int{}
	for _, m := range numberTok.FindAllStringSubmatch(content, -1) {
		label, serial, id := m[1], m[2], m[3]
		if _, done := bySerial[serial]; done {
			continue
		}
		next[label]++
		n := fmt.Sprintf("%s %d", label, next[label])
		bySerial[serial] = n
		if id != "" && byID[id] == "" {
			byID[id] = n
		}
	}
	content = numberTok.ReplaceAllStringFunc(content, func(tok string) string {
		return template.HTMLEscapeString(bySerial[numberTok.FindStringSubmatch(tok)[2]])
	})
	return xrefTok.ReplaceAllStringFunc(content, func(tok string) string {
		m := xrefTok.FindStringSubmatch(tok)
		id, label, ordinary := m[1], m[2], m[3]
		n := byID[id]
		if n == "" {
			return ordinary
		}
		text := n
		if label != "" {
			text = label
		}
		return fmt.Sprintf(`<a class="ref xref" href="#%s">%s</a>`, template.HTMLEscapeString(anchorID(id)), template.HTMLEscapeString(text))
	})
}
