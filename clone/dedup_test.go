package clone

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWritePageDedup checks that identical page bytes are stored once and shared
// by a hard link, while different bytes are written as their own file.
func TestWritePageDedup(t *testing.T) {
	dir := t.TempDir()
	c := &Cloner{outRoot: dir, seenContent: map[string]string{}}

	body := []byte("<html><body>same page</body></html>")

	if deduped, err := c.writePage("a/index.html", body); err != nil || deduped {
		t.Fatalf("first write: deduped=%v err=%v, want false/nil", deduped, err)
	}
	if deduped, err := c.writePage("b/index.html", body); err != nil || !deduped {
		t.Fatalf("second identical write: deduped=%v err=%v, want true/nil", deduped, err)
	}
	if deduped, err := c.writePage("c/index.html", []byte("<html>other</html>")); err != nil || deduped {
		t.Fatalf("third different write: deduped=%v err=%v, want false/nil", deduped, err)
	}

	// The two identical pages must be the same file on disk (one inode).
	fa, err := os.Stat(filepath.Join(dir, "a/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	fb, err := os.Stat(filepath.Join(dir, "b/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(fa, fb) {
		t.Error("identical pages were not hard-linked to the same file")
	}

	// The different page must stand alone with its own bytes.
	got, err := os.ReadFile(filepath.Join(dir, "c/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "<html>other</html>" {
		t.Errorf("third page content = %q", got)
	}
}

// TestRecordPageCounts checks that pages counts every write, pagePaths counts
// distinct query-stripped paths, and pagesLinked counts deduped writes.
func TestRecordPageCounts(t *testing.T) {
	var s stats
	s.recordPage("showcase/index.html", false)
	s.recordPage("showcase/index.html", true) // a ?q= variant of the same path
	s.recordPage("showcase/index.html", true)
	s.recordPage("about/index.html", false)

	p := s.snapshot()
	if p.Pages != 4 {
		t.Errorf("Pages = %d, want 4", p.Pages)
	}
	if p.PagePaths != 2 {
		t.Errorf("PagePaths = %d, want 2", p.PagePaths)
	}
	if p.PagesLinked != 2 {
		t.Errorf("PagesLinked = %d, want 2", p.PagesLinked)
	}
}
