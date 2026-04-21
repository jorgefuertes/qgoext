package proxy

import (
	"context"
	"encoding/json"

	"go.lsp.dev/protocol"
)

// interceptFromEditor looks at frames the editor sends and, if one is a
// request this proxy wants to own end-to-end, dispatches it to a handler
// goroutine and returns true. The caller must not forward the frame when
// this returns true.
//
// Blind-forwarding remains the default: any method we do not recognise
// falls through, including responses to server-to-client requests and any
// workspace-level commands.
func (p *Proxy) interceptFromEditor(ctx context.Context, body []byte) bool {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	if !m.isRequest() {
		return false
	}
	switch m.Method {
	case protocol.MethodTextDocumentHover:
		go p.handleHover(ctx, &m)
		return true
	case protocol.MethodTextDocumentCodeLens:
		go p.handleCodeLens(ctx, &m)
		return true
	case protocol.MethodCodeLensResolve:
		go p.handleCodeLensResolve(ctx, &m)
		return true
	}
	return false
}
