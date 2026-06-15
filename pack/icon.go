package pack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	// Register the decoders image.Decode dispatches to. A favicon is almost
	// always one of these; SVG is skipped explicitly, and a BMP-in-ICO is
	// decoded by hand below since the stdlib has no BMP decoder.
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
)

// iconNames ranks the file names sites use for their icon, best first. A large
// PNG (an apple-touch icon, typically 180px) makes a far better app icon than a
// 16px favicon.ico, so we prefer those even though .ico is the classic name.
var iconNames = []string{
	"apple-touch-icon-precomposed.png",
	"apple-touch-icon.png",
	"icon.png",
	"favicon.png",
	"favicon.ico",
}

// FindIcon looks through a cloned mirror for the site's icon and decodes it. It
// returns the image, the path it came from (for a friendly log line), and
// ok=false when nothing usable is found, in which case the caller just builds a
// bundle with the default icon. Discovery never fails the pack.
func FindIcon(mirrorDir string) (image.Image, string, bool) {
	for _, name := range iconNames {
		for _, p := range globIcon(mirrorDir, name) {
			if img, err := DecodeIcon(p); err == nil {
				return img, p, true
			}
		}
	}
	return nil, "", false
}

// Favicon48 finds the mirror's icon and renders it to a 48x48 PNG, the form the
// ZIM Illustrator_48x48@1 metadata takes and the icon Kiwix shows for the book.
// It returns ok=false when the mirror has no usable icon, in which case the
// archive simply ships without one rather than failing the pack.
func Favicon48(mirrorDir string) ([]byte, bool) {
	img, _, ok := FindIcon(mirrorDir)
	if !ok {
		return nil, false
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaleSquare(img, 48)); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// globIcon returns every file under dir whose base name equals name, nearest the
// root first. Clones store assets under rewritten paths, so the icon may sit a
// few directories deep rather than at the mirror root.
func globIcon(dir, name string) []string {
	var hits []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), name) {
			hits = append(hits, p)
		}
		return nil
	})
	// Shallower paths (fewer separators) are likelier to be the real site icon.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && depth(hits[j]) < depth(hits[j-1]); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	return hits
}

func depth(p string) int { return strings.Count(p, string(filepath.Separator)) }

// DecodeIcon reads an icon file into an image. It handles the stdlib raster
// formats (PNG, JPEG, GIF) directly and unwraps a .ico container, decoding
// either the PNG a modern high-resolution favicon embeds or the classic BMP/DIB
// bitmap older ones (Apple's among them) still ship.
func DecodeIcon(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isICO(data) {
		img, err := decodeICO(data)
		if err != nil {
			return nil, fmt.Errorf("pack: decode icon %q: %w", path, err)
		}
		return img, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("pack: decode icon %q: %w", path, err)
	}
	return img, nil
}

