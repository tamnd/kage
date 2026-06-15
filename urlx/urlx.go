// Package urlx is the URL ⇄ filesystem contract at the heart of kage.
//
// Every reference kage meets — a page link, a stylesheet, an image, a font —
// is funnelled through Normalize so that two different-looking URLs that point
// at the same resource collapse to one canonical key. LocalPath then maps that
// canonical URL to a deterministic path on disk, and Rel turns two such paths
// into the relative link that goes back into the rewritten HTML or CSS.
//
// The package is pure: no network, no filesystem, no clock. That is what makes
// the rest of kage easy to reason about — a page worker can rewrite a link to
// an asset long before the asset has been downloaded, because both sides agree
// on where the bytes will live.
package urlx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// Kind distinguishes a crawlable page from a downloadable asset; the two map to
// different places on disk (pages mirror the URL path, assets live under the
// reserved prefix).
type Kind int

const (
	// Page is an HTML document kage renders and rewrites.
	Page Kind = iota
	// Asset is a stylesheet, image, font, or media file kage downloads verbatim.
	Asset
)

// DefaultReserved is the directory under the mirror root where every asset and
// kage's own state live. It is deliberately unlikely to collide with a real URL
// path segment.
const DefaultReserved = "_kage"

// binaryExts are extensions that, when seen on an <a href>, mean the link points
// at a file to download rather than a page to render.
var binaryExts = map[string]bool{
	".pdf": true, ".zip": true, ".gz": true, ".tar": true, ".tgz": true,
	".rar": true, ".7z": true, ".dmg": true, ".exe": true, ".msi": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".svg": true, ".ico": true, ".bmp": true, ".avif": true,
	".mp3": true, ".mp4": true, ".webm": true, ".mov": true, ".avi": true,
	".wav": true, ".ogg": true, ".flac": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".css": true, ".js": true, ".json": true, ".xml": true, ".rss": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true,
	".pptx": true, ".csv": true, ".txt": true,
}

// ParseSeed turns a command-line argument like "example.com",
// "https://example.com/docs" or "http://ex.com" into a canonical absolute URL.
// A bare host (no scheme) is assumed to be https.
func ParseSeed(arg string) (*url.URL, error) {
	s := strings.TrimSpace(arg)
	if s == "" {
		return nil, fmt.Errorf("empty seed")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("parse seed %q: %w", arg, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("seed %q has no host", arg)
	}
	return canonical(u, true), nil
}

// Normalize resolves ref against base and canonicalises the result. It returns
// an error for references kage cannot crawl or download: empty, fragment-only,
// or a non-http(s) scheme (mailto:, tel:, data:, javascript:, blob:, …).
func Normalize(base *url.URL, ref string) (*url.URL, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty reference")
	}
	if strings.HasPrefix(ref, "#") {
		return nil, fmt.Errorf("fragment-only reference")
	}
	low := strings.ToLower(ref)
	for _, bad := range []string{"javascript:", "mailto:", "tel:", "data:", "blob:", "about:", "ftp:", "sms:"} {
		if strings.HasPrefix(low, bad) {
			return nil, fmt.Errorf("non-crawlable scheme: %s", bad)
		}
	}
	r, err := url.Parse(ref)
	if err != nil {
		return nil, fmt.Errorf("parse ref %q: %w", ref, err)
	}
	var abs *url.URL
	if base != nil {
		abs = base.ResolveReference(r)
	} else {
		abs = r
	}
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return nil, fmt.Errorf("non-http scheme: %q", abs.Scheme)
	}
	if abs.Host == "" {
		return nil, fmt.Errorf("reference has no host")
	}
	return canonical(abs, false), nil
}

