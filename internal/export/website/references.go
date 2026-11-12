package website

import (
	"fmt"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/dynamatt/provenance/internal/markdown"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/tmpl"
)

// Section and item references (DES-0034). A heading or an ordered list
// item tagged {#tag} (markdown/tags.go) and a caption with an id can be
// referred to: [[#tag]] within the entity that writes it, [[ID#tag]] in
// another. A reference shows the number its target has where it is
// rendered: on the page being rendered when the target is on it (an
// entity embedded here has its headings numbered within this page), else
// on the tagged entity's own page, followed by "of ID".
//
// So a page is finished in two steps, once every page has rendered.
// prepare numbers it: captions, one sequence per kind; every heading below
// the page's title (h2–h6, 1, 1.2, 1.2.3), after embeds are shifted; and
// ordered list items (2, then 2(b), then 2(b)(iii)). It gives each tagged
// element its id and returns the page's anchors. resolve then replaces
// each reference's placeholder from this page's anchors, or the tagged
// entity's own page's. Both are passes over the page's HTML tokens, so
// every byte they do not change passes through as rendered.

// Reference modes (X5): what a reference shows.
const (
	modeFull   = "full"   // "Section 3.2", "item 4", "Figure 1": the default
	modeNumber = "number" // "3.2"
	modeTitle  = "title"  // the heading's text, item's first paragraph or caption's text
	modeLong   = "long"   // "Section 3.2 Setup"
)

func knownMode(m string) bool {
	switch m {
	case "", modeFull, modeNumber, modeTitle, modeLong:
		return true
	}
	return false
}

// anchor is something on a page a reference can point at.
type anchor struct {
	kind   string // "Section", "item", or a caption's label ("Figure")
	class  string // the reference's class: section-ref, item-ref or xref
	number string // "3.2", "2(b)", "1"; "" for an unnumbered heading
	title  string // plain text
	id     string // its id on the page
}

// text is what a reference in mode shows.
func (a anchor) text(mode string) string {
	if a.number == "" {
		return a.title
	}
	switch mode {
	case modeNumber:
		return a.number
	case modeTitle:
		return a.title
	case modeLong:
		if a.title == "" {
			return a.kind + " " + a.number
		}
		return a.kind + " " + a.number + " " + a.title
	}
	return a.kind + " " + a.number
}

// anchorKey is a tag or caption id of an entity.
type anchorKey struct{ owner, tag string }

// pageAnchors are a page's anchors; the first of a key on the page wins.
type pageAnchors map[anchorKey]anchor

func (p pageAnchors) add(k anchorKey, a anchor) {
	if _, ok := p[k]; !ok {
		p[k] = a
	}
}

// ref is one reference to a tag or caption, as written: by owner (the
// entity whose Markdown it is in) to target's tag, target "" meaning
// owner's own. at is where it is written, for a check.
type ref struct {
	owner, target, tag, mode, label string
	at                              Problem
	// seen is set once a page has resolved it.
	seen bool
}

func (r ref) of() string {
	if r.target == "" {
		return r.owner
	}
	return r.target
}

// source is the reference as written: [[#tag]], [[ID#tag:mode]].
func (r ref) source() string {
	s := r.target + "#" + r.tag
	if r.mode != "" {
		s += ":" + r.mode
	}
	return "[[" + s + "]]"
}

// refElement is the placeholder a reference leaves until its page is
// finished: an element, which neither html/template nor the heading shift
// changes, naming the reference by its index in site.refs.
const refElement = "provenance-ref"

var refPlaceholder = regexp.MustCompile(`<` + refElement + ` n="([0-9]+)"></` + refElement + `>`)

// reference writes the placeholder of a reference to a tag or caption.
func (r *resolver) reference(w interface{ WriteString(string) (int, error) }, l markdown.Link, target string) {
	s := r.ctx.site
	at := Problem{Path: r.ctx.path(), Line: r.ctx.fileLine(l.Line), Entity: r.ctx.entityID()}
	if !knownMode(l.Mode) {
		p := at
		p.Check, p.Msg = "links/reference", fmt.Sprintf("%s: unknown mode %q (full, number, title or long)", wikilink(l), l.Mode)
		s.check.add(p)
	}
	s.refs = append(s.refs, ref{owner: r.ctx.entityID(), target: target, tag: l.Field, mode: l.Mode, label: l.Label, at: at})
	_, _ = w.WriteString(fmt.Sprintf(`<%s n="%d"></%s>`, refElement, len(s.refs)-1, refElement))
}

