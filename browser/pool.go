// Package browser drives a real headless Chrome through the DevTools Protocol so
// JavaScript-built pages are captured as they actually render. kage always goes
// through here: navigate, let the page settle, then serialise the final DOM —
// the same markup a human would have seen — which the rest of the pipeline then
// strips of scripts and localises.
//
// Chrome is launched by this package directly (os/exec + remote debugging), not
// through go-rod's launcher. That keeps github.com/ysmood/leakless — and the
// antivirus-flagged embedded helper it ships — out of the dependency graph, so
// go install and Windows package managers stay clean (issues #68, #72).
package browser

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Options configure a Pool.
type Options struct {
	Headless      bool          // run Chrome without a window
	Workers       int           // max concurrent pages
	Settle        time.Duration // network-idle quiet period after load
	RenderTimeout time.Duration // hard cap per page render
	Scroll        bool          // auto-scroll to trigger lazy-loaded media
	ChromeBin     string        // explicit binary; empty = autodetect
	ControlURL    string        // attach to an existing Chrome instead of launching
}

// DefaultOptions returns the baseline render settings.
func DefaultOptions() Options {
	return Options{
		Headless:      true,
		Workers:       4,
		Settle:        1500 * time.Millisecond,
		RenderTimeout: 30 * time.Second,
	}
}

// Pool owns one Chrome process shared across a run and bounds the number of
// pages open at once.
type Pool struct {
	opts Options
	sem  chan struct{}

	mu       sync.Mutex
	allocCtx context.Context
	cancel   context.CancelFunc
	closed   bool
}

// New creates a Pool. Chrome is launched lazily on the first Render.
func New(opts Options) *Pool {
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	return &Pool{opts: opts, sem: make(chan struct{}, opts.Workers)}
}

// RenderResult is the outcome of rendering one page.
type RenderResult struct {
	HTML     string // the serialised final DOM
	FinalURL string // URL after any client-side redirects
	Title    string
}

// ErrNotHTML reports that a URL kage tried to render as a page is not HTML: the
// server returned some other content type (a zip, a CSV, a PDF, a bare image).
// Such a URL reaches the page worker when its link carried no file extension to
// classify it by. The caller reroutes it to the asset downloader, where the
// asset policy decides whether to localise or leave it remote, instead of saving
// an empty or broken page or letting Chrome download it (issue #32).
type ErrNotHTML struct {
	URL         string
	ContentType string
}

func (e *ErrNotHTML) Error() string {
	return fmt.Sprintf("not HTML (%s): %s", e.ContentType, e.URL)
}

// Render navigates to rawURL, lets it settle, and returns the final rendered
// HTML. It acquires a page slot from the pool and releases it when done.
func (p *Pool) Render(ctx context.Context, rawURL string) (RenderResult, error) {
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return RenderResult{}, ctx.Err()
	}

	if err := p.ensureBrowser(); err != nil {
		return RenderResult{}, err
	}

	tabCtx, cancel := chromedp.NewContext(p.allocCtx)
	defer cancel()
	// The tab context is rooted at the pool's allocator, which outlives any
	// single Render; forward the caller's cancellation so an interrupt (Ctrl-C
	// during a clone) aborts an in-flight page at once instead of waiting out
	// the render timeout below.
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	timeout := p.opts.RenderTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tabCtx, cancelTimeout := context.WithTimeout(tabCtx, timeout)
	defer cancelTimeout()

	// Enable network events and deny browser-initiated downloads before any
	// navigation so a zip/CSV never lands in the user's Downloads folder and so
	// the main-document content-type watcher can classify non-HTML navigations
	// (issue #32). Best-effort: if a call is unsupported, the content-type
	// watcher below still keeps binaries out of the mirror.
	_ = chromedp.Run(tabCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := network.Enable().Do(ctx); err != nil {
			return err
		}
		return browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorDeny).Do(ctx)
	}))
	mainContentType := watchMainDocument(tabCtx)

	// chromedp.Navigate waits for the frame's load event. A denied download
	// aborts navigation, so inspect the captured content type before treating a
	// navigation error as a hard failure.
	navErr := chromedp.Run(tabCtx, chromedp.Navigate(rawURL))
	if ct := waitFor(ctx, mainContentType, 2*time.Second); ct != "" && !isHTML(ct) {
		return RenderResult{}, &ErrNotHTML{URL: rawURL, ContentType: ct}
	}
	if navErr != nil && !isObjRefChainError(navErr) {
		// Object-reference-chain errors from Chrome are non-fatal when the
		// document still loaded (issue #36).
		return RenderResult{}, fmt.Errorf("navigate %s: %w", rawURL, navErr)
	}

	settle(tabCtx, p.opts.Settle)
	if p.opts.Scroll {
		autoScroll(tabCtx)
		settle(tabCtx, p.opts.Settle)
	}

	var html, finalURL, title string
	if err := chromedp.Run(tabCtx,
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	); err != nil {
		if html == "" {
			return RenderResult{}, fmt.Errorf("serialise %s: %w", rawURL, err)
		}
		// Partial success: the DOM serialised but a follow-up read (final URL or
		// title) failed. Keep the rendered page and say so, rather than dropping
		// it or failing silently.
		fmt.Fprintf(os.Stderr, "kage: serialise %s: %v (keeping the rendered DOM)\n", rawURL, err)
	}
	if finalURL == "" {
		finalURL = rawURL
	}
	return RenderResult{HTML: html, FinalURL: finalURL, Title: title}, nil
}