// canonical lower-cases scheme/host, drops the fragment and any default port,
// and cleans the path while preserving a meaningful trailing slash. When root
// is true an empty path becomes "/".
func canonical(u *url.URL, root bool) *url.URL {
	c := *u
	c.Scheme = strings.ToLower(c.Scheme)
	c.Host = strings.ToLower(c.Host)
	c.Fragment = ""
	// Strip a default port.
	if h := c.Hostname(); h != "" {
		switch {
		case c.Scheme == "http" && c.Port() == "80":
			c.Host = h
		case c.Scheme == "https" && c.Port() == "443":
			c.Host = h
		}
	}
	if c.Path == "" {
		if root {
			c.Path = "/"
		}
	} else {
		trailing := strings.HasSuffix(c.Path, "/")
		cleaned := path.Clean(c.Path)
		if cleaned == "." {
			cleaned = "/"
		}
		if trailing && cleaned != "/" {
			cleaned += "/"
		}
		c.Path = cleaned
	}
	c.RawQuery = safeRawQuery(c.RawQuery)
	return &c
}

// safeRawQuery percent-encodes the characters a query string is not allowed to
// carry on the wire (a space, a control byte, a non-ASCII byte) while leaving
// everything already legal untouched, including existing %XX escapes and the
// query sub-delimiters a cache-buster relies on (& = , ; etc.). It exists
// because real sites emit hrefs with raw spaces in the query, e.g. a CSS link
// busted with a date string like "?Thursday, 26-Feb-2026 16:26:41 UTC"; a
// browser encodes the spaces before requesting, but url.Parse keeps RawQuery
// verbatim, so without this the request line is malformed and the server
// answers 400. Re-encoding here, on the canonical URL, fixes both the fetch and
// the on-disk key in one place.
func safeRawQuery(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch == '%' && i+2 < len(raw) && isHex(raw[i+1]) && isHex(raw[i+2]):
			// Already a percent-escape; copy it through unchanged.
			b.WriteString(raw[i : i+3])
			i += 2
		case queryByteAllowed(ch):
			b.WriteByte(ch)
		default:
			b.WriteByte('%')
			const hexDigits = "0123456789ABCDEF"
			b.WriteByte(hexDigits[ch>>4])
			b.WriteByte(hexDigits[ch&0x0f])
		}
	}
	return b.String()
}

// queryByteAllowed reports whether ch may appear literally in a URL query.
// It is the RFC 3986 query grammar (pchar / "/" / "?"), which keeps the
// sub-delims that structure a query and rejects spaces, quotes, and controls.
func queryByteAllowed(ch byte) bool {
	switch {
	case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		return true
	}
	return strings.IndexByte("-._~!$&'()*+,;=:@/?", ch) >= 0
}