// tagged records a tag or caption id written in e, reporting one used
// twice in e (tags and caption ids share a namespace, X1) and a tag named
// like a field or facet of e's type, which [[ID#name]] would show instead
// (X2).
func (s *site) tagged(e *model.Entity, at placed, isTag bool) {
	if e == nil {
		return
	}
	if isTag && (e.Schema.Field(at.id) != nil || e.Facet(at.id) != nil || hasFacet(e, at.id)) {
		s.check.add(Problem{Check: "content/tag", Path: at.path, Line: at.line, Entity: e.ID,
			Msg: fmt.Sprintf("tag {#%s} is named like a field of %s, so [[%s#%s]] would show the field", at.id, e.Schema.Name, e.ID, at.id)})
	}
	byTag := s.tags[e.ID]
	if byTag == nil {
		byTag = map[string]written{}
		s.tags[e.ID] = byTag
	}
	first, dup := byTag[at.id]
	if !dup {
		byTag[at.id] = written{at, isTag}
		return
	}
	// Two captions with one id are content/caption's, on the page.
	if first.placed != at && (isTag || first.tag) {
		s.check.add(Problem{Check: "content/tag", Path: at.path, Line: at.line, Entity: e.ID,
			Msg: fmt.Sprintf("#%s is used twice in %s (first at %s:%d): tags and caption ids must be unique within an entity", at.id, e.ID, first.path, first.line)})
	}
}

// written is a tag or caption id where it is written.
type written struct {
	placed
	tag bool
}

func hasFacet(e *model.Entity, name string) bool {
	for _, f := range e.Schema.Facets {
		if f.Name == name {
			return true
		}
	}
	return false
}

// prepare numbers a rendered page and returns it with its anchors. root is
// the entity the page is about, whose tags keep their own name as their
// id; another entity's are prefixed with its ID, so an entity embedded
// here cannot clash with the page's own. Only a page about an entity has
// its headings numbered: the main page and a type's page list entities
// (X3, clarified 2026-11-12).
func (s *site) prepare(content, root string) (string, pageAnchors) {
	anchors := pageAnchors{}
	next := map[string]int{}
	bySerial := map[string]string{}
	content = numberTok.ReplaceAllStringFunc(content, func(tok string) string {
		m := numberTok.FindStringSubmatch(tok)
		label, serial, id, owner, title := m[1], m[2], m[3], m[4], m[5]
		n, done := bySerial[serial]
		if !done {
			next[label]++
			n = strconv.Itoa(next[label])
			bySerial[serial] = n
		}
		if id != "" {
			anchors.add(anchorKey{owner, id}, anchor{kind: label, class: "xref", number: n, title: title, id: anchorID(id)})
		}
		return template.HTMLEscapeString(label + " " + n)
	})
	return numberElements(content, root, root != "", anchors), anchors
}

// listFrame is an open <ol> or <ul>.
type listFrame struct {
	ordered bool
	next    int
	depth   int // which ordered list this is, counting outward ones, from 1
}

// itemFrame is an open <li>: its number, and while its first paragraph
// is read, the tagged item's text.
type itemFrame struct {
	label   string
	tag     anchorKey
	tagged  bool
	reading bool
	text    strings.Builder
}

