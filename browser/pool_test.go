package browser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLookChromeReadsEnv(t *testing.T) {
	t.Setenv("KAGE_CHROME", "/custom/chrome")
	bin, ok := LookChrome()
	if !ok || bin != "/custom/chrome" {
		t.Fatalf("LookChrome() = %q, %v; want /custom/chrome, true", bin, ok)
	}
}

func TestEnvBool(t *testing.T) {
	cases := []struct {
		in      string
		set     bool
		wantVal bool
		wantOk  bool
	}{
		{"", false, false, false},
		{"1", true, true, true},
		{"true", true, true, true},
		{"TRUE", true, true, true},
		{"yes", true, true, true},
		{"on", true, true, true},
		{"0", true, false, true},
		{"false", true, false, true},
		{"off", true, false, true},
		{"no", true, false, true},
		{"docker", true, true, true},   // any other non-empty value is true
		{"  true  ", true, true, true}, // trimmed
	}
	for _, c := range cases {
		if c.set {
			t.Setenv("KAGE_TEST_BOOL", c.in)
		} else {
			_ = os.Unsetenv("KAGE_TEST_BOOL")
		}
		val, ok := envBool("KAGE_TEST_BOOL")
		if val != c.wantVal || ok != c.wantOk {
			t.Errorf("envBool(%q) = (%v, %v); want (%v, %v)", c.in, val, ok, c.wantVal, c.wantOk)
		}
	}
}

func TestDisableSandboxDefaultKeepsItOn(t *testing.T) {
	// Not in a container and not root, the sandbox stays on. (When the test
	// itself runs as root, e.g. some CI containers, "root" is the honest
	// reason; accept that rather than asserting a false negative.)
	t.Setenv("IN_DOCKER", "")
	off, reason := disableSandbox()
	if isRoot() || inContainer() {
		if !off {
			t.Errorf("disableSandbox() = false as root/container; want true")
		}
		return
	}
	if off {
		t.Errorf("disableSandbox() = true (%q) on a normal host; want sandbox kept on", reason)
	}
}

func TestInContainerHonorsEnv(t *testing.T) {
	t.Setenv("IN_DOCKER", "1")
	if !inContainer() {
		t.Errorf("inContainer() = false with IN_DOCKER=1; want true")
	}
}

func TestDisableSandboxContainer(t *testing.T) {
	t.Setenv("IN_DOCKER", "true")
	if off, reason := disableSandbox(); !off || reason != "container" {
		t.Errorf("in container: got (%v, %q); want (true, container)", off, reason)
	}
}

func TestLauncherLeaklessDisabledOnWindows(t *testing.T) {
	got := launcherLeakless()
	want := runtime.GOOS != "windows"
	if got != want {
		t.Errorf("launcherLeakless() = %v on %s; want %v", got, runtime.GOOS, want)
	}
}

func TestRenderCapturesFinalDOM(t *testing.T) {
	if testing.Short() {
		t.Skip("render test drives Chrome; skipped under -short")
	}
	if _, ok := LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping render test")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// A page whose visible content is built by JavaScript: only a real
		// browser render captures the injected node.
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<div id="app"></div>
<script>document.getElementById("app").textContent = "rendered-by-js";</script>
</body></html>`))
	}))
	defer srv.Close()

	p := New(Options{Headless: true, Workers: 1, Settle: 300 * time.Millisecond, RenderTimeout: 20 * time.Second})
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := p.Render(ctx, srv.URL)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(res.HTML, "rendered-by-js") {
		t.Errorf("render did not capture the JS-built DOM:\n%s", res.HTML)
	}
}

// TestRenderInjectsStealthScript verifies that Render still injects the stealth
// anti-detection script after switching from stealth.Page(b) to a manual
// b.Page(Background:true) + EvalOnNewDocument(stealth.JS). The stealth script
// normalizes navigator.languages to ["en-US","en"] (without it, Chrome reports
// ["en"]), which serves as a reliable indicator that injection succeeded.
func TestRenderInjectsStealthScript(t *testing.T) {
	if testing.Short() {
		t.Skip("render test drives Chrome; skipped under -short")
	}
	if _, ok := LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping render test")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Page JS writes navigator.languages into the DOM for assertion.
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<div id="langs"></div>
<script>document.getElementById("langs").textContent = JSON.stringify(navigator.languages);</script>
</body></html>`))
	}))
	defer srv.Close()

	p := New(Options{Headless: true, Workers: 1, Settle: 300 * time.Millisecond, RenderTimeout: 20 * time.Second})
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := p.Render(ctx, srv.URL)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// With stealth injected, navigator.languages is ["en-US","en"]; without it, ["en"].
	if !strings.Contains(res.HTML, `["en-US","en"]`) {
		t.Errorf("stealth script not injected: navigator.languages should be [\"en-US\",\"en\"], got HTML:\n%s", res.HTML)
	}
}

