// Package sanitize removes every trace of JavaScript from an HTML document so
// the saved page is inert: a photograph, not a program.
//
// It parses with golang.org/x/net/html, walks the tree, and deletes scripts,
// event handlers, javascript: URLs, downlevel IE conditional comments (which
// can smuggle a <script> past an element-only walk), and the dead
// preconnect/preload hints that mean nothing offline — while leaving styles,
// images, fonts, forms, and all semantic markup untouched so the layout
// survives intact.
package sanitize

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Options tune a few edge behaviours; the zero value is the safe default
// (scripts and noscript removed, meta-refresh removed).
type Options struct {
	// KeepNoscript unwraps <noscript> content into the document instead of
	// deleting it, for sites whose real content hides behind a JS check.
	KeepNoscript bool
	// KeepMetaRefresh preserves a plain timed <meta http-equiv="refresh">
	// (a JS-target refresh is always removed).
	KeepMetaRefresh bool
	// Banner, when non-empty, is inserted as an HTML comment at the top of the
	// document.
	Banner string
	// MobileReadable injects a viewport meta tag and a small CSS block that
	// makes legacy, font-era sites readable on mobile. It is intended for
	// archives of 1990s/2000s sites that use <font size="2">, table layouts,
	// and no viewport declaration — all of which render as microscopic text on
	// a phone. The injected CSS overrides font sizes, loosens line height, caps
	// the content width, and hides image-map navigation elements that are
	// useless offline.
	MobileReadable bool
}

// Report counts what was removed, for the run summary and for tests.
type Report struct {
	ScriptsRemoved      int
	HandlersRemoved     int
	NoscriptRemoved     int
	NoscriptUnwrapped   int
	JSURLsNeutralized   int
	MetaRefreshRemoved  int
	DeadLinksRemoved    int
	CondCommentsRemoved int
	CharsetAdded        bool
	CharsetRewritten    bool
}

// jsURLAttrs are attributes whose value may be a javascript: URL.
var jsURLAttrs = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true,
	"poster": true, "data": true, "background": true, "xlink:href": true,
}

// Strip parses doc, removes all JavaScript, and returns the rewritten HTML plus
// a Report. A parse error is returned unchanged to the caller.
func Strip(doc []byte, opts Options) ([]byte, Report, error) {
	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		return nil, Report{}, err
	}
	rep := CleanTree(root, opts)
	var buf bytes.Buffer
	if err := html.Render(&buf, root); err != nil {
		return nil, rep, err
	}
	return buf.Bytes(), rep, nil
}

// CleanTree removes all JavaScript from an already-parsed document in place and
// returns the Report. The cloner uses this so the HTML is parsed only once and
// shared with the asset rewriter.
func CleanTree(root *html.Node, opts Options) Report {
	var rep Report
	clean(root, opts, &rep)
	rep.CharsetAdded, rep.CharsetRewritten = ensureCharset(root)
	if opts.MobileReadable {
		ensureViewport(root)
		injectMobileCSS(root)
	}
	if opts.Banner != "" {
		insertBanner(root, opts.Banner)
	}
	return rep
}

// clean walks n's children, removing or scrubbing each before recursing.
func clean(n *html.Node, opts Options, rep *Report) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		if c.Type == html.CommentNode {
			// A downlevel IE conditional comment (<!--[if lt IE 9]>...<![endif]-->)
			// parses as one comment whose data holds raw markup — a <script src>
			// among it. The element walk never sees that script, so drop the whole
			// comment. Downlevel-revealed content lives in sibling nodes, not the
			// comment data, so it is untouched.
			if isConditionalComment(c.Data) {
				n.RemoveChild(c)
				rep.CondCommentsRemoved++
			}
			continue
		}
		if c.Type == html.ElementNode {
			switch c.DataAtom {
			case atom.Script:
				n.RemoveChild(c)
				rep.ScriptsRemoved++
				continue
			case atom.Noscript:
				if opts.KeepNoscript {
					unwrapNoscript(n, c)
					rep.NoscriptUnwrapped++
				} else {
					n.RemoveChild(c)
					rep.NoscriptRemoved++
				}
				continue
			case atom.Meta:
				if isMetaRefresh(c) && (!opts.KeepMetaRefresh || isJSRefresh(c)) {
					n.RemoveChild(c)
					rep.MetaRefreshRemoved++
					continue
				}
			case atom.Link:
				if isDeadLink(c) {
					n.RemoveChild(c)
					rep.DeadLinksRemoved++
					continue
				}
			}
			stripHandlers(c, rep)
			neutralizeJSURLs(c, rep)
		}
		clean(c, opts, rep)
	}
}

