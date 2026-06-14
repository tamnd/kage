package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tamnd/kage/clone"
	"github.com/tamnd/kage/pack"
)

// packFlags holds the parsed flags for one invocation of kage pack.
type packFlags struct {
	format      string
	out         string
	base        string
	noCompress  bool
	title       string
	description string
	language    string
	date        string
}

func newPackCmd() *cobra.Command {
	f := &packFlags{}
	cmd := &cobra.Command{
		Use:   "pack <mirror-dir>",
		Short: "Pack a cloned mirror into a ZIM file or a self-contained viewer",
		Long: "pack turns a cloned folder into one distributable file. With --format zim\n" +
			"it writes an open ZIM archive (the format Kiwix uses) that kage open or any\n" +
			"ZIM reader can browse. With --format binary it appends that archive to a copy\n" +
			"of kage, producing a single executable that serves the site offline when run.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPack(args[0], f)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&f.format, "format", "zim", "output format: zim or binary")
	fs.StringVarP(&f.out, "out", "o", "", "output path (default per format)")
	fs.StringVar(&f.base, "base", "", "base kage binary for --format binary (default this kage)")
	fs.BoolVar(&f.noCompress, "no-compress", false, "store every cluster raw, no zstd")
	fs.StringVar(&f.title, "title", "", "archive title (default the main page's <title>)")
	fs.StringVar(&f.description, "description", "", "archive description")
	fs.StringVar(&f.language, "language", "eng", "archive language code")
	fs.StringVar(&f.date, "date", time.Now().UTC().Format("2006-01-02"), "archive date (YYYY-MM-DD)")
	return cmd
}

func runPack(mirrorArg string, f *packFlags) error {
	dir := resolveMirror(mirrorArg)
	zopts := pack.ZIMOptions{
		Out:         f.out,
		NoCompress:  f.noCompress,
		Title:       f.title,
		Description: f.description,
		Language:    f.language,
		Date:        f.date,
		Version:     Version,
	}

	switch f.format {
	case "zim":
		out, size, err := pack.BuildZIM(dir, zopts)
		if err != nil {
			return err
		}
		printPackResult(out, size)
		fmt.Fprintf(os.Stderr, "  open %s\n", styleAccent.Render("kage open "+out))
		return nil

	case "binary":
		zbytes, err := pack.BuildZIMBytes(dir, zopts)
		if err != nil {
			return err
		}
		out := f.out
		if out == "" {
			out = defaultBinaryName(dir, f.base)
		}
		path, size, err := pack.BuildBinary(zbytes, pack.BinaryOptions{Out: out, Base: f.base})
		if err != nil {
			return err
		}
		printPackResult(path, size)
		printRunHint(path)
		return nil

	default:
		return fmt.Errorf("unknown --format %q (want zim or binary)", f.format)
	}
}

// resolveMirror accepts either a path to a mirror dir or a bare host. A bare
// host that is not a directory in the working dir is resolved against the
// default out dir, so "kage pack paulgraham.com" works right after a clone.
func resolveMirror(arg string) string {
	if info, err := os.Stat(arg); err == nil && info.IsDir() {
		return arg
	}
	candidate := filepath.Join(clone.DefaultOutDir(), arg)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return arg
}

// defaultBinaryName derives a clean program name from the mirror's host: strip a
// trailing dot-suffix (paulgraham.com -> paulgraham), and append .exe when the
// target is Windows. The target is the running OS unless --base names a binary
// that looks like it was built for Windows.
func defaultBinaryName(dir, base string) string {
	host := filepath.Base(dir)
	name := host
	if i := strings.IndexByte(host, '.'); i > 0 {
		name = host[:i]
	}
	if windowsTarget(base) {
		name += ".exe"
	}
	return name
}

func windowsTarget(base string) bool {
	if base == "" {
		return runtime.GOOS == "windows"
	}
	return strings.HasSuffix(strings.ToLower(base), ".exe")
}

func printPackResult(path string, size int64) {
	fmt.Fprintln(os.Stderr, styleOK.Render("packed")+" "+styleTitle.Render(path))
	fmt.Fprintf(os.Stderr, "  %s %s\n", styleAccent.Render("size"), humanBytes(size))
}

func printRunHint(path string) {
	rel := path
	if !strings.ContainsAny(path, "/\\") {
		rel = "./" + path
	}
	if windowsTarget(path) && runtime.GOOS != "windows" {
		fmt.Fprintf(os.Stderr, "  this is a Windows viewer; run %s on Windows\n", styleAccent.Render(filepath.Base(path)))
		return
	}
	fmt.Fprintf(os.Stderr, "  run %s to view the site offline\n", styleAccent.Render(rel))
	if runtime.GOOS == "darwin" {
		fmt.Fprintln(os.Stderr, styleDim.Render("  (macOS may quarantine it: xattr -d com.apple.quarantine "+rel+")"))
	}
}

// humanBytes renders a byte count in B, KiB, MiB, or GiB.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
