//go:build webview

package viewer

import (
	"context"
	"runtime"

	webview "github.com/webview/webview_go"
)

// Native is true in the webview build: Show opens a real window backed by the
// operating system's WebView (WKWebView on macOS, WebView2 on Windows,
// WebKitGTK on Linux), so a packed kage feels like a standalone app.
//
// This build needs cgo and links the platform WebView, so it is opt-in
// (-tags webview) and kept out of the default CGO_ENABLED=0 release pipeline.
const Native = true

// LockMainThread pins the calling goroutine to its OS thread. main calls it
// first thing, while the main goroutine is still on the process's initial
// thread, because the macOS WebView must be driven from that thread.
func LockMainThread() { runtime.LockOSThread() }

// Show opens a native window pointed at o.URL and runs the UI event loop on the
// calling (main) goroutine, blocking until the window is closed. A cancelled
// context terminates the loop too, so Ctrl-C still shuts the viewer down. The
// o.Browser flag is ignored: the whole point of this build is the native window.
func Show(ctx context.Context, o Options) error {
	w := webview.New(false)
	defer w.Destroy()

	title := o.Title
	if title == "" {
		title = "kage"
	}
	w.SetTitle(title)
	w.SetSize(1024, 768, webview.HintNone)
	w.Navigate(o.URL)

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			w.Dispatch(func() { w.Terminate() })
		case <-done:
		}
	}()

	w.Run()
	close(done)
	return nil
}
