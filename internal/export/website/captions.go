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

// Captions (REQ-0087, DES-0034) are written where a
// figure, table or equation is used, as a ```caption block after it
// (markdown/captions.go). Each page numbers its captions in document order,
// one sequence per kind.
//
// A number is only known once the whole page is rendered, so rendering
// leaves a placeholder for it that the page's finishing resolves
// (references.go). The placeholder is delimited by Unicode private-use
// characters (U+E000 to U+E003, invisible in an editor), which neither
// Markdown, html/template nor the heading shift changes, and carries the
// caption's id, the entity whose caption it is and its text as plain text,
// for references to show.

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
	tokOpen  = "\uE000"
	tokSep   = "\uE001"
	tokBody  = "\uE002"
	tokClose = "\uE003"
)

// numberToken stands for the number of caption serial in sequence label;
// id is the caption's id, or "", owner the entity whose Markdown it is in,
// and title its text as plain text.
func numberToken(label string, serial int, id, owner, title string) string {
	return tokOpen + "num" + tokSep + label + tokSep + strconv.Itoa(serial) + tokSep + id + tokSep + owner + tokBody + title + tokClose
}

var numberTok = regexp.MustCompile(tokOpen + "num" + tokSep + "([^" + tokSep + "]*)" + tokSep + "([0-9]+)" + tokSep + "([^" + tokSep + "]*)" + tokSep + "([^" + tokBody + "]*)" + tokBody + "([^" + tokClose + "]*)" + tokClose)

func anchorID(id string) string { return "caption-" + id }

func (r *resolver) kind(c markdown.Caption) (captionKind, error) {
	k, ok := captionKinds[c.Kind]
	if !ok {
		err := &markdown.Error{Line: c.Line, Msg: fmt.Sprintf("unknown caption kind %q (kinds: %s)", c.Kind, strings.Join(slices.Sorted(maps.Keys(captionKinds)), ", "))}
		if err := r.ctx.markdownProblem(err, err); err != nil {
			return k, err
		}
		return captionKinds["figure"], nil
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
	text := ""
	if c.Text != "" {
		// Problems in the caption's text are placed at the caption.
		prev := r.ctx.lineAt
		r.ctx.lineAt = r.ctx.fileLine(c.Line)
		html, errs := markdown.ConvertAll(c.Text, r, r.options())
		var stop error
		for _, err := range errs {
			if stop = r.ctx.markdownProblem(err, err); stop != nil {
				break
			}
		}
		r.ctx.lineAt = prev
		if stop != nil {
			return stop
		}
		text = strings.TrimSpace(html)
		if inner, ok := strings.CutPrefix(text, "<p>"); ok && strings.Count(text, "<p>") == 1 {
			text = strings.TrimSuffix(inner, "</p>")
		}
	}
	owner := r.ctx.entityID()
	fmt.Fprintf(w, `<figcaption><span class="caption-number">%s</span>`, numberToken(k.Label, r.ctx.site.serial, c.ID, owner, plainText(text)))
	if c.ID != "" {
		check := r.ctx.site.check
		at := placed{id: c.ID, path: r.ctx.path(), entity: owner, line: r.ctx.fileLine(c.Line)}
		check.captions = append(check.captions, at)
		r.ctx.site.tagged(r.ctx.entity(), at, false)
	}
	if text != "" {
		_, _ = w.WriteString(" " + text)
	}
	_, _ = w.WriteString("</figcaption>\n")
	return nil
}
