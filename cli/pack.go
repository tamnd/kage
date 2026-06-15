package cli

import (
	"fmt"
	"image"
	"os"
	"os/exec"
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
	app         bool
	icon        string
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
			"of kage, producing a single executable that serves the site offline when run.\n" +
			"Add --app to wrap that executable in a double-click desktop app (a .app bundle\n" +
			"on macOS, an AppImage-style .AppDir on Linux) with the site's favicon as the icon.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPack(args[0], f)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&f.format, "format", "zim", "output format: zim or binary")
	fs.StringVarP(&f.out, "out", "o", "", "output path (default per format)")
	fs.StringVar(&f.base, "base", "", "base kage binary for the viewer (default this kage)")
	fs.BoolVar(&f.app, "app", false, "wrap the viewer in a double-click desktop app (.app on macOS, .AppImage/.AppDir on Linux)")
	fs.StringVar(&f.icon, "icon", "", "icon file for --app (default the site's favicon)")
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

	// --app wraps the packed viewer in a desktop bundle. It builds on the binary
	// format, so it owns the flow rather than being one more --format value.
	if f.app {
		return runPackApp(dir, f, zopts)
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
		target := resolveTargetOS(f.base)
		out := f.out
		if out == "" {
			out = defaultBinaryName(dir)
		}
		// A Windows viewer must end in .exe to run, whether the name came from
		// --out or the default, so make sure it does.
		if target == "windows" && !strings.HasSuffix(strings.ToLower(out), ".exe") {
			out += ".exe"
		}
		path, size, err := pack.BuildBinary(zbytes, pack.BinaryOptions{Out: out, Base: f.base})
		if err != nil {
			return err
		}
		printPackResult(path, size)
		printRunHint(path, target)
		return nil

	default:
		return fmt.Errorf("unknown --format %q (want zim or binary)", f.format)
	}
}

// runPackApp builds a double-clickable desktop app around the packed viewer,
// shaped for whichever OS the base targets: a .app bundle on macOS, an
// AppImage-style .AppDir on Linux. Windows needs no bundle (the .exe is the
// app), so it is redirected to --format binary with a GUI base.
func runPackApp(dir string, f *packFlags, zopts pack.ZIMOptions) error {
	target := resolveTargetOS(f.base)
	switch target {
	case "windows":
		return fmt.Errorf("a Windows app is just the .exe, with no bundle to build: use --format binary and a GUI base (kage built with -ldflags -H=windowsgui)")
	case "":
		if f.base != "" {
			return fmt.Errorf("--app could not tell which OS %q is for; pass a macOS or Linux kage as --base", f.base)
		}
		// No base and an unknown runtime: fall through with the host's GOOS.
		target = runtime.GOOS
	}

	prog := defaultBinaryName(dir)
	name := f.title
	if name == "" {
		name = prog
	}
	icon, iconSrc, err := resolveIcon(dir, f.icon)
	if err != nil {
		return err
	}
	zbytes, err := pack.BuildZIMBytes(dir, zopts)
	if err != nil {
		return err
	}

	switch target {
	case "darwin":
		return packMacApp(zbytes, dir, f, prog, name, icon, iconSrc)
	case "linux":
		return packLinuxApp(zbytes, dir, f, prog, name, icon, iconSrc)
	default:
		return fmt.Errorf("--app supports macOS and Linux bases; %s is not one of them", osLabel(target))
	}
}

// packMacApp writes the .app bundle and prints how to launch it.
func packMacApp(zbytes []byte, dir string, f *packFlags, prog, name string, icon image.Image, iconSrc string) error {
	out := f.out
	if out == "" {
		out = prog + ".app"
	} else if !strings.HasSuffix(strings.ToLower(out), ".app") {
		out += ".app"
	}
	path, size, err := pack.BuildApp(zbytes, pack.AppOptions{
		Out:        out,
		Base:       f.base,
		Name:       name,
		ExecName:   prog,
		Identifier: bundleID(prog),
		Version:    appVersion(),
		Icon:       icon,
	})
	if err != nil {
		return err
	}
	printPackResult(path, size)
	printIconLine(iconSrc)
	fmt.Fprintf(os.Stderr, "  double-click %s to open the site offline\n", styleAccent.Render(filepath.Base(path)))
	if f.base == "" {
		fmt.Fprintln(os.Stderr, styleDim.Render("  (built around this kage; pass --base a webview build to open a native window instead of the browser)"))
	}
	fmt.Fprintln(os.Stderr, styleDim.Render("  (macOS may quarantine it: xattr -dr com.apple.quarantine "+path+")"))
	return nil
}

