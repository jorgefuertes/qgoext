package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.lsp.dev/protocol"
)

// enrichBudget bounds how long we wait for the parallel reference and
// implementation sub-requests. The base hover is not bound by this; gopls
// decides its own deadline.
const enrichBudget = 500 * time.Millisecond

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

// enrichHoverPayload takes the raw Result bytes from gopls' hover response
// and returns a new Result with an appended counts line. If the base
// response is null or undecodable the original bytes are returned unchanged
// so the editor still sees something useful.
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

	refs := p.referenceCount(subCtx, params.TextDocument.URI, params.Position)
	impls := p.implementationCount(subCtx, params.TextDocument.URI, params.Position)

	tail := formatCountsLine(refs, impls)
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

// formatCountsLine renders the tail markdown. Returns "" when neither piece
// of information is available (treat as "nothing to add").
func formatCountsLine(refs, impls int) string {
	if refs < 0 && impls < 0 {
		return ""
	}
	line := "\n\n---\n"
	switch {
	case refs < 0:
		// refs query failed; only show implementations
		line += fmt.Sprintf("**%s**", countLabel(impls, "implementation"))
	case impls <= 0:
		line += fmt.Sprintf("**%s**", countLabel(refs, "reference"))
	default:
		line += fmt.Sprintf("**%s** · **%s**",
			countLabel(refs, "reference"),
			countLabel(impls, "implementation"))
	}
	return line
}

func countLabel(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// referenceCount asks gopls for references at (uri, pos) excluding the
// declaration itself and returns the count. Returns -1 on error/timeout so
// the caller can distinguish "unknown" from "none". Shares the resolve
// cache with code-lens resolution so repeated queries are free.
func (p *Proxy) referenceCount(ctx context.Context, uri protocol.DocumentURI, pos protocol.Position) int {
	locs := p.fetchReferences(ctx, uri, pos)
	if locs == nil {
		return -1
	}
	return len(locs)
}

// implementationCount mirrors referenceCount for textDocument/implementation.
// Returns -1 on error and 0 when gopls replies with an empty set (which is
// the common case on positions that are not interface-related).
func (p *Proxy) implementationCount(ctx context.Context, uri protocol.DocumentURI, pos protocol.Position) int {
	locs := p.fetchImplementations(ctx, uri, pos)
	if locs == nil {
		return -1
	}
	return len(locs)
}
