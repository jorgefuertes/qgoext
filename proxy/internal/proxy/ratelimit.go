package proxy

import (
	"context"
	"sync"

	"go.lsp.dev/protocol"
	"golang.org/x/sync/semaphore"
)

// maxConcurrentRefs bounds the number of in-flight references /
// implementations sub-requests this proxy issues to gopls at once. Gopls
// handles these serially per document internally, so flooding it only
// stretches end-to-end latency; 4 is a safe ceiling that keeps interactive
// work snappy while still parallelising across files.
const maxConcurrentRefs = 4

// refThrottle caps concurrent outbound references and implementations
// requests. It is acquired inside fetchReferences / fetchImplementations.
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

// queryKind distinguishes a references query from an implementations query
// inside the location cache.
type queryKind uint8

const (
	queryReferences queryKind = iota
	queryImplementations
)

// resolveCache memoises location lists keyed by (uri, line, char, kind) so
// repeated hovers over the same symbol do not re-hit gopls. Entries are
// dropped per-uri on didChange.
type resolveCache struct {
	mu      sync.Mutex
	entries map[protocol.DocumentURI]map[locCacheKey][]protocol.Location
}

type locCacheKey struct {
	Line      uint32
	Character uint32
	Kind      queryKind
}

func newResolveCache() *resolveCache {
	return &resolveCache{entries: make(map[protocol.DocumentURI]map[locCacheKey][]protocol.Location)}
}

func (c *resolveCache) get(uri protocol.DocumentURI, k locCacheKey) ([]protocol.Location, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	locs, ok := c.entries[uri][k]
	return locs, ok
}

func (c *resolveCache) put(uri protocol.DocumentURI, k locCacheKey, locs []protocol.Location) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.entries[uri]
	if m == nil {
		m = make(map[locCacheKey][]protocol.Location)
		c.entries[uri] = m
	}
	m[k] = locs
}

func (c *resolveCache) invalidate(uri protocol.DocumentURI) {
	c.mu.Lock()
	delete(c.entries, uri)
	c.mu.Unlock()
}