// numberElements numbers headings and ordered list items and gives tagged
// ones their id, recording them in anchors.
func numberElements(content, root string, headings bool, anchors pageAnchors) string {
	var out strings.Builder
	var counters [7]int
	var (
		heading    int // the open heading's level, 0 when none
		headingKey anchorKey
		headingNum string
		headingTag bool
		headingTxt strings.Builder
	)
	var lists []listFrame
	var items []*itemFrame
	stopReading := func() {
		for _, it := range items {
			it.reading = false
		}
	}
	_ = scan(content, func(tt xhtml.TokenType, raw []byte, z *xhtml.Tokenizer) {
		name, _ := z.TagName()
		tag := string(name)
		switch tt {
		case xhtml.StartTagToken:
			attrs := tagAttrs(z)
			if n := headingLevelOf(tag); n > 0 {
				heading, headingNum, headingTag = n, "", false
				headingTxt.Reset()
				if n >= 2 && headings {
					counters[n]++
					for i := n + 1; i < len(counters); i++ {
						counters[i] = 0
					}
					parts := make([]string, 0, n-1)
					for i := 2; i <= n; i++ {
						parts = append(parts, strconv.Itoa(counters[i]))
					}
					headingNum = strings.Join(parts, ".")
				}
				if t, ok := attrs.get("data-tag"); ok {
					headingKey, headingTag = anchorKey{attrs.value("data-owner"), t}, true
					out.WriteString(startTag(tag, attrs, elementID(headingKey, root)))
				} else {
					out.Write(raw)
				}
				if headingNum != "" {
					out.WriteString(`<span class="section-number">` + headingNum + `</span> `)
				}
				stopReading()
				return
			}
			switch tag {
			case "ol", "ul":
				f := listFrame{ordered: tag == "ol", next: 1}
				if f.ordered {
					if v, err := strconv.Atoi(attrs.value("start")); err == nil {
						f.next = v
					}
					for _, l := range lists {
						if l.ordered {
							f.depth++
						}
					}
					f.depth++
				}
				lists = append(lists, f)
				stopReading()
			case "li":
				stopReading()
				it := &itemFrame{}
				if len(lists) > 0 && lists[len(lists)-1].ordered {
					l := &lists[len(lists)-1]
					n := l.next
					if v, err := strconv.Atoi(attrs.value("value")); err == nil {
						n = v
					}
					l.next = n + 1
					it.label = itemLabel(n, l.depth)
					if l.depth > 1 {
						for i := len(items) - 1; i >= 0; i-- {
							if items[i].label != "" {
								it.label = items[i].label + "(" + it.label + ")"
								break
							}
						}
					}
				}
				items = append(items, it)
				if t, ok := attrs.get("data-tag"); ok {
					it.tag, it.tagged, it.reading = anchorKey{attrs.value("data-owner"), t}, true, true
					out.WriteString(startTag(tag, attrs, elementID(it.tag, root)))
					return
				}
			case "table", "pre", "div", "blockquote", "figure", "section":
				stopReading()
			}
		case xhtml.EndTagToken:
			if n := headingLevelOf(tag); n > 0 && n == heading {
				if headingTag {
					anchors.add(headingKey, anchor{kind: "Section", class: "section-ref", number: headingNum,
						title: collapse(headingTxt.String()), id: elementID(headingKey, root)})
				}
				heading = 0
			}
			switch tag {
			case "ol", "ul":
				if len(lists) > 0 {
					lists = lists[:len(lists)-1]
				}
			case "li":
				if len(items) > 0 {
					it := items[len(items)-1]
					items = items[:len(items)-1]
					if it.tagged {
						anchors.add(it.tag, anchor{kind: "item", class: "item-ref", number: it.label,
							title: collapse(it.text.String()), id: elementID(it.tag, root)})
					}
				}
			case "p":
				stopReading()
			}
		case xhtml.TextToken:
			text := string(z.Text())
			if heading > 0 {
				headingTxt.WriteString(text)
			}
			for _, it := range items {
				if it.reading {
					it.text.WriteString(text)
				}
			}
		}
		out.Write(raw)
	})
	return out.String()
}

// elementID is the id of a tagged heading or item on a page about root.
func elementID(k anchorKey, root string) string {
	if k.owner == root || k.owner == "" {
		return k.tag
	}
	return k.owner + "-" + k.tag
}

// itemLabel is an ordered item's own number at a depth of ordered lists:
// 2, then b, then iii, then decimal again.
func itemLabel(n, depth int) string {
	switch {
	case depth == 2 && n > 0:
		return alpha(n)
	case depth == 3 && n > 0 && n < 4000:
		return roman(n)
	}
	return strconv.Itoa(n)
}

func alpha(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('a'+n%26)) + s
		n /= 26
	}
	return s
}

func roman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

func headingLevelOf(tag string) int {
	if len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6' {
		return int(tag[1] - '0')
	}
	return 0
}

type attrList [][2]string

func tagAttrs(z *xhtml.Tokenizer) attrList {
	var out attrList
	for {
		k, v, more := z.TagAttr()
		if k == nil {
			break
		}
		out = append(out, [2]string{string(k), string(v)})
		if !more {
			break
		}
	}
	return out
}

func (a attrList) get(k string) (string, bool) {
	for _, kv := range a {
		if kv[0] == k {
			return kv[1], true
		}
	}
	return "", false
}

func (a attrList) value(k string) string { v, _ := a.get(k); return v }