// isICO reports whether data begins with an ICONDIR header: reserved 0, type 1
// (icon), and a non-zero image count.
func isICO(data []byte) bool {
	return len(data) >= 6 &&
		data[0] == 0 && data[1] == 0 &&
		data[2] == 1 && data[3] == 0 &&
		(uint16(data[4])|uint16(data[5])<<8) > 0
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// decodeICO decodes the best entry in an .ico container. It reads the directory,
// orders the entries largest first (a bigger source rescales to a cleaner 48x48
// favicon), and decodes each in turn until one works: a PNG entry through the
// stdlib, a BMP/DIB entry through decodeICOBMP. It errors only when no entry
// decodes, so a truly garbage .ico still falls back to the default icon.
func decodeICO(data []byte) (image.Image, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("pack: .ico too short")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	type entry struct{ w, h, off, size int }
	var ents []entry
	for i := 0; i < count; i++ {
		e := 6 + i*16
		if e+16 > len(data) {
			break
		}
		w, h := int(data[e]), int(data[e+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		size := int(binary.LittleEndian.Uint32(data[e+8 : e+12]))
		off := int(binary.LittleEndian.Uint32(data[e+12 : e+16]))
		if off < 0 || size <= 0 || off+size > len(data) {
			continue
		}
		ents = append(ents, entry{w, h, off, size})
	}
	sort.SliceStable(ents, func(i, j int) bool { return ents[i].w*ents[i].h > ents[j].w*ents[j].h })

	var firstErr error
	for _, en := range ents {
		chunk := data[en.off : en.off+en.size]
		var (
			img image.Image
			err error
		)
		if bytes.HasPrefix(chunk, pngMagic) {
			img, _, err = image.Decode(bytes.NewReader(chunk))
		} else {
			img, err = decodeICOBMP(chunk, en.w, en.h)
		}
		if err == nil {
			return img, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = fmt.Errorf("pack: .ico holds no decodable entry")
	}
	return nil, firstErr
}

// decodeICOBMP decodes one BMP/DIB icon entry: a BITMAPINFOHEADER, an optional
// color table, the XOR color bitmap, then a 1-bpp AND transparency mask. The
// header's height covers both bitmaps stacked, so the real image is half of it.
// Rows run bottom-up and are padded to a 4-byte boundary. Only uncompressed
// (BI_RGB) entries are handled, which covers every icon that is not a PNG.
func decodeICOBMP(p []byte, dirW, dirH int) (image.Image, error) {
	if len(p) < 40 {
		return nil, fmt.Errorf("pack: ico bmp header truncated")
	}
	headerSize := int(binary.LittleEndian.Uint32(p[0:4]))
	width := int(int32(binary.LittleEndian.Uint32(p[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(p[8:12])))
	bits := int(binary.LittleEndian.Uint16(p[14:16]))
	compression := binary.LittleEndian.Uint32(p[16:20])
	clrUsed := int(binary.LittleEndian.Uint32(p[32:36]))

	if compression != 0 {
		return nil, fmt.Errorf("pack: ico bmp compression %d unsupported", compression)
	}
	height /= 2 // drop the AND-mask half to get the picture height
	if width <= 0 || height <= 0 {
		width, height = dirW, dirH // fall back to the directory dimensions
	}
	if width <= 0 || height <= 0 || width > 1024 || height > 1024 {
		return nil, fmt.Errorf("pack: ico bmp size %dx%d unreasonable", width, height)
	}

	off := headerSize
	if headerSize < 40 || off > len(p) {
		off = 40
	}

	// A color table follows the header for the palettized depths (BGRA quads).
	var palette [][4]byte
	if bits <= 8 {
		n := clrUsed
		if n == 0 {
			n = 1 << bits
		}
		for i := 0; i < n && off+4 <= len(p); i++ {
			palette = append(palette, [4]byte{p[off], p[off+1], p[off+2], p[off+3]})
			off += 4
		}
	}

	xorStride := ((width*bits + 31) / 32) * 4
	andStride := ((width + 31) / 32) * 4
	xor := p[off:]
	var and []byte
	if andOff := xorStride * height; andOff < len(xor) {
		and = xor[andOff:]
	}

	maskBit := func(mask []byte, rowStart, x int) bool {
		i := rowStart + x/8
		return i < len(mask) && (mask[i]>>(7-uint(x%8)))&1 == 1
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	anyAlpha := false
	for y := 0; y < height; y++ {
		srcY := height - 1 - y // rows are bottom-up
		row := srcY * xorStride
		andRow := srcY * andStride
		for x := 0; x < width; x++ {
			var r, g, b uint8
			a := uint8(255)
			switch bits {
			case 32:
				i := row + x*4
				if i+4 > len(xor) {
					continue
				}
				b, g, r, a = xor[i], xor[i+1], xor[i+2], xor[i+3]
				if a != 0 {
					anyAlpha = true
				}
			case 24:
				i := row + x*3
				if i+3 > len(xor) {
					continue
				}
				b, g, r = xor[i], xor[i+1], xor[i+2]
			case 8:
				i := row + x
				if i >= len(xor) {
					continue
				}
				if c, ok := paletteAt(palette, int(xor[i])); ok {
					b, g, r = c[0], c[1], c[2]
				}
			case 4:
				i := row + x/2
				if i >= len(xor) {
					continue
				}
				idx := xor[i] >> 4
				if x&1 == 1 {
					idx = xor[i] & 0x0f
				}
				if c, ok := paletteAt(palette, int(idx)); ok {
					b, g, r = c[0], c[1], c[2]
				}
			case 1:
				i := row + x/8
				if i >= len(xor) {
					continue
				}
				bit := (xor[i] >> (7 - uint(x%8))) & 1
				if c, ok := paletteAt(palette, int(bit)); ok {
					b, g, r = c[0], c[1], c[2]
				}
			default:
				return nil, fmt.Errorf("pack: ico bmp %d-bpp unsupported", bits)
			}
			if bits != 32 && maskBit(and, andRow, x) {
				a = 0 // AND-mask transparency for the non-alpha depths
			}
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}

	// A 32-bpp icon whose alpha channel is entirely zero is opaque, not
	// invisible: it leans on the AND mask instead. Reapply opacity from the mask.
	if bits == 32 && !anyAlpha {
		for y := 0; y < height; y++ {
			srcY := height - 1 - y
			andRow := srcY * andStride
			for x := 0; x < width; x++ {
				c := img.NRGBAAt(x, y)
				if maskBit(and, andRow, x) {
					c.A = 0
				} else {
					c.A = 255
				}
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img, nil
}

func paletteAt(pal [][4]byte, i int) ([4]byte, bool) {
	if i < 0 || i >= len(pal) {
		return [4]byte{}, false
	}
	return pal[i], true
}