// ensureBrowser lazily connects to or launches Chrome.
func (p *Pool) ensureBrowser() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("pool is closed")
	}
	if p.allocCtx != nil {
		return nil
	}

	if p.opts.ControlURL != "" {
		allocCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), p.opts.ControlURL)
		p.allocCtx = allocCtx
		p.cancel = cancel
		return nil
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("enable-automation", false),
	)
	if p.opts.Headless {
		opts = append(opts, chromedp.Headless)
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}

	// Chrome's sandbox is the main line of defense when rendering pages from
	// the open web, so kage keeps it on by default (issue #10). It is dropped
	// only where it genuinely cannot initialize: inside a container, or when
	// running as root, where Chrome otherwise refuses to start.
	if off, reason := disableSandbox(); off {
		opts = append(opts, chromedp.NoSandbox)
		warnSandboxDisabled(reason)
	}
	// In a container, the default /dev/shm is only 64 MB, too small for
	// Chrome's renderer on large pages (issue #7 notes related container pain).
	if inContainer() {
		opts = append(opts, chromedp.Flag("disable-dev-shm-usage", true))
	}
	if bin := p.chromeBin(); bin != "" {
		opts = append(opts, chromedp.ExecPath(bin))
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	// Touch the browser once so launch failures surface here, not on first page.
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(browserCtx); err != nil {
		browserCancel()
		cancel()
		return fmt.Errorf("launch Chrome: %w", err)
	}
	browserCancel()

	p.allocCtx = allocCtx
	p.cancel = cancel
	return nil
}

// Close shuts down the managed Chrome process.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
		p.allocCtx = nil
	}
	return nil
}

// LookChrome reports the path of a usable Chrome/Chromium binary and whether one
// was found, checking KAGE_CHROME, CHROME_BIN, and the common system install
// locations. Tests use it to skip when no browser is present.
func LookChrome() (string, bool) {
	for _, env := range []string{"KAGE_CHROME", "CHROME_BIN"} {
		if v := os.Getenv(env); v != "" {
			return v, true
		}
	}
	for _, c := range systemChromeCandidates() {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}
	// chromedp's default lookup (google-chrome, chromium, …) on PATH.
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(name); err == nil && p != "" {
			return p, true
		}
	}
	return "", false
}

// chromeBin returns an explicit Chrome path from options or the environment, or
// "" to let the allocator find one.
func (p *Pool) chromeBin() string {
	if p.opts.ChromeBin != "" {
		return p.opts.ChromeBin
	}
	for _, env := range []string{"KAGE_CHROME", "CHROME_BIN"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	for _, c := range systemChromeCandidates() {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func systemChromeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "windows":
		return []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		}
	default:
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	}
}

// disableSandbox decides whether Chrome should launch without its sandbox, with
// a short reason for the log. The secure default is to keep the sandbox on; it
// is dropped only where it cannot run: inside a container, or when running as
// root (Chrome refuses to start a sandbox as root).
func disableSandbox() (off bool, reason string) {
	if inContainer() {
		return true, "container"
	}
	if isRoot() {
		return true, "root"
	}
	return false, ""
}