// packLinuxApp writes the .AppDir and, when appimagetool is installed, folds it
// into a single double-clickable .AppImage.
func packLinuxApp(zbytes []byte, dir string, f *packFlags, prog, name string, icon image.Image, iconSrc string) error {
	out := f.out
	if out == "" {
		out = prog + ".AppDir"
	} else if !strings.HasSuffix(out, ".AppDir") {
		out += ".AppDir"
	}
	path, size, hasIcon, err := pack.BuildAppDir(zbytes, pack.LinuxAppOptions{
		Out:      out,
		Base:     f.base,
		Name:     name,
		ExecName: prog,
		Comment:  f.description,
		Version:  appVersion(),
		Icon:     icon,
	})
	if err != nil {
		return err
	}
	printPackResult(path, size)
	printIconLine(iconSrc)

	// appimagetool turns the directory into one portable file. It needs an icon,
	// so only attempt it when the mirror gave us one.
	if hasIcon {
		if img, ok := tryAppImage(path, prog); ok {
			fmt.Fprintf(os.Stderr, "  built %s\n", styleTitle.Render(img))
			fmt.Fprintf(os.Stderr, "  double-click %s to open the site offline\n", styleAccent.Render(filepath.Base(img)))
			return nil
		}
	}
	fmt.Fprintf(os.Stderr, "  run %s to open the site offline\n", styleAccent.Render("./"+filepath.Join(filepath.Base(path), "AppRun")))
	fmt.Fprintln(os.Stderr, styleDim.Render("  (install appimagetool to fold this .AppDir into one double-clickable .AppImage)"))
	return nil
}

// tryAppImage runs appimagetool over the AppDir if it is installed, returning
// the .AppImage path on success. A missing tool or a build failure is not fatal:
// the caller falls back to the AppDir.
func tryAppImage(appDir, prog string) (string, bool) {
	tool, err := exec.LookPath("appimagetool")
	if err != nil {
		return "", false
	}
	out := prog + ".AppImage"
	cmd := exec.Command(tool, appDir, out)
	// appimagetool reads the target arch from the AppRun ELF; suppress its noisy
	// progress so kage's own output stays clean, but surface a real failure.
	if err := cmd.Run(); err != nil {
		return "", false
	}
	if _, err := os.Stat(out); err != nil {
		return "", false
	}
	return out, true
}

func printIconLine(iconSrc string) {
	if iconSrc != "" {
		fmt.Fprintf(os.Stderr, "  icon %s\n", styleDim.Render(iconSrc))
	}
}

// resolveIcon picks the bundle icon: an explicit --icon path if given (an error
// there is fatal, since the user asked for that file), otherwise the site's
// favicon discovered in the mirror. A mirror with no usable icon is fine; the
// bundle just ships without a custom one.
func resolveIcon(dir, iconFlag string) (img image.Image, src string, err error) {
	if iconFlag != "" {
		img, err = pack.DecodeIcon(iconFlag)
		if err != nil {
			return nil, "", err
		}
		return img, iconFlag, nil
	}
	if img, src, ok := pack.FindIcon(dir); ok {
		return img, src, nil
	}
	return nil, "", nil
}

// bundleID builds a reverse-DNS CFBundleIdentifier from the program name,
// keeping only characters Apple allows in an identifier.
func bundleID(prog string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(prog) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		id = "app"
	}
	return "com.kage." + id
}

// appVersion uses kage's own version for the bundle, falling back to 1.0 for a
// dev build whose version is the default "dev".
func appVersion() string {
	if Version == "" || Version == "dev" {
		return "1.0"
	}
	return strings.TrimPrefix(Version, "v")
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

// defaultBinaryName derives a clean program name from the mirror's host by
// stripping a trailing dot-suffix (paulgraham.com -> paulgraham). The caller
// appends .exe for Windows targets.
func defaultBinaryName(dir string) string {
	host := filepath.Base(dir)
	if i := strings.IndexByte(host, '.'); i > 0 {
		return host[:i]
	}
	return host
}

// resolveTargetOS reports which OS the packed viewer will run on. With no
// --base it is this kage's OS; with one, we sniff the base's executable header
// so detection does not hinge on the file being named ".exe". If the header is
// unrecognised we fall back to that name heuristic.
func resolveTargetOS(base string) string {
	if base == "" {
		return runtime.GOOS
	}
	if os := pack.SniffOS(base); os != "" {
		return os
	}
	if strings.HasSuffix(strings.ToLower(base), ".exe") {
		return "windows"
	}
	return ""
}

func printPackResult(path string, size int64) {
	fmt.Fprintln(os.Stderr, styleOK.Render("packed")+" "+styleTitle.Render(path))
	fmt.Fprintf(os.Stderr, "  %s %s\n", styleAccent.Render("size"), humanBytes(size))
}

func printRunHint(path, target string) {
	rel := path
	if !strings.ContainsAny(path, "/\\") {
		rel = "./" + path
	}
	// A viewer built for another OS cannot run here, so say where it goes
	// instead of printing a run command that would not work.
	if target != "" && target != runtime.GOOS {
		fmt.Fprintf(os.Stderr, "  this is a %s viewer; copy %s to that machine to run it\n",
			osLabel(target), styleAccent.Render(filepath.Base(path)))
		return
	}
	fmt.Fprintf(os.Stderr, "  run %s to view the site offline\n", styleAccent.Render(rel))
	if target == "darwin" {
		fmt.Fprintln(os.Stderr, styleDim.Render("  (macOS may quarantine it: xattr -d com.apple.quarantine "+rel+")"))
	}
}

// osLabel turns a GOOS value into a friendly name for the run hint.
func osLabel(goos string) string {
	switch goos {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return goos
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
