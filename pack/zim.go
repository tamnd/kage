// Package pack turns a kage mirror on disk into a distributable artifact: a ZIM
// archive, or a self-contained executable that serves the mirror offline. It is
// the only pack-side package that touches the filesystem and the running
// executable; the byte-level format work lives in the zim package.
package pack

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/tamnd/kage/urlx"
	"github.com/tamnd/kage/zim"
)

// ZIMOptions controls how a mirror is packed into a ZIM archive. Date is passed
// in from the CLI boundary rather than read from the clock, so the zim and pack
// packages stay pure and packing the same mirror twice is byte-identical.
type ZIMOptions struct {
	Out         string // output path (default <mirror-base>.zim)
	NoCompress  bool   // store every cluster raw (code 1)
	Title       string // overrides M/Title
	Description string // M/Description
	Language    string // M/Language (default "eng")
	Date        string // M/Date, e.g. "2026-06-14"
	Version     string // kage version, recorded as M/Scraper

	// CachePath, when set, points at a content-addressed cluster cache sidecar.
	// Compression of unchanged clusters is reused from it, and the cache is
	// rewritten after a successful pack. Empty disables the cache. It has no
	// effect with NoCompress, where nothing is compressed.
	CachePath string
	// Stats, when non-nil, is filled with how many clusters were reused from the
	// cache versus compressed fresh. It lets the caller report incremental gains
	// without changing the function's return signature.
	Stats *PackStats
}

// PackStats reports how a pack reused cached compression. ClustersReused is the
// number of clusters whose compressed bytes came straight from the cache;
// ClustersCompressed is the number that were zstd-compressed this run.
type PackStats struct {
	ClustersReused     int
	ClustersCompressed int
}

// BuildZIM walks mirrorDir, turns every file into a C/ content entry, infers the
// MIME from the extension, picks a main page, adds M/ metadata and a W/mainPage
// redirect, and writes a .zim to opts.Out. It returns the output path and the
// number of bytes written.
func BuildZIM(mirrorDir string, opts ZIMOptions) (string, int64, error) {
	w, cache, err := buildWriter(mirrorDir, opts)
	if err != nil {
		return "", 0, err
	}
	out := opts.Out
	if out == "" {
		out = filepath.Base(mirrorDir) + ".zim"
	}
	f, err := os.Create(out)
	if err != nil {
		return "", 0, err
	}
	bw := bufio.NewWriter(f)
	n, err := w.WriteTo(bw)
	if err != nil {
		_ = f.Close()
		return out, n, err
	}
	if err := bw.Flush(); err != nil {
		_ = f.Close()
		return out, n, err
	}
	if err := f.Close(); err != nil {
		return out, n, err
	}
	if err := persistCache(opts.CachePath, cache); err != nil {
		return out, n, err
	}
	fillStats(opts.Stats, cache)
	return out, n, nil
}

// BuildZIMBytes is the buffer-returning sibling of BuildZIM: it runs the same
// walk and returns the archive in memory, which the binary path appends to a
// base executable without writing the ZIM to disk first.
func BuildZIMBytes(mirrorDir string, opts ZIMOptions) ([]byte, error) {
	w, cache, err := buildWriter(mirrorDir, opts)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		return nil, err
	}
	if err := persistCache(opts.CachePath, cache); err != nil {
		return nil, err
	}
	fillStats(opts.Stats, cache)
	return buf.Bytes(), nil
}

// persistCache writes the cluster cache back to its sidecar after a successful
// pack. It is a no-op when caching is off.
func persistCache(path string, c *clusterCache) error {
	if c == nil || path == "" {
		return nil
	}
	return c.save(path)
}

// fillStats copies the cache's reuse counters into the caller's PackStats when
// both are present, so the CLI can report incremental gains.
func fillStats(dst *PackStats, c *clusterCache) {
	if dst == nil || c == nil {
		return
	}
	dst.ClustersReused = c.reused
	dst.ClustersCompressed = c.compressed
}

