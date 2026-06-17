package pack

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/kage/urlx"
	"github.com/tamnd/kage/zim"
)

// writeMirror lays down a small kage-style mirror under a temp dir and returns
// the host dir.
func writeMirror(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	host := filepath.Join(root, "example.com")
	files := map[string]string{
		"index.html":                   "<!doctype html><title>Example Home</title><h1>Hi</h1>",
		"about/index.html":             "<!doctype html><title>About</title><h1>About</h1>",
		"_kage/example.com/x/logo.png": "\x89PNGfake",
		"_kage/state.json":             `{"visited":[]}`, // must be skipped
	}
	for rel, body := range files {
		p := filepath.Join(host, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return host
}

func TestMimeForExt(t *testing.T) {
	cases := map[string]string{
		"a/b/index.html":  "text/html",
		"style.CSS":       "text/css",
		"data.json":       "application/json",
		"icon.svg":        "image/svg+xml",
		"logo.png":        "image/png",
		"photo.JPEG":      "image/jpeg",
		"font.woff2":      "font/woff2",
		"clip.mp4":        "video/mp4",
		"doc.pdf":         "application/pdf",
		"mystery":         "application/octet-stream",
		"archive.tar.zst": "application/octet-stream",
	}
	for in, want := range cases {
		if got := MimeForExt(in); got != want {
			t.Errorf("MimeForExt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildZIMRoundTrip(t *testing.T) {
	host := writeMirror(t)
	out := filepath.Join(t.TempDir(), "example.zim")
	path, size, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14", Version: "test"})
	if err != nil {
		t.Fatalf("BuildZIM: %v", err)
	}
	if path != out {
		t.Errorf("path = %q, want %q", path, out)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != size {
		t.Errorf("reported size %d, file is %d", size, fi.Size())
	}

	r, err := zim.Open(out)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	// Main page is the root index.
	mp, err := r.MainPage()
	if err != nil {
		t.Fatalf("MainPage: %v", err)
	}
	if !bytes.Contains(mp.Data, []byte("Example Home")) {
		t.Errorf("main page wrong: %.40q", mp.Data)
	}
	if mp.MimeType != "text/html" {
		t.Errorf("main page mime = %q", mp.MimeType)
	}

	// Binary asset round-trips byte-for-byte.
	logo, err := r.Get(zim.NamespaceContent, "_kage/example.com/x/logo.png")
	if err != nil {
		t.Fatalf("Get logo: %v", err)
	}
	if string(logo.Data) != "\x89PNGfake" {
		t.Errorf("logo bytes wrong: %q", logo.Data)
	}

	// Title metadata comes from the main page's <title>.
	title, err := r.Get(zim.NamespaceMetadata, "Title")
	if err != nil || string(title.Data) != "Example Home" {
		t.Errorf("M/Title = %q, %v", title.Data, err)
	}

	// state.json was skipped.
	if _, err := r.Get(zim.NamespaceContent, urlx.DefaultReserved+"/state.json"); err == nil {
		t.Error("state.json should not be packed")
	}
}

func TestBuildZIMDeterministic(t *testing.T) {
	host := writeMirror(t)
	dir := t.TempDir()
	a, _, err := BuildZIM(host, ZIMOptions{Out: filepath.Join(dir, "a.zim"), Date: "2026-06-14"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := BuildZIM(host, ZIMOptions{Out: filepath.Join(dir, "b.zim"), Date: "2026-06-14"})
	if err != nil {
		t.Fatal(err)
	}
	ba, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if !bytes.Equal(ba, bb) {
		t.Error("same mirror produced different archives")
	}
}

// TestBuildZIMMetadata checks the metadata zimcheck treats as mandatory: a
// Title, Name, Language, Description, and the Illustrator_48x48@1 favicon. The
// mirror carries a real PNG favicon so the icon path is exercised end to end.
func TestBuildZIMMetadata(t *testing.T) {
	host := writeMirror(t)

	// Drop a 64x64 PNG favicon into the mirror so Favicon48 has something to
	// find, decode, and rescale to 48x48.
	icon := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			icon.Set(x, y, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, icon); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, "favicon.png"), pngBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "meta.zim")
	if _, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14", Version: "test"}); err != nil {
		t.Fatalf("BuildZIM: %v", err)
	}
	r, err := zim.Open(out)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	wantText := map[string]string{
		"Name":        "example.com",
		"Language":    "eng",
		"Description": "Offline mirror of example.com, cloned by kage.",
	}
	for k, want := range wantText {
		e, err := r.Get(zim.NamespaceMetadata, k)
		if err != nil {
			t.Errorf("M/%s: %v", k, err)
			continue
		}
		if string(e.Data) != want {
			t.Errorf("M/%s = %q, want %q", k, e.Data, want)
		}
	}

	// Illustrator_48x48@1 is a 48x48 PNG with an image/png MIME.
	ill, err := r.Get(zim.NamespaceMetadata, "Illustrator_48x48@1")
	if err != nil {
		t.Fatalf("M/Illustrator_48x48@1: %v", err)
	}
	if ill.MimeType != "image/png" {
		t.Errorf("illustrator mime = %q, want image/png", ill.MimeType)
	}
	img, format, err := image.Decode(bytes.NewReader(ill.Data))
	if err != nil {
		t.Fatalf("decode illustrator: %v", err)
	}
	if format != "png" {
		t.Errorf("illustrator format = %q, want png", format)
	}
	if b := img.Bounds(); b.Dx() != 48 || b.Dy() != 48 {
		t.Errorf("illustrator size = %dx%d, want 48x48", b.Dx(), b.Dy())
	}

	// A caller-supplied description overrides the host-derived default.
	out2 := filepath.Join(t.TempDir(), "meta2.zim")
	if _, _, err := BuildZIM(host, ZIMOptions{Out: out2, Description: "Custom blurb.", Date: "2026-06-14"}); err != nil {
		t.Fatalf("BuildZIM: %v", err)
	}
	r2, err := zim.Open(out2)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r2.Close() }()
	if d, err := r2.Get(zim.NamespaceMetadata, "Description"); err != nil || string(d.Data) != "Custom blurb." {
		t.Errorf("M/Description = %q, %v; want %q", d.Data, err, "Custom blurb.")
	}
}

func TestPickMainPage(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"a/index.html", "index.html", "b.html"}, "index.html"},
		{[]string{"z/deep/p.html", "top.html", "a/p.html"}, "top.html"},
		{[]string{"b/x.html", "a/x.html"}, "a/x.html"}, // same depth, lexical
		{nil, ""},
	}
	for _, c := range cases {
		if got := pickMainPage(c.in); got != c.want {
			t.Errorf("pickMainPage(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBinaryTrailerRoundTrip exercises the BuildBinary append contract and the
// trailer it leaves, without depending on os.Executable: it appends a ZIM to a
// fake base, reads the trailer back the way Embedded does, and serves the
// recovered archive.
func TestBinaryTrailerRoundTrip(t *testing.T) {
	host := writeMirror(t)
	zbytes, err := BuildZIMBytes(host, ZIMOptions{Date: "2026-06-14"})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	base := filepath.Join(dir, "fakekage")
	baseBytes := bytes.Repeat([]byte("BASE"), 64) // stand-in for a kage binary
	if err := os.WriteFile(base, baseBytes, 0o755); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "viewer")
	_, total, err := BuildBinary(zbytes, BinaryOptions{Out: out, Base: base})
	if err != nil {
		t.Fatalf("BuildBinary: %v", err)
	}
	if total != int64(len(baseBytes)+len(zbytes)+trailerLen) {
		t.Errorf("total %d, want %d", total, len(baseBytes)+len(zbytes)+trailerLen)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	fi, _ := f.Stat()
	end := fi.Size()

	tr := make([]byte, trailerLen)
	if _, err := f.ReadAt(tr, end-int64(trailerLen)); err != nil {
		t.Fatal(err)
	}
	if string(tr[:8]) != trailerMagic || string(tr[trailerLen-8:]) != trailerMagic {
		t.Fatal("trailer magic missing")
	}
	zlen := int64(binary.LittleEndian.Uint64(tr[8:16]))
	if zlen != int64(len(zbytes)) {
		t.Errorf("trailer length %d, want %d", zlen, len(zbytes))
	}
	start := end - int64(trailerLen) - zlen
	if start != int64(len(baseBytes)) {
		t.Errorf("archive start %d, want %d", start, len(baseBytes))
	}

	sec := io.NewSectionReader(f, start, zlen)
	r, err := zim.NewReader(sec, zlen)
	if err != nil {
		t.Fatalf("reopen appended zim: %v", err)
	}
	mp, err := r.MainPage()
	if err != nil || !bytes.Contains(mp.Data, []byte("Example Home")) {
		t.Errorf("recovered main page wrong: %.40q (%v)", mp.Data, err)
	}
}

func TestHandler(t *testing.T) {
	host := writeMirror(t)
	out := filepath.Join(t.TempDir(), "h.zim")
	if _, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14"}); err != nil {
		t.Fatal(err)
	}
	r, err := zim.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	srv := httptest.NewServer(Handler(r))
	defer srv.Close()

	get := func(p string) (int, string) {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, body := get("/"); code != 200 || !bytes.Contains([]byte(body), []byte("Example Home")) {
		t.Errorf("GET / = %d %.30q", code, body)
	}

	// "/" must redirect to the main page's canonical content path, not serve its
	// bytes in place, so the page's mirror-relative asset URLs resolve against the
	// right base instead of 404ing.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noFollow.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("GET / status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/index.html" {
		t.Errorf("GET / Location = %q, want %q", loc, "/index.html")
	}
	if code, _ := get("/about/index.html"); code != 200 {
		t.Errorf("GET /about/index.html = %d", code)
	}
	if code, _ := get("/" + urlx.DefaultReserved + "/state.json"); code != 404 {
		t.Errorf("GET state.json = %d, want 404", code)
	}
	if code, _ := get("/missing.html"); code != 404 {
		t.Errorf("GET missing = %d, want 404", code)
	}
}

// titleOf returns the stored entry title for a content url, scanning entries in
// url order since the reader exposes titles only through EntryAt.
func titleOf(t *testing.T, r *zim.Reader, url string) string {
	t.Helper()
	for i := uint32(0); i < r.Count(); i++ {
		e, err := r.EntryAt(i)
		if err != nil {
			continue
		}
		if e.Namespace == zim.NamespaceContent && e.URL == url {
			return e.Title
		}
	}
	t.Fatalf("no content entry for %q", url)
	return ""
}

// TestBuildZIMPageTitles checks that each HTML page entry carries its own
// <title> (collapsed to one line), that a page with no title falls back to its
// url, and that a non-HTML asset keeps the url as its title.
func TestBuildZIMPageTitles(t *testing.T) {
	root := t.TempDir()
	host := filepath.Join(root, "example.com")
	files := map[string]string{
		"index.html":       "<!doctype html><title>Home</title><h1>Hi</h1>",
		"essay/index.html": "<!doctype html><title>\n  A Long\n  Title  </title><p>x</p>",
		"bare/index.html":  "<!doctype html><h1>No title here</h1>",
		"logo.png":         "\x89PNGfake",
	}
	for rel, body := range files {
		p := filepath.Join(host, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := filepath.Join(t.TempDir(), "titles.zim")
	if _, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14"}); err != nil {
		t.Fatalf("BuildZIM: %v", err)
	}
	r, err := zim.Open(out)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	cases := map[string]string{
		"index.html":       "Home",
		"essay/index.html": "A Long Title", // newlines and runs collapsed to single spaces
		"bare/index.html":  "bare/index.html",
		"logo.png":         "logo.png",
	}
	for url, want := range cases {
		if got := titleOf(t, r, url); got != want {
			t.Errorf("title of %q = %q, want %q", url, got, want)
		}
	}
}

func TestCollapseSpaces(t *testing.T) {
	cases := map[string]string{
		"  hello  world ":      "hello world",
		"line\n\tone\r\n  two": "line one two",
		"":                     "",
		"   ":                  "",
		"single":               "single",
	}
	for in, want := range cases {
		if got := collapseSpaces(in); got != want {
			t.Errorf("collapseSpaces(%q) = %q, want %q", in, got, want)
		}
	}
}
