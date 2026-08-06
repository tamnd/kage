package clone

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// frontier is the deduped set of page URLs kage has already seen, plus the work
// it has not finished. It is small, concurrency-safe, and persists to disk so
// --resume can skip what is done and pick up what is not. The actual queueing is
// handled by the cloner's channels; the frontier only answers "is this URL new?"
// and remembers the answer.
type frontier struct {
	mu      sync.Mutex
	seen    map[string]bool        // queued or visited
	visited map[string]bool        // fully written
	pending map[string]pendingPage // offered, not yet finished
	resumed []pendingPage          // unfinished work read from disk
}

// pendingPage is a page kage meant to fetch and did not finish. The depth
// travels with it because --max-depth is measured from the seed, and a resumed
// run has no way to recompute it.
type pendingPage struct {
	URL   string `json:"url"`
	Depth int    `json:"depth"`
}

func newFrontier() *frontier {
	return &frontier{
		seen:    map[string]bool{},
		visited: map[string]bool{},
		pending: map[string]pendingPage{},
	}
}

// offer reports whether key is new, recording it as seen and as unfinished work.
// A repeated key returns false so it is enqueued only once.
func (f *frontier) offer(key string, p pendingPage) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen[key] {
		return false
	}
	f.seen[key] = true
	f.pending[key] = p
	return true
}

// markVisited records that a page was written.
func (f *frontier) markVisited(key string) {
	f.mu.Lock()
	f.visited[key] = true
	delete(f.pending, key)
	f.mu.Unlock()
}

// markDone records that a page needs no more work although nothing was written
// for it, so it is not carried into the next run. robots.txt disallowing a page
// is the case: a resumed run would only fetch robots.txt and skip it again.
//
// A page that failed is deliberately not marked done. It stays in the frontier
// and a resumed run retries it, which is the "memory of what failed" asked for
// in issue #36.
func (f *frontier) markDone(key string) {
	f.mu.Lock()
	delete(f.pending, key)
	f.mu.Unlock()
}

// unfinished returns the work loaded from a previous run's state file, for the
// cloner to re-queue at startup. It is empty on a fresh run.
func (f *frontier) unfinished() []pendingPage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resumed
}

// pendingCount reports how many pages are queued or in flight but not finished.
func (f *frontier) pendingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending)
}

// isVisited reports whether a page was already written in a previous run.
func (f *frontier) isVisited(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.visited[key]
}

func (f *frontier) visitedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.visited)
}

// state is the JSON shape persisted for resume. A file written before Pending
// existed still loads; it simply resumes with nothing left to do, which is what
// that run recorded.
type state struct {
	Visited []string      `json:"visited"`
	Pending []pendingPage `json:"pending,omitempty"`
}

// load reads a previously saved visited set and the frontier that was still
// outstanding; a missing file is not an error.
func (f *frontier) load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range s.Visited {
		f.visited[v] = true
		f.seen[v] = true
	}
	// Unfinished pages are held for the cloner to re-queue. They are deliberately
	// not marked seen and not put in pending here, because offer() is what does
	// both, keyed the same way as every other entry. A page the cloner then turns
	// down, which only happens when --max-depth is lower than it was, drops out of
	// the frontier; that follows the narrower scope the user just asked for.
	f.resumed = s.Pending
	return nil
}

// save writes the visited set and the unfinished frontier atomically (write
// temp, rename).
func (f *frontier) save(path string) error {
	f.mu.Lock()
	visited := make([]string, 0, len(f.visited))
	for v := range f.visited {
		visited = append(visited, v)
	}
	pending := make([]pendingPage, 0, len(f.pending))
	for _, p := range f.pending {
		pending = append(pending, p)
	}
	f.mu.Unlock()
	sort.Strings(visited)
	sort.Slice(pending, func(i, j int) bool { return pending[i].URL < pending[j].URL })

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state{Visited: visited, Pending: pending}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