func TestIsHTML(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"text/html", true},
		{"text/html; charset=utf-8", true},
		{"TEXT/HTML", true},
		{"  text/html ", true},
		{"application/xhtml+xml", true},
		{"", true}, // unknown: render rather than misclassify
		{"application/zip", false},
		{"text/csv", false},
		{"application/pdf", false},
		{"image/png", false},
		{"application/json", false},
		{"application/octet-stream", false},
	}
	for _, c := range cases {
		if got := isHTML(c.ct); got != c.want {
			t.Errorf("isHTML(%q) = %v, want %v", c.ct, got, c.want)
		}
	}
}

func TestRenderRoutesNonHTML(t *testing.T) {
	if testing.Short() {
		t.Skip("render test drives Chrome; skipped under -short")
	}
	if _, ok := LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping render test")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body><p>a real page</p></body></html>`))
		case "/file.zip", "/download":
			// A binary served with no useful extension on the path, the shape that
			// makes Chrome download to ~/Downloads when navigated to (issue #32).
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("PK\x03\x04 not really a zip"))
		case "/data":
			w.Header().Set("Content-Type", "text/csv")
			_, _ = w.Write([]byte("a,b\n1,2\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New(Options{Headless: true, Workers: 1, Settle: 300 * time.Millisecond, RenderTimeout: 20 * time.Second})
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// A real HTML page renders as before.
	if res, err := p.Render(ctx, srv.URL+"/page"); err != nil {
		t.Errorf("render HTML page: %v", err)
	} else if !strings.Contains(res.HTML, "a real page") {
		t.Errorf("HTML page did not render:\n%s", res.HTML)
	}

	// Non-HTML navigation targets come back as *ErrNotHTML so the caller can route
	// them to the asset downloader instead of saving a broken page or downloading.
	for _, tc := range []struct{ path, wantCT string }{
		{"/download", "application/zip"},
		{"/data", "text/csv"},
	} {
		_, err := p.Render(ctx, srv.URL+tc.path)
		var notHTML *ErrNotHTML
		if !errors.As(err, &notHTML) {
			t.Errorf("Render(%s) error = %v, want *ErrNotHTML", tc.path, err)
			continue
		}
		if !strings.Contains(notHTML.ContentType, tc.wantCT) {
			t.Errorf("Render(%s) content type = %q, want %q", tc.path, notHTML.ContentType, tc.wantCT)
		}
	}
}

func TestRenderDoctype(t *testing.T) {
	cases := []struct {
		name, doctype, public, system, want string
	}{
		{"html5", "html", "", "", "<!DOCTYPE html>"},
		{
			"html401 transitional", "html",
			"-//W3C//DTD HTML 4.01 Transitional//EN",
			"http://www.w3.org/TR/html4/loose.dtd",
			`<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">`,
		},
		{
			// No system identifier: the difference between standards mode and
			// quirks mode for this doctype, so it has to survive verbatim.
			"html401 no system", "html",
			"-//W3C//DTD HTML 4.01//EN", "",
			`<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01//EN">`,
		},
		{
			"xhtml", "html",
			"-//W3C//DTD XHTML 1.0 Strict//EN",
			"http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd",
			`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">`,
		},
		{"legacy compat", "html", "", "about:legacy-compat", `<!DOCTYPE html SYSTEM "about:legacy-compat">`},

		// A page can patch its own DOM, and whatever comes back is written to the
		// top of a file we save, so anything that could close the token early or
		// carry markup is dropped rather than escaped.
		{"no name", "", "", "", ""},
		{"name with markup", "html><script>alert(1)</script", "", "", ""},
		{"quote in public id", "html", `x" "y><script>alert(1)</script`, "", ""},
		{"angle bracket in system id", "html", "", "x><script>alert(1)</script", ""},
		{"newline in public id", "html", "a\nb", "", ""},
	}
	for _, c := range cases {
		if got := renderDoctype(c.doctype, c.public, c.system); got != c.want {
			t.Errorf("%s: renderDoctype(%q, %q, %q) = %q, want %q",
				c.name, c.doctype, c.public, c.system, got, c.want)
		}
	}
}

func TestRenderPreservesDoctype(t *testing.T) {
	if testing.Short() {
		t.Skip("render test drives Chrome; skipped under -short")
	}
	if _, ok := LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping render test")
	}

	pages := map[string]string{
		"/html5":  `<!DOCTYPE html><html><body><p>modern</p></body></html>`,
		"/legacy": `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd"><html><body><p>legacy</p></body></html>`,
		"/quirks": `<html><body><p>quirks</p></body></html>`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := New(Options{Headless: true, Workers: 1, Settle: 300 * time.Millisecond, RenderTimeout: 20 * time.Second})
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cases := []struct{ path, want string }{
		{"/html5", "<!DOCTYPE html>"},
		{"/legacy", `<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">`},
		// A page that was quirks mode on the live web stays quirks mode offline,
		// so it keeps laying out the way its author saw it.
		{"/quirks", ""},
	}
	for _, c := range cases {
		res, err := p.Render(ctx, srv.URL+c.path)
		if err != nil {
			t.Errorf("render %s: %v", c.path, err)
			continue
		}
		if c.want == "" {
			if strings.Contains(strings.ToUpper(res.HTML), "<!DOCTYPE") {
				t.Errorf("%s: got a doctype for a page that had none:\n%s", c.path, res.HTML)
			}
			continue
		}
		if !strings.HasPrefix(res.HTML, c.want) {
			t.Errorf("%s: render should start with %s, got:\n%s", c.path, c.want, res.HTML)
		}
	}
}

func TestScrollBudget(t *testing.T) {
	cases := []struct {
		in, want time.Duration
	}{
		{0, 5 * time.Second},                 // unset: the floor
		{30 * time.Second, 15 * time.Second}, // the default render timeout
		{2 * time.Minute, time.Minute},       // a patient run scrolls longer
		{8 * time.Second, 5 * time.Second},   // half is under the floor
		{3 * time.Second, 3 * time.Second},   // never more than the whole timeout
	}
	for _, c := range cases {
		if got := scrollBudget(c.in); got != c.want {
			t.Errorf("scrollBudget(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

// appShell is the layout behind issue #61: a fixed-height body with overflow
// hidden, and the document itself inside an inner container with
// overflow-y:auto. window.scrollBy moves nothing on a page like this, and
// document.body.scrollHeight is the viewport height, so the old autoScroll both
// scrolled the wrong thing and decided it was finished after one step. Feishu,
// Notion, Linear and most dashboards are built this way.
const appShell = `<!doctype html><html><head><style>
html,body{height:100%;margin:0;overflow:hidden}
#shell{height:100%;overflow-y:auto}
#filler{height:6000px}
</style></head><body>
<div id="shell"><div id="content"><p>first screen</p></div><div id="filler"></div></div>
<script>
const shell = document.getElementById("shell");
shell.addEventListener("scroll", () => {
  if (shell.scrollTop > 1500 && !document.getElementById("lazy")) {
    const p = document.createElement("p");
    p.id = "lazy";
    p.textContent = ["lazy", "loaded", "marker"].join("-");
    document.getElementById("content").appendChild(p);
  }
});
</script></body></html>`

// windowScroll is the ordinary case, a tall page the window scrolls, kept as a
// guard that finding the inner scroller did not break it.
const windowScroll = `<!doctype html><html><head><style>#filler{height:6000px}</style></head>
<body><div id="content"><p>first screen</p></div><div id="filler"></div>
<script>
window.addEventListener("scroll", () => {
  if (window.scrollY > 1500 && !document.getElementById("lazy")) {
    const p = document.createElement("p");
    p.id = "lazy";
    p.textContent = ["lazy", "loaded", "marker"].join("-");
    document.getElementById("content").appendChild(p);
  }
});
</script></body></html>`

func TestAutoScrollTriggersLazyContent(t *testing.T) {
	if testing.Short() {
		t.Skip("scroll test drives Chrome; skipped under -short")
	}
	if _, ok := LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping scroll test")
	}

	pages := map[string]string{"/app-shell": appShell, "/window": windowScroll}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := New(Options{
		Headless: true, Workers: 1, Scroll: true,
		Settle: 300 * time.Millisecond, RenderTimeout: 40 * time.Second,
	})
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	for _, path := range []string{"/app-shell", "/window"} {
		res, err := p.Render(ctx, srv.URL+path)
		if err != nil {
			t.Errorf("render %s: %v", path, err)
			continue
		}
		if !strings.Contains(res.HTML, "lazy-loaded-marker") {
			t.Errorf("%s: --scroll did not trigger the lazy content:\n%s", path, res.HTML)
		}
		if !strings.Contains(res.HTML, "first screen") {
			t.Errorf("%s: the content that was there before the scroll is gone:\n%s", path, res.HTML)
		}
	}
}

func TestAutoScrollLeavesAStaticPageAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("scroll test drives Chrome; skipped under -short")
	}
	if _, ok := LookChrome(); !ok {
		t.Skip("no Chrome/Chromium found; skipping scroll test")
	}

	// Nothing to scroll: the loop must notice at once rather than spin out its
	// whole budget.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body><p>short page</p></body></html>`))
	}))
	defer srv.Close()

	p := New(Options{
		Headless: true, Workers: 1, Scroll: true,
		Settle: 200 * time.Millisecond, RenderTimeout: 40 * time.Second,
	})
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	res, err := p.Render(ctx, srv.URL)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(res.HTML, "short page") {
		t.Errorf("content missing:\n%s", res.HTML)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("scrolling a page with nothing to scroll took %s", elapsed)
	}
}
