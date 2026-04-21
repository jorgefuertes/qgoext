package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.lsp.dev/protocol"
)

// qgoextSource tags code-lens Data objects we own, so codeLens/resolve can
// tell ours apart from those gopls originated. Gopls' own lens data is
// never touched and flows through unchanged.
const qgoextSource = "qgoext"

// lensKind is the discriminator between the two types of lens this proxy
// emits for a symbol declaration.
type lensKind string

const (
	lensKindReferences     lensKind = "references"
	lensKindImplementations lensKind = "implementations"
)

// ourLensData is serialised into CodeLens.Data so that codeLens/resolve can
// reconstruct the query it must make against gopls. Fields are flat JSON
// (not nested) to keep the wire shape compact.
type ourLensData struct {
	Source string   `json:"qgoext_source"`
	Kind   lensKind `json:"kind"`
	URI    protocol.DocumentURI `json:"uri"`
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

// handleCodeLens answers a textDocument/codeLens request. It fetches the
// lenses gopls natively provides and appends our synthesised ones, all
// still unresolved (no command yet). Zed will call codeLens/resolve on each
// to get the title and navigation payload.
func (p *Proxy) handleCodeLens(ctx context.Context, req *message) {
	var params protocol.CodeLensParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		p.sendErrorResponse(req.ID, -32602, fmt.Sprintf("invalid codeLens params: %v", err))
		return
	}

	// Fetch gopls' own lenses first; failure is not fatal, we still emit ours.
	var upstream []protocol.CodeLens
	if resp, err := p.call(ctx, protocol.MethodTextDocumentCodeLens, params); err == nil && len(resp.Error) == 0 {
		_ = json.Unmarshal(resp.Result, &upstream)
	}

	syms, err := p.symbolsFor(ctx, params.TextDocument.URI)
	if err != nil {
		// Still return whatever upstream gave us.
		p.sendResultResponse(req.ID, upstream)
		return
	}

	synth := synthesizeLenses(params.TextDocument.URI, syms)
	resolved := p.resolveAllInParallel(ctx, synth)
	p.sendResultResponse(req.ID, append(upstream, resolved...))
}

// resolveAllInParallel turns a list of unresolved lenses into resolved ones
// by issuing references / implementations in parallel, throttled by the
// shared semaphore. Zed renders the actions menu using lens.command.title,
// so unresolved lenses (command == nil) display as "Unknown command";
// resolving up-front is required for correct titles.
func (p *Proxy) resolveAllInParallel(ctx context.Context, lenses []protocol.CodeLens) []protocol.CodeLens {
	out := make([]protocol.CodeLens, len(lenses))
	var wg sync.WaitGroup
	for i := range lenses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, ok := parseOurLensData(lenses[i].Data)
			if !ok {
				out[i] = lenses[i]
				return
			}
			lens := lenses[i]
			resolved, err := p.resolveOurLens(ctx, &lens, data)
			if err != nil {
				out[i] = lenses[i]
				return
			}
			out[i] = *resolved
		}()
	}
	wg.Wait()
	return out
}

// synthesizeLenses walks the symbol tree and emits one or two unresolved
// CodeLens per eligible declaration. "Eligible" means top-level or one
// level deep under a struct/interface, and of a kind where counts are
// meaningful for the MVP: Function, Method, Struct, Interface, Constant,
// Variable.
func synthesizeLenses(uri protocol.DocumentURI, syms []protocol.DocumentSymbol) []protocol.CodeLens {
	var out []protocol.CodeLens
	var walk func(path []protocol.DocumentSymbol)
	walk = func(path []protocol.DocumentSymbol) {
		for i := range path[len(path)-1].Children {
			child := path[len(path)-1].Children[i]
			newPath := append(append([]protocol.DocumentSymbol{}, path...), child)
			if lensWorthy(newPath) {
				out = append(out, buildLens(uri, child, lensKindReferences))
				if wantsImplementations(newPath) {
					out = append(out, buildLens(uri, child, lensKindImplementations))
				}
			}
			walk(newPath)
		}
	}
	// Seed with synthetic roots wrapping each top-level symbol.
	for i := range syms {
		s := syms[i]
		if lensWorthy([]protocol.DocumentSymbol{s}) {
			out = append(out, buildLens(uri, s, lensKindReferences))
			if wantsImplementations([]protocol.DocumentSymbol{s}) {
				out = append(out, buildLens(uri, s, lensKindImplementations))
			}
		}
		walk([]protocol.DocumentSymbol{s})
	}
	return out
}

// lensWorthy decides whether a symbol in the tree should get a reference
// lens. Only kinds where a count is actually useful are allowed, and only
// at depths where the count is legible (top-level or directly nested in a
// struct/interface).
func lensWorthy(path []protocol.DocumentSymbol) bool {
	if len(path) == 0 {
		return false
	}
	leaf := path[len(path)-1]
	switch leaf.Kind {
	case protocol.SymbolKindFunction, protocol.SymbolKindStruct,
		protocol.SymbolKindInterface, protocol.SymbolKindConstant,
		protocol.SymbolKindVariable:
		return len(path) == 1
	case protocol.SymbolKindMethod, protocol.SymbolKindField:
		if len(path) < 2 {
			return false
		}
		parent := path[len(path)-2].Kind
		return parent == protocol.SymbolKindStruct || parent == protocol.SymbolKindInterface
	}
	return false
}

