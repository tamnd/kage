package pack

import (
	"path"
	"strings"
)

// mimeByExt maps a lower-case file extension (with the dot) to the MIME type
// kage records for it. Inference is by extension only, never by sniffing the
// bytes, so the same input always yields the same output. Anything not listed
// falls back to application/octet-stream and is stored uncompressed.
var mimeByExt = map[string]string{
	".html":  "text/html",
	".htm":   "text/html",
	".css":   "text/css",
	".js":    "text/javascript",
	".mjs":   "text/javascript",
	".json":  "application/json",
	".xml":   "application/xml",
	".svg":   "image/svg+xml",
	".txt":   "text/plain",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".avif":  "image/avif",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",
	".mp4":   "video/mp4",
	".m4v":   "video/mp4",
	".webm":  "video/webm",
	".mp3":   "audio/mpeg",
	".ogg":   "audio/ogg",
	".pdf":   "application/pdf",
	".zip":   "application/zip",
	".wasm":  "application/wasm",
}

// MimeForExt returns the MIME type for a path's extension, defaulting to
// application/octet-stream when the extension is unknown or absent.
func MimeForExt(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if m, ok := mimeByExt[ext]; ok {
		return m
	}
	return "application/octet-stream"
}
