package clone

import (
	"sync"
	"sync/atomic"
)

// maxRecordedFailures caps how many individual failures Run keeps for the final
// report, so a huge broken site cannot grow the slice without bound. The error
// counters still count every failure.
const maxRecordedFailures = 100

// stats are the live counters of a run, read by the CLI's progress ticker.
type stats struct {
	pages       atomic.Int64 // page documents written (one per output file)
	pagePaths   atomic.Int64 // distinct URL paths among those, ignoring query
	pagesLinked atomic.Int64 // pages stored as a hard link to identical content
	assets      atomic.Int64
	pageErrors  atomic.Int64
	assetErrors atomic.Int64
	skipped     atomic.Int64 // robots-disallowed or out of budget

	muPaths  sync.Mutex
	seenPath map[string]struct{}

	muFail   sync.Mutex
	failures []Failure
}

// recordPage counts a freshly written page. Every write bumps pages; the first
// write for a given query-stripped path also bumps pagePaths, so the display can
// separate real pages from the query-string variants (?q=…, ?page=…) that a
// single path can spawn by the thousand on a faceted site. deduped marks a page
// whose bytes were stored as a hard link to identical content already on disk.
func (s *stats) recordPage(pathKey string, deduped bool) {
	s.pages.Add(1)
	if deduped {
		s.pagesLinked.Add(1)
	}
	s.muPaths.Lock()
	if s.seenPath == nil {
		s.seenPath = make(map[string]struct{})
	}
	if _, ok := s.seenPath[pathKey]; !ok {
		s.seenPath[pathKey] = struct{}{}
		s.pagePaths.Add(1)
	}
	s.muPaths.Unlock()
}

// Failure is one thing that went wrong, kept for the end-of-run report so the
// errors are visible as a list rather than only as a count.
type Failure struct {
	Kind    string // "page" or "asset"
	URL     string
	Referer string // the page that referenced it, when known
	Reason  string // e.g. "HTTP 403 Forbidden"
}

func (s *stats) recordFailure(f Failure) {
	s.muFail.Lock()
	if len(s.failures) < maxRecordedFailures {
		s.failures = append(s.failures, f)
	}
	s.muFail.Unlock()
}

func (s *stats) recordedFailures() []Failure {
	s.muFail.Lock()
	defer s.muFail.Unlock()
	out := make([]Failure, len(s.failures))
	copy(out, s.failures)
	return out
}

// Progress is a snapshot of a run for display. Pages is every page document
// written (it equals the count of HTML files on disk); PagePaths is how many
// distinct URL paths those represent once query strings are ignored. The
// difference, Pages-PagePaths, is the number of query-string variants.
type Progress struct {
	Pages       int64
	PagePaths   int64
	PagesLinked int64
	Assets      int64
	PageErrors  int64
	AssetErrors int64
	Skipped     int64
}

func (s *stats) snapshot() Progress {
	return Progress{
		Pages:       s.pages.Load(),
		PagePaths:   s.pagePaths.Load(),
		PagesLinked: s.pagesLinked.Load(),
		Assets:      s.assets.Load(),
		PageErrors:  s.pageErrors.Load(),
		AssetErrors: s.assetErrors.Load(),
		Skipped:     s.skipped.Load(),
	}
}

// Result is the final outcome returned by Run.
type Result struct {
	Progress
	OutDir string
	// Failures is a capped sample of what went wrong, for the final report.
	Failures []Failure
}