// buildWriter does the shared work of both BuildZIM and BuildZIMBytes: it loads
// every file under mirrorDir into a zim.Writer with metadata and a main page.
func buildWriter(mirrorDir string, opts ZIMOptions) (*zim.Writer, *clusterCache, error) {
	info, err := os.Stat(mirrorDir)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("pack: %q is not a directory", mirrorDir)
	}

	w := zim.NewWriter()
	if opts.NoCompress {
		w.SetNoCompress(true)
	}

	// The cluster cache only helps when clusters are compressed; with NoCompress
	// there is nothing to reuse, so it is skipped.
	var cache *clusterCache
	if opts.CachePath != "" && !opts.NoCompress {
		cache = loadClusterCache(opts.CachePath)
		w.SetCompress(cache.Compress)
	}

	skip := urlx.DefaultReserved + "/state.json"
	var htmlPages []string
	counts := map[string]int{}

	walkErr := filepath.WalkDir(mirrorDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := slashRel(mirrorDir, p)
		if rel == skip {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		mime := MimeForExt(rel)
		if mime == "text/html" {
			htmlPages = append(htmlPages, rel)
		}
		counts[mime]++
		w.AddContent(zim.NamespaceContent, rel, "", mime, data)
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}

	main := pickMainPage(htmlPages)
	if main != "" {
		w.SetMainPage(zim.NamespaceContent, main)
		w.AddRedirect(zim.NamespaceWellKnown, "mainPage", "", zim.NamespaceContent, main)
	}

	host := filepath.Base(mirrorDir)
	title := firstNonEmpty(opts.Title, htmlTitleOf(mirrorDir, main), host)
	w.AddMetadata("Title", title)
	w.AddMetadata("Name", host)
	w.AddMetadata("Language", firstNonEmpty(opts.Language, "eng"))
	// Description is mandatory metadata in the ZIM spec, so it is always written:
	// the caller's text when given, otherwise a line derived from the host.
	w.AddMetadata("Description", firstNonEmpty(opts.Description, "Offline mirror of "+host+", cloned by kage."))
	w.AddMetadata("Creator", "kage")
	w.AddMetadata("Publisher", "kage")
	if opts.Date != "" {
		w.AddMetadata("Date", opts.Date)
	}
	w.AddMetadata("Scraper", strings.TrimSpace("kage "+opts.Version))
	w.AddMetadata("Source", host)
	w.AddMetadata("Counter", counterString(counts))
	// Illustrator_48x48@1 is the 48x48 PNG favicon Kiwix shows as the archive's
	// icon. When the mirror has no usable icon the archive ships without one.
	if png, ok := Favicon48(mirrorDir); ok {
		w.AddMetadataBytes("Illustrator_48x48@1", "image/png", png)
	}
	return w, cache, nil
}

// pickMainPage chooses the archive's entry point: the root index if present,
// else the shallowest HTML page, ties broken lexicographically for determinism.
// It returns "" when the mirror has no HTML at all.
func pickMainPage(htmlPages []string) string {
	for _, p := range htmlPages {
		if p == "index.html" {
			return p
		}
	}
	sorted := append([]string(nil), htmlPages...)
	sort.Slice(sorted, func(i, j int) bool {
		di, dj := strings.Count(sorted[i], "/"), strings.Count(sorted[j], "/")
		if di != dj {
			return di < dj
		}
		return sorted[i] < sorted[j]
	})
	if len(sorted) > 0 {
		return sorted[0]
	}
	return ""
}

// htmlTitleOf reads the main page off disk and returns its <title>, or "" if
// there is no main page or no title.
func htmlTitleOf(mirrorDir, mainURL string) string {
	if mainURL == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(mirrorDir, filepath.FromSlash(mainURL)))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	doc, err := html.Parse(f)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(findTitle(doc))
}

// findTitle returns the text of the first <title> element in depth-first order.
func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			}
		}
		return b.String()
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}
	return ""
}

// counterString renders the M/Counter value Kiwix uses for stats: a
// semicolon-separated list of mime=count pairs, sorted for determinism.
func counterString(counts map[string]int) string {
	mimes := make([]string, 0, len(counts))
	for m := range counts {
		mimes = append(mimes, m)
	}
	sort.Strings(mimes)
	parts := make([]string, len(mimes))
	for i, m := range mimes {
		parts[i] = fmt.Sprintf("%s=%d", m, counts[m])
	}
	return strings.Join(parts, ";")
}

// slashRel returns p relative to root using forward slashes, the form ZIM urls
// take regardless of the host filesystem separator.
func slashRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		rel = p
	}
	return filepath.ToSlash(rel)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
