// Package dataset converts between a packed ZIM archive and a columnar Parquet
// file. The Parquet form is a flat table with one row per archive entry and
// clear columns (url, mime, title, content, extracted text, redirect target),
// which is the shape a dataset host such as Hugging Face expects. The conversion
// is lossless: every entry, its metadata, and the main page survive a ZIM ->
// Parquet -> ZIM round trip, so the table doubles as an archival representation,
// not just an export.
package dataset

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go"
	"golang.org/x/net/html"

	"github.com/tamnd/kage/zim"
)

// Row is one archive entry as a Parquet record. A content or metadata entry
// carries its bytes in Content, its type in Mime, and, for HTML, its visible
// text in Text. A redirect sets IsRedirect and names its destination in
// RedirectTarget ("<namespace>/<url>", e.g. "C/index.html"), with Content empty.
//
// The leading columns (doc_id, url, host, crawl_date, the length pair, text)
// follow the field names the open-index/open-markdown dataset uses, so a kage
// export drops into the same tooling other web-crawl datasets on Hugging Face
// are read with. The trailing columns (namespace, the redirect pair, content)
// are kage's own: they carry the raw bytes and the ZIM structure that make the
// table a lossless, reversible copy of the archive rather than a one-way export.
//
// Namespace is the single-letter ZIM namespace: "C" for pages and assets, "M"
// for archive metadata (Title, Description, Language, ...), "W" for well-known
// entries such as the main-page pointer. Keeping every namespace as a row is
// what makes the table reversible.
type Row struct {
	DocID          string `parquet:"doc_id,dict"`
	URL            string `parquet:"url"`
	Host           string `parquet:"host,dict"`
	Title          string `parquet:"title"`
	Mime           string `parquet:"mime,dict"`
	CrawlDate      string `parquet:"crawl_date,dict"`
	ContentLength  int64  `parquet:"content_length"`
	TextLength     int64  `parquet:"text_length"`
	Text           string `parquet:"text"`
	Namespace      string `parquet:"namespace,dict"`
	IsRedirect     bool   `parquet:"is_redirect"`
	RedirectTarget string `parquet:"redirect_target"`
	Content        []byte `parquet:"content"`
}

// docNamespace is the UUID v5 namespace for kage doc_id values: the standard
// URL namespace, matching how open-markdown derives a deterministic id from a
// page's canonical URL.
var docNamespace = uuid.NameSpaceURL

// docID returns the deterministic UUID v5 an entry gets in the doc_id column,
// derived from its host and url so the same page always hashes to the same id
// across exports. Entries with no host (metadata, well-known) fall back to the
// namespaced url, which is still stable.
func docID(host, namespace, url string) string {
	name := namespace + "/" + url
	if host != "" {
		name = host + "/" + url
	}
	return uuid.NewSHA1(docNamespace, []byte(name)).String()
}

// Stats summarises a conversion for the CLI to report.
type Stats struct {
	Rows         int64 // total entries written
	Redirects    int64 // of those, redirects
	ContentBytes int64 // sum of stored content bytes (uncompressed)
}

// writeBatch bounds how many rows are buffered before a write to the Parquet
// writer. It only paces the calls; the writer manages its own row groups.
const writeBatch = 256

