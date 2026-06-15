package pack

import (
	"encoding/binary"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildApp(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "kage-darwin")
	baseBytes := []byte("\xcf\xfa\xed\xfeMACHO-BASE-BYTES")
	if err := os.WriteFile(base, baseBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	zim := []byte("ZIM-ARCHIVE-PAYLOAD")
	out := filepath.Join(dir, "Paulgraham.app")

	path, size, err := BuildApp(zim, AppOptions{
		Out:        out,
		Base:       base,
		Name:       "Paul Graham",
		ExecName:   "paulgraham",
		Identifier: "com.kage.paulgraham",
		Version:    "1.0",
		Icon:       solid(48, 48, color.RGBA{A: 0xff}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != out {
		t.Errorf("path = %q, want %q", path, out)
	}

	// The bundle skeleton.
	exe := filepath.Join(out, "Contents", "MacOS", "paulgraham")
	for _, f := range []string{
		filepath.Join(out, "Contents", "Info.plist"),
		filepath.Join(out, "Contents", "PkgInfo"),
		filepath.Join(out, "Contents", "Resources", "icon.icns"),
		exe,
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing bundle file %s: %v", f, err)
		}
	}

	// The executable is base++zim++trailer, and size reports its length.
	exeBytes, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(exeBytes)) != size {
		t.Errorf("size = %d, want %d", size, len(exeBytes))
	}
	if !strings.HasPrefix(string(exeBytes), string(baseBytes)) {
		t.Error("executable does not start with the base bytes")
	}
	tr := exeBytes[len(exeBytes)-trailerLen:]
	if string(tr[:8]) != trailerMagic || string(tr[trailerLen-8:]) != trailerMagic {
		t.Error("trailer magic missing from the executable")
	}
	if got := binary.LittleEndian.Uint64(tr[8:16]); got != uint64(len(zim)) {
		t.Errorf("trailer records zim length %d, want %d", got, len(zim))
	}

	// On a unix host the executable bit must be set so Finder can launch it.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(exe)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("executable mode = %v, want the execute bit set", info.Mode().Perm())
		}
	}

	// Info.plist carries the identity and points at the icon.
	plist, err := os.ReadFile(filepath.Join(out, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>paulgraham</string>",          // CFBundleExecutable
		"<string>Paul Graham</string>",         // CFBundleName / DisplayName
		"<string>com.kage.paulgraham</string>", // CFBundleIdentifier
		"<key>CFBundleIconFile</key>",
		"<string>icon</string>",
		"NSHighResolutionCapable",
	} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist missing %q", want)
		}
	}
}

func TestBuildAppNoIcon(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "kage")
	if err := os.WriteFile(base, []byte("\xcf\xfa\xed\xfeBASE"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "Site.app")
	if _, _, err := BuildApp([]byte("ZIM"), AppOptions{Out: out, Base: base, ExecName: "site"}); err != nil {
		t.Fatal(err)
	}
	// No icon means no Resources dir and no icon key in the plist.
	if _, err := os.Stat(filepath.Join(out, "Contents", "Resources")); !os.IsNotExist(err) {
		t.Errorf("Resources dir should be absent without an icon, got err=%v", err)
	}
	plist, err := os.ReadFile(filepath.Join(out, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plist), "CFBundleIconFile") {
		t.Error("plist names an icon file but none was provided")
	}
}

func TestBuildAppRejectsBadOut(t *testing.T) {
	if _, _, err := BuildApp([]byte("ZIM"), AppOptions{Out: "noapp", Base: "x"}); err == nil {
		t.Error("BuildApp should reject an output path without a .app extension")
	}
	if _, _, err := BuildApp([]byte("ZIM"), AppOptions{Out: ""}); err == nil {
		t.Error("BuildApp should reject an empty output path")
	}
}
