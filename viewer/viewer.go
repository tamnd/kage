// Package viewer presents a served site to the user. It has two
// implementations chosen at build time: by default (pure Go, CGO_ENABLED=0) it
// opens the system browser, and with the "webview" build tag (which needs cgo)
// it opens a native window backed by the operating system's WebView, so a
// packed kage binary feels like a standalone app rather than a browser tab.
//
// Both builds expose the same three symbols: Native, LockMainThread, and Show.
// The caller starts an HTTP server, then calls Show on the main goroutine; Show
// blocks until the window is closed (native) or the context is cancelled
// (browser), at which point the caller shuts the server down.
package viewer

// Options configures a viewer window.
type Options struct {
	Title string // window title; the archive's M/Title, falling back to "kage"
	URL   string // local URL the server is listening on
	// Browser, in the default build, opens the system browser. The native build
	// ignores it and always shows its own window.
	Browser bool
}

// Native reports whether this build opens a native window (webview tag) or
// falls back to the system browser. Show and LockMainThread are defined in the
// per-build files browser.go and webview.go.
