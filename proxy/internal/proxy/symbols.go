package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.lsp.dev/protocol"
)

// symbolsCache stores the document-symbol tree per open file. Entries are
// invalidated on textDocument/didChange; next access re-fetches from gopls.
type symbolsCache struct {
	mu      sync.Mutex
	entries map[protocol.DocumentURI][]protocol.DocumentSymbol
	inFlight map[protocol.DocumentURI]chan struct{}
}

func newSymbolsCache() *symbolsCache {
	return &symbolsCache{
		entries:  make(map[protocol.DocumentURI][]protocol.DocumentSymbol),
		inFlight: make(map[protocol.DocumentURI]chan struct{}),
	}
}

func (c *symbolsCache) invalidate(uri protocol.DocumentURI) {
	c.mu.Lock()
	delete(c.entries, uri)
	c.mu.Unlock()
}

// symbolsFor returns the cached document-symbol tree for uri, fetching from
// gopls on miss. Concurrent callers for the same uri coalesce on a single
// in-flight request.
func (p *Proxy) symbolsFor(ctx context.Context, uri protocol.DocumentURI) ([]protocol.DocumentSymbol, error) {
	c := p.symbols

	c.mu.Lock()
	if syms, ok := c.entries[uri]; ok {
		c.mu.Unlock()
		return syms, nil
	}
	if wait, ok := c.inFlight[uri]; ok {
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		syms := c.entries[uri]
		c.mu.Unlock()
		return syms, nil
	}
	done := make(chan struct{})
	c.inFlight[uri] = done
	c.mu.Unlock()

	syms, err := p.fetchSymbols(ctx, uri)

	c.mu.Lock()
	delete(c.inFlight, uri)
	if err == nil {
		c.entries[uri] = syms
	}
	c.mu.Unlock()
	close(done)

	return syms, err
}

func (p *Proxy) fetchSymbols(ctx context.Context, uri protocol.DocumentURI) ([]protocol.DocumentSymbol, error) {
	params := protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	resp, err := p.call(ctx, protocol.MethodTextDocumentDocumentSymbol, params)
	if err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, fmt.Errorf("gopls error: %s", string(resp.Error))
	}
	// gopls returns DocumentSymbol[] (hierarchical). Older servers could
	// return SymbolInformation[] (flat), but gopls is consistent on the
	// newer shape, so we only decode that.
	var syms []protocol.DocumentSymbol
	if len(resp.Result) == 0 || string(resp.Result) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(resp.Result, &syms); err != nil {
		return nil, fmt.Errorf("decode documentSymbol: %w", err)
	}
	return syms, nil
}

