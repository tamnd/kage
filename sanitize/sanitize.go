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
	rep.CharsetAdded = ensureCharset(root)
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

// ensureCharset guarantees the document declares UTF-8, inserting a
// <meta charset="utf-8"> at the top of <head> when none is present, and reports
// whether it added one. kage renders every saved page as UTF-8, but a source
// that set its charset only in the HTTP Content-Type header, with no <meta>
// charset in the markup, loses that signal once the page is a standalone file.
// A reader then serving the bytes without a charset falls back to its locale
// encoding and mojibakes every multibyte character (curly quotes, dashes, a
// non-breaking space). Declaring the charset in the markup makes the page
// self-describing in any reader, kage's own viewer and Kiwix alike.
func ensureCharset(root *html.Node) bool {
	head := findElement(root, atom.Head)
	if head == nil {
		return false
	}
	if hasCharsetMeta(head) {
		return false
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
	return true
}

// hasCharsetMeta reports whether head already declares a character encoding,
// either as <meta charset="..."> or the older <meta http-equiv="Content-Type"
// content="...; charset=...">.
func hasCharsetMeta(head *html.Node) bool {
	for c := head.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.DataAtom != atom.Meta {
			continue
		}
		if attr(c, "charset") != "" {
			return true
		}
		if strings.EqualFold(attr(c, "http-equiv"), "content-type") &&
			strings.Contains(strings.ToLower(attr(c, "content")), "charset=") {
			return true
		}
	}
	return false
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

// insertBanner prepends an HTML comment to the document.
func insertBanner(root *html.Node, text string) {
	c := &html.Node{Type: html.CommentNode, Data: " " + text + " "}
	if root.FirstChild != nil {
		root.InsertBefore(c, root.FirstChild)
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
