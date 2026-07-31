package sanitize

import (
	"strings"
	"testing"
)

const page = `<!doctype html>
<html><head>
<meta charset="utf-8">
<meta http-equiv="refresh" content="5;url=https://ex.com/next">
<title>Hi</title>
<link rel="stylesheet" href="/css/main.css">
<link rel="preconnect" href="https://cdn.io">
<link rel="modulepreload" href="/app.js">
<link rel="preload" as="script" href="/runtime.js">
<style>.a{color:red}</style>
<script src="/vendor.js"></script>
<script>window.x=1</script>
</head>
<body onload="boot()">
<h1 onclick="go()">Title</h1>
<a href="javascript:open()">js link</a>
<a href="/real">real link</a>
<img src="/logo.png" onerror="fail()">
<form action="/submit"><input name="q"></form>
<noscript><p>need js</p></noscript>
<p style="background:url(/bg.png)">styled</p>
</body></html>`

func TestStripRemovesAllJS(t *testing.T) {
	out, rep, err := Strip([]byte(page), Options{Banner: "cloned by kage"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if strings.Contains(s, "<script") {
		t.Error("a <script> survived")
	}
	if strings.Contains(s, "onload") || strings.Contains(s, "onclick") || strings.Contains(s, "onerror") {
		t.Error("an on* handler survived")
	}
	if strings.Contains(strings.ToLower(s), "javascript:") {
		t.Error("a javascript: URL survived")
	}
	if strings.Contains(s, "modulepreload") || strings.Contains(s, "preconnect") {
		t.Error("a dead resource hint survived")
	}
	if strings.Contains(s, "http-equiv") {
		t.Error("a meta refresh survived")
	}

	// Layout-bearing markup must survive untouched.
	for _, want := range []string{
		`rel="stylesheet"`, `href="/css/main.css"`,
		`<style>`, `color:red`,
		`src="/logo.png"`, `<form action="/submit">`,
		`background:url(/bg.png)`, `href="/real"`,
		"<!-- cloned by kage -->",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q to survive, output:\n%s", want, s)
		}
	}

	// The js link keeps an anchor but points nowhere dangerous.
	if !strings.Contains(s, `href="#"`) {
		t.Error("javascript: link should be neutralized to href=#")
	}

	if rep.ScriptsRemoved != 2 {
		t.Errorf("ScriptsRemoved = %d, want 2", rep.ScriptsRemoved)
	}
	if rep.HandlersRemoved != 3 {
		t.Errorf("HandlersRemoved = %d, want 3", rep.HandlersRemoved)
	}
	if rep.JSURLsNeutralized != 1 {
		t.Errorf("JSURLsNeutralized = %d, want 1", rep.JSURLsNeutralized)
	}
	if rep.MetaRefreshRemoved != 1 {
		t.Errorf("MetaRefreshRemoved = %d, want 1", rep.MetaRefreshRemoved)
	}
	if rep.DeadLinksRemoved != 3 {
		t.Errorf("DeadLinksRemoved = %d, want 3", rep.DeadLinksRemoved)
	}
	if rep.NoscriptRemoved != 1 {
		t.Errorf("NoscriptRemoved = %d, want 1", rep.NoscriptRemoved)
	}
}

func TestKeepNoscriptUnwraps(t *testing.T) {
	in := `<html><body><noscript><p>fallback text</p></noscript></body></html>`
	out, rep, err := Strip([]byte(in), Options{KeepNoscript: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<noscript") {
		t.Error("noscript wrapper should be gone")
	}
	if !strings.Contains(s, "fallback text") {
		t.Errorf("unwrapped content missing: %s", s)
	}
	if rep.NoscriptUnwrapped != 1 {
		t.Errorf("NoscriptUnwrapped = %d, want 1", rep.NoscriptUnwrapped)
	}
}

func TestConditionalCommentScriptRemoved(t *testing.T) {
	// A downlevel-hidden IE conditional comment hides a <script src> inside a
	// single comment node, where an element-only walk never reaches it.
	in := `<html><head>
<!--[if lt IE 9]><script src="//oss.maxcdn.com/html5shiv/3.7.2/html5shiv.min.js"></script><![endif]-->
</head><body><p>real</p></body></html>`
	out, rep, err := Strip([]byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<script") || strings.Contains(s, "html5shiv") {
		t.Errorf("conditional-comment script survived:\n%s", s)
	}
	if strings.Contains(s, "[if lt IE 9]") {
		t.Errorf("conditional comment survived:\n%s", s)
	}
	if rep.CondCommentsRemoved != 1 {
		t.Errorf("CondCommentsRemoved = %d, want 1", rep.CondCommentsRemoved)
	}
	if !strings.Contains(s, "<p>real</p>") {
		t.Errorf("real content must survive:\n%s", s)
	}
}

func TestConditionalCommentRevealedContentKept(t *testing.T) {
	// The downlevel-revealed form shows its content to non-IE browsers; the
	// content lives in sibling nodes, so only the two markers are stripped.
	in := `<html><body><!--[if gte IE 9]><!--><span class="modern">keep me</span><!--<![endif]--></body></html>`
	out, _, err := Strip([]byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `<span class="modern">keep me</span>`) {
		t.Errorf("revealed content was dropped:\n%s", s)
	}
	if strings.Contains(s, "[if") || strings.Contains(s, "<![endif]") {
		t.Errorf("conditional markers survived:\n%s", s)
	}
}

func TestKeepMetaRefreshPlain(t *testing.T) {
	in := `<html><head><meta http-equiv="refresh" content="5;url=/next"></head><body></body></html>`
	out, _, err := Strip([]byte(in), Options{KeepMetaRefresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "http-equiv") {
		t.Error("plain meta refresh should be kept when KeepMetaRefresh is set")
	}

	// A JS-target refresh is removed even when KeepMetaRefresh is set.
	js := `<html><head><meta http-equiv="refresh" content="0;url=javascript:alert(1)"></head><body></body></html>`
	out2, _, _ := Strip([]byte(js), Options{KeepMetaRefresh: true})
	if strings.Contains(strings.ToLower(string(out2)), "javascript:") {
		t.Error("JS-target meta refresh must be removed regardless")
	}
}

func TestCharsetAddedWhenMissing(t *testing.T) {
	// A page whose source declared its charset only in the HTTP header has no
	// <meta charset>. The saved file must gain one so a reader does not fall back
	// to its locale encoding and mojibake the UTF-8 text.
	in := `<html><head><title>Quotes</title></head><body><p>` +
		"“curly” — café</p></body></html>"
	out, rep, err := Strip([]byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.CharsetFixed {
		t.Error("CharsetFixed = false, want true")
	}
	s := string(out)
	if !strings.Contains(strings.ToLower(s), `<meta charset="utf-8"/>`) {
		t.Errorf("expected an injected meta charset:\n%s", s)
	}
	// It must sit at the very start of <head>, before any content.
	headIdx := strings.Index(s, "<head>")
	metaIdx := strings.Index(strings.ToLower(s), "<meta charset")
	titleIdx := strings.Index(s, "<title>")
	if headIdx >= metaIdx || metaIdx >= titleIdx {
		t.Errorf("meta charset must come first in head (head=%d meta=%d title=%d)", headIdx, metaIdx, titleIdx)
	}
	// The original bytes are preserved as UTF-8.
	if !strings.Contains(s, "café") {
		t.Error("UTF-8 content should be preserved")
	}
}

func TestMobileReadableInjectsViewportAndCSS(t *testing.T) {
	// A paulgraham.com-style page: no viewport, tiny <font size="2"> markup.
	in := `<html><head><title>Essay</title></head>` +
		`<body><font size="2" face="verdana"><p>Hello world.</p></font></body></html>`
	out, _, err := Strip([]byte(in), Options{MobileReadable: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `name="viewport"`) {
		t.Error("viewport meta not injected")
	}
	if !strings.Contains(s, "width=device-width") {
		t.Error("viewport content wrong")
	}
	if !strings.Contains(s, "font-size:18px") {
		t.Error("mobile CSS not injected")
	}
	if !strings.Contains(s, "font{font-size:1rem") {
		t.Error("font override not in mobile CSS")
	}
	// Fluid table rules must be present.
	if !strings.Contains(s, "table{width:100%") {
		t.Error("fluid table rule missing")
	}
	// [width] override must be present so fixed HTML width attributes are cancelled.
	if !strings.Contains(s, "[width]{width:auto") {
		t.Error("[width] override missing")
	}
}

func TestMobileReadableHidesNavColumnTd(t *testing.T) {
	// paulgraham.com wraps its image-map nav in a <td>. Hiding only the img
	// leaves a tall empty box; the CSS must also target the containing td.
	in := `<html><head><title>T</title></head><body>` +
		`<table><tr>` +
		`<td><map name="nav"><area shape="rect" coords="0,0,67,21" href="index.html"></map>` +
		`<img src="nav.gif" width="69" height="357" usemap="#nav"/></td>` +
		`<td><img src="https://cdn.example.com/trans_1x1.gif" height="1" width="26"/></td>` +
		`<td><p>Essay text.</p></td>` +
		`</tr></table>` +
		`</body></html>`
	out, _, err := Strip([]byte(in), Options{MobileReadable: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// td:has(>img[usemap]) rule must be present so the whole nav column is hidden.
	if !strings.Contains(s, "td:has(>img[usemap])") {
		t.Error("td:has(>img[usemap]) rule missing from mobile CSS")
	}
	// td:has(>img[src*="trans_1x1"]) rule must be present for spacer column.
	if !strings.Contains(s, `td:has(>img[src*="trans_1x1"]`) {
		t.Error(`td:has(>img[src*="trans_1x1"]) spacer-column rule missing from mobile CSS`)
	}
}

func TestMobileReadableSkipsExistingViewport(t *testing.T) {
	in := `<html><head><meta name="viewport" content="width=device-width"><title>x</title></head><body></body></html>`
	out, _, err := Strip([]byte(in), Options{MobileReadable: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(out), `name="viewport"`); n != 1 {
		t.Errorf("viewport injected when one already existed (count %d)", n)
	}
}

func TestCharsetNotDuplicated(t *testing.T) {
	// A page that already declares utf-8 is left alone.
	in := `<html><head><meta charset="utf-8"><title>x</title></head><body></body></html>`
	out, rep, err := Strip([]byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.CharsetFixed {
		t.Error("CharsetFixed = true for a page that already declares utf-8")
	}
	if n := strings.Count(strings.ToLower(string(out)), "charset"); n != 1 {
		t.Errorf("charset count = %d, want 1:\n%s", n, out)
	}
}

func TestCharsetRewritesNonUTF8(t *testing.T) {
	// Output is always UTF-8; a stale ISO-8859-1 declaration must be rewritten
	// so a reader does not mojibake the file (issue #16).
	in := `<html><head><meta http-equiv="Content-Type" content="text/html; charset=ISO-8859-1"><title>x</title></head><body></body></html>`
	out, rep, err := Strip([]byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.CharsetFixed {
		t.Error("CharsetFixed = false, want true")
	}
	s := strings.ToLower(string(out))
	if strings.Contains(s, "iso-8859-1") {
		t.Errorf("ISO-8859-1 declaration survived:\n%s", out)
	}
	if !strings.Contains(s, "charset=utf-8") {
		t.Errorf("expected charset=utf-8 rewrite:\n%s", out)
	}
}

func TestActiveContentEscapesRemoved(t *testing.T) {
	in := `<!doctype html><html><head><base href="https://evil.example/"></head><body>
<iframe srcdoc="<script>alert(1)</script>"></iframe>
<iframe src="data:text/html,<script>alert(1)</script>"></iframe>
<iframe src="https://tracker.example/frame"></iframe>
<object data="/widget.html" type="text/html"></object>
<embed src="https://cdn.example/player.html">
<object data="/logo.png" type="image/png"></object>
</body></html>`
	out, rep, err := Strip([]byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<base") {
		t.Error("<base> survived")
	}
	if strings.Contains(s, "srcdoc") {
		t.Error("srcdoc survived")
	}
	if strings.Contains(strings.ToLower(s), "data:text/html") {
		t.Error("data:text/html iframe survived")
	}
	if strings.Contains(s, "tracker.example") {
		t.Error("external iframe src survived")
	}
	if strings.Contains(s, "widget.html") || strings.Contains(s, "player.html") {
		t.Error("HTML object/embed survived")
	}
	// A non-script image object must survive.
	if !strings.Contains(s, "logo.png") {
		t.Error("image object should survive")
	}
	if rep.BaseTagsRemoved < 1 {
		t.Errorf("BaseTagsRemoved = %d, want >= 1", rep.BaseTagsRemoved)
	}
	if rep.ActiveFramesRemoved < 1 {
		t.Errorf("ActiveFramesRemoved = %d, want >= 1", rep.ActiveFramesRemoved)
	}
}
