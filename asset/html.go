package asset

import (
	"net/url"
	"strings"

	"github.com/tamnd/kage/urlx"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// assetRels are the individual <link rel> tokens whose href kage downloads as
// an asset. A rel attribute is a space-separated token list, so a single known
// token in it (for example the "stylesheet" in "preload stylesheet") is enough.
var assetRels = map[string]bool{
	"stylesheet": true, "icon": true,
	"apple-touch-icon": true, "apple-touch-icon-precomposed": true,
	"mask-icon": true, "manifest": true, "preload": true, "prefetch": true,
}

// linkRelDownloadable reports whether a <link rel> names a resource kage should
// download. It treats rel as the space-separated token list the HTML spec
// defines, so "preload stylesheet", "shortcut icon", or a bare "stylesheet" all
// match on a single recognised token.
func linkRelDownloadable(rel string) bool {
	for _, tok := range strings.Fields(strings.ToLower(rel)) {
		if assetRels[tok] {
			return true
		}
	}
	return false
}

// RewriteHTML walks the parsed document and rewrites every resource and link
// reference through sink, resolving relative URLs against base. It mutates the
// tree in place; the caller renders it afterwards. References kage cannot handle
// (data:, mailto:, fragment-only, …) are left untouched.
func RewriteHTML(root *html.Node, base *url.URL, sink RefSink) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			rewriteElement(n, base, sink)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

func rewriteElement(n *html.Node, base *url.URL, sink RefSink) {
	switch n.DataAtom {
	case atom.A, atom.Area:
		rewriteAttr(n, "href", base, sink, pageOrAsset)
	case atom.Iframe, atom.Frame:
		rewriteAttr(n, "src", base, sink, pageOrAsset)
	case atom.Link:
		if linkRelDownloadable(attrVal(n, "rel")) {
			rewriteAttr(n, "href", base, sink, alwaysAsset)
		}
	case atom.Img:
		rewriteAttr(n, "src", base, sink, alwaysAsset)
		rewriteSrcset(n, base, sink)
	case atom.Source:
		rewriteAttr(n, "src", base, sink, alwaysAsset)
		rewriteSrcset(n, base, sink)
	case atom.Video:
		rewriteAttr(n, "src", base, sink, alwaysAsset)
		rewriteAttr(n, "poster", base, sink, alwaysAsset)
	case atom.Audio, atom.Track, atom.Embed:
		rewriteAttr(n, "src", base, sink, alwaysAsset)
	case atom.Object:
		rewriteAttr(n, "data", base, sink, alwaysAsset)
	case atom.Style:
		rewriteStyleText(n, base, sink)
	}
	// Any element may carry an inline style="" with url() references.
	rewriteInlineStyle(n, base, sink)
}

// kindFunc decides whether a normalized URL is a page or an asset.
type kindFunc func(u *url.URL) urlx.Kind

func alwaysAsset(*url.URL) urlx.Kind { return urlx.Asset }

func pageOrAsset(u *url.URL) urlx.Kind {
	if urlx.LikelyPage(u) {
		return urlx.Page
	}
	return urlx.Asset
}

func rewriteAttr(n *html.Node, key string, base *url.URL, sink RefSink, kf kindFunc) {
	for i := range n.Attr {
		if !strings.EqualFold(n.Attr[i].Key, key) {
			continue
		}
		u, err := urlx.Normalize(base, n.Attr[i].Val)
		if err != nil {
			return
		}
		n.Attr[i].Val = sink(u, kf(u))
		return
	}
}

// rewriteSrcset rewrites each candidate URL in a srcset attribute, keeping the
// width/density descriptors intact.
func rewriteSrcset(n *html.Node, base *url.URL, sink RefSink) {
	for i := range n.Attr {
		if !strings.EqualFold(n.Attr[i].Key, "srcset") {
			continue
		}
		n.Attr[i].Val = rewriteSrcsetValue(n.Attr[i].Val, base, sink)
		return
	}
}

func rewriteSrcsetValue(val string, base *url.URL, sink RefSink) string {
	parts := strings.Split(val, ",")
	for i, p := range parts {
		fields := strings.Fields(strings.TrimSpace(p))
		if len(fields) == 0 {
			continue
		}
		u, err := urlx.Normalize(base, fields[0])
		if err != nil {
			continue
		}
		fields[0] = sink(u, urlx.Asset)
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}

func rewriteInlineStyle(n *html.Node, base *url.URL, sink RefSink) {
	for i := range n.Attr {
		if !strings.EqualFold(n.Attr[i].Key, "style") {
			continue
		}
		n.Attr[i].Val = string(RewriteCSS([]byte(n.Attr[i].Val), base, sink))
		return
	}
}

func rewriteStyleText(n *html.Node, base *url.URL, sink RefSink) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			c.Data = string(RewriteCSS([]byte(c.Data), base, sink))
		}
	}
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}