// startTag writes a tagged element's start tag: its attributes but the
// tag's own, with its id.
func startTag(name string, attrs attrList, id string) string {
	var b strings.Builder
	b.WriteString("<" + name + ` id="` + html.EscapeString(id) + `"`)
	for _, kv := range attrs {
		switch kv[0] {
		case "data-tag", "data-owner", "id":
			continue
		}
		b.WriteString(" " + kv[0] + `="` + html.EscapeString(kv[1]) + `"`)
	}
	b.WriteString(">")
	return b.String()
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// plainText is HTML's text, for a caption's title.
func plainText(s string) string {
	var b strings.Builder
	_ = scan(s, func(tt xhtml.TokenType, _ []byte, z *xhtml.Tokenizer) {
		if tt == xhtml.TextToken {
			b.Write(z.Text())
		}
	})
	return collapse(b.String())
}

// resolve replaces a prepared page's reference placeholders. linkBase is
// the relative path from the page to the entity pages.
func (s *site) resolve(content string, anchors pageAnchors, linkBase string) (string, error) {
	var stop error
	content = refPlaceholder.ReplaceAllStringFunc(content, func(tok string) string {
		i, _ := strconv.Atoi(refPlaceholder.FindStringSubmatch(tok)[1])
		s.refs[i].seen = true
		r := s.refs[i]
		key := anchorKey{r.of(), r.tag}
		if a, ok := anchors[key]; ok {
			return s.refLink(a, r, "#"+a.id, "")
		}
		own, err := s.ownAnchors(r.of())
		if err != nil {
			if stop == nil {
				stop = err
			}
			return ""
		}
		if a, ok := own[key]; ok {
			l := tmpl.Linker{Base: linkBase, HasPage: s.hasPage, ShownOn: s.shownOn, Schema: s.graph.Schema}
			href, _ := l.Href(s.data.ByID[r.of()])
			if href != "" && !strings.Contains(href, "#") {
				href += "#" + a.id
			}
			return s.refLink(a, r, href, r.of())
		}
		s.unresolved(r)
		what := "#" + r.tag
		text := r.label
		if text == "" {
			text = r.target + what
		}
		return tmpl.UnresolvedHTML(text, "no tag or caption "+what)
	})
	return content, stop
}

// unresolved reports r, which resolves on no page.
func (s *site) unresolved(r ref) {
	p := r.at
	p.Check = "links/reference"
	if r.owner == "" {
		p.Msg = fmt.Sprintf("%s: a reference to a tag or caption must be in an entity's Markdown", r.source())
	} else {
		p.Msg = fmt.Sprintf("%s: %s has no field, tag or caption %q", r.source(), r.of(), r.tag)
	}
	s.check.add(p)
}

// checkUnseen checks the references no finished page showed, such as those
// on a page a template error cut short, against their entity's own page.
func (s *site) checkUnseen() error {
	for i := 0; i < len(s.refs); i++ {
		r := s.refs[i]
		if r.seen {
			continue
		}
		own, err := s.ownAnchors(r.of())
		if err != nil {
			return err
		}
		if _, ok := own[anchorKey{r.of(), r.tag}]; !ok {
			s.unresolved(r)
		}
	}
	return nil
}

// refLink renders a resolved reference: a link to href, or the text alone
// when no page in the export shows it; of is the other entity whose page
// it is on, or "".
func (s *site) refLink(a anchor, r ref, href, of string) string {
	text := a.text(r.mode)
	if of != "" {
		text += " of " + of
	}
	if r.label != "" {
		text = r.label
	}
	class := "ref " + a.class
	if href == "" {
		return fmt.Sprintf(`<span class="%s out-of-scope">%s</span>`, class, template.HTMLEscapeString(text))
	}
	return fmt.Sprintf(`<a class="%s" href="%s">%s</a>`, class, template.HTMLEscapeString(href), template.HTMLEscapeString(text))
}

// ownAnchors are the anchors of id's own page: the page it is the entity
// of, rendered on its own when the export has none, being out of its
// scope or of a type with pages: false.
func (s *site) ownAnchors(id string) (pageAnchors, error) {
	if a, ok := s.own[id]; ok {
		return a, nil
	}
	e := s.byID[id]
	if e == nil {
		return nil, nil
	}
	s.own[id] = nil // a page referring to itself while it renders
	content, _, err := s.renderPage(e, "")
	if err := s.failed(err); err != nil {
		return nil, err
	}
	_, a := s.prepare(content, id)
	s.own[id] = a
	return a, nil
}
