package clone

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tamnd/kage/browser"
	"github.com/tamnd/kage/robots"
	"github.com/tamnd/kage/urlx"
)

// testSite is a tiny two-page site with a stylesheet, an image, an inline
// script, an onclick handler, and a javascript: link, so a full clone exercises
// rendering, asset localisation, and JavaScript stripping at once.
func testSite(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<link rel="stylesheet" href="/site.css">
<script src="/app.js"></script>
</head><body>
<h1>Home</h1>
<img src="/logo.png" alt="logo">
<a href="/about">About</a>
<a href="javascript:void(0)" onclick="boom()">Danger</a>
<script>console.log("inline")</script>
</body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<link rel="stylesheet" href="/site.css">
</head><body><h1>About</h1><a href="/">Home</a></body></html>`))
	})
	mux.HandleFunc("/site.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte(`body{background:url("/bg.png")} h1{color:red}`))
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(`document.body.dataset.ran = "1";`))
	})
	mux.HandleFunc("/logo.png", servePNG)
	mux.HandleFunc("/bg.png", servePNG)
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
	})
	return httptest.NewServer(mux)
}

// a 1x1 transparent PNG.
var pngBytes = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func servePNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(pngBytes)
}

func TestCloneEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("clone end-to-end drives Chrome; skipped under -short")
	}
	if _, ok := browser.LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping clone end-to-end")
	}

	srv := testSite(t)
	defer srv.Close()

	seed, err := urlx.ParseSeed(srv.URL)
	if err != nil {
		t.Fatalf("parse seed: %v", err)
	}

	out := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutDir = out
	cfg.Settle = 300 * time.Millisecond
	cfg.RenderTimeout = 20 * time.Second
	cfg.Timeout = 10 * time.Second

	c := New(seed, cfg, t.Logf)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Pages < 2 {
		t.Fatalf("expected at least 2 pages, got %d", res.Pages)
	}

	root := res.OutDir
	indexPath := filepath.Join(root, "index.html")
	body := readFile(t, indexPath)

	// JavaScript is gone: no <script>, no onclick, no javascript: URL.
	if strings.Contains(strings.ToLower(body), "<script") {
		t.Error("index.html still contains a <script> tag")
	}
	if strings.Contains(strings.ToLower(body), "onclick") {
		t.Error("index.html still contains an onclick handler")
	}
	if strings.Contains(strings.ToLower(body), "javascript:") {
		t.Error("index.html still contains a javascript: URL")
	}

	// Layout is preserved: the stylesheet link survives and points local.
	if !strings.Contains(body, "stylesheet") {
		t.Error("index.html lost its stylesheet link")
	}
	if strings.Contains(body, srv.URL+"/site.css") {
		t.Error("stylesheet still points at the live origin")
	}

	// The about page and the localised assets exist on disk.
	if !fileExists(filepath.Join(root, "about", "index.html")) {
		t.Error("about page was not written")
	}
	assetDir := filepath.Join(root, cfg.Reserved)
	if !anyFileUnder(t, assetDir, "site.css") {
		t.Error("site.css was not downloaded")
	}
	if !anyFileUnder(t, assetDir, "logo.png") {
		t.Error("logo.png was not downloaded")
	}

	// The localised CSS had its url() rewritten away from the origin.
	css := readAnyFile(t, assetDir, "site.css")
	if strings.Contains(css, srv.URL) {
		t.Error("site.css still references the live origin in url()")
	}
}

// TestPageKeyCollapsesDuplicates guards the dedup identity: http vs https, a
// trailing slash, and "/index.html" vs "/" all name the same output file, so a
// page is crawled once rather than two-to-four times.
func TestPageKeyCollapsesDuplicates(t *testing.T) {
	seed, _ := urlx.ParseSeed("https://ex.com")
	c := New(seed, DefaultConfig(), nil)

	groups := [][]string{
		{"https://ex.com/", "http://ex.com/", "https://ex.com/index.html", "http://ex.com/index.html"},
		{"https://ex.com/docs", "http://ex.com/docs", "https://ex.com/docs/", "https://ex.com/docs/index.html"},
	}
	for _, g := range groups {
		want := c.pageKey(mustURL(t, g[0]))
		for _, raw := range g[1:] {
			if got := c.pageKey(mustURL(t, raw)); got != want {
				t.Errorf("pageKey(%q) = %q, want %q (same page)", raw, got, want)
			}
		}
	}

	// Genuinely different pages must not collapse.
	if c.pageKey(mustURL(t, "https://ex.com/a")) == c.pageKey(mustURL(t, "https://ex.com/b")) {
		t.Error("distinct pages share a key")
	}
}

func TestCrawlDelaySpacesPageStarts(t *testing.T) {
	seed, _ := urlx.ParseSeed("https://ex.com")
	cfg := DefaultConfig()
	cfg.RespectRobots = true
	c := New(seed, cfg, nil)
	c.robots = &robots.Matcher{CrawlDelay: 20 * time.Millisecond}
	c.setupCrawlDelayLimiter()

	ctx := context.Background()
	if !c.waitForCrawlDelay(ctx) {
		t.Fatal("first crawl-delay wait returned false")
	}

	start := time.Now()
	if !c.waitForCrawlDelay(ctx) {
		t.Fatal("second crawl-delay wait returned false")
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("second crawl-delay wait = %v, want at least 15ms", elapsed)
	}
}

func TestCrawlDelayFlagOverridesRobots(t *testing.T) {
	seed, _ := urlx.ParseSeed("https://ex.com")
	cfg := DefaultConfig()
	cfg.RespectRobots = true
	cfg.CrawlDelay = 20 * time.Millisecond
	c := New(seed, cfg, nil)
	c.robots = &robots.Matcher{CrawlDelay: time.Minute}
	c.setupCrawlDelayLimiter()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if !c.waitForCrawlDelay(ctx) {
		t.Fatal("first crawl-delay wait returned false")
	}

	start := time.Now()
	if !c.waitForCrawlDelay(ctx) {
		t.Fatal("second crawl-delay wait returned false")
	}
	elapsed := time.Since(start)
	if elapsed < 15*time.Millisecond {
		t.Fatalf("second crawl-delay wait = %v, want at least 15ms", elapsed)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("second crawl-delay wait = %v, override likely ignored", elapsed)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestCloneResumeSkipsVisited(t *testing.T) {
	if testing.Short() {
		t.Skip("resume test drives Chrome; skipped under -short")
	}
	if _, ok := browser.LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping resume test")
	}

	srv := testSite(t)
	defer srv.Close()
	seed, _ := urlx.ParseSeed(srv.URL)

	out := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutDir = out
	cfg.Settle = 300 * time.Millisecond

	c1 := New(seed, cfg, t.Logf)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := c1.Run(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run with resume on should find the state and re-render nothing new.
	c2 := New(seed, cfg, t.Logf)
	res2, err := c2.Run(ctx)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res2.Pages != 0 {
		t.Fatalf("resume should skip all visited pages, but rendered %d", res2.Pages)
	}
}

func TestCloneRefreshReRenders(t *testing.T) {
	if testing.Short() {
		t.Skip("refresh test drives Chrome; skipped under -short")
	}
	if _, ok := browser.LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping refresh test")
	}

	srv := testSite(t)
	defer srv.Close()
	seed, _ := urlx.ParseSeed(srv.URL)

	out := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutDir = out
	cfg.Settle = 300 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if _, err := New(seed, cfg, t.Logf).Run(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Resume keeps everything in place but renders nothing new; refresh, on the
	// same mirror, re-renders every page so changed content is pulled back in.
	cfg.Refresh = true
	res, err := New(seed, cfg, t.Logf).Run(ctx)
	if err != nil {
		t.Fatalf("refresh run: %v", err)
	}
	if res.Pages < 2 {
		t.Fatalf("refresh should re-render every page, got %d", res.Pages)
	}
	if !fileExists(filepath.Join(out, seed.Hostname(), "index.html")) {
		t.Error("refresh removed the mirror instead of overwriting in place")
	}
}

// TestCloneRoutesNonHTMLToAsset guards issue #32: an extensionless link that
// turns out to be a file (a zip) is classified as a page up front, but once the
// page worker sees it is not HTML it must be handed to the asset downloader, not
// saved as a broken page nor downloaded by Chrome to ~/Downloads.
func TestCloneRoutesNonHTMLToAsset(t *testing.T) {
	if testing.Short() {
		t.Skip("clone test drives Chrome; skipped under -short")
	}
	if _, ok := browser.LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping clone test")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The link has no extension, so it is queued as a page; the server then
		// answers it with a zip.
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<h1>Home</h1><a href="/download">grab the bundle</a></body></html>`))
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04 pretend bundle"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	seed, err := urlx.ParseSeed(srv.URL)
	if err != nil {
		t.Fatalf("parse seed: %v", err)
	}

	out := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutDir = out
	cfg.Settle = 300 * time.Millisecond
	cfg.RenderTimeout = 20 * time.Second
	cfg.Timeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := New(seed, cfg, t.Logf).Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	root := res.OutDir
	// The home page is a real page and is written.
	if !fileExists(filepath.Join(root, "index.html")) {
		t.Error("home page was not written")
	}
	// The zip is NOT saved as a page: no download/index.html exists.
	if fileExists(filepath.Join(root, "download", "index.html")) {
		t.Error("non-HTML target was saved as a page")
	}
	// The zip is fetched as an asset under the reserved tree instead.
	if res.Assets < 1 {
		t.Errorf("expected the zip to be fetched as an asset, assets=%d", res.Assets)
	}
	assetDir := filepath.Join(root, cfg.Reserved)
	if !anyFileUnder(t, assetDir, "download") {
		t.Error("the zip was not downloaded into the reserved asset tree")
	}
	if res.PageErrors != 0 {
		t.Errorf("a rerouted non-HTML target must not count as a page error, got %d", res.PageErrors)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func anyFileUnder(t *testing.T, dir, name string) bool {
	t.Helper()
	found := false
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, name) {
			found = true
		}
		return nil
	})
	return found
}

