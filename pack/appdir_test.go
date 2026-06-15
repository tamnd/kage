package pack

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAppDir(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "kage-linux")
	baseBytes := []byte("\x7fELF-LINUX-BASE")
	if err := os.WriteFile(base, baseBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "paulgraham.AppDir")

	path, size, hasIcon, err := BuildAppDir([]byte("ZIM-PAYLOAD"), LinuxAppOptions{
		Out:      out,
		Base:     base,
		Name:     "Paul Graham",
		ExecName: "paulgraham",
		Comment:  "Offline mirror",
		Version:  "1.0",
		Icon:     solid(64, 64, color.RGBA{B: 0xff, A: 0xff}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != out || !hasIcon {
		t.Fatalf("path=%q hasIcon=%v", path, hasIcon)
	}

	apprun := filepath.Join(out, "AppRun")
	for _, f := range []string{
		apprun,
		filepath.Join(out, "paulgraham.desktop"),
		filepath.Join(out, "paulgraham.png"),
		filepath.Join(out, ".DirIcon"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}

	exe, err := os.ReadFile(apprun)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(exe)) != size {
		t.Errorf("size = %d, want %d", size, len(exe))
	}
	if !strings.HasPrefix(string(exe), string(baseBytes)) {
		t.Error("AppRun does not start with the base bytes")
	}
	tr := exe[len(exe)-trailerLen:]
	if string(tr[:8]) != trailerMagic {
		t.Error("AppRun missing the trailer")
	}
	if info, _ := os.Stat(apprun); info.Mode().Perm()&0o111 == 0 {
		t.Error("AppRun is not executable")
	}

	// The icon is a real 256px PNG.
	f, err := os.Open(filepath.Join(out, "paulgraham.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("icon is not a PNG: %v", err)
	}
	if img.Bounds().Dx() != 256 {
		t.Errorf("icon width = %d, want 256", img.Bounds().Dx())
	}

	desktop, err := os.ReadFile(filepath.Join(out, "paulgraham.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Name=Paul Graham",
		"Exec=AppRun",
		"Icon=paulgraham",
		"Terminal=false",
		"Comment=Offline mirror",
	} {
		if !strings.Contains(string(desktop), want) {
			t.Errorf(".desktop missing %q", want)
		}
	}
}

func TestBuildAppDirNoIcon(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "kage")
	if err := os.WriteFile(base, []byte("\x7fELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "site.AppDir")
	_, _, hasIcon, err := BuildAppDir([]byte("ZIM"), LinuxAppOptions{Out: out, Base: base, ExecName: "site"})
	if err != nil {
		t.Fatal(err)
	}
	if hasIcon {
		t.Error("hasIcon should be false without an icon")
	}
	if _, err := os.Stat(filepath.Join(out, "site.png")); !os.IsNotExist(err) {
		t.Errorf("icon should be absent, got err=%v", err)
	}
	desktop, _ := os.ReadFile(filepath.Join(out, "site.desktop"))
	if strings.Contains(string(desktop), "Icon=") {
		t.Error(".desktop names an icon but none was written")
	}
}

func TestBuildAppDirRejectsBadOut(t *testing.T) {
	if _, _, _, err := BuildAppDir([]byte("Z"), LinuxAppOptions{Out: "site", Base: "x"}); err == nil {
		t.Error("BuildAppDir should reject a path without .AppDir")
	}
}
