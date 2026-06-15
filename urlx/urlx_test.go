package urlx

import (
	"net/url"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return u
}

func TestParseSeed(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"example.com", "https://example.com/", false},
		{"https://example.com", "https://example.com/", false},
		{"http://ex.com/docs", "http://ex.com/docs", false},
		{"HTTPS://Example.COM/A", "https://example.com/A", false},
		{"https://ex.com:443/", "https://ex.com/", false},
		{"http://ex.com:80/", "http://ex.com/", false},
		{"", "", true},
		{"http://", "", true},
	}
	for _, c := range cases {
		got, err := ParseSeed(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseSeed(%q) want error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSeed(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("ParseSeed(%q) = %q, want %q", c.in, got.String(), c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	base := mustParse(t, "https://ex.com/docs/intro")
	cases := []struct {
		ref  string
		want string
		err  bool
	}{
		{"../guide", "https://ex.com/guide", false},
		{"/a/b/../c", "https://ex.com/a/c", false},
		{"page#frag", "https://ex.com/docs/page", false},
		{"//cdn.io/x.css", "https://cdn.io/x.css", false},
		{"HTTPS://Other.COM/Y", "https://other.com/Y", false},
		{"sub/", "https://ex.com/docs/sub/", false},
		// A query busted with a raw date string must come back with its spaces
		// percent-encoded so the request line is valid, while the commas and
		// colons a query legally carries stay as they are.
		{"a.css?Thursday, 26-Feb-2026 16:26:41 UTC", "https://ex.com/docs/a.css?Thursday,%2026-Feb-2026%2016:26:41%20UTC", false},
		{"b.css?v=1&t=a b", "https://ex.com/docs/b.css?v=1&t=a%20b", false},
		{"c.css?x=%20done", "https://ex.com/docs/c.css?x=%20done", false},
		{"#top", "", true},
		{"javascript:void(0)", "", true},
		{"mailto:a@b.com", "", true},
		{"data:image/png;base64,xxx", "", true},
		{"tel:+123", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := Normalize(base, c.ref)
		if c.err {
			if err == nil {
				t.Errorf("Normalize(%q) want error, got %v", c.ref, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Normalize(%q) unexpected error: %v", c.ref, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.ref, got.String(), c.want)
		}
	}
}

func TestInScope(t *testing.T) {
	seed := mustParse(t, "https://ex.com/")
	cases := []struct {
		u    string
		cfg  ScopeConfig
		want bool
	}{
		{"https://ex.com/a", ScopeConfig{}, true},
		{"https://other.com/a", ScopeConfig{}, false},
		{"https://sub.ex.com/a", ScopeConfig{}, false},
		{"https://sub.ex.com/a", ScopeConfig{IncludeSubdomains: true}, true},
		{"https://ex.com/docs/x", ScopeConfig{ScopePrefix: "/docs/"}, true},
		{"https://ex.com/blog/x", ScopeConfig{ScopePrefix: "/docs/"}, false},
		{"https://ex.com/a/private/x", ScopeConfig{ExcludePaths: []string{"/private/"}}, false},
		{"https://ex.com/a/public/x", ScopeConfig{ExcludePaths: []string{"/private/"}}, true},
	}
	for _, c := range cases {
		got := InScope(seed, mustParse(t, c.u), c.cfg)
		if got != c.want {
			t.Errorf("InScope(%q, %+v) = %v, want %v", c.u, c.cfg, got, c.want)
		}
	}
}

func TestLikelyPage(t *testing.T) {
	cases := map[string]bool{
		"https://ex.com/docs":      true,
		"https://ex.com/docs/":     true,
		"https://ex.com/a.html":    true,
		"https://ex.com/file.pdf":  false,
		"https://ex.com/img.png":   false,
		"https://ex.com/style.css": false,
		"https://ex.com/app.js":    false,
	}
	for u, want := range cases {
		if got := LikelyPage(mustParse(t, u)); got != want {
			t.Errorf("LikelyPage(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestLocalPathPages(t *testing.T) {
	seed := "ex.com"
	cases := []struct {
		u    string
		want string
	}{
		{"https://ex.com/", "index.html"},
		{"https://ex.com/docs", "docs/index.html"},
		{"https://ex.com/docs/", "docs/index.html"},
		{"https://ex.com/docs/intro", "docs/intro/index.html"},
		{"https://ex.com/a.html", "a.html/index.html"},
		{"https://sub.ex.com/x", "sub.ex.com/x/index.html"},
		// A directory-index document is the directory itself, so it shares a
		// path with the bare directory and with the http/https variant.
		{"https://ex.com/index.html", "index.html"},
		{"http://ex.com/", "index.html"},
		{"https://ex.com/docs/index.html", "docs/index.html"},
	}
	for _, c := range cases {
		got := LocalPath(seed, mustParse(t, c.u), Page, "")
		if got != c.want {
			t.Errorf("LocalPath(page %q) = %q, want %q", c.u, got, c.want)
		}
	}
}

func TestLocalPathPageQuery(t *testing.T) {
	got := LocalPath("ex.com", mustParse(t, "https://ex.com/search?q=cats"), Page, "")
	if !strings.HasPrefix(got, "search/index__q-") || !strings.HasSuffix(got, ".html") {
		t.Errorf("query page mapped to %q", got)
	}
	// Stable hash.
	got2 := LocalPath("ex.com", mustParse(t, "https://ex.com/search?q=cats"), Page, "")
	if got != got2 {
		t.Errorf("query hash not stable: %q vs %q", got, got2)
	}
	// Different query → different file.
	got3 := LocalPath("ex.com", mustParse(t, "https://ex.com/search?q=dogs"), Page, "")
	if got == got3 {
		t.Errorf("different queries collided: %q", got)
	}
}

func TestLocalPathAssets(t *testing.T) {
	seed := "ex.com"
	cases := []struct {
		u    string
		want string
	}{
		{"https://ex.com/css/main.css", "_kage/ex.com/css/main.css"},
		{"https://cdn.io/font/x.woff2", "_kage/cdn.io/font/x.woff2"},
		{"https://ex.com/avatar", "_kage/ex.com/avatar"},
		{"https://ex.com/assets/", "_kage/ex.com/assets/index"},
	}
	for _, c := range cases {
		got := LocalPath(seed, mustParse(t, c.u), Asset, "")
		if got != c.want {
			t.Errorf("LocalPath(asset %q) = %q, want %q", c.u, got, c.want)
		}
	}
	// Query on an asset folds into the filename before the extension.
	got := LocalPath(seed, mustParse(t, "https://ex.com/img/logo.png?v=3"), Asset, "")
	if !strings.HasPrefix(got, "_kage/ex.com/img/logo__q-") || !strings.HasSuffix(got, ".png") {
		t.Errorf("asset query mapped to %q", got)
	}
}

func TestRel(t *testing.T) {
	cases := []struct {
		from string // page file
		to   string // target file
		want string
	}{
		{"docs/intro/index.html", "docs/index.html", "../index.html"},
		{"docs/intro/index.html", "index.html", "../../index.html"},
		{"docs/intro/index.html", "_kage/ex.com/css/main.css", "../../_kage/ex.com/css/main.css"},
		{"index.html", "docs/index.html", "docs/index.html"},
		{"index.html", "_kage/ex.com/x.png", "_kage/ex.com/x.png"},
		{"a/b/c/index.html", "a/b/c/index.html", "index.html"},
	}
	for _, c := range cases {
		got := Rel(Dir(c.from), c.to)
		if got != c.want {
			t.Errorf("Rel(dir(%q), %q) = %q, want %q", c.from, c.to, got, c.want)
		}
	}
}

// TestRelResolves checks the invariant that follows: joining a page's directory
// with the relative link Rel produced lands exactly on the target file.
func TestRelResolves(t *testing.T) {
	pairs := [][2]string{
		{"docs/intro/index.html", "_kage/cdn.io/a/b.css"},
		{"index.html", "deep/a/b/c/index.html"},
		{"x/y/index.html", "x/index.html"},
	}
	for _, p := range pairs {
		from, to := p[0], p[1]
		rel := Rel(Dir(from), to)
		joined := joinAndClean(Dir(from), rel)
		if joined != to {
			t.Errorf("Rel round-trip: from=%q to=%q rel=%q joined=%q", from, to, rel, joined)
		}
	}
}

func joinAndClean(dir, rel string) string {
	full := dir + "/" + rel
	parts := splitNonEmpty(full)
	var stack []string
	for _, p := range parts {
		switch p {
		case ".":
		case "..":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, p)
		}
	}
	return strings.Join(stack, "/")
}
