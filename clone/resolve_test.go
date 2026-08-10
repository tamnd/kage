package clone

import (
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/tamnd/kage/urlx"
)

func TestPageResolveBaseUsesFinalURL(t *testing.T) {
	enqueued, _ := url.Parse("https://ex.com/old")
	root, err := html.Parse(strings.NewReader(`<html><head></head><body><a href="next">n</a></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	base := pageResolveBase(enqueued, "https://ex.com/new/", root)
	if base.String() != "https://ex.com/new/" {
		t.Fatalf("resolve base = %q, want final URL", base)
	}
}

func TestPageResolveBasePrefersDocumentBase(t *testing.T) {
	enqueued, _ := url.Parse("https://ex.com/page")
	root, err := html.Parse(strings.NewReader(
		`<html><head><base href="https://ex.com/dir/"></head><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	base := pageResolveBase(enqueued, "https://ex.com/page", root)
	if base.String() != "https://ex.com/dir/" {
		t.Fatalf("resolve base = %q, want document <base>", base)
	}
}

// An apex→www redirect (or the reverse) must not carry the page's links out of
// scope, or the crawl saves the seed page and stops.
func TestScopedResolveBaseKeepsRedirectInScope(t *testing.T) {
	doc := func(s string) *html.Node {
		root, err := html.Parse(strings.NewReader(s))
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	plain := `<html><head></head><body><a href="/about">a</a></body></html>`

	cases := []struct {
		name     string
		seed     string
		enqueued string
		finalURL string
		html     string
		want     string
	}{
		{"apex redirects to www", "https://ex.com/", "https://ex.com/", "https://www.ex.com/", plain, "https://ex.com/"},
		{"www redirects to apex", "https://www.ex.com/", "https://www.ex.com/", "https://ex.com/", plain, "https://www.ex.com/"},
		{"same-host redirect still wins", "https://ex.com/", "https://ex.com/old", "https://ex.com/new/", plain, "https://ex.com/new/"},
		{"off-scope document base is dropped", "https://ex.com/", "https://ex.com/p", "",
			`<html><head><base href="https://cdn.other.test/app/"></head></html>`, "https://ex.com/p"},
		{"in-scope document base survives an off-scope redirect", "https://ex.com/", "https://ex.com/p", "https://www.ex.com/p",
			`<html><head><base href="https://ex.com/dir/"></head></html>`, "https://ex.com/dir/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed, err := url.Parse(tc.seed)
			if err != nil {
				t.Fatal(err)
			}
			enqueued, err := url.Parse(tc.enqueued)
			if err != nil {
				t.Fatal(err)
			}
			got := scopedResolveBase(seed, enqueued, tc.finalURL, doc(tc.html), urlx.ScopeConfig{})
			if got.String() != tc.want {
				t.Fatalf("scopedResolveBase = %q, want %q", got, tc.want)
			}
			if !urlx.InScope(seed, got, urlx.ScopeConfig{}) {
				t.Fatalf("resolve base %q is out of scope, links would never be enqueued", got)
			}
		})
	}
}

func TestDocumentBaseHref(t *testing.T) {
	root, err := html.Parse(strings.NewReader(
		`<html><head><base href="/subdir/"><base href="https://ignored.example/"></head></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := documentBaseHref(root); got != "/subdir/" {
		t.Fatalf("documentBaseHref = %q, want first base", got)
	}
}
