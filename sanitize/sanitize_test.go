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
	if !rep.CharsetAdded {
		t.Error("CharsetAdded = false, want true")
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

func TestCharsetNotDuplicated(t *testing.T) {
	// A page that already declares a charset, in either form, is left alone.
	cases := []string{
		`<html><head><meta charset="utf-8"><title>x</title></head><body></body></html>`,
		`<html><head><meta http-equiv="Content-Type" content="text/html; charset=ISO-8859-1"><title>x</title></head><body></body></html>`,
	}
	for _, in := range cases {
		out, rep, err := Strip([]byte(in), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if rep.CharsetAdded {
			t.Errorf("CharsetAdded = true for a page that already declares one:\n%s", in)
		}
		if n := strings.Count(strings.ToLower(string(out)), "charset"); n != 1 {
			t.Errorf("charset count = %d, want 1:\n%s", n, out)
		}
	}
}