// wantsImplementations returns true when an "implementations" lens is
// meaningful at this symbol: the symbol is an interface type, or a method
// directly owned by an interface.
func wantsImplementations(path []protocol.DocumentSymbol) bool {
	if len(path) == 0 {
		return false
	}
	leaf := path[len(path)-1]
	if leaf.Kind == protocol.SymbolKindInterface {
		return true
	}
	if leaf.Kind == protocol.SymbolKindMethod && len(path) >= 2 &&
		path[len(path)-2].Kind == protocol.SymbolKindInterface {
		return true
	}
	// For struct types, implementation requests return the interfaces the
	// struct satisfies — also useful, but we skip it in MVP to avoid noisy
	// results on every struct declaration.
	return false
}

func buildLens(uri protocol.DocumentURI, s protocol.DocumentSymbol, kind lensKind) protocol.CodeLens {
	data := ourLensData{
		Source:    qgoextSource,
		Kind:      kind,
		URI:       uri,
		Line:      s.SelectionRange.Start.Line,
		Character: s.SelectionRange.Start.Character,
	}
	return protocol.CodeLens{
		Range: s.SelectionRange,
		Data:  data,
	}
}

// handleCodeLensResolve completes a code lens: for ours, it queries gopls
// for references or implementations and fills in the navigation command.
// For gopls-originated lenses it forwards the resolve verbatim.
func (p *Proxy) handleCodeLensResolve(ctx context.Context, req *message) {
	var lens protocol.CodeLens
	if err := json.Unmarshal(req.Params, &lens); err != nil {
		p.sendErrorResponse(req.ID, -32602, fmt.Sprintf("invalid codeLens resolve params: %v", err))
		return
	}

	data, ok := parseOurLensData(lens.Data)
	if !ok {
		// Not ours — forward to gopls verbatim.
		resp, err := p.call(ctx, protocol.MethodCodeLensResolve, lens)
		if err != nil {
			p.sendErrorResponse(req.ID, -32603, err.Error())
			return
		}
		p.sendEditorRaw(req.ID, resp)
		return
	}

	resolved, err := p.resolveOurLens(ctx, &lens, data)
	if err != nil {
		p.sendErrorResponse(req.ID, -32603, err.Error())
		return
	}
	p.sendResultResponse(req.ID, resolved)
}

func parseOurLensData(raw any) (ourLensData, bool) {
	var out ourLensData
	if raw == nil {
		return out, false
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return out, false
	}
	if err := json.Unmarshal(buf, &out); err != nil {
		return out, false
	}
	if out.Source != qgoextSource {
		return out, false
	}
	return out, true
}

func (p *Proxy) resolveOurLens(ctx context.Context, lens *protocol.CodeLens, data ourLensData) (*protocol.CodeLens, error) {
	pos := protocol.Position{Line: data.Line, Character: data.Character}

	var locations []protocol.Location
	switch data.Kind {
	case lensKindReferences:
		locations = p.fetchReferences(ctx, data.URI, pos)
	case lensKindImplementations:
		locations = p.fetchImplementations(ctx, data.URI, pos)
	default:
		return nil, fmt.Errorf("unknown lens kind %q", data.Kind)
	}

	noun := "reference"
	cmdName := qgoextShowReferences
	if data.Kind == lensKindImplementations {
		noun = "implementation"
		cmdName = qgoextShowImplementations
	}
	lens.Command = &protocol.Command{
		Title:   countLabel(len(locations), noun),
		Command: cmdName,
		Arguments: []any{
			data.URI,
			pos,
			locations,
		},
	}
	return lens, nil
}

// fetchReferences returns locations for the references at (uri, pos),
// excluding the declaration. Results are memoised per (uri, pos) until the
// file is edited. Nil signals "unknown" (error, timeout, or throttle
// cancellation); an empty slice means "zero". Access is gated by the shared
// semaphore to keep gopls responsive under burst loads.
func (p *Proxy) fetchReferences(ctx context.Context, uri protocol.DocumentURI, pos protocol.Position) []protocol.Location {
	key := lensCacheKey{Line: pos.Line, Character: pos.Character, Kind: lensKindReferences}
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
	key := lensCacheKey{Line: pos.Line, Character: pos.Character, Kind: lensKindImplementations}
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

// sendEditorRaw forwards a gopls response back to the editor verbatim,
// rewriting the ID to the one the editor is waiting on. Used when we
// passed a resolve through and need to hand its body back.
func (p *Proxy) sendEditorRaw(editorID json.RawMessage, resp *message) {
	body, err := json.Marshal(message{
		JSONRPC: "2.0",
		ID:      editorID,
		Result:  resp.Result,
		Error:   resp.Error,
	})
	if err != nil {
		return
	}
	_ = p.sendToEditor(body)
}
