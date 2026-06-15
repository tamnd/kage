package pack

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	// Register the decoders image.Decode dispatches to. A favicon is almost
	// always one of these; SVG and legacy BMP-in-ICO are handled (or skipped)
	// explicitly below.
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
// formats (PNG, JPEG, GIF) directly and unwraps a PNG stored inside a .ico
// container, which is how modern sites ship a high-resolution favicon.ico. A
// legacy BMP-only .ico returns an error rather than a mangled image, so the
// caller falls back to the default icon.
func DecodeIcon(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isICO(data) {
		png, err := largestPNGInICO(data)
		if err != nil {
			return nil, err
		}
		data = png
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

// largestPNGInICO scans an .ico directory for PNG-encoded entries and returns
// the bytes of the largest one. It ignores BMP entries; if none of the entries
// are PNG it returns an error.
func largestPNGInICO(data []byte) ([]byte, error) {
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	var best []byte
	var bestArea int
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
		chunk := data[off : off+size]
		if !bytes.HasPrefix(chunk, pngMagic) {
			continue // a BMP/DIB entry; skip it
		}
		if w*h > bestArea {
			bestArea, best = w*h, chunk
		}
	}
	if best == nil {
		return nil, fmt.Errorf("pack: .ico holds no PNG entry")
	}
	return best, nil
}