// warnSandboxDisabled prints why the sandbox was turned off, so dropping a
// security boundary is always visible rather than silent.
func warnSandboxDisabled(reason string) {
	switch reason {
	case "container":
		fmt.Fprintln(os.Stderr, "kage: container detected, Chrome sandbox disabled")
	case "root":
		fmt.Fprintln(os.Stderr, "kage: running as root, Chrome sandbox disabled (run as a non-root user to keep it on)")
	}
}

// inContainer reports whether kage is running inside a container, where Chrome
// needs container-specific flags. It honors IN_DOCKER (set it in your image)
// and the /.dockerenv marker that Docker writes into every container.
func inContainer() bool {
	if envTrue("IN_DOCKER") {
		return true
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

// isRoot reports whether the process runs as the superuser. On Windows
// os.Geteuid returns -1, so this is false there.
func isRoot() bool {
	return os.Geteuid() == 0
}

// envTrue reports whether the named environment variable is set to a truthy
// value.
func envTrue(name string) bool {
	v, ok := envBool(name)
	return ok && v
}

// envBool parses a boolean-ish environment variable. It returns ok=false when
// the variable is unset or empty. "1", "true", "yes", "on" are true and "0",
// "false", "no", "off" are false (case-insensitive); any other non-empty value
// counts as true, so IN_DOCKER=docker reads as set.
func envBool(name string) (val, ok bool) {
	s := strings.TrimSpace(os.Getenv(name))
	if s == "" {
		return false, false
	}
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return true, true
	}
}

// watchMainDocument subscribes to network responses and returns an accessor for
// the main document's content type. The first Document-type response is the main
// frame's navigation; later Document responses are sub-frames (iframes), whose
// type kage does not police, so only the first is kept.
func watchMainDocument(ctx context.Context) func() string {
	var (
		mu sync.Mutex
		ct string
	)
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		e, ok := ev.(*network.EventResponseReceived)
		if !ok || e.Type != network.ResourceTypeDocument || e.Response == nil {
			return
		}
		mu.Lock()
		if ct == "" {
			ct = e.Response.MimeType
		}
		mu.Unlock()
	})
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return ct
	}
}

// waitFor polls get until it returns a non-empty value, the deadline passes, or
// the context is cancelled, then returns whatever it last saw.
func waitFor(ctx context.Context, get func() string, deadline time.Duration) string {
	const step = 20 * time.Millisecond
	for waited := time.Duration(0); waited < deadline; waited += step {
		if v := get(); v != "" {
			return v
		}
		select {
		case <-ctx.Done():
			return get()
		case <-time.After(step):
		}
	}
	return get()
}

// isHTML reports whether a document content type is one kage renders and saves as
// a page. HTML and XHTML qualify; an empty type is treated as HTML so an unlabelled
// response still renders. Anything else is an asset that reached the page worker
// because its link carried no file extension to classify it by.
func isHTML(contentType string) bool {
	mt := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = strings.TrimSpace(mt[:i])
	}
	return mt == "" || mt == "text/html" || mt == "application/xhtml+xml"
}

// isObjRefChainError reports whether err is the Chrome DevTools Protocol error
// "Object reference chain is too long" (code -32000). This surfaces when a
// page's JavaScript builds deeply nested object graphs. The page has still
// loaded — Chrome's internal state tracking hit a limit, not the document
// itself (issue #36).
func isObjRefChainError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Object reference chain is too long")
}

// settle waits a fixed quiet window d after load so late-arriving DOM changes
// land in the snapshot. It approximates network idle with a plain sleep —
// chromedp has no built-in equivalent of rod's WaitRequestIdle — bounded by
// ctx so a cancelled or timed-out render never hangs the worker.
func settle(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// autoScroll scrolls to the bottom in steps to trigger lazy-loaded images. The
// evaluation awaits the scroll promise — chromedp's Evaluate does not await
// promises unless asked, unlike rod's Eval — so Render only continues once the
// page has been walked to the bottom and back.
func autoScroll(ctx context.Context) {
	await := func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}
	_ = chromedp.Run(ctx, chromedp.Evaluate(`(() => new Promise((resolve) => {
		let total = 0;
		const step = 800;
		const timer = setInterval(() => {
			window.scrollBy(0, step);
			total += step;
			if (total >= document.body.scrollHeight) {
				clearInterval(timer);
				window.scrollTo(0, 0);
				resolve(true);
			}
		}, 100);
	}))()`, nil, await))
}
