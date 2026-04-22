package proxy

import (
	"testing"

	"go.lsp.dev/protocol"
)

func TestResolveCachePutGetInvalidate(t *testing.T) {
	c := newResolveCache()
	uri := protocol.DocumentURI("file:///a.go")
	key := locCacheKey{Line: 1, Character: 2, Kind: queryReferences}
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
	refs := locCacheKey{Line: 1, Character: 2, Kind: queryReferences}
	impls := locCacheKey{Line: 1, Character: 2, Kind: queryImplementations}

	c.put(uri, refs, []protocol.Location{{URI: "ref"}})
	c.put(uri, impls, []protocol.Location{{URI: "imp1"}, {URI: "imp2"}})

	r, _ := c.get(uri, refs)
	i, _ := c.get(uri, impls)
	if len(r) != 1 || len(i) != 2 {
		t.Fatalf("mixed entries: refs=%d impls=%d", len(r), len(i))
	}
}
