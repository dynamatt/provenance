package diff

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"mime"
	"path"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/export/website"
	"github.com/dynamatt/provenance/internal/history"
)

// PageOnly is a page whose entity didn't change but which renders
// differently, because its inputs or a template did (DES-0056).
const PageOnly Op = "page"

// Page is a rendered page that differs, as the reader of the exported site
// sees it (DES-0056).
type Page struct {
	// Name is the entity's ID, or the type's name for a type page (a type
	// with pages: false, DES-0054).
	Name string
	// Type is the entity's type, or the type of a type page.
	Type     string
	TypePage bool
	Title    string
	// Op is the entity's: added, removed, changed or retyped; PageOnly for
	// a page whose entity, or type page, changed only through what it
	// shows.
	Op Op
	// Entity is the entity's change, nil for a PageOnly page.
	Entity *Entity
	// HTML is the page's body, each block marked: a changed block's words
	// as <del> and <ins>, an added block .pd-added, a removed one
	// .pd-removed, unchanged runs folded in <details class="pd-fold">.
	HTML htmltemplate.HTML
	// Changed counts the blocks marked.
	Changed int
}

// Render exports each side in memory, without git stamps or revision
// history, which differ on every page, and without copying images, and
// compares their pages block by block (DES-0056). A side that can't be
// exported leaves the pages uncompared, NotRendered saying why.
func (d *Diff) Render() error {
	if d.Rendered || d.NotRendered != "" {
		return nil
	}
	oldSite, err := exportSide(d.Old)
	if err != nil {
		d.NotRendered = d.Old.Name() + " doesn't export: " + err.Error()
		return nil
	}
	newSite, err := exportSide(d.New)
	if err != nil {
		d.NotRendered = d.New.Name() + " doesn't export: " + err.Error()
		return nil
	}
	d.Style = htmltemplate.CSS(newSite["style.css"])
	changes := map[string]*Entity{}
	for _, e := range d.Entities {
		changes[e.ID] = e
	}
	types := map[string]string{}
	for _, s := range []*Side{d.Old, d.New} {
		for _, e := range s.res.Parsed {
			types[e.ID] = e.Type
		}
	}
	var names []string
	for _, site := range []export.Files{oldSite, newSite} {
		for p := range site {
			if name, ok := pageName(p); ok && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	page := func(name string) *Page {
		p := &Page{Name: name, Type: types[name], Op: PageOnly}
		if p.Type == "" {
			p.Type, p.TypePage = name, true
		}
		return p
	}
	slices.SortFunc(names, func(a, b string) int {
		pa, pb := page(a), page(b)
		return cmp.Or(strings.Compare(pa.Type, pb.Type), boolCmp(pa.TypePage, pb.TypePage), strings.Compare(a, b))
	})
	oldImages, newImages := images0(d.Old), images0(d.New)
	for _, name := range names {
		key := "entities/" + name + ".html"
		ob, nb := oldSite[key], newSite[key]
		// A page's bytes are dropped once compared.
		delete(oldSite, key)
		delete(newSite, key)
		p := page(name)
		if d.scope != nil && (p.TypePage || !d.scope[name]) {
			continue
		}
		if e := changes[name]; e != nil {
			p.Op, p.Entity, p.Title = e.Op, e, e.Title
		}
		var err error
		switch {
		case ob == nil && nb == nil:
			continue
		case ob == nil:
			p.HTML, p.Changed, err = whole(nb, Added, newImages)
			if p.Op == PageOnly {
				p.Op = Added
			}
		case nb == nil:
			p.HTML, p.Changed, err = whole(ob, Removed, oldImages)
			if p.Op == PageOnly {
				p.Op = Removed
			}
		case bytes.Equal(ob, nb):
			continue
		default:
			p.HTML, p.Changed, err = comparePages(ob, nb, oldImages, newImages)
		}
		if err != nil {
			return fmt.Errorf("comparing the page of %s: %w", name, err)
		}
		if p.Changed == 0 {
			continue
		}
		d.Pages = append(d.Pages, p)
	}
	// The pages of the entities that changed come first, in the change
	// list's order, then those changed only by another change.
	slices.SortStableFunc(d.Pages, func(a, b *Page) int { return boolCmp(a.Op == PageOnly, b.Op == PageOnly) })
	d.Rendered = true
	return nil
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func pageName(p string) (string, bool) {
	name, ok := strings.CutPrefix(p, "entities/")
	if !ok || strings.Contains(name, "/") {
		return "", false
	}
	return strings.CutSuffix(name, ".html")
}

func exportSide(s *Side) (export.Files, error) {
	if s.res.SchemaBroken {
		return nil, fmt.Errorf("its schema has problems")
	}
	if err := s.res.Err(); err != nil {
		return nil, err
	}
	return website.WithoutImages().Export(s.res.Input())
}

// sideImages reads a side's images, for their git object hashes, which
// blocks are compared by, and, for a page shown, their bytes.
type sideImages struct {
	side   *Side
	hashes map[string]string
}

func images0(s *Side) *sideImages { return &sideImages{side: s, hashes: map[string]string{}} }

// repoPath is the repository path of an image a page in entities/ shows,
// "" for one that is not a file of the site.
func repoPath(src string) string {
	if strings.Contains(src, ":") || strings.HasPrefix(src, "/") || strings.HasPrefix(src, "#") {
		return ""
	}
	src, _, _ = strings.Cut(src, "#")
	src, _, _ = strings.Cut(src, "?")
	p := path.Clean(path.Join("entities", src))
	if strings.HasPrefix(p, "../") || p == ".." {
		return ""
	}
	return unescape(p)
}

func unescape(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '%' && i+2 < len(p) {
			var c byte
			if _, err := fmt.Sscanf(p[i+1:i+3], "%02X", &c); err == nil {
				b.WriteByte(c)
				i += 2
				continue
			}
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

func (si *sideImages) hash(src string) string {
	p := repoPath(src)
	if p == "" {
		return src
	}
	if h, ok := si.hashes[p]; ok {
		return h
	}
	h, err := history.BlobHash(si.side.Files(), p)
	if err != nil {
		h = "missing"
	}
	si.hashes[p] = h
	return h
}

// dataURL is an image as a data: URL, so the file holds it.
func (si *sideImages) dataURL(src string) string {
	p := repoPath(src)
	if p == "" {
		return src
	}
	b, err := fs.ReadFile(si.side.Files(), p)
	if err != nil {
		return ""
	}
	typ := mime.TypeByExtension(strings.ToLower(path.Ext(p)))
	if typ == "" {
		typ = "application/octet-stream"
	}
	if strings.HasPrefix(typ, "image/svg") {
		typ = "image/svg+xml"
	}
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// atomic are the elements always compared whole: a paragraph, a heading, a
// list item, a table row and the like.
var atomic = map[atom.Atom]bool{
	atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Li: true, atom.Tr: true, atom.Pre: true, atom.Figcaption: true, atom.Caption: true,
	atom.Dt: true, atom.Dd: true, atom.Summary: true,
}

// containers hold blocks; one holding none, or text of its own, is a
// block itself.
var containers = map[atom.Atom]bool{
	atom.Body: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Header: true,
	atom.Footer: true, atom.Main: true, atom.Nav: true, atom.Aside: true, atom.Details: true,
	atom.Table: true, atom.Thead: true, atom.Tbody: true, atom.Tfoot: true, atom.Ul: true, atom.Ol: true,
	atom.Dl: true, atom.Figure: true, atom.Blockquote: true, atom.Form: true,
}

// flow are the containers whose unchanged children fold into a
// <details>: never inside a table or a list.
var flow = map[atom.Atom]bool{
	atom.Body: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Header: true,
	atom.Footer: true, atom.Main: true, atom.Aside: true, atom.Blockquote: true, atom.Figure: true,
}

type hblock struct {
	node *html.Node
	key  string
	text string
}

func parseBody(page []byte) (*html.Node, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if body != nil {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Body {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if body == nil {
		return nil, fmt.Errorf("no <body>")
	}
	return body, nil
}

// leafBlocks are a page's blocks in reading order: atomic elements, and
// containers holding no block or text of their own.
func leafBlocks(body *html.Node, si *sideImages) []*hblock {
	var out []*hblock
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				if strings.TrimSpace(c.Data) != "" {
					out = append(out, &hblock{node: c, key: "text:" + norm(c.Data), text: norm(c.Data)})
				}
			case c.Type != html.ElementNode:
			case atomic[c.DataAtom] || (containers[c.DataAtom] && !hasBlocks(c)):
				out = append(out, &hblock{node: c, key: blockKey(c, si), text: norm(textOf(c))})
			case containers[c.DataAtom]:
				walk(c)
			default:
				// Inline content outside any block, such as a lone link
				// or image: a block of its own.
				out = append(out, &hblock{node: c, key: blockKey(c, si), text: norm(textOf(c))})
			}
		}
	}
	walk(body)
	return out
}

func hasBlocks(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) != "" {
			return false // text of its own: compared whole
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (atomic[c.DataAtom] || containers[c.DataAtom]) {
			return true
		}
	}
	return false
}

func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// blockKey is a block as compared: its HTML, whitespace normalized, with
// each image's git object hash, so a changed image changes its block.
func blockKey(n *html.Node, si *sideImages) string {
	var b strings.Builder
	for _, t := range tokens(n, si, true) {
		b.WriteString(t.key)
		b.WriteByte(' ')
	}
	return b.String()
}

// token is a tag or a word of a block, compared by key and written as
// is; space says whitespace came before it.
type token struct {
	key   string
	node  *html.Node // the element of a tag token
	end   bool       // an end tag
	text  string     // a word, unescaped
	space bool
}

func (t token) isTag() bool { return t.node != nil }

var void = map[atom.Atom]bool{atom.Img: true, atom.Br: true, atom.Hr: true, atom.Input: true, atom.Wbr: true, atom.Col: true, atom.Source: true}

// tokens are a block's tags and words in order; outer includes the
// block's own tags.
func tokens(n *html.Node, si *sideImages, outer bool) []token {
	var out []token
	space := false
	add := func(t token) {
		t.space = space && len(out) > 0
		space = false
		out = append(out, t)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			text := n.Data
			for text != "" {
				trimmed := strings.TrimLeft(text, " \t\n\r\f")
				if len(trimmed) < len(text) {
					space = true
				}
				if trimmed == "" {
					return
				}
				end := strings.IndexAny(trimmed, " \t\n\r\f")
				if end < 0 {
					end = len(trimmed)
				}
				add(token{key: trimmed[:end], text: trimmed[:end]})
				text = trimmed[end:]
			}
			return
		case html.ElementNode:
			key := startTag(n, nil, "")
			if n.DataAtom == atom.Img {
				key += "#" + si.hash(attr(n, "src"))
			}
			add(token{key: key, node: n})
			if void[n.DataAtom] {
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			add(token{key: "</" + n.Data + ">", node: n, end: true})
		}
	}
	if outer {
		walk(n)
	} else {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// out writes a page for the diff file: links to pages the file holds go
// to them, other links are dropped, and images are inlined.
type out struct {
	b      strings.Builder
	images *sideImages
}

// startTag renders an element's start tag, adding class extra; with a
// writer, its href and src are rewritten for the diff file.
func startTag(n *html.Node, w *out, extra string) string {
	var b strings.Builder
	b.WriteString("<" + n.Data)
	hasClass := false
	for _, a := range n.Attr {
		val := a.Val
		switch {
		case a.Key == "class" && extra != "":
			val, hasClass = strings.TrimSpace(val+" "+extra), true
		case w != nil && a.Key == "href" && n.DataAtom == atom.A:
			val = link(val)
			if val == "" {
				continue
			}
		case w != nil && a.Key == "src" && n.DataAtom == atom.Img:
			val = w.images.dataURL(val)
		case w != nil && a.Key == "id":
			continue // ids repeat across the file's pages
		}
		fmt.Fprintf(&b, ` %s="%s"`, a.Key, html.EscapeString(val))
	}
	if extra != "" && !hasClass {
		fmt.Fprintf(&b, ` class="%s"`, extra)
	}
	b.WriteString(">")
	return b.String()
}

// link rewrites a page's link for the diff file: to another entity page,
// the page's section of the file; anything else is dropped.
func link(href string) string {
	if strings.Contains(href, ":") {
		return href // an external link
	}
	file, _, _ := strings.Cut(href, "#")
	name, ok := strings.CutSuffix(path.Base(file), ".html")
	if !ok || file == "" || strings.Contains(file, "index.html") {
		return ""
	}
	return "#pd-page-" + name
}

// write writes n with its children for the diff file.
func (w *out) write(n *html.Node, extra string) {
	switch n.Type {
	case html.TextNode:
		w.b.WriteString(html.EscapeString(n.Data))
	case html.ElementNode:
		w.b.WriteString(startTag(n, w, extra))
		if void[n.DataAtom] {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.write(c, "")
		}
		w.b.WriteString("</" + n.Data + ">")
	case html.CommentNode:
	}
}

// whole is an added or removed page, every block marked.
func whole(page []byte, op Op, si *sideImages) (htmltemplate.HTML, int, error) {
	body, err := parseBody(page)
	if err != nil {
		return "", 0, err
	}
	w := &out{images: si}
	w.b.WriteString(`<div class="pd-whole pd-` + string(op) + `">`)
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		w.write(c, "")
	}
	w.b.WriteString("</div>")
	return htmltemplate.HTML(w.b.String()), max(1, len(leafBlocks(body, si))), nil
}

// mark is how a block of the new page is shown.
type mark struct {
	op  Op
	old *html.Node // the old block a changed one replaces
}

// comparePages marks the new page's blocks against the old page's: the
// new page's structure, its blocks changed, added, or with the removed
// ones placed after the block before them.
func comparePages(ob, nb []byte, oi, ni *sideImages) (htmltemplate.HTML, int, error) {
	oldBody, err := parseBody(ob)
	if err != nil {
		return "", 0, err
	}
	newBody, err := parseBody(nb)
	if err != nil {
		return "", 0, err
	}
	a, b := leafBlocks(oldBody, oi), leafBlocks(newBody, ni)
	ka, kb := make([]string, len(a)), make([]string, len(b))
	for i, x := range a {
		ka[i] = x.key
	}
	for i, x := range b {
		kb[i] = x.key
	}
	marks := map[*html.Node]mark{}
	removedAfter := map[*html.Node][]*html.Node{}
	var removedFirst []*html.Node
	var prev *html.Node // the new block before the run
	changed := 0
	es := script(ka, kb)
	for i := 0; i < len(es); {
		if es[i].Kept() {
			prev = b[es[i].B].node
			i++
			continue
		}
		j := i
		for j < len(es) && !es[j].Kept() {
			j++
		}
		var dels, ins []*hblock
		for _, e := range es[i:j] {
			if e.Deleted() {
				dels = append(dels, a[e.A])
			} else {
				ins = append(ins, b[e.B])
			}
		}
		i = j
		// Removed and added blocks of one kind sharing half their words
		// pair, as text blocks do; a removed block is placed after the
		// new block before it.
		matched := pairs(len(dels), len(ins), func(i, j int) bool {
			return sameKind(dels[i].node, ins[j].node) && similarText(dels[i].text, ins[j].text)
		})
		k := 0
		added := func(upto int) {
			for ; k < upto; k++ {
				marks[ins[k].node] = mark{op: Added}
				prev = ins[k].node
				changed++
			}
		}
		for x, mj := range matched {
			if mj < 0 {
				if prev == nil {
					removedFirst = append(removedFirst, dels[x].node)
				} else {
					removedAfter[prev] = append(removedAfter[prev], dels[x].node)
				}
				changed++
				continue
			}
			added(mj)
			marks[ins[mj].node] = mark{op: Changed, old: dels[x].node}
			prev = ins[mj].node
			changed++
			k = mj + 1
		}
		added(len(ins))
	}
	if changed == 0 {
		return "", 0, nil
	}
	touched := map[*html.Node]bool{}
	touch := func(n *html.Node) {
		for ; n != nil; n = n.Parent {
			touched[n] = true
		}
	}
	for n := range marks {
		touch(n)
	}
	for n := range removedAfter {
		touch(n)
	}
	w := &out{images: ni}
	pw := &pageWriter{w: w, marks: marks, removedAfter: removedAfter, touched: touched, old: oi, new: ni}
	for _, n := range removedFirst {
		pw.removed(n, nil)
	}
	pw.children(newBody)
	return htmltemplate.HTML(w.b.String()), changed, nil
}

func sameKind(a, b *html.Node) bool {
	return a.Type == b.Type && (a.Type != html.ElementNode || a.DataAtom == b.DataAtom)
}

func similarText(a, b string) bool {
	if a == b {
		return true
	}
	return similar(a, b)
}

type pageWriter struct {
	w            *out
	marks        map[*html.Node]mark
	removedAfter map[*html.Node][]*html.Node
	touched      map[*html.Node]bool
	old, new     *sideImages
}

// children writes n's children, folding runs of untouched ones when n is
// a flow container.
func (pw *pageWriter) children(n *html.Node) {
	var run []*html.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		if !flow[n.DataAtom] || !hasElement(run) {
			for _, c := range run {
				pw.node(c)
			}
		} else {
			pw.w.b.WriteString(`<details class="pd-fold"><summary>` + html.EscapeString(foldSummary(run)) + `</summary>`)
			for _, c := range run {
				pw.w.write(c, "")
			}
			pw.w.b.WriteString(`</details>`)
		}
		run = nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if pw.touched[c] {
			flush()
			pw.node(c)
			continue
		}
		run = append(run, c)
	}
	flush()
}

func hasElement(ns []*html.Node) bool {
	for _, n := range ns {
		if n.Type == html.ElementNode || (n.Type == html.TextNode && strings.TrimSpace(n.Data) != "") {
			return true
		}
	}
	return false
}

// foldSummary says what a folded run holds: its headings, or how many
// blocks.
func foldSummary(run []*html.Node) string {
	var heads []string
	blocks := 0
	for _, n := range run {
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			switch n.DataAtom {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				heads = append(heads, norm(textOf(n)))
				blocks++
				return
			}
			if atomic[n.DataAtom] {
				blocks++
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(n)
	}
	what := plural(max(blocks, 1), "unchanged block", "unchanged blocks")
	if len(heads) > 0 {
		what += ": " + strings.Join(heads, ", ")
	}
	return what
}

func (pw *pageWriter) node(n *html.Node) {
	if m, ok := pw.marks[n]; ok {
		switch m.op {
		case Added:
			pw.w.write(n, "pd-added")
		case Changed:
			pw.changed(n, m.old)
		}
	} else if n.Type == html.ElementNode && pw.touched[n] {
		pw.w.b.WriteString(startTag(n, pw.w, ""))
		if !void[n.DataAtom] {
			pw.children(n)
			pw.w.b.WriteString("</" + n.Data + ">")
		}
	} else {
		pw.w.write(n, "")
	}
	for _, r := range pw.removedAfter[n] {
		pw.removed(r, n)
	}
}

// removed writes a removed block after anchor, in a container of its own
// where anchor's isn't one it can sit in.
func (pw *pageWriter) removed(r, anchor *html.Node) {
	w := &out{images: pw.old}
	w.write(r, "pd-removed")
	s := w.b.String()
	if r.Type == html.TextNode {
		s = `<span class="pd-removed">` + s + `</span>`
	}
	at := atom.Atom(0)
	if anchor != nil && anchor.Type == html.ElementNode {
		at = anchor.DataAtom
	}
	switch {
	case r.DataAtom == atom.Tr && at != atom.Tr:
		s = `<table class="pd-removed-table"><tbody>` + s + `</tbody></table>`
	case r.DataAtom == atom.Li && at != atom.Li:
		s = `<ul class="pd-removed-list">` + s + `</ul>`
	case (r.DataAtom == atom.Dt || r.DataAtom == atom.Dd) && at != atom.Dt && at != atom.Dd:
		s = `<dl>` + s + `</dl>`
	case at == atom.Tr && r.DataAtom != atom.Tr:
		s = `<tr class="pd-removed"><td colspan="99">` + s + `</td></tr>`
	case at == atom.Li && r.DataAtom != atom.Li:
		s = `<li class="pd-removed">` + s + `</li>`
	}
	pw.w.b.WriteString(s)
}

// changed writes a changed block: the new block's tags, its words kept,
// the old block's removed words as <del>, the new block's added ones as
// <ins>; a changed image as <ins>.
func (pw *pageWriter) changed(n, old *html.Node) {
	if n.Type == html.TextNode {
		pw.w.b.WriteString(`<span class="pd-changed">`)
		pw.words(tokens(old, pw.old, true), tokens(n, pw.new, true))
		pw.w.b.WriteString(`</span>`)
		return
	}
	pw.w.b.WriteString(startTag(n, pw.w, "pd-changed"))
	if void[n.DataAtom] {
		return
	}
	pw.words(tokens(old, pw.old, false), tokens(n, pw.new, false))
	pw.w.b.WriteString("</" + n.Data + ">")
}

func (pw *pageWriter) words(ta, tb []token) {
	ka, kb := make([]string, len(ta)), make([]string, len(tb))
	for i, t := range ta {
		ka[i] = t.key
	}
	for i, t := range tb {
		kb[i] = t.key
	}
	b := &pw.w.b
	open := Same
	set := func(op Op) {
		if op == open {
			return
		}
		switch open {
		case Removed:
			b.WriteString("</del>")
		case Added:
			b.WriteString("</ins>")
		}
		switch op {
		case Removed:
			b.WriteString(`<del class="pd">`)
		case Added:
			b.WriteString(`<ins class="pd">`)
		}
		open = op
	}
	// emit writes a token: a word inside its <del> or <ins>, the space
	// before it outside one that starts there; a tag outside both.
	last := Same
	emit := func(op Op, t token, extra string) {
		replacing := op == Added && last == Removed && !t.isTag()
		if t.isTag() || op != open {
			set(Same)
		}
		// The words added in place of removed ones take their space.
		if t.space && !replacing {
			b.WriteByte(' ')
		}
		last = op
		if t.isTag() {
			pw.tag(t, extra)
			return
		}
		set(op)
		b.WriteString(html.EscapeString(t.text))
	}
	for _, e := range script(ka, kb) {
		switch {
		case e.Kept():
			emit(Same, tb[e.B], "")
		case e.Deleted():
			if t := ta[e.A]; !t.isTag() {
				emit(Removed, t, "") // the new block's tags are written
			}
		default:
			t := tb[e.B]
			extra := ""
			if t.isTag() && t.node.DataAtom == atom.Img && !t.end {
				extra = "pd-image-changed"
			}
			emit(Added, t, extra)
		}
	}
	set(Same)
}

func (pw *pageWriter) tag(t token, extra string) {
	if t.end {
		pw.w.b.WriteString("</" + t.node.Data + ">")
		return
	}
	pw.w.b.WriteString(startTag(t.node, pw.w, extra))
}
