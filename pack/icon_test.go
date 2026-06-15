package pack

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// pngBytes encodes a solid square as PNG, used to seed icon fixtures.
func pngBytes(t *testing.T, px int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solid(px, px, color.RGBA{R: 0x10, G: 0x80, B: 0x40, A: 0xff})); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// leWrite appends v to w in little-endian, swallowing the error a bytes.Buffer
// never returns. It keeps the .ico fixtures readable without an errcheck flag on
// every field.
func leWrite(w *bytes.Buffer, v any) { _ = binary.Write(w, binary.LittleEndian, v) }

// icoWithPNG wraps PNG payloads in a minimal .ico container so the ICO path is
// exercised without a binary fixture on disk.
func icoWithPNG(pngs [][]byte) []byte {
	var dir, body bytes.Buffer
	put := leWrite
	put(&dir, uint16(0))         // reserved
	put(&dir, uint16(1))         // type: icon
	put(&dir, uint16(len(pngs))) // count
	offset := 6 + len(pngs)*16
	for i, p := range pngs {
		// width/height bytes: 0 means 256; use a distinct small size per entry.
		dim := byte(16 * (i + 1))
		dir.WriteByte(dim)        // width
		dir.WriteByte(dim)        // height
		dir.WriteByte(0)          // colours
		dir.WriteByte(0)          // reserved
		put(&dir, uint16(1))      // planes
		put(&dir, uint16(32))     // bit count
		put(&dir, uint32(len(p))) // bytes in resource
		put(&dir, uint32(offset)) // offset
		offset += len(p)
		body.Write(p)
	}
	return append(dir.Bytes(), body.Bytes()...)
}

func TestDecodeIconPNG(t *testing.T) {
	p := filepath.Join(t.TempDir(), "favicon.png")
	if err := os.WriteFile(p, pngBytes(t, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeIcon(p)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 64 {
		t.Errorf("decoded width = %d, want 64", img.Bounds().Dx())
	}
}

func TestDecodeIconICOWithPNG(t *testing.T) {
	// Two PNG entries; the decoder should pick the larger (second, 32px square).
	ico := icoWithPNG([][]byte{pngBytes(t, 16), pngBytes(t, 32)})
	p := filepath.Join(t.TempDir(), "favicon.ico")
	if err := os.WriteFile(p, ico, 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeIcon(p)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 32 {
		t.Errorf("decoded width = %d, want the larger 32px entry", img.Bounds().Dx())
	}
}

func TestDecodeIconBMPOnlyICOFails(t *testing.T) {
	// A one-entry .ico whose payload is not a PNG (here just filler) must error
	// so the caller falls back to the default icon.
	var ico bytes.Buffer
	leWrite(&ico, uint16(0))
	leWrite(&ico, uint16(1))
	leWrite(&ico, uint16(1))
	ico.Write([]byte{32, 32, 0, 0})
	leWrite(&ico, uint16(1))
	leWrite(&ico, uint16(32))
	leWrite(&ico, uint32(4))
	leWrite(&ico, uint32(22))
	ico.Write([]byte{0x42, 0x4d, 0x00, 0x00}) // "BM" DIB filler
	p := filepath.Join(t.TempDir(), "favicon.ico")
	if err := os.WriteFile(p, ico.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIcon(p); err == nil {
		t.Fatal("BMP-only .ico should fail to decode")
	}
}

func TestFindIconPrefersAppleTouch(t *testing.T) {
	dir := t.TempDir()
	// A tiny favicon at the root and a bigger apple-touch icon a level down.
	if err := os.WriteFile(filepath.Join(dir, "favicon.png"), pngBytes(t, 16), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "assets")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "apple-touch-icon.png"), pngBytes(t, 180), 0o644); err != nil {
		t.Fatal(err)
	}
	img, src, ok := FindIcon(dir)
	if !ok {
		t.Fatal("FindIcon found nothing")
	}
	if filepath.Base(src) != "apple-touch-icon.png" {
		t.Errorf("picked %s, want apple-touch-icon.png", filepath.Base(src))
	}
	if img.Bounds().Dx() != 180 {
		t.Errorf("width = %d, want 180", img.Bounds().Dx())
	}
}

func TestFindIconNone(t *testing.T) {
	if _, _, ok := FindIcon(t.TempDir()); ok {
		t.Fatal("FindIcon should report nothing in an empty dir")
	}
}
