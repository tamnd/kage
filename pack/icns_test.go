package pack

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// solid returns a w-by-h image filled with one colour, enough to exercise the
// encoder without depending on any fixture file.
func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestEncodeICNS(t *testing.T) {
	data, err := EncodeICNS(solid(48, 48, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff}))
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:4]) != "icns" {
		t.Fatalf("magic = %q, want icns", data[:4])
	}
	if got := binary.BigEndian.Uint32(data[4:8]); int(got) != len(data) {
		t.Fatalf("header length = %d, want %d", got, len(data))
	}

	// Walk the entries and confirm each declared OSType is present and its PNG
	// decodes to the size it claims.
	seen := map[string]int{}
	off := 8
	for off < len(data) {
		osType := string(data[off : off+4])
		size := int(binary.BigEndian.Uint32(data[off+4 : off+8]))
		if size < 8 || off+size > len(data) {
			t.Fatalf("entry %s has bogus length %d at offset %d", osType, size, off)
		}
		img, err := png.Decode(bytes.NewReader(data[off+8 : off+size]))
		if err != nil {
			t.Fatalf("entry %s payload is not a PNG: %v", osType, err)
		}
		seen[osType] = img.Bounds().Dx()
		off += size
	}

	for _, s := range icnsSizes {
		px, ok := seen[s.osType]
		if !ok {
			t.Errorf("missing OSType %s", s.osType)
			continue
		}
		if px != s.px {
			t.Errorf("OSType %s is %dpx, want %d", s.osType, px, s.px)
		}
	}
}

func TestEncodeICNSEmpty(t *testing.T) {
	if _, err := EncodeICNS(nil); err == nil {
		t.Fatal("EncodeICNS(nil) should error")
	}
	if _, err := EncodeICNS(image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Fatal("EncodeICNS(empty) should error")
	}
}
