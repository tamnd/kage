package pack

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// LinuxAppOptions controls how a Linux application directory is assembled around
// a packed viewer.
type LinuxAppOptions struct {
	Out      string      // path to the .AppDir directory
	Base     string      // base kage binary (must be a Linux build); default os.Executable()
	Name     string      // display name shown in menus
	ExecName string      // base name for the .desktop and icon files
	Comment  string      // optional one-line description for the launcher
	Version  string      // version string recorded in the .desktop
	Icon     image.Image // optional; written as the launcher icon
}

// BuildAppDir writes an AppImage-style application directory. The layout follows
// the AppDir convention so `appimagetool` can fold it into a single
// double-clickable .AppImage, but it is useful on its own: AppRun is the packed
// viewer, the .desktop file launches it with Terminal=false (no console), and
// the icon gives it a face in the file manager and menus.
//
// It returns the AppDir path, the size of the executable inside it, and whether
// an icon was written (the caller needs that to decide if an .AppImage can be
// built, since AppImage requires one).
func BuildAppDir(zimBytes []byte, opts LinuxAppOptions) (path string, size int64, hasIcon bool, err error) {
	if opts.Out == "" {
		return "", 0, false, fmt.Errorf("pack: BuildAppDir requires an output path")
	}
	if !strings.HasSuffix(opts.Out, ".AppDir") {
		return "", 0, false, fmt.Errorf("pack: app dir path must end in .AppDir, got %q", opts.Out)
	}

	base := opts.Base
	if base == "" {
		exe, e := os.Executable()
		if e != nil {
			return "", 0, false, fmt.Errorf("pack: locate base binary: %w", e)
		}
		base = exe
	}
	baseBytes, err := os.ReadFile(base)
	if err != nil {
		return "", 0, false, fmt.Errorf("pack: read base binary %q: %w", base, err)
	}

	execName := opts.ExecName
	if execName == "" {
		execName = "kage"
	}
	name := opts.Name
	if name == "" {
		name = execName
	}

	if err := os.RemoveAll(opts.Out); err != nil {
		return "", 0, false, err
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return "", 0, false, err
	}

	// AppRun is the AppImage entrypoint; pointing it straight at the packed
	// binary keeps the directory to a single executable with no wrapper script.
	exe := assemble(baseBytes, zimBytes)
	if err := os.WriteFile(filepath.Join(opts.Out, "AppRun"), exe, 0o755); err != nil {
		return "", 0, false, err
	}

	hasIcon = opts.Icon != nil
	if hasIcon {
		pngBytes, err := encodeIconPNG(opts.Icon, 256)
		if err != nil {
			return "", 0, false, err
		}
		// The icon lives at the root under the name the .desktop references, and
		// .DirIcon is the AppImage convention for the directory's own icon.
		if err := os.WriteFile(filepath.Join(opts.Out, execName+".png"), pngBytes, 0o644); err != nil {
			return "", 0, false, err
		}
		if err := os.WriteFile(filepath.Join(opts.Out, ".DirIcon"), pngBytes, 0o644); err != nil {
			return "", 0, false, err
		}
	}

	desktop := desktopEntry(desktopData{
		Name:     name,
		ExecName: execName,
		Comment:  opts.Comment,
		Version:  opts.Version,
		HasIcon:  hasIcon,
	})
	if err := os.WriteFile(filepath.Join(opts.Out, execName+".desktop"), []byte(desktop), 0o644); err != nil {
		return "", 0, false, err
	}

	return opts.Out, int64(len(exe)), hasIcon, nil
}

type desktopData struct {
	Name     string
	ExecName string
	Comment  string
	Version  string
	HasIcon  bool
}

// desktopEntry renders a freedesktop .desktop launcher. Terminal=false is the
// line that keeps a console from opening, the Linux echo of the macOS .app.
func desktopEntry(d desktopData) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=" + desktopValue(d.Name) + "\n")
	if d.Comment != "" {
		b.WriteString("Comment=" + desktopValue(d.Comment) + "\n")
	}
	// AppRun is on PATH inside a running AppImage, so Exec names it directly.
	b.WriteString("Exec=AppRun\n")
	if d.HasIcon {
		b.WriteString("Icon=" + d.ExecName + "\n")
	}
	b.WriteString("Categories=Network;Utility;\n")
	b.WriteString("Terminal=false\n")
	if d.Version != "" {
		b.WriteString("X-AppImage-Version=" + desktopValue(d.Version) + "\n")
	}
	return b.String()
}

// desktopValue strips the characters that would break a .desktop key=value line
// (newlines and the leading-space/escape pitfalls), which is enough for a name
// or comment drawn from a page title.
func desktopValue(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

// encodeIconPNG scales img to a px-by-px square and encodes it as PNG, the icon
// format the freedesktop world expects.
func encodeIconPNG(img image.Image, px int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaleSquare(img, px)); err != nil {
		return nil, fmt.Errorf("pack: encode icon png: %w", err)
	}
	return buf.Bytes(), nil
}
