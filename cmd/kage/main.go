// Command kage clones a website into a self-contained offline folder: it renders
// every page in headless Chrome, strips all JavaScript, and localises the CSS,
// images, and fonts so the saved copy looks like the live site but runs no code.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/tamnd/kage/cli"
	"github.com/tamnd/kage/viewer"
)

func main() {
	// Pin the main goroutine to the process's initial OS thread before anything
	// else. In the webview build the native window must be driven from that
	// thread; in the default build this is a harmless no-op.
	viewer.LockMainThread()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.Execute(ctx))
}
