//go:build !webview

package viewer

import (
	"context"
	"os/exec"
	"runtime"
)

// Native is false in the default pure-Go build: there is no native window, so
// the viewer hands the URL to the system browser.
const Native = false

// LockMainThread is a no-op without a native UI to pin to the main thread.
func LockMainThread() {}

// Show opens the system browser at o.URL when o.Browser is set, then blocks
// until the context is cancelled (Ctrl-C), leaving the caller's HTTP server up
// in the meantime. Launching the browser is best-effort; a failure is ignored
// because the URL has already been printed for the user to open by hand.
func Show(ctx context.Context, o Options) error {
	if o.Browser {
		_ = openInBrowser(o.URL)
	}
	<-ctx.Done()
	return nil
}

func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