// stripHandlers removes every on* event-handler attribute from n.
func stripHandlers(n *html.Node, rep *Report) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		if len(a.Key) > 2 && strings.HasPrefix(strings.ToLower(a.Key), "on") {
			rep.HandlersRemoved++
			continue
		}
		kept = append(kept, a)
	}
	n.Attr = kept
}

// neutralizeJSURLs replaces javascript: URLs: links become "#", other carriers
// lose the attribute entirely.
func neutralizeJSURLs(n *html.Node, rep *Report) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if jsURLAttrs[key] && strings.HasPrefix(strings.ToLower(strings.TrimSpace(a.Val)), "javascript:") {
			rep.JSURLsNeutralized++
			if key == "href" {
				a.Val = "#"
				kept = append(kept, a)
			}
			// non-href carriers: drop the attribute.
			continue
		}
		kept = append(kept, a)
	}
	n.Attr = kept
}

// isMetaRefresh reports whether n is a <meta http-equiv="refresh">.
func isMetaRefresh(n *html.Node) bool {
	return strings.EqualFold(attr(n, "http-equiv"), "refresh")
}

// isJSRefresh reports whether a meta-refresh target is a javascript: URL.
func isJSRefresh(n *html.Node) bool {
	return strings.Contains(strings.ToLower(attr(n, "content")), "javascript:")
}

// isDeadLink reports whether a <link> is a resource hint that is useless or
// script-bound offline: preconnect, dns-prefetch, modulepreload, or a
// preload/prefetch that targets a script.
func isDeadLink(n *html.Node) bool {
	for r := range strings.FieldsSeq(strings.ToLower(attr(n, "rel"))) {
		switch r {
		case "preconnect", "dns-prefetch", "modulepreload":
			return true
		case "preload", "prefetch":
			as := strings.ToLower(attr(n, "as"))
			href := strings.ToLower(attr(n, "href"))
			if as == "script" || strings.HasSuffix(href, ".js") {
				return true
			}
		}
	}
	return false
}

// isConditionalComment reports whether a comment's data is a downlevel IE
// conditional-comment marker. Both the downlevel-hidden form (the whole
// "[if lt IE 9]>...<![endif]" in one comment) and the two markers of the
// downlevel-revealed form ("[if gte IE 9]><!" and "<![endif]") match, so the
// markers are stripped while any revealed content, which sits in sibling
// nodes, stays.
func isConditionalComment(data string) bool {
	d := strings.TrimSpace(data)
	return strings.HasPrefix(d, "[if") ||
		strings.HasPrefix(d, "<![endif]") ||
		strings.HasPrefix(d, "[endif]")
}

