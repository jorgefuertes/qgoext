package proxy

import (
	"encoding/json"

	"go.lsp.dev/protocol"
)

// observeFromEditor peeks at frames flowing from the editor to gopls and
// invalidates per-document caches when edits land. The frame is still
// forwarded by the caller; we only look, never mutate.
func (p *Proxy) observeFromEditor(body []byte) {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return
	}
	if !m.isNotification() {
		return
	}
	switch m.Method {
	case protocol.MethodTextDocumentDidChange:
		var params protocol.DidChangeTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err == nil {
			p.resolved.invalidate(params.TextDocument.URI)
		}
	case protocol.MethodTextDocumentDidClose:
		var params protocol.DidCloseTextDocumentParams
		if err := json.Unmarshal(m.Params, &params); err == nil {
			p.resolved.invalidate(params.TextDocument.URI)
		}
	}
}
