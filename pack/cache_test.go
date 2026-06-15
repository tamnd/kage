package pack

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/kage/zim"
)

// TestIncrementalPackReusesCompression checks the core promise of the cache: a
// second pack of an unchanged mirror compresses nothing, reuses every cluster,
// and produces a byte-identical archive.
func TestIncrementalPackReusesCompression(t *testing.T) {
	host := writeMirror(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "example.zim")
	cache := out + ".kagecache"

	var first PackStats
	a, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14", CachePath: cache, Stats: &first})
	if err != nil {
		t.Fatalf("first pack: %v", err)
	}
	if first.ClustersCompressed == 0 {
		t.Fatal("first pack reported no clusters compressed")
	}
	if first.ClustersReused != 0 {
		t.Errorf("first pack reused %d clusters, want 0", first.ClustersReused)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("cache sidecar not written: %v", err)
	}
	bytesA, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}

	var second PackStats
	b, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14", CachePath: cache, Stats: &second})
	if err != nil {
		t.Fatalf("second pack: %v", err)
	}
	if second.ClustersCompressed != 0 {
		t.Errorf("second pack compressed %d clusters, want 0 (all reused)", second.ClustersCompressed)
	}
	if second.ClustersReused != first.ClustersCompressed {
		t.Errorf("second pack reused %d clusters, want %d", second.ClustersReused, first.ClustersCompressed)
	}
	bytesB, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytesA, bytesB) {
		t.Error("a cached re-pack produced a different archive than the cold pack")
	}
}

// TestIncrementalPackRecompressesChange checks that editing one page forces a
// fresh compression of the cluster that holds it while the rest are reused, and
// that the archive still opens and serves the new content.
func TestIncrementalPackRecompressesChange(t *testing.T) {
	host := writeMirror(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "example.zim")
	cache := out + ".kagecache"

	if _, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14", CachePath: cache}); err != nil {
		t.Fatalf("first pack: %v", err)
	}

	// Change one page. Its text cluster must be recompressed.
	edited := filepath.Join(host, "about", "index.html")
	if err := os.WriteFile(edited, []byte("<!doctype html><title>About</title><h1>Changed</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	var st PackStats
	if _, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14", CachePath: cache, Stats: &st}); err != nil {
		t.Fatalf("second pack: %v", err)
	}
	if st.ClustersCompressed == 0 {
		t.Error("editing a page compressed nothing; expected the changed cluster to be recompressed")
	}

	r, err := zim.Open(out)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = r.Close() }()
	about, err := r.Get(zim.NamespaceContent, "about/index.html")
	if err != nil {
		t.Fatalf("about page missing after re-pack: %v", err)
	}
	if !bytes.Contains(about.Data, []byte("Changed")) {
		t.Errorf("re-packed page did not carry the edit: %q", about.Data)
	}
}

// TestClusterCacheSurvivesCorruptSidecar checks that a damaged cache file is
// treated as a cold start rather than failing the pack.
func TestClusterCacheSurvivesCorruptSidecar(t *testing.T) {
	host := writeMirror(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "example.zim")
	cache := out + ".kagecache"
	if err := os.WriteFile(cache, []byte("not a real cache file"), 0o644); err != nil {
		t.Fatal(err)
	}

	var st PackStats
	if _, _, err := BuildZIM(host, ZIMOptions{Out: out, Date: "2026-06-14", CachePath: cache, Stats: &st}); err != nil {
		t.Fatalf("pack over corrupt cache: %v", err)
	}
	if st.ClustersReused != 0 {
		t.Errorf("corrupt cache should reuse nothing, reused %d", st.ClustersReused)
	}
	if st.ClustersCompressed == 0 {
		t.Error("pack over corrupt cache compressed nothing")
	}
}