func readAnyFile(t *testing.T, dir, name string) string {
	t.Helper()
	var out string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, name) {
			b, _ := os.ReadFile(p)
			out = string(b)
		}
		return nil
	})
	return out
}

// chainSite is a five-page site linked in a chain, home to /p1 to /p2 to /p3 to
// /p4. Nothing but following links finds the far end, so a resumed run that has
// lost the frontier cannot reach it.
func chainSite(t *testing.T) *httptest.Server {
	t.Helper()
	page := func(title, next string) string {
		body := `<!doctype html><html><head><title>` + title + `</title></head><body><h1>` + title + `</h1>`
		if next != "" {
			body += `<a href="` + next + `">next</a>`
		}
		return body + `</body></html>`
	}
	links := map[string]string{
		"/":   "/p1",
		"/p1": "/p2",
		"/p2": "/p3",
		"/p3": "/p4",
		"/p4": "",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		next, ok := links[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page(r.URL.Path, next)))
	})
	return httptest.NewServer(mux)
}

// TestCloneResumeFinishesTheCrawl guards issue #36. Only the visited set used to
// be persisted, and the frontier was rebuilt purely by re-rendering pages and
// following their links, which resume guarantees never happens. So an
// interrupted crawl, restarted, found its seed already visited, queued nothing,
// and exited successfully having done no work at all. The README promised the
// opposite.
func TestCloneResumeFinishesTheCrawl(t *testing.T) {
	if testing.Short() {
		t.Skip("resume test drives Chrome; skipped under -short")
	}
	if _, ok := browser.LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping resume test")
	}

	srv := chainSite(t)
	defer srv.Close()
	seed, _ := urlx.ParseSeed(srv.URL)

	out := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutDir = out
	cfg.Settle = 300 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Stop the first run partway through, the way Ctrl-C would.
	first := cfg
	first.MaxPages = 2
	res1, err := New(seed, first, t.Logf).Run(ctx)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if res1.Pages != 2 {
		t.Fatalf("first run wrote %d pages, want 2", res1.Pages)
	}

	// Resume with no cap: it must pick up the frontier the first run left and
	// walk the rest of the chain.
	res2, err := New(seed, cfg, t.Logf).Run(ctx)
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if res2.Pages != 3 {
		t.Errorf("resume wrote %d pages, want the 3 that were left", res2.Pages)
	}

	root := filepath.Join(out, seed.Hostname())
	for _, p := range []string{"index.html", "p1/index.html", "p2/index.html", "p3/index.html", "p4/index.html"} {
		if !fileExists(filepath.Join(root, p)) {
			t.Errorf("%s was never written", p)
		}
	}

	// Nothing is left over, so a third run has genuinely nothing to do.
	res3, err := New(seed, cfg, t.Logf).Run(ctx)
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if res3.Pages != 0 {
		t.Errorf("third run wrote %d pages, want 0", res3.Pages)
	}
}

