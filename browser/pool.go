// Package browser drives a real headless Chrome through the DevTools Protocol so
// JavaScript-built pages are captured as they actually render. kage always goes
// through here: navigate, let the page settle, then serialise the final DOM —
// the same markup a human would have seen — which the rest of the pipeline then
// strips of scripts and localises.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/tamnd/kage/internal/rod"
	"github.com/tamnd/kage/internal/stealth"
	"golang.org/x/net/html"
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

	mu      sync.Mutex
	browser *rod.Browser
	closed  bool
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

	b, err := p.getBrowser()
	if err != nil {
		return RenderResult{}, err
	}

	// Create the page in the background so headful mode does not pull the Chrome
	// window to the foreground and steal focus on every new tab. The only
	// difference from stealth.Page is Background:true; the anti-detection script
	// is still injected via stealth.JS.
	page, err := b.Page(proto.TargetCreateTarget{Background: true})
	if err != nil {
		return RenderResult{}, fmt.Errorf("new page: %w", err)
	}
	defer func() { _ = page.Close() }()
	if _, err := page.EvalOnNewDocument(stealth.JS); err != nil {
		return RenderResult{}, fmt.Errorf("inject stealth: %w", err)
	}

	page = page.Context(ctx).Timeout(p.opts.RenderTimeout)

	// Watch the main document's response so a navigation that turns out to be a
	// non-HTML resource (a zip, a CSV, a bare image) is caught and handed back for
	// the asset downloader, rather than rendered as a broken page or, with downloads
	// denied, left as an aborted navigation (issue #32). The content type arrives in
	// the response headers whether Chrome renders the body or aborts it as a denied
	// download, so this catches both.
	mainContentType := watchMainDocument(page)

	navErr := page.Navigate(rawURL)
	// A denied download aborts the navigation, so inspect the captured content type
	// before treating a navigation error as a failure. waitFor gives the response
	// event a brief moment to be processed; for an HTML page it returns at once.
	if ct := waitFor(ctx, mainContentType, 2*time.Second); ct != "" && !isHTML(ct) {
		return RenderResult{}, &ErrNotHTML{URL: rawURL, ContentType: ct}
	}
	if navErr != nil {
		return RenderResult{}, fmt.Errorf("navigate %s: %w", rawURL, navErr)
	}
	if err := page.WaitLoad(); err != nil {
		// Chrome's DevTools Protocol may return "Object reference chain is too
		// long" when a page's JavaScript builds deeply nested object graphs.
		// The page has still loaded its HTML — the error is only about Chrome's
		// internal object tracking, not about the document. Log the warning and
		// continue rendering rather than failing the entire page (issue #36).
		if !isObjRefChainError(err) {
			return RenderResult{}, fmt.Errorf("wait load %s: %w", rawURL, err)
		}
	}
	settle(page, p.opts.Settle)
	if p.opts.Scroll {
		autoScroll(page, scrollBudget(p.opts.RenderTimeout))
		settle(page, p.opts.Settle)
	}

	doc, err := page.HTML()
	if err != nil {
		return RenderResult{}, fmt.Errorf("serialise %s: %w", rawURL, err)
	}
	// page.HTML() is the outerHTML of <html>, which cannot contain the doctype,
	// so put it back (issue #16).
	if dt := pageDoctype(page); dt != "" {
		doc = dt + "\n" + doc
	}

	res := RenderResult{HTML: doc, FinalURL: rawURL}
	if info, err := page.Info(); err == nil && info != nil {
		res.FinalURL = info.URL
		res.Title = info.Title
	}
	return res, nil
}

// doctypeJS reads the parts of document.doctype. It returns them as a JSON
// array rather than a ready-made string so the source form is assembled in Go,
// where a hostile page cannot influence it.
const doctypeJS = `() => {
	const d = document.doctype;
	return d ? JSON.stringify([d.name, d.publicId, d.systemId]) : "";
}`

