package browser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
