package proxy

import (
	"sync"
	"testing"
	"time"

	"go.lsp.dev/protocol"
)

func TestResolveCachePutGetInvalidate(t *testing.T) {
	c := newResolveCache()
	uri := protocol.DocumentURI("file:///a.go")
	key := lensCacheKey{Line: 1, Character: 2, Kind: lensKindReferences}
	want := []protocol.Location{{URI: "file:///b.go"}}
	c.put(uri, key, want)

	got, ok := c.get(uri, key)
	if !ok || len(got) != 1 {
		t.Fatalf("want cached, got ok=%v locs=%+v", ok, got)
	}

	c.invalidate(uri)
	if _, ok := c.get(uri, key); ok {
		t.Fatal("invalidate did not drop entry")
	}
}

func TestResolveCacheSeparatesKinds(t *testing.T) {
	c := newResolveCache()
	uri := protocol.DocumentURI("file:///a.go")
	refs := lensCacheKey{Line: 1, Character: 2, Kind: lensKindReferences}
	impls := lensCacheKey{Line: 1, Character: 2, Kind: lensKindImplementations}

	c.put(uri, refs, []protocol.Location{{URI: "ref"}})
	c.put(uri, impls, []protocol.Location{{URI: "imp1"}, {URI: "imp2"}})

	r, _ := c.get(uri, refs)
	i, _ := c.get(uri, impls)
	if len(r) != 1 || len(i) != 2 {
		t.Fatalf("mixed entries: refs=%d impls=%d", len(r), len(i))
	}
}

func TestPrewarmerCoalesces(t *testing.T) {
	w := newPrewarmer()
	uri := protocol.DocumentURI("file:///a.go")

	var count int
	var mu sync.Mutex
	refill := func() {
		mu.Lock()
		count++
		mu.Unlock()
	}

	for i := 0; i < 5; i++ {
		w.schedule(uri, refill)
		time.Sleep(50 * time.Millisecond)
	}
	// One full debounce window after the last schedule call.
	time.Sleep(prewarmDebounce + 100*time.Millisecond)

	mu.Lock()
	got := count
	mu.Unlock()
	if got != 1 {
		t.Fatalf("want 1 refill, got %d", got)
	}
}
