package dataset

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/kage/zim"
)

// buildZIM writes a small archive with a page, an asset, metadata, and a
// main-page redirect to a temp file, and returns its path.
func buildZIM(t *testing.T) string {
	t.Helper()
	w := zim.NewWriter()
	w.AddContent(zim.NamespaceContent, "index.html", "Home",
		"text/html", []byte("<html><head><title>Home</title></head><body><script>ignore()</script><h1>Hello</h1><p>World</p></body></html>"))
	w.AddContent(zim.NamespaceContent, "logo.png", "", "image/png", []byte{0x89, 'P', 'N', 'G', 1, 2, 3})
	w.AddMetadata("Title", "Test Site")
	w.AddMetadata("Language", "eng")
	w.SetMainPage(zim.NamespaceContent, "index.html")
	w.AddRedirect(zim.NamespaceWellKnown, "mainPage", "", zim.NamespaceContent, "index.html")

	path := filepath.Join(t.TempDir(), "site.zim")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteTo(f); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestZIMParquetRoundTrip(t *testing.T) {
	src := buildZIM(t)
	dir := t.TempDir()
	pq := filepath.Join(dir, "site.parquet")
	dst := filepath.Join(dir, "site2.zim")

	exp, err := ZIMToParquet(src, pq, "test")
	if err != nil {
		t.Fatalf("ZIMToParquet: %v", err)
	}
	if exp.Rows == 0 {
		t.Fatal("exported zero rows")
	}
	if exp.Redirects == 0 {
		t.Fatal("expected the mainPage redirect to be exported")
	}

	imp, err := ParquetToZIM(pq, dst, "test")
	if err != nil {
		t.Fatalf("ParquetToZIM: %v", err)
	}
	if imp.Rows != exp.Rows {
		t.Fatalf("row count changed across round trip: exported %d, imported %d", exp.Rows, imp.Rows)
	}
	if imp.Redirects != exp.Redirects {
		t.Fatalf("redirect count changed: exported %d, imported %d", exp.Redirects, imp.Redirects)
	}

	// The rebuilt archive must serve the same content and resolve its main page.
	r, err := zim.Open(dst)
	if err != nil {
		t.Fatalf("open rebuilt zim: %v", err)
	}
	defer func() { _ = r.Close() }()

	home, err := r.Get(zim.NamespaceContent, "index.html")
	if err != nil {
		t.Fatalf("get index.html: %v", err)
	}
	if got := string(home.Data); got != "<html><head><title>Home</title></head><body><script>ignore()</script><h1>Hello</h1><p>World</p></body></html>" {
		t.Fatalf("page content changed: %q", got)
	}
	if home.MimeType != "text/html" {
		t.Fatalf("page mime changed: %q", home.MimeType)
	}

	logo, err := r.Get(zim.NamespaceContent, "logo.png")
	if err != nil {
		t.Fatalf("get logo.png: %v", err)
	}
	if string(logo.Data) != string([]byte{0x89, 'P', 'N', 'G', 1, 2, 3}) {
		t.Fatal("asset bytes changed across round trip")
	}

	title, err := r.Get(zim.NamespaceMetadata, "Title")
	if err != nil {
		t.Fatalf("get M/Title: %v", err)
	}
	if string(title.Data) != "Test Site" {
		t.Fatalf("metadata changed: %q", string(title.Data))
	}

	main, err := r.MainPage()
	if err != nil {
		t.Fatalf("main page not set after round trip: %v", err)
	}
	if main.URL != "index.html" {
		t.Fatalf("main page url changed: %q", main.URL)
	}
}

func TestHTMLTextStripsScript(t *testing.T) {
	got := htmlText([]byte("<html><body><script>secret()</script><style>.x{}</style><h1>Visible</h1>  <p>Text\nhere</p></body></html>"))
	want := "Visible Text here"
	if got != want {
		t.Fatalf("htmlText = %q, want %q", got, want)
	}
}