// TestCloneResumeRetriesFailures covers the other half of issue #36: the
// reporter asked for "a memory of what failed" so a later run could pick the
// missing pages up. A page that errored is not marked visited, so it stays in
// the frontier and a resumed run tries it again.
func TestCloneResumeRetriesFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("resume test drives Chrome; skipped under -short")
	}
	if _, ok := browser.LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping resume test")
	}

	var flaky atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body><a href="/flaky">flaky</a></body></html>`))
		case "/flaky":
			// Down for the first run, back up for the second. The connection is
			// dropped rather than answered with a 5xx because Chrome renders an
			// error page happily; only a dead connection is a render failure.
			if !flaky.Load() {
				if hj, ok := w.(http.Hijacker); ok {
					if conn, _, err := hj.Hijack(); err == nil {
						_ = conn.Close()
						return
					}
				}
				http.Error(w, "down", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body><h1>back up</h1></body></html>`))
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	seed, _ := urlx.ParseSeed(srv.URL)

	out := t.TempDir()
	cfg := DefaultConfig()
	cfg.OutDir = out
	cfg.Settle = 300 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	if _, err := New(seed, cfg, t.Logf).Run(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	flakyPath := filepath.Join(out, seed.Hostname(), "flaky", "index.html")
	if fileExists(flakyPath) {
		t.Fatal("the flaky page should not have been written while it was down")
	}

	flaky.Store(true)
	if _, err := New(seed, cfg, t.Logf).Run(ctx); err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if !fileExists(flakyPath) {
		t.Error("resume should have retried the page that failed")
	}
}
