package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"
)

// hoverLinkLimit caps how many file links are inlined into the hover tail
// per category. Keeping this small keeps tooltips usable; navigation to
// unlisted entries can still be done with Zed's built-in find-all-references.
const hoverLinkLimit = 12

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
// content. The tail is empty when both queries failed or returned nothing.
//
// Layout: a one-line summary with bold counts, then up to hoverLinkLimit
// clickable links per category. The links use file:// URIs with the
// 1-indexed line number as the URL fragment, the only navigation form
// Zed's hover renderer recognises (see editor::hover_popover::open_markdown_url
// in zed-industries/zed).
func formatHoverTail(refs, impls []protocol.Location) string {
	hasRefs := refs != nil
	hasImpls := impls != nil
	if !hasRefs && !hasImpls {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n---\n")
	parts := []string{}
	if hasRefs {
		parts = append(parts, fmt.Sprintf("**%s**", countLabel(len(refs), "reference")))
	}
	if hasImpls && len(impls) > 0 {
		parts = append(parts, fmt.Sprintf("**%s**", countLabel(len(impls), "implementation")))
	}
	b.WriteString(strings.Join(parts, " · "))

	if hasRefs && len(refs) > 0 {
		b.WriteString("\n\n**References**\n")
		writeLocationList(&b, refs)
	}
	if hasImpls && len(impls) > 0 {
		b.WriteString("\n\n**Implementations**\n")
		writeLocationList(&b, impls)
	}
	return b.String()
}

func writeLocationList(b *strings.Builder, locs []protocol.Location) {
	limit := hoverLinkLimit
	if len(locs) < limit {
		limit = len(locs)
	}
	for i := 0; i < limit; i++ {
		loc := locs[i]
		path := filepath.FromSlash(loc.URI.Filename())
		display := fmt.Sprintf("%s:%d", filepath.Base(path), loc.Range.Start.Line+1)
		link := buildFileLink(string(loc.URI), loc.Range.Start.Line+1)
		fmt.Fprintf(b, "- [%s](%s)\n", display, link)
	}
	if extra := len(locs) - limit; extra > 0 {
		fmt.Fprintf(b, "- _and %d more_\n", extra)
	}
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

