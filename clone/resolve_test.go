package clone

import (
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
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
