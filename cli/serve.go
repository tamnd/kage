package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve [dir]",
		Short: "Preview a cloned folder in your browser",
		Long: "serve runs a local static file server over a cloned folder so you can click\n" +
			"through the mirror exactly as a visitor would. With no dir it serves the\n" +
			"current directory.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			return runServe(cmd.Context(), dir, addr)
		},
	}
	cmd.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:8800", "address to listen on")
	return cmd
}

func runServe(ctx context.Context, dir, addr string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot serve %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", dir)
	}
	abs, _ := filepath.Abs(dir)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", addr, err)
	}

	srv := &http.Server{Handler: http.FileServer(http.Dir(abs))}
	fmt.Fprintln(os.Stderr, styleTitle.Render("kage serve")+" "+styleDim.Render(abs))
	fmt.Fprintln(os.Stderr, "  open "+styleAccent.Render("http://"+ln.Addr().String()))
	fmt.Fprintln(os.Stderr, styleDim.Render("  press Ctrl-C to stop"))

	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.Serve(ln) }()

	select {
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		_ = srv.Close()
		if err := <-srvErr; err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	return nil
}
