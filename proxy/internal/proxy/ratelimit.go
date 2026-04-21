package proxy

import (
	"context"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"golang.org/x/sync/semaphore"
)

// maxConcurrentRefs bounds the number of in-flight references /
// implementations sub-requests this proxy issues to gopls at once. Gopls
// handles these serially per document internally, so flooding it only
// stretches end-to-end latency; 4 is a safe ceiling that keeps interactive
// work snappy while still parallelising across files.
const maxConcurrentRefs = 4

// prewarmDebounce is how long we wait after a didChange before kicking off
// a refill of the document-symbol cache for the affected file. One edit in
// a burst of typing schedules one refill for the whole burst.
const prewarmDebounce = 750 * time.Millisecond

// refSem throttles the number of concurrent outbound references and
// implementations requests. It is acquired inside fetchReferences /
// fetchImplementations / referenceCount / implementationCount.
type refThrottle struct {
	sem *semaphore.Weighted
}

func newRefThrottle() *refThrottle {
	return &refThrottle{sem: semaphore.NewWeighted(maxConcurrentRefs)}
}

// acquire blocks until a slot is free or ctx is cancelled. The returned
// release function must always be called.
func (r *refThrottle) acquire(ctx context.Context) (release func(), err error) {
	if err := r.sem.Acquire(ctx, 1); err != nil {
		return func() {}, err
	}
	return func() { r.sem.Release(1) }, nil
}

// resolveCache memoises code-lens resolve results so repeated resolves from
// Zed (e.g. after scrolling away and back) do not re-hit gopls. Entries are
// dropped per-uri on didChange.
type resolveCache struct {
	mu      sync.Mutex
	entries map[protocol.DocumentURI]map[lensCacheKey][]protocol.Location
}

type lensCacheKey struct {
	Line      uint32
	Character uint32
	Kind      lensKind
}

func newResolveCache() *resolveCache {
	return &resolveCache{entries: make(map[protocol.DocumentURI]map[lensCacheKey][]protocol.Location)}
}

func (c *resolveCache) get(uri protocol.DocumentURI, k lensCacheKey) ([]protocol.Location, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	locs, ok := c.entries[uri][k]
	return locs, ok
}

func (c *resolveCache) put(uri protocol.DocumentURI, k lensCacheKey, locs []protocol.Location) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.entries[uri]
	if m == nil {
		m = make(map[lensCacheKey][]protocol.Location)
		c.entries[uri] = m
	}
	m[k] = locs
}

func (c *resolveCache) invalidate(uri protocol.DocumentURI) {
	c.mu.Lock()
	delete(c.entries, uri)
	c.mu.Unlock()
}

// prewarmer schedules symbol-cache refills after edits, coalescing bursts
// into a single post-debounce refill per uri.
type prewarmer struct {
	mu     sync.Mutex
	timers map[protocol.DocumentURI]*time.Timer
}

func newPrewarmer() *prewarmer {
	return &prewarmer{timers: make(map[protocol.DocumentURI]*time.Timer)}
}

// schedule asks for refill after prewarmDebounce of inactivity for uri.
// Subsequent calls for the same uri reset the timer.
func (w *prewarmer) schedule(uri protocol.DocumentURI, refill func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[uri]; ok {
		t.Stop()
	}
	w.timers[uri] = time.AfterFunc(prewarmDebounce, func() {
		w.mu.Lock()
		delete(w.timers, uri)
		w.mu.Unlock()
		refill()
	})
}
