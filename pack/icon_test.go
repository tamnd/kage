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

func TestDecodeIconGarbageICOFails(t *testing.T) {
	// A one-entry .ico whose payload is neither a PNG nor a complete DIB header
	// (here four filler bytes) must error so the caller falls back to the default
	// icon rather than crashing.
	var ico bytes.Buffer
	leWrite(&ico, uint16(0))
	leWrite(&ico, uint16(1))
	leWrite(&ico, uint16(1))
	ico.Write([]byte{32, 32, 0, 0})
	leWrite(&ico, uint16(1))
	leWrite(&ico, uint16(32))
	leWrite(&ico, uint32(4))
	leWrite(&ico, uint32(22))
	ico.Write([]byte{0x42, 0x4d, 0x00, 0x00}) // truncated DIB filler
	p := filepath.Join(t.TempDir(), "favicon.ico")
	if err := os.WriteFile(p, ico.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIcon(p); err == nil {
		t.Fatal("garbage .ico should fail to decode")
	}
}

// icoWithBMP32 wraps a solid px-by-px 32-bpp BMP/DIB bitmap (the format Apple's
// favicon.ico uses) in a single-entry .ico container, with an all-opaque AND
// mask. It exercises the hand-written BMP path without a binary fixture.
func icoWithBMP32(px int, c color.RGBA) []byte {
	var dib bytes.Buffer
	leWrite(&dib, uint32(40))    // biSize
	leWrite(&dib, int32(px))     // biWidth
	leWrite(&dib, int32(px*2))   // biHeight (color + mask, stacked)
	leWrite(&dib, uint16(1))     // biPlanes
	leWrite(&dib, uint16(32))    // biBitCount
	leWrite(&dib, uint32(0))     // biCompression = BI_RGB
	leWrite(&dib, uint32(0))     // biSizeImage
	leWrite(&dib, int32(0))      // biXPelsPerMeter
	leWrite(&dib, int32(0))      // biYPelsPerMeter
	leWrite(&dib, uint32(0))     // biClrUsed
	leWrite(&dib, uint32(0))     // biClrImportant
	for i := 0; i < px*px; i++ { // XOR: BGRA, full alpha
		dib.Write([]byte{c.B, c.G, c.R, 0xff})
	}
	andStride := ((px + 31) / 32) * 4
	dib.Write(make([]byte, andStride*px)) // AND mask, all zero = opaque

	var ico bytes.Buffer
	leWrite(&ico, uint16(0))
	leWrite(&ico, uint16(1))
	leWrite(&ico, uint16(1))
	ico.WriteByte(byte(px))
	ico.WriteByte(byte(px))
	ico.WriteByte(0)
	ico.WriteByte(0)
	leWrite(&ico, uint16(1))
	leWrite(&ico, uint16(32))
	leWrite(&ico, uint32(dib.Len()))
	leWrite(&ico, uint32(22))
	ico.Write(dib.Bytes())
	return ico.Bytes()
}

func TestDecodeIconBMP32(t *testing.T) {
	want := color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff}
	p := filepath.Join(t.TempDir(), "favicon.ico")
	if err := os.WriteFile(p, icoWithBMP32(64, want), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := DecodeIcon(p)
	if err != nil {
		t.Fatalf("DecodeIcon: %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatalf("size = %v, want 64x64", img.Bounds())
	}
	r, g, b, a := img.At(10, 10).RGBA()
	if r>>8 != 0x33 || g>>8 != 0x66 || b>>8 != 0x99 || a>>8 != 0xff {
		t.Errorf("pixel = %02x%02x%02x%02x, want 336699ff", r>>8, g>>8, b>>8, a>>8)
	}
}

// A BMP-only .ico now decodes end to end through FindIcon, the path that gives a
// favicon-only site (such as developer.apple.com) its book icon.
func TestFindIconDecodesBMPICO(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "favicon.ico"), icoWithBMP32(48, color.RGBA{R: 1, G: 2, B: 3, A: 0xff}), 0o644); err != nil {
		t.Fatal(err)
	}
	img, src, ok := FindIcon(dir)
	if !ok {
		t.Fatal("FindIcon should decode a BMP .ico")
	}
	if filepath.Base(src) != "favicon.ico" {
		t.Errorf("src = %s, want favicon.ico", filepath.Base(src))
	}
	if img.Bounds().Dx() != 48 {
		t.Errorf("width = %d, want 48", img.Bounds().Dx())
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