// pageDoctype returns the document's doctype in source form, or "" when the
// page has none or Chrome will not say.
//
// Chrome's serialisation of a page is the outerHTML of <html>, and a doctype is
// a sibling of <html> rather than a child, so it is never in that string. Left
// alone, every page kage saves comes out with no doctype and every browser
// renders it in quirks mode: the box model reverts to the pre-CSS2 IE one, so
// the saved copy lays out differently from the original, and the <meta charset>
// declaration loses its authority, so a reader is free to fall back to its
// locale encoding and mojibake the text. A reader with no encoding menu, a
// webview or an e-reader, has no way back from that (issue #16).
//
// The doctype is reproduced exactly rather than replaced with <!DOCTYPE html>,
// because the string itself selects the rendering mode: HTML 4.01 Transitional
// is standards mode with its system identifier and quirks mode without it. A
// page that was genuinely quirks mode on the live web keeps no doctype and so
// keeps rendering the way its author saw it.
func pageDoctype(page *rod.Page) string {
	obj, err := page.Eval(doctypeJS)
	if err != nil || obj == nil {
		return ""
	}
	var parts []string
	if err := json.Unmarshal([]byte(obj.Value.Str()), &parts); err != nil || len(parts) != 3 {
		return ""
	}
	return renderDoctype(parts[0], parts[1], parts[2])
}

// renderDoctype rebuilds the source form of a doctype from its DOM parts with
// the same renderer that writes the saved page, so the two always agree.
//
// The parts arrive from an untrusted page and land at the very top of a file we
// write, so anything that does not look like a doctype a parser produced is
// dropped rather than escaped. x/net/html quotes the identifiers but does not
// escape a quote inside one, and it writes the name verbatim.
func renderDoctype(name, publicID, systemID string) string {
	if !validDoctypeName(name) || !validDoctypeID(publicID) || !validDoctypeID(systemID) {
		return ""
	}
	n := &html.Node{Type: html.DoctypeNode, Data: name}
	if publicID != "" {
		n.Attr = append(n.Attr, html.Attribute{Key: "public", Val: publicID})
	}
	if systemID != "" {
		n.Attr = append(n.Attr, html.Attribute{Key: "system", Val: systemID})
	}
	var b strings.Builder
	if err := html.Render(&b, n); err != nil {
		return ""
	}
	return b.String()
}

// validDoctypeName accepts the name of a doctype: ASCII letters only. In
// practice it is always "html", but "math" and "svg" are legal too.
func validDoctypeName(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// validDoctypeID accepts a public or system identifier: printable ASCII with no
// quote or angle bracket, which is every identifier any real doctype uses and
// nothing that could close the token early.
func validDoctypeID(id string) bool {
	if len(id) > 256 {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\'' || r == '<' || r == '>' {
			return false
		}
	}
	return true
}

// getBrowser lazily connects to or launches Chrome.
func (p *Pool) getBrowser() (*rod.Browser, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, fmt.Errorf("pool is closed")
	}
	if p.browser != nil {
		return p.browser, nil
	}

	controlURL := p.opts.ControlURL
	if controlURL == "" {
		l := newChromeLauncher().Leakless(launcherLeakless()).
			Headless(p.opts.Headless).
			Set("disable-blink-features", "AutomationControlled").
			Set("disable-gpu", "")

		// Chrome's sandbox is the main line of defense when rendering pages from
		// the open web, so kage keeps it on by default (issue #10). It is dropped
		// only where it genuinely cannot initialize: inside a container, or when
		// running as root, where Chrome otherwise refuses to start. The decision
		// is logged so it is never silent.
		if off, reason := disableSandbox(); off {
			l = l.Set("no-sandbox", "")
			warnSandboxDisabled(reason)
		}

		// In a container, the default /dev/shm is only 64 MB, too small for
		// Chrome's renderer on large pages, so steer it to a temp file instead.
		// Outside a container /dev/shm is roomy and faster, so leave it alone.
		//
		// The "chrome_crashpad_handler: --database is required" abort seen in
		// containers (issue #7) is not fixed here: the crash-reporter flags do not
		// stop Chrome from spawning the handler. Its real cause is an unwritable
		// HOME, which leaves the crash database path empty; the image keeps HOME
		// writable instead (see the Dockerfile).
		if inContainer() {
			l = l.Set("disable-dev-shm-usage", "")
		}

		if bin := p.chromeBin(); bin != "" {
			l = l.Bin(bin)
		}
		u, err := l.Launch()
		if err != nil {
			return nil, fmt.Errorf("launch Chrome: %w", err)
		}
		controlURL = u
	}

	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("connect Chrome: %w", err)
	}

	// kage never wants Chrome to write a file to disk. Every asset is fetched
	// through kage's own downloader, which applies the size and media policy, so a
	// Chrome-initiated download is only ever an accident: navigating an <a> link
	// that turns out to be a binary (a zip, an installer, a CSV) makes Chrome save
	// it to the user's Downloads folder, a surprise side effect of a crawl
	// (issue #32). Denying downloads browser-wide stops that. The navigation is
	// aborted instead, and Render's non-HTML detection reroutes the URL through the
	// asset downloader, where the asset policy decides its fate. This is
	// best-effort: if the call is unsupported, the non-HTML detection still keeps
	// the binary out of the saved mirror.
	_ = proto.BrowserSetDownloadBehavior{
		Behavior: proto.BrowserSetDownloadBehaviorBehaviorDeny,
	}.Call(b)

	p.browser = b
	return b, nil
}