// unwrapNoscript replaces a <noscript> with its content. Because x/net/html
// parses noscript content as raw text (scripting enabled), the text is
// re-parsed as a fragment in the parent's context and spliced in before the
// noscript node, which is then removed.
func unwrapNoscript(parent, ns *html.Node) {
	var raw strings.Builder
	for c := ns.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			raw.WriteString(c.Data)
		}
	}
	frag, err := html.ParseFragment(strings.NewReader(raw.String()), &html.Node{
		Type:     html.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err == nil {
		for _, fn := range frag {
			parent.InsertBefore(fn, ns)
		}
	}
	parent.RemoveChild(ns)
}

// ensureCharset guarantees the document declares UTF-8. kage serialises every
// saved page as UTF-8, so a stale source declaration must be rewritten or it
// can make a standalone reader mojibake the output (issue #16). Missing
// declarations are inserted at the start of <head>. The return values report
// insertion and rewriting separately so the exported Report keeps the existing
// meaning of CharsetAdded.
func ensureCharset(root *html.Node) (added, rewritten bool) {
	head := findElement(root, atom.Head)
	if head == nil {
		return false, false
	}
	fix := fixCharsetMetas(root)
	if fix.declared {
		return false, fix.rewritten
	}
	meta := &html.Node{
		Type:     html.ElementNode,
		Data:     "meta",
		DataAtom: atom.Meta,
		Attr:     []html.Attribute{{Key: "charset", Val: "utf-8"}},
	}
	// The declaration must precede any content for a reader to honour it, so it
	// goes first in <head>.
	head.InsertBefore(meta, head.FirstChild)
	return true, fix.rewritten
}

type charsetMetaFix struct {
	declared  bool
	rewritten bool
}

// fixCharsetMetas finds charset declarations anywhere in the parsed document
// and rewrites non-UTF-8 values. Searching the whole tree also handles malformed
// input whose meta node Chrome serialised outside <head> without adding a
// second, contradictory declaration. Declarations inside template content are
// rewritten but do not count as declarations for the containing document.
func fixCharsetMetas(n *html.Node) charsetMetaFix {
	var fix charsetMetaFix
	if n.Type == html.ElementNode && n.DataAtom == atom.Meta {
		if charset := strings.TrimSpace(attr(n, "charset")); charset != "" {
			fix.declared = true
			if !strings.EqualFold(charset, "utf-8") {
				setAttr(n, "charset", "utf-8")
				fix.rewritten = true
			}
		} else if strings.EqualFold(attr(n, "http-equiv"), "content-type") {
			content, contentFix := rewriteContentTypeCharset(attr(n, "content"))
			fix = contentFix
			if contentFix.rewritten {
				setAttr(n, "content", content)
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		childFix := fixCharsetMetas(c)
		fix.rewritten = fix.rewritten || childFix.rewritten
		if n.Type != html.ElementNode || n.DataAtom != atom.Template {
			fix.declared = fix.declared || childFix.declared
		}
	}
	return fix
}

// rewriteContentTypeCharset rewrites a charset parameter while preserving the
// media type and other parameters. It accepts optional whitespace around '='.
func rewriteContentTypeCharset(content string) (string, charsetMetaFix) {
	var fix charsetMetaFix
	parts := strings.Split(content, ";")
	for i := 1; i < len(parts); i++ {
		key, value, ok := strings.Cut(parts[i], "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "charset") {
			continue
		}
		fix.declared = true
		if !strings.EqualFold(strings.Trim(strings.TrimSpace(value), `"'`), "utf-8") {
			parts[i] = " charset=utf-8"
			fix.rewritten = true
		}
	}
	return strings.Join(parts, ";"), fix
}

// findElement returns the first element node of the given atom in document
// order, or nil if none exists.
func findElement(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, a); found != nil {
			return found
		}
	}
	return nil
}

// mobileCSS is injected when MobileReadable is set. It rewrites font-era
// HTML for comfortable reading on a phone. Key rules:
//
//   - box-sizing:border-box — makes padding predictable in a layout built with
//     HTML width attributes, so our padding doesn't cause overflow.
//   - body — no fixed max-width here; let the table rules handle width instead.
//     overflow-x:hidden catches any stray overflow without a scrollbar.
//   - font element — overrides in-HTML size/face attributes (e.g. <font size="2">).
//   - [width],[height] — cancels all HTML attribute widths/heights on every
//     element (tables, tds, imgs, etc.) so fixed-pixel columns become fluid.
//   - table — fluid, auto layout, no horizontal scroll.
//   - td — auto width so a three-column table (nav | spacer | content) collapses
//     to one usable column once the nav td is hidden.
//   - img — responsive: never wider than its container.
//   - td:has(>img[usemap]) — hides the entire nav column td, not just the image
//     inside it; hiding only the img left a tall empty white box.
//   - td:has(>img[src*="trans_1x1"]) — hides the 26 px spacer column td (a 1×1
//     transparent GIF whose only job was spacing in the original table layout).
const mobileCSS = `*{box-sizing:border-box}` +
	`:root{font-size:18px}` +
	`body{margin:0;padding:.75em 1em;line-height:1.7;font-family:Georgia,"Times New Roman",serif;overflow-x:hidden}` +
	`font{font-size:1rem!important;font-family:inherit!important;color:inherit!important}` +
	`[width]{width:auto!important;max-width:100%!important}` +
	`[height]{height:auto!important}` +
	`table{width:100%!important;max-width:100%!important;table-layout:auto!important;border-collapse:collapse!important;word-break:break-word}` +
	`td,th{width:auto!important;max-width:100%!important;padding:.35em .5em!important;vertical-align:top!important;overflow-wrap:break-word}` +
	`img{max-width:100%!important;height:auto!important}` +
	`img[usemap],map{display:none!important}` +
	`td:has(>img[usemap]),td:has(>map){display:none!important}` +
	`img[src*="trans_1x1"],img[src*="spacer"],img[height="1"],img[width="1"]{display:none!important}` +
	`td:has(>img[src*="trans_1x1"]:only-child),td:has(>img[height="1"]:only-child){display:none!important}`

// ensureViewport inserts <meta name="viewport" content="width=device-width,
// initial-scale=1"> at the top of <head> when the document does not already
// carry one. Without it a mobile browser shrinks the page to fit the screen
// at desktop scale, making text unreadably small regardless of CSS font sizes.
func ensureViewport(root *html.Node) {
	head := findElement(root, atom.Head)
	if head == nil {
		return
	}
	// Check whether a viewport meta already exists.
	for c := head.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Meta &&
			strings.EqualFold(attr(c, "name"), "viewport") {
			return
		}
	}
	meta := &html.Node{
		Type:     html.ElementNode,
		Data:     "meta",
		DataAtom: atom.Meta,
		Attr: []html.Attribute{
			{Key: "name", Val: "viewport"},
			{Key: "content", Val: "width=device-width, initial-scale=1"},
		},
	}
	head.InsertBefore(meta, head.FirstChild)
}

