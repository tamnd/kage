package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/tamnd/kage/pack"
	"github.com/tamnd/kage/viewer"
	"github.com/tamnd/kage/zim"
)

func newOpenCmd() *cobra.Command {
	var addr string
	var openBrowser bool
	cmd := &cobra.Command{
		Use:   "open <file.zim>",
		Short: "Serve a ZIM archive in your browser for offline reading",
		Long: "open serves a packed ZIM file over a local HTTP server so you can browse the\n" +
			"site exactly as it was cloned. It is the read side of kage pack --format zim.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpen(cmd.Context(), args[0], addr, openBrowser)
		},
	}
	cmd.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:8800", "address to listen on")
	cmd.Flags().BoolVar(&openBrowser, "open", true, "open the default browser")
	return cmd
}

func runOpen(ctx context.Context, path, addr string, openBrowser bool) error {
	r, err := zim.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open %q: %w", path, err)
	}
	defer func() { _ = r.Close() }()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", addr, err)
	}
	url := "http://" + ln.Addr().String()

	fmt.Fprintln(os.Stderr, styleTitle.Render("kage open")+" "+styleDim.Render(path))
	fmt.Fprintln(os.Stderr, "  open "+styleAccent.Render(url))
	if viewer.Native {
		fmt.Fprintln(os.Stderr, styleDim.Render("  close the window to stop"))
	} else {
		fmt.Fprintln(os.Stderr, styleDim.Render("  press Ctrl-C to stop"))
	}

	srv := &http.Server{Handler: pack.Handler(r)}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(ln) }()

	_ = viewer.Show(ctx, viewer.Options{Title: archiveTitle(r), URL: url, Browser: openBrowser})
	_ = srv.Close()
	if err := <-srvErr; err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