// Close shuts down the managed Chrome process.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.browser == nil {
		return nil
	}
	err := p.browser.Close()
	p.browser = nil
	return err
}

// LookChrome reports the path of a usable Chrome/Chromium binary and whether one
// was found, checking KAGE_CHROME, CHROME_BIN, rod's own lookup, and the common
// system install locations. Tests use it to skip when no browser is present.
func LookChrome() (string, bool) {
	for _, env := range []string{"KAGE_CHROME", "CHROME_BIN"} {
		if v := os.Getenv(env); v != "" {
			return v, true
		}
	}
	if bin, ok := launcherLookPath(); ok {
		return bin, true
	}
	for _, c := range systemChromeCandidates() {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}
	return "", false
}

// chromeBin returns an explicit Chrome path from options or the environment, or
// "" to let the launcher find/download one.
func (p *Pool) chromeBin() string {
	if p.opts.ChromeBin != "" {
		return p.opts.ChromeBin
	}
	for _, env := range []string{"KAGE_CHROME", "CHROME_BIN"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	if bin, ok := launcherLookPath(); ok {
		return bin
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
//
// Keeping the sandbox on by default and dropping it only here was prompted by
// Dimitrios Prasakis (issue #10); the IN_DOCKER opt-in was suggested on Hacker
// News (https://news.ycombinator.com/item?id=48534865). Thanks to both.
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
// type kage does not police, so only the first is kept. The accessor is safe to
// call from another goroutine. Any setup error leaves the accessor returning "",
// which the caller reads as "unknown, render normally".
func watchMainDocument(page *rod.Page) func() string {
	var (
		mu sync.Mutex
		ct string
	)
	if err := (proto.NetworkEnable{}).Call(page); err != nil {
		return func() string { return "" }
	}
	wait := page.EachEvent(func(e *proto.NetworkResponseReceived) {
		if e.Type != proto.NetworkResourceTypeDocument || e.Response == nil {
			return
		}
		mu.Lock()
		if ct == "" {
			ct = e.Response.MIMEType
		}
		mu.Unlock()
	})
	// EachEvent's wait blocks until the page context ends, draining events as they
	// arrive; run it for the page's lifetime. The deferred page.Close in Render
	// cancels the context and unblocks it.
	go wait()
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return ct
	}
}

// waitFor polls get until it returns a non-empty value, the deadline passes, or
// the context is cancelled, then returns whatever it last saw. It exists because
// the network response is processed on another goroutine, so the value may not be
// set the instant Navigate returns; an HTML page sets it within a few
// milliseconds, while a never-arriving response simply waits out the deadline.
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
// response still renders. Anything else (a zip, a CSV, a PDF, a bare image or
// JSON) is an asset that reached the page worker because its link carried no file
// extension to classify it by.
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

// settle waits for the network to go quiet for d, recovering from any rod
// panic and capping the wait so a chatty page can never hang the worker.
func settle(page *rod.Page, d time.Duration) {
	if d <= 0 {
		return
	}
	defer func() { _ = recover() }()
	done := make(chan struct{})
	go func() {
		defer func() { _ = recover(); close(done) }()
		wait := page.WaitRequestIdle(d, nil, nil, []proto.NetworkResourceType{})
		wait()
	}()
	select {
	case <-done:
	case <-time.After(d + 5*time.Second):
	}
}

// autoScrollJS scrolls a page to the bottom in steps so lazy-loaded content
// mounts before the DOM is serialised, then returns to the top.
//
// It scrolls whichever element actually scrolls rather than the window. An app
// shell keeps a fixed-height <body> with overflow hidden and puts the document
// in an inner container with overflow-y:auto, so window.scrollBy moves nothing
// at all and nothing ever lazy-loads. Feishu, Notion, Linear, Google Docs and
// most admin dashboards are built this way (issue #61).
//
// Progress is measured by reading scrollTop back rather than by summing the
// deltas asked for. The old code compared its running total against
// document.body.scrollHeight, which in an app shell is the viewport height, so
// the very first 800px step cleared the bound and the loop resolved after one
// tick. Summing deltas is wrong on ordinary pages too: once the page is at its
// bottom scrollBy stops moving but the total keeps climbing, so a short page
// exits early and an infinite feed exits at a fixed distance no matter what
// loaded.
//
// A tick counts as progress if the scroller moved or the document grew under
// it, the second being what an infinite feed does. Stopping only after several
// ticks with neither is also what gives content fetched on intersection time to
// arrive, since the request often has not started when the scroll reaches the
// bottom.
const autoScrollJS = `(budgetMs) => new Promise((resolve) => {
	const scrollable = (el) => {
		const s = getComputedStyle(el);
		return (s.overflowY === "auto" || s.overflowY === "scroll" || s.overflowY === "overlay");
	};
	// Start from the document scroller, which scrolls whatever its computed
	// overflow says, then take any inner container with more to scroll.
	let target = document.scrollingElement || document.documentElement;
	let range = target ? target.scrollHeight - target.clientHeight : 0;
	for (const el of document.querySelectorAll("*")) {
		const r = el.scrollHeight - el.clientHeight;
		if (r > range && scrollable(el)) {
			target = el;
			range = r;
		}
	}
	if (!target) {
		resolve("no scroller");
		return;
	}

	const tick = 150;
	const step = Math.max(200, Math.round(target.clientHeight * 0.8));
	const stallLimit = 10;
	const deadline = Date.now() + budgetMs;
	let stalls = 0, lastTop = -1, lastHeight = -1, steps = 0;

	const timer = setInterval(() => {
		const top = target.scrollTop, height = target.scrollHeight;
		if (top === lastTop && height === lastHeight) {
			stalls++;
		} else {
			stalls = 0;
		}
		lastTop = top;
		lastHeight = height;
		if (stalls >= stallLimit || Date.now() >= deadline) {
			clearInterval(timer);
			// Back to the top. On a virtualised document the bottom blocks unmount
			// as the top remounts, so this at least captures the beginning rather
			// than the end.
			target.scrollTop = 0;
			resolve(steps + " steps");
			return;
		}
		target.scrollTop = top + step;
		steps++;
	}, tick);
})`

// autoScroll drives the page to the bottom and back, within budget. A budget is
// enforced inside the page because the render timeout applies to the whole page
// handle: a scroll that ran until the timeout would take page.HTML() down with
// it and fail a render that used to succeed.
func autoScroll(page *rod.Page, budget time.Duration) {
	defer func() { _ = recover() }()
	if budget <= 0 {
		return
	}
	_, _ = page.Eval(autoScrollJS, budget.Milliseconds())
}

// scrollBudget is how long autoScroll may spend, leaving the rest of the render
// timeout for the settle and the serialisation that follow it.
func scrollBudget(renderTimeout time.Duration) time.Duration {
	const floor = 5 * time.Second
	if renderTimeout <= 0 {
		return floor
	}
	if half := renderTimeout / 2; half > floor {
		return half
	}
	return min(floor, renderTimeout)
}