// ZIMToParquet reads the ZIM at zimPath and writes a Parquet table to outPath,
// one row per entry. The archive's main page is recorded both as its W/mainPage
// redirect row and as file-level metadata, and a short generator/source line is
// attached so the dataset is self-describing. version is kage's version string.
func ZIMToParquet(zimPath, outPath, version string) (Stats, error) {
	r, err := zim.Open(zimPath)
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = r.Close() }()

	f, err := os.Create(outPath)
	if err != nil {
		return Stats{}, err
	}
	bw := bufio.NewWriter(f)
	pw := parquet.NewGenericWriter[Row](bw, parquet.Compression(&parquet.Zstd))

	host := metaValue(r, "Source")
	if host == "" {
		host = metaValue(r, "Name")
	}
	host = strings.ToLower(host)
	crawlDate := metaValue(r, "Date")

	if ns, u, ok := r.MainPageRef(); ok {
		pw.SetKeyValueMetadata("kage.main_page", string(ns)+"/"+u)
	}
	pw.SetKeyValueMetadata("kage.generator", strings.TrimSpace("kage "+version))
	pw.SetKeyValueMetadata("kage.source", filepath.Base(zimPath))
	if host != "" {
		pw.SetKeyValueMetadata("kage.host", host)
	}

	var st Stats
	count := r.Count()
	batch := make([]Row, 0, writeBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := pw.Write(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	for i := uint32(0); i < count; i++ {
		e, err := r.EntryAt(i)
		if err != nil {
			return st, fmt.Errorf("read entry %d: %w", i, err)
		}
		row := Row{
			Namespace: string(e.Namespace),
			URL:       e.URL,
			Title:     e.Title,
			Host:      host,
			CrawlDate: crawlDate,
			DocID:     docID(host, string(e.Namespace), e.URL),
		}
		if e.Redirect {
			row.IsRedirect = true
			row.RedirectTarget = string(e.RedirectNamespace) + "/" + e.RedirectURL
			st.Redirects++
		} else {
			row.Mime = e.MimeType
			row.Content = e.Data
			row.ContentLength = int64(len(e.Data))
			st.ContentBytes += int64(len(e.Data))
			if e.MimeType == "text/html" {
				row.Text = htmlText(e.Data)
				row.TextLength = int64(len(row.Text))
			}
		}
		st.Rows++
		batch = append(batch, row)
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				return st, err
			}
		}
	}
	if err := flush(); err != nil {
		return st, err
	}
	if err := pw.Close(); err != nil {
		return st, err
	}
	if err := bw.Flush(); err != nil {
		return st, err
	}
	return st, f.Close()
}

// ParquetToZIM reads the Parquet table at parquetPath and writes the ZIM archive
// it describes to outPath, reproducing every entry, its metadata, and the main
// page. version is unused for now; it is accepted so the signature matches its
// sibling and can record provenance later.
func ParquetToZIM(parquetPath, outPath, _ string) (Stats, error) {
	f, err := os.Open(parquetPath)
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = f.Close() }()

	pr := parquet.NewGenericReader[Row](f)
	defer func() { _ = pr.Close() }()

	w := zim.NewWriter()
	var st Stats
	var mainNS byte
	var mainURL string
	haveMain := false

	buf := make([]Row, writeBatch)
	for {
		n, readErr := pr.Read(buf)
		for i := 0; i < n; i++ {
			row := buf[i]
			ns := namespaceByte(row.Namespace)
			if row.IsRedirect {
				tns, turl := splitTarget(row.RedirectTarget)
				w.AddRedirect(ns, row.URL, row.Title, tns, turl)
				if ns == zim.NamespaceWellKnown && row.URL == "mainPage" {
					mainNS, mainURL, haveMain = tns, turl, true
				}
				st.Redirects++
			} else {
				w.AddContent(ns, row.URL, row.Title, row.Mime, row.Content)
				st.ContentBytes += int64(len(row.Content))
			}
			st.Rows++
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return st, readErr
		}
		if n == 0 {
			break
		}
	}
	if haveMain {
		w.SetMainPage(mainNS, mainURL)
	}

	out, err := os.Create(outPath)
	if err != nil {
		return st, err
	}
	obw := bufio.NewWriter(out)
	if _, err := w.WriteTo(obw); err != nil {
		_ = out.Close()
		return st, err
	}
	if err := obw.Flush(); err != nil {
		_ = out.Close()
		return st, err
	}
	return st, out.Close()
}

// metaValue reads an M/ metadata entry as a string, returning "" when it is
// absent. It lets the exporter fill the host and crawl_date columns from the
// archive's own metadata.
func metaValue(r *zim.Reader, name string) string {
	b, err := r.Get(zim.NamespaceMetadata, name)
	if err != nil {
		return ""
	}
	return string(b.Data)
}

// namespaceByte turns a one-letter namespace column back into the ZIM byte,
// defaulting to the content namespace for an unexpectedly empty value.
func namespaceByte(s string) byte {
	if s == "" {
		return zim.NamespaceContent
	}
	return s[0]
}

// splitTarget parses a "<namespace>/<url>" redirect target back into its parts.
// The namespace is a single byte, so the url is everything after the first two
// characters; a malformed value yields the content namespace and the raw string.
func splitTarget(s string) (byte, string) {
	if len(s) >= 2 && s[1] == '/' {
		return s[0], s[2:]
	}
	return zim.NamespaceContent, s
}

// htmlText extracts the visible text of an HTML document: the concatenated text
// nodes outside script, style, and noscript, with runs of whitespace collapsed
// to single spaces. It is a derived convenience column for dataset consumers and
// plays no part in the round trip, which reconstructs pages from Content.
func htmlText(data []byte) string {
	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript":
				return
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(b.String()), " ")
}