func isHex(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// Key is the canonical string form used to dedup pages and assets.
func Key(u *url.URL) string { return u.String() }

// ScopeConfig controls which page URLs are in scope for crawling.
type ScopeConfig struct {
	IncludeSubdomains bool
	ScopePrefix       string   // only crawl paths under this prefix, e.g. "/docs/"
	ExcludePaths      []string // skip any path containing one of these substrings
}

// SameSite reports whether u belongs to the seed's site: the same host, or a
// subdomain of it when allowSub is set.
func SameSite(seed, u *url.URL, allowSub bool) bool {
	sh, uh := seed.Hostname(), u.Hostname()
	if sh == uh {
		return true
	}
	if allowSub && strings.HasSuffix(uh, "."+sh) {
		return true
	}
	return false
}

// InScope reports whether u should be crawled as a page given the seed and cfg.
func InScope(seed, u *url.URL, cfg ScopeConfig) bool {
	if !SameSite(seed, u, cfg.IncludeSubdomains) {
		return false
	}
	if cfg.ScopePrefix != "" && !strings.HasPrefix(u.Path, cfg.ScopePrefix) {
		return false
	}
	for _, ex := range cfg.ExcludePaths {
		if ex != "" && strings.Contains(u.Path, ex) {
			return false
		}
	}
	return true
}

// LikelyPage reports whether an <a href> target should be rendered as a page
// rather than downloaded as a file. Links ending in a known binary/document
// extension are treated as assets.
func LikelyPage(u *url.URL) bool {
	ext := strings.ToLower(path.Ext(lastSegment(u.Path)))
	return !binaryExts[ext]
}

// LocalPath maps a canonical URL to a slash-separated path relative to the
// mirror root (out/<seedHost>/). Pages mirror the URL path as a directory index;
// assets live under reserved/<host>/<path>. See spec §4.3.
func LocalPath(seedHost string, u *url.URL, kind Kind, reserved string) string {
	if reserved == "" {
		reserved = DefaultReserved
	}
	host := u.Hostname()
	switch kind {
	case Asset:
		dir, base := splitAsset(u)
		base = applyQuery(base, u)
		return joinClean(reserved, host, dir, base)
	default: // Page
		dir := collapseIndex(strings.Trim(u.Path, "/"))
		leaf := applyQuery("index.html", u)
		if strings.EqualFold(host, seedHost) {
			return joinClean(dir, leaf)
		}
		// Subdomain pages get a host segment so they never collide with the
		// seed host's tree.
		return joinClean(host, dir, leaf)
	}
}

// collapseIndex treats a directory-index document as the directory itself, so
// "/" and "/index.html" map to one page, and "/docs/index.html" collapses to
// "/docs". The argument is a path already trimmed of surrounding slashes.
func collapseIndex(p string) string {
	for _, idx := range []string{"index.html", "index.htm"} {
		if p == idx {
			return ""
		}
		if rest, ok := strings.CutSuffix(p, "/"+idx); ok {
			return rest
		}
	}
	return p
}

// splitAsset breaks an asset URL path into a directory and a filename, inventing
// a filename for directory-like or empty paths.
func splitAsset(u *url.URL) (dir, base string) {
	clean := strings.Trim(u.Path, "/")
	switch {
	case clean == "":
		return "", "index"
	case strings.HasSuffix(u.Path, "/"):
		return clean, "index"
	default:
		if i := strings.LastIndex(clean, "/"); i >= 0 {
			return clean[:i], clean[i+1:]
		}
		return "", clean
	}
}

// applyQuery folds a URL's query string into a filename as "__q-<hash>",
// inserting it before the extension so the file keeps a sensible suffix.
func applyQuery(name string, u *url.URL) string {
	if u.RawQuery == "" {
		return name
	}
	sum := sha256.Sum256([]byte(u.RawQuery))
	suffix := "__q-" + hex.EncodeToString(sum[:])[:6]
	if dot := strings.LastIndex(name, "."); dot > 0 {
		return name[:dot] + suffix + name[dot:]
	}
	return name + suffix
}

// Rel returns the relative link from the directory of fromFile to toFile, both
// slash paths relative to the mirror root. The result always uses '/'.
func Rel(fromDir, toFile string) string {
	fromDir = path.Clean("/" + strings.TrimPrefix(fromDir, "/"))
	toFile = path.Clean("/" + strings.TrimPrefix(toFile, "/"))
	rel := relPath(fromDir, toFile)
	if rel == "" {
		return "."
	}
	return rel
}

// relPath computes a relative path between two cleaned absolute slash paths,
// treating 'from' as a directory.
func relPath(fromDir, toFile string) string {
	from := splitNonEmpty(fromDir)
	to := splitNonEmpty(toFile)
	// Common prefix.
	i := 0
	for i < len(from) && i < len(to) && from[i] == to[i] {
		i++
	}
	var out []string
	for range from[i:] {
		out = append(out, "..")
	}
	out = append(out, to[i:]...)
	return strings.Join(out, "/")
}

func splitNonEmpty(p string) []string {
	var out []string
	for s := range strings.SplitSeq(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Dir returns the directory portion of a slash file path.
func Dir(file string) string {
	if i := strings.LastIndex(file, "/"); i >= 0 {
		return file[:i]
	}
	return ""
}

// joinClean joins path elements with '/', dropping empties and cleaning the
// result, and never returns a leading slash.
func joinClean(elems ...string) string {
	var parts []string
	for _, e := range elems {
		e = strings.Trim(e, "/")
		if e != "" {
			parts = append(parts, e)
		}
	}
	return strings.Join(parts, "/")
}

func lastSegment(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