// injectMobileCSS appends a <style> block containing mobileCSS to <head>.
// It goes at the end of <head> so it wins specificity ties over any existing
// inline styles the page already carries.
func injectMobileCSS(root *html.Node) {
	head := findElement(root, atom.Head)
	if head == nil {
		return
	}
	style := &html.Node{
		Type:     html.ElementNode,
		Data:     "style",
		DataAtom: atom.Style,
	}
	style.AppendChild(&html.Node{Type: html.TextNode, Data: mobileCSS})
	head.AppendChild(style)
}

// insertBanner prepends an HTML comment to the document, after the doctype so
// the doctype stays the first thing in the file. A comment ahead of it is legal
// and modern browsers still read the doctype that follows, but older ones and
// several offline readers take anything before it as a reason to drop into
// quirks mode, which is the whole thing the doctype is there to prevent.
func insertBanner(root *html.Node, text string) {
	c := &html.Node{Type: html.CommentNode, Data: " " + text + " "}
	at := root.FirstChild
	if at != nil && at.Type == html.DoctypeNode {
		at = at.NextSibling
	}
	if at != nil {
		root.InsertBefore(c, at)
	} else {
		root.AppendChild(c)
	}
}

// attr returns the value of n's attribute key (case-insensitive), or "".
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, key, value string) {
	for i := range n.Attr {
		if strings.EqualFold(n.Attr[i].Key, key) {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: value})
}
