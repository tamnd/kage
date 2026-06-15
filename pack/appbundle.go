package pack

import (
	"encoding/xml"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
)

// AppOptions controls how a macOS .app bundle is assembled around a packed
// viewer.
type AppOptions struct {
	Out        string      // path to the .app directory
	Base       string      // base kage binary (must be a macOS build); default os.Executable()
	Name       string      // display name shown in Finder and the Dock
	ExecName   string      // file name of the executable inside Contents/MacOS
	Identifier string      // CFBundleIdentifier, e.g. com.kage.paulgraham
	Version    string      // CFBundleShortVersionString; default 1.0
	Icon       image.Image // optional; written as Resources/icon.icns
}

// BuildApp writes a double-clickable macOS application bundle that serves the
// packed site. Finder runs Contents/MacOS/<ExecName> with no terminal attached,
// which is the whole point: the bare appended binary opens a Terminal window
// when double-clicked, a .app does not. The executable is the same
// base++zim++trailer image BuildBinary produces, so Embedded finds the archive
// at runtime exactly as it does for a plain viewer.
//
// It returns the bundle path and the size of the executable inside it (the part
// that dominates, since the plist and icon are tiny).
func BuildApp(zimBytes []byte, opts AppOptions) (string, int64, error) {
	if opts.Out == "" {
		return "", 0, fmt.Errorf("pack: BuildApp requires an output path")
	}
	if filepath.Ext(opts.Out) != ".app" {
		return "", 0, fmt.Errorf("pack: app bundle path must end in .app, got %q", opts.Out)
	}

	base := opts.Base
	if base == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", 0, fmt.Errorf("pack: locate base binary: %w", err)
		}
		base = exe
	}
	baseBytes, err := os.ReadFile(base)
	if err != nil {
		return "", 0, fmt.Errorf("pack: read base binary %q: %w", base, err)
	}

	execName := opts.ExecName
	if execName == "" {
		execName = "kage"
	}
	name := opts.Name
	if name == "" {
		name = execName
	}
	version := opts.Version
	if version == "" {
		version = "1.0"
	}

	// Start from a clean bundle so a rebuild never leaves a stale icon or
	// executable behind. The path is known to end in .app from the guard above.
	if err := os.RemoveAll(opts.Out); err != nil {
		return "", 0, err
	}
	contents := filepath.Join(opts.Out, "Contents")
	macOS := filepath.Join(contents, "MacOS")
	resources := filepath.Join(contents, "Resources")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		return "", 0, err
	}

	hasIcon := opts.Icon != nil
	if hasIcon {
		if err := os.MkdirAll(resources, 0o755); err != nil {
			return "", 0, err
		}
		icns, err := EncodeICNS(opts.Icon)
		if err != nil {
			return "", 0, err
		}
		if err := os.WriteFile(filepath.Join(resources, "icon.icns"), icns, 0o644); err != nil {
			return "", 0, err
		}
	}

	plist := infoPlist(infoPlistData{
		Name:       name,
		ExecName:   execName,
		Identifier: opts.Identifier,
		Version:    version,
		HasIcon:    hasIcon,
	})
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		return "", 0, err
	}
	// PkgInfo is the eight-byte legacy type/creator stamp; APPL???? is the
	// generic application value every modern bundle uses.
	if err := os.WriteFile(filepath.Join(contents, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return "", 0, err
	}

	exe := assemble(baseBytes, zimBytes)
	if err := os.WriteFile(filepath.Join(macOS, execName), exe, 0o755); err != nil {
		return "", 0, err
	}
	return opts.Out, int64(len(exe)), nil
}

type infoPlistData struct {
	Name       string
	ExecName   string
	Identifier string
	Version    string
	HasIcon    bool
}

// infoPlist renders the bundle's Info.plist. Values are XML-escaped so a site
// title with an ampersand or angle bracket cannot corrupt the file.
func infoPlist(d infoPlistData) string {
	pairs := [][2]string{
		{"CFBundleName", d.Name},
		{"CFBundleDisplayName", d.Name},
		{"CFBundleExecutable", d.ExecName},
		{"CFBundleIdentifier", d.Identifier},
		{"CFBundleInfoDictionaryVersion", "6.0"},
		{"CFBundlePackageType", "APPL"},
		{"CFBundleShortVersionString", d.Version},
		{"CFBundleVersion", d.Version},
		{"LSMinimumSystemVersion", "10.13"},
	}
	if d.HasIcon {
		pairs = append(pairs, [2]string{"CFBundleIconFile", "icon"})
	}

	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	for _, kv := range pairs {
		b.WriteString("\t<key>" + esc(kv[0]) + "</key>\n")
		b.WriteString("\t<string>" + esc(kv[1]) + "</string>\n")
	}
	// NSHighResolutionCapable is a boolean, not a string, so the icon and text
	// render sharp on Retina displays.
	b.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
