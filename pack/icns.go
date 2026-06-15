package pack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// An .icns file is a tiny container: the magic "icns", a uint32 big-endian total
// length, then a run of entries. Each entry is a four-byte OSType, a uint32
// big-endian length covering the 8-byte entry header plus the payload, and the
// payload itself. Since OS X 10.7 the payload may be a PNG, which lets us avoid
// the old packed-RGBA formats entirely and just store one PNG per size.
//
// We emit the retina-era PNG OSTypes. macOS picks whichever size it needs for
// the Dock, Finder, and Cmd-Tab, so covering 16 through 1024 keeps the icon
// crisp everywhere without shipping a huge file.
var icnsSizes = []struct {
	osType string
	px     int
}{
	{"icp4", 16},
	{"icp5", 32},
	{"icp6", 64},
	{"ic07", 128},
	{"ic08", 256},
	{"ic09", 512},
	{"ic10", 1024},
}

// EncodeICNS renders img into a macOS .icns at every standard size. The source
// is scaled to each size with Catmull-Rom resampling, which keeps a small
// favicon from turning to mush when it is enlarged for the Dock. It returns an
// error only if img is empty or a PNG fails to encode.
func EncodeICNS(img image.Image) ([]byte, error) {
	if img == nil || img.Bounds().Empty() {
		return nil, fmt.Errorf("pack: icns source image is empty")
	}

	var body bytes.Buffer
	for _, s := range icnsSizes {
		scaled := scaleSquare(img, s.px)
		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, scaled); err != nil {
			return nil, fmt.Errorf("pack: encode %s icon: %w", s.osType, err)
		}
		body.WriteString(s.osType)
		writeU32BE(&body, uint32(8+pngBuf.Len()))
		body.Write(pngBuf.Bytes())
	}

	var out bytes.Buffer
	out.WriteString("icns")
	writeU32BE(&out, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// scaleSquare returns img resampled to a px-by-px RGBA image. A non-square
// source is fitted into the square and centred, so a wide or tall favicon is not
// stretched.
func scaleSquare(img image.Image, px int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, px, px))
	src := img.Bounds()
	sw, sh := src.Dx(), src.Dy()
	// Scale to fit, preserving aspect ratio.
	scale := float64(px) / float64(sw)
	if float64(sh)*scale > float64(px) {
		scale = float64(px) / float64(sh)
	}
	dw, dh := int(float64(sw)*scale), int(float64(sh)*scale)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	off := image.Pt((px-dw)/2, (px-dh)/2)
	rect := image.Rectangle{Min: off, Max: off.Add(image.Pt(dw, dh))}
	xdraw.CatmullRom.Scale(dst, rect, img, src, xdraw.Over, nil)
	return dst
}

func writeU32BE(w *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	w.Write(b[:])
}
