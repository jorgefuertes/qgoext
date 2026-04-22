package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"
)

// enrichBudget bounds how long we wait for the parallel reference and
// implementation sub-requests. The base hover is not bound by this; gopls
// decides its own deadline. References can be slow on large projects, so
// the budget is generous; refs and impls run concurrently and share it.
const enrichBudget = 3 * time.Second

// handleHover takes a request the editor sent to us, obtains the base
// hover from gopls, augments its markdown with reference / implementation
// counts and sends the result back to the editor under the editor's
// original ID. Runs in its own goroutine; never blocks the pump loop.
func (p *Proxy) handleHover(ctx context.Context, req *message) {
	var params protocol.HoverParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		p.sendErrorResponse(req.ID, -32602, fmt.Sprintf("invalid hover params: %v", err))
		return
	}

	baseResp, err := p.call(ctx, protocol.MethodTextDocumentHover, params)
	if err != nil {
		p.sendErrorResponse(req.ID, -32603, fmt.Sprintf("hover upstream: %v", err))
		return
	}

	enriched := enrichHoverPayload(ctx, p, &params, baseResp.Result)
	p.sendResultResponse(req.ID, enriched)
}

func enrichHoverPayload(
	ctx context.Context,
	p *Proxy,
	params *protocol.HoverParams,
	baseResult json.RawMessage,
) json.RawMessage {
	if len(baseResult) == 0 || string(baseResult) == "null" {
		return baseResult
	}
	var base protocol.Hover
	if err := json.Unmarshal(baseResult, &base); err != nil {
		return baseResult
	}

	subCtx, cancel := context.WithTimeout(ctx, enrichBudget)
	defer cancel()

	var refLocs, implLocs []protocol.Location
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		refLocs = p.fetchReferences(subCtx, params.TextDocument.URI, params.Position)
	}()
	go func() {
		defer wg.Done()
		implLocs = p.fetchImplementations(subCtx, params.TextDocument.URI, params.Position)
	}()
	wg.Wait()

	tail := formatHoverTail(refLocs, implLocs)
	if tail == "" {
		return baseResult
	}

	if base.Contents.Kind == "" {
		base.Contents.Kind = protocol.Markdown
	}
	base.Contents.Value += tail
	out, err := json.Marshal(base)
	if err != nil {
		return baseResult
	}
	return out
}

// formatHoverTail renders the markdown appended after the gopls hover
// content: a single line with the reference and implementation counts.
// Each count is wrapped in a markdown link pointing to the first location
// of its kind (the only form of navigation Zed's hover renderer supports —
// file:// URLs with a line fragment, via editor::hover_popover::open_markdown_url).
//
// If a category's query failed or returned no locations, that slot
// degrades to plain bold text without a link.
func formatHoverTail(refs, impls []protocol.Location) string {
	hasRefs := refs != nil
	hasImpls := impls != nil
	if !hasRefs && !hasImpls {
		return ""
	}

	var parts []string
	if hasRefs {
		parts = append(parts, formatCountLink(refs, "reference"))
	}
	if hasImpls && len(impls) > 0 {
		parts = append(parts, formatCountLink(impls, "implementation"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n---\n" + strings.Join(parts, " · ")
}

// formatCountLink returns the bold count, wrapped in a link to the first
// location when at least one exists; otherwise plain bold text.
func formatCountLink(locs []protocol.Location, noun string) string {
	label := countLabel(len(locs), noun)
	if len(locs) == 0 {
		return fmt.Sprintf("**%s**", label)
	}
	first := locs[0]
	link := buildFileLink(string(first.URI), first.Range.Start.Line+1)
	return fmt.Sprintf("[**%s**](%s)", label, link)
}

// buildFileLink returns a file:// URL with the 1-indexed line number as
// the fragment. If the input uri is malformed it is returned unchanged.
func buildFileLink(uri string, line uint32) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	u.Fragment = fmt.Sprintf("%d", line)
	return u.String()
}

func countLabel(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// fetchReferences returns locations for the references at (uri, pos),
// excluding the declaration. Results are memoised per (uri, pos) until the
// file is edited. Nil signals "unknown" (error, timeout, or throttle
// cancellation); an empty slice means "zero". Access is gated by the shared
// semaphore to keep gopls responsive under burst loads.
func (p *Proxy) fetchReferences(ctx context.Context, uri protocol.DocumentURI, pos protocol.Position) []protocol.Location {
	key := locCacheKey{Line: pos.Line, Character: pos.Character, Kind: queryReferences}
	if locs, ok := p.resolved.get(uri, key); ok {
		return locs
	}
	release, err := p.throttle.acquire(ctx)
	if err != nil {
		return nil
	}
	defer release()

	params := protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: false},
	}
	resp, err := p.call(ctx, protocol.MethodTextDocumentReferences, params)
	if err != nil || len(resp.Error) > 0 {
		return nil
	}
	locs := []protocol.Location{}
	if len(resp.Result) > 0 && string(resp.Result) != "null" {
		if err := json.Unmarshal(resp.Result, &locs); err != nil {
			return nil
		}
	}
	p.resolved.put(uri, key, locs)
	return locs
}

// fetchImplementations mirrors fetchReferences for
// textDocument/implementation. Same caching and throttle apply.
func (p *Proxy) fetchImplementations(ctx context.Context, uri protocol.DocumentURI, pos protocol.Position) []protocol.Location {
	key := locCacheKey{Line: pos.Line, Character: pos.Character, Kind: queryImplementations}
	if locs, ok := p.resolved.get(uri, key); ok {
		return locs
	}
	release, err := p.throttle.acquire(ctx)
	if err != nil {
		return nil
	}
	defer release()

	params := protocol.ImplementationParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
	}
	resp, err := p.call(ctx, protocol.MethodTextDocumentImplementation, params)
	if err != nil || len(resp.Error) > 0 {
		return nil
	}
	if len(resp.Result) == 0 || string(resp.Result) == "null" {
		p.resolved.put(uri, key, []protocol.Location{})
		return []protocol.Location{}
	}
	var locs []protocol.Location
	if err := json.Unmarshal(resp.Result, &locs); err == nil {
		p.resolved.put(uri, key, locs)
		return locs
	}
	var single protocol.Location
	if err := json.Unmarshal(resp.Result, &single); err == nil {
		out := []protocol.Location{single}
		p.resolved.put(uri, key, out)
		return out
	}
	return nil
}
