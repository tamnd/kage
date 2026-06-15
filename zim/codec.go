package zim

import (
	"sync"

	"github.com/klauspost/compress/zstd"
)

// A single shared zstd codec. Both EncodeAll and DecodeAll are safe for
// concurrent use, so one encoder and one decoder serve the whole process.
var (
	zstdOnce sync.Once
	zstdEnc  *zstd.Encoder
	zstdDec  *zstd.Decoder
)

func initZstd() {
	zstdOnce.Do(func() {
		zstdEnc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
		zstdDec, _ = zstd.NewReader(nil)
	})
}

func zstdEncode(p []byte) []byte {
	initZstd()
	return zstdEnc.EncodeAll(p, nil)
}

// Compress zstd-compresses p with the exact codec the writer uses for its
// clusters. It is exported so a caller can cache cluster compression across
// packs and feed the result back through Writer.SetCompress; a cached cluster is
// then byte-for-byte what a fresh compression would have produced.
func Compress(p []byte) []byte { return zstdEncode(p) }

func zstdDecode(p []byte) ([]byte, error) {
	initZstd()
	return zstdDec.DecodeAll(p, nil)
}

// isTextMime reports whether content of this MIME type is worth compressing.
// Already-compressed media (images, fonts, audio, video, archives) is stored
// uncompressed so we do not burn CPU inflating it by a few bytes.
func isTextMime(mime string) bool {
	switch mime {
	case "application/json", "application/xml", "application/javascript",
		"application/x-javascript":
		return true
	}
	if len(mime) >= 5 && mime[:5] == "text/" {
		return true
	}
	// Any structured-XML type: application/rss+xml, image/svg+xml, ...
	return len(mime) >= 4 && mime[len(mime)-4:] == "+xml"
}
