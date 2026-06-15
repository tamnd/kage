package asset

import (
	"bytes"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/tamnd/kage/urlx"
	"golang.org/x/net/html"
)

// testSink builds a RefSink that records every URL it sees and rewrites it to a
// local relative path from fromDir, mirroring what the cloner does.
func testSink(seedHost, fromDir string, seen *[]string) RefSink {
	return func(u *url.URL, kind urlx.Kind) string {
		*seen = append(*seen, u.String())
		if !urlx.SameSite(&url.URL{Host: seedHost}, u, true) && kind == urlx.Page {
			return u.String() // external page links stay absolute
		}
		local := urlx.LocalPath(seedHost, u, kind, "")
		return urlx.Rel(fromDir, local)
	}
}

func TestRewriteCSS(t *testing.T) {
	base, _ := url.Parse("https://ex.com/css/main.css")
	in := `@import "base.css";
@import url('https://cdn.io/reset.css');
body { background: url(../img/bg.png); }
.x { background: url("https://cdn.io/sprite.svg"); }
.y { content: url(data:image/gif;base64,AAAA); }`
	var seen []string
	// CSS lives at _kage/ex.com/css/main.css → its dir.
	fromDir := "_kage/ex.com/css"
	out := string(RewriteCSS([]byte(in), base, testSink("ex.com", fromDir, &seen)))

	if !strings.Contains(out, `@import "base.css"`) {
		t.Errorf("local @import not rewritten relative: %s", out)
	}
	if !strings.Contains(out, `url("../img/bg.png")`) {
		t.Errorf("local url() not rewritten relative: %s", out)
	}
	// Cross-origin asset goes under _kage/cdn.io/...; from _kage/ex.com/css that
	// is ../../cdn.io/...
	if !strings.Contains(out, `url("../../cdn.io/reset.css")`) {
		t.Errorf("cross-origin @import not localised: %s", out)
	}
	if !strings.Contains(out, `url("../../cdn.io/sprite.svg")`) {
		t.Errorf("cross-origin url() not localised: %s", out)
	}
	// data: URL must be left exactly as-is.
	if !strings.Contains(out, "url(data:image/gif;base64,AAAA)") {
		t.Errorf("data: URL was altered: %s", out)
	}

	sort.Strings(seen)
	want := []string{
		"https://cdn.io/reset.css",
		"https://cdn.io/sprite.svg",
		"https://ex.com/css/base.css",
		"https://ex.com/img/bg.png",
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("sink saw %v, want %v", seen, want)
	}
}

const htmlDoc = `<!doctype html><html><head>
<link rel="stylesheet" href="/css/main.css">
<link rel="preload stylesheet" href="/css/vp.css" as="style">
<link rel="icon" href="/favicon.ico">
<link rel="canonical" href="https://ex.com/canon">
</head><body>
<a href="/docs/intro">internal</a>
<a href="https://other.com/x">external</a>
<a href="/files/report.pdf">a pdf</a>
<img src="/img/logo.png" srcset="/img/logo.png 1x, /img/logo@2x.png 2x">
<p style="background:url(/img/bg.png)">hi</p>
<style>.a{background:url(/img/tile.png)}</style>
</body></html>`

func TestRewriteHTML(t *testing.T) {
	base, _ := url.Parse("https://ex.com/")
	root, err := html.Parse(strings.NewReader(htmlDoc))
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	// The page is the root index.html, so fromDir is "".
	RewriteHTML(root, base, testSink("ex.com", "", &seen))

	var buf bytes.Buffer
	if err := html.Render(&buf, root); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	checks := map[string]bool{
		`href="_kage/ex.com/css/main.css"`:                  true, // stylesheet localised
		`href="_kage/ex.com/css/vp.css"`:                    true, // multi-value "preload stylesheet" rel localised
		`href="_kage/ex.com/favicon.ico"`:                   true, // icon localised
		`href="https://ex.com/canon"`:                       true, // canonical left alone
		`href="docs/intro/index.html"`:                      true, // internal page → local
		`href="https://other.com/x"`:                        true, // external page stays absolute
		`href="_kage/ex.com/files/report.pdf"`:              true, // pdf link treated as asset
		`src="_kage/ex.com/img/logo.png"`:                   true,
		`_kage/ex.com/img/logo@2x.png 2x`:                   true, // srcset candidate rewritten
		`background:url(&#34;_kage/ex.com/img/bg.png&#34;)`: true, // inline style (attr-escaped quotes)
		`background:url("_kage/ex.com/img/tile.png")`:       true, // <style> text
	}
	for frag, want := range checks {
		if strings.Contains(out, frag) != want {
			t.Errorf("fragment %q present=%v, want %v\n--- output ---\n%s", frag, !want, want, out)
		}
	}
}
