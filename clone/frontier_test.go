package clone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFrontierOfferDedups(t *testing.T) {
	f := newFrontier()
	if !f.offer("a", pendingPage{URL: "https://example.com/a"}) {
		t.Fatal("first offer of a should be new")
	}
	if f.offer("a", pendingPage{URL: "https://example.com/a"}) {
		t.Fatal("second offer of a should be a duplicate")
	}
	if !f.offer("b", pendingPage{URL: "https://example.com/b"}) {
		t.Fatal("first offer of b should be new")
	}
}

func TestFrontierVisited(t *testing.T) {
	f := newFrontier()
	if f.isVisited("x") {
		t.Fatal("x should not be visited yet")
	}
	f.markVisited("x")
	if !f.isVisited("x") {
		t.Fatal("x should be visited after markVisited")
	}
	if f.visitedCount() != 1 {
		t.Fatalf("visitedCount = %d, want 1", f.visitedCount())
	}
}

func TestFrontierSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "state.json")

	f := newFrontier()
	f.markVisited("https://example.com/")
	f.markVisited("https://example.com/about")
	if err := f.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	g := newFrontier()
	if err := g.load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if g.visitedCount() != 2 {
		t.Fatalf("loaded visitedCount = %d, want 2", g.visitedCount())
	}
	if !g.isVisited("https://example.com/about") {
		t.Fatal("about should be visited after load")
	}
	// A loaded visited URL is also seen, so it is not re-offered.
	if g.offer("https://example.com/about", pendingPage{URL: "https://example.com/about"}) {
		t.Fatal("a loaded URL should not be offered again")
	}
}

func TestFrontierPersistsUnfinishedWork(t *testing.T) {
	// The bug in issue #36: only the visited set was saved, so a resumed run had
	// no frontier to work from and did nothing at all.
	path := filepath.Join(t.TempDir(), "state.json")

	f := newFrontier()
	f.offer("/index.html", pendingPage{URL: "https://example.com/", Depth: 0})
	f.offer("/about/index.html", pendingPage{URL: "https://example.com/about", Depth: 1})
	f.offer("/deep/index.html", pendingPage{URL: "https://example.com/deep", Depth: 3})
	f.markVisited("/index.html")
	if got := f.pendingCount(); got != 2 {
		t.Fatalf("pendingCount = %d, want 2", got)
	}
	if err := f.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	g := newFrontier()
	if err := g.load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []pendingPage{
		{URL: "https://example.com/about", Depth: 1},
		{URL: "https://example.com/deep", Depth: 3},
	}
	got := g.unfinished()
	if len(got) != len(want) {
		t.Fatalf("unfinished = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unfinished[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	// Unfinished pages must not come back marked seen, or re-queueing them would
	// be refused and the resumed run would go back to doing nothing.
	if !g.offer("/about/index.html", want[0]) {
		t.Error("an unfinished page should be offerable again on resume")
	}
}

func TestFrontierMarkDoneDropsWork(t *testing.T) {
	// A page robots.txt disallows is finished with, even though nothing was
	// written for it, so it must not ride along in the state file forever.
	f := newFrontier()
	f.offer("/private/index.html", pendingPage{URL: "https://example.com/private"})
	f.markDone("/private/index.html")
	if got := f.pendingCount(); got != 0 {
		t.Fatalf("pendingCount = %d, want 0", got)
	}
	if f.isVisited("/private/index.html") {
		t.Error("markDone should not claim the page was written")
	}
}

func TestFrontierLoadsStateWrittenBeforePending(t *testing.T) {
	// State files from an older kage have no "pending" key. They must still load,
	// with nothing outstanding, which is exactly what that run recorded.
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"visited":["/index.html"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFrontier()
	if err := f.load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !f.isVisited("/index.html") {
		t.Error("visited set should still load")
	}
	if n := len(f.unfinished()); n != 0 {
		t.Errorf("unfinished = %d, want 0", n)
	}
}

func TestFrontierLoadMissingIsNotError(t *testing.T) {
	f := newFrontier()
	if err := f.load(filepath.Join(t.TempDir(), "nope.json")); err != nil {
		t.Fatalf("loading a missing file should be a no-op, got %v", err)
	}
}
