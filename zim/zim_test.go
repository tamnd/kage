package zim

import (
	"bytes"
	"crypto/md5"
	"strings"
	"testing"
)

// buildSample writes a small archive exercising text, binary, metadata, a
// redirect, and a main page, and returns its bytes.
func buildSample(t *testing.T, noCompress bool) []byte {
	t.Helper()
	w := NewWriter()
	w.SetNoCompress(noCompress)
	w.AddContent(NamespaceContent, "index.html", "Home", "text/html",
		[]byte("<h1>Home</h1>"+strings.Repeat(" word", 500)))
	w.AddContent(NamespaceContent, "about/index.html", "About", "text/html",
		[]byte("<h1>About</h1>"))
	w.AddContent(NamespaceContent, "_kage/h/logo.png", "", "image/png",
		[]byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3, 4, 5})
	w.AddMetadata("Title", "Sample")
	w.AddMetadata("Language", "eng")
	w.AddRedirect(NamespaceWellKnown, "mainPage", "Main", NamespaceContent, "index.html")
	w.SetMainPage(NamespaceContent, "index.html")

	var buf bytes.Buffer
	n, err := w.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if int(n) != buf.Len() {
		t.Fatalf("WriteTo reported %d bytes, buffer has %d", n, buf.Len())
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	for _, noCompress := range []bool{false, true} {
		data := buildSample(t, noCompress)
		r, err := NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("NewReader (noCompress=%v): %v", noCompress, err)
		}

		// Content round-trips with the right mime.
		home, err := r.Get(NamespaceContent, "index.html")
		if err != nil {
			t.Fatalf("Get home: %v", err)
		}
		if !strings.HasPrefix(string(home.Data), "<h1>Home</h1>") {
			t.Errorf("home content wrong: %.20q", home.Data)
		}
		if home.MimeType != "text/html" {
			t.Errorf("home mime = %q", home.MimeType)
		}

		// Binary blob survives byte-for-byte.
		logo, err := r.Get(NamespaceContent, "_kage/h/logo.png")
		if err != nil {
			t.Fatalf("Get logo: %v", err)
		}
		if !bytes.Equal(logo.Data, []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3, 4, 5}) {
			t.Errorf("logo bytes wrong: %v", logo.Data)
		}
		if logo.MimeType != "image/png" {
			t.Errorf("logo mime = %q", logo.MimeType)
		}

		// Metadata.
		meta, err := r.Get(NamespaceMetadata, "Title")
		if err != nil || string(meta.Data) != "Sample" {
			t.Errorf("metadata Title = %q, %v", meta.Data, err)
		}

		// Redirect resolves to the target's content.
		red, err := r.Get(NamespaceWellKnown, "mainPage")
		if err != nil {
			t.Fatalf("Get redirect: %v", err)
		}
		if !strings.HasPrefix(string(red.Data), "<h1>Home</h1>") {
			t.Errorf("redirect did not resolve to home: %.20q", red.Data)
		}

		// Main page.
		mp, err := r.MainPage()
		if err != nil {
			t.Fatalf("MainPage: %v", err)
		}
		if !strings.HasPrefix(string(mp.Data), "<h1>Home</h1>") {
			t.Errorf("main page wrong: %.20q", mp.Data)
		}

		// Misses error.
		if _, err := r.Get(NamespaceContent, "nope.html"); err == nil {
			t.Error("expected miss to error")
		}
	}
}

func TestChecksum(t *testing.T) {
	data := buildSample(t, false)
	if len(data) < 16 {
		t.Fatal("archive too short")
	}
	body, sum := data[:len(data)-16], data[len(data)-16:]
	want := md5.Sum(body)
	if !bytes.Equal(sum, want[:]) {
		t.Errorf("trailing MD5 does not match body hash")
	}
}

func TestDeterministic(t *testing.T) {
	a := buildSample(t, false)
	b := buildSample(t, false)
	if !bytes.Equal(a, b) {
		t.Error("same input produced different archives; packing is not deterministic")
	}
}

func TestMagicAndHeader(t *testing.T) {
	data := buildSample(t, false)
	h, err := parseHeader(data[:headerLen])
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	if h.checksumPos != uint64(len(data)-16) {
		t.Errorf("checksumPos = %d, want %d", h.checksumPos, len(data)-16)
	}
	if h.articleCount != 6 {
		t.Errorf("articleCount = %d, want 6", h.articleCount)
	}
	if h.mainPage == noMainPage {
		t.Error("main page not set in header")
	}
}
