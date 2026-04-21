package proxy

import (
	"encoding/json"

	"go.lsp.dev/protocol"
)

// observeFromEditor peeks at frames flowing from the editor to gopls and
// reacts to those that change proxy state. The frame is still forwarded by
// the caller; we only look, never mutate.
func (p *Proxy) observeFromEditor(body []byte) {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return
	}
	if !m.isNotification() {
		return
	}
	switch m.Method {
	case protocol.MethodTextDocumentDidOpen:
		var params protocol.DidOpenTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err == nil {
			p.schedulePrewarm(params.TextDocument.URI)
		}
	case protocol.MethodTextDocumentDidChange:
		var params protocol.DidChangeTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err == nil {
			p.symbols.invalidate(params.TextDocument.URI)
			p.resolved.invalidate(params.TextDocument.URI)
			p.schedulePrewarm(params.TextDocument.URI)
		}
	case protocol.MethodTextDocumentDidClose:
		var params protocol.DidCloseTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err == nil {
			p.symbols.invalidate(params.TextDocument.URI)
			p.resolved.invalidate(params.TextDocument.URI)
		}
	}
}

// schedulePrewarm asks the debouncer to refill the symbol cache for uri
// once editing settles. Uses the long-lived background context so the
// refill is not tied to any single editor request.
func (p *Proxy) schedulePrewarm(uri protocol.DocumentURI) {
	if p.bgCtx == nil {
		return
	}
	p.prewarm.schedule(uri, func() {
		_, _ = p.symbolsFor(p.bgCtx, uri)
	})
}
