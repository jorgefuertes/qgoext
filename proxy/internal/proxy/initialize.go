package proxy

import (
	"bytes"
	"encoding/json"
	"sync/atomic"
)

// qgoextShowReferences and qgoextShowImplementations are the executeCommand
// names this proxy advertises to Zed via the modified initialize response,
// and the same names referenced by the synthesised CodeLens entries.
//
// Zed filters out CodeLens entries whose command is not present in
// executeCommandProvider.commands of the originating server (see
// crates/project/src/lsp_command.rs in zed-industries/zed). Without this
// pair (advertising + handling), the lens entries would be silently
// dropped from the actions menu.
const (
	qgoextShowReferences      = "qgoext.show_references"
	qgoextShowImplementations = "qgoext.show_implementations"
)

// initializeRequestID stores the JSON id of the editor's initialize request
// so the response from gopls can be matched and modified.
type initializeRequestID struct {
	value atomic.Value
}

func (s *initializeRequestID) set(id json.RawMessage) {
	cp := append(json.RawMessage(nil), id...)
	s.value.Store(cp)
}

func (s *initializeRequestID) matches(id json.RawMessage) bool {
	stored, ok := s.value.Load().(json.RawMessage)
	if !ok || len(stored) == 0 {
		return false
	}
	return bytes.Equal(stored, id)
}

func (p *Proxy) rememberInitializeID(id json.RawMessage) {
	p.initID.set(id)
}

// rewriteInitializeResponse takes the raw body of gopls' response to the
// initialize request and returns a body with executeCommandProvider.commands
// extended to include our qgoext.* commands. If anything fails, the
// original body is returned untouched.
func rewriteInitializeResponse(body []byte) []byte {
	var top struct {
		JSONRPC string                 `json:"jsonrpc"`
		ID      json.RawMessage        `json:"id"`
		Result  map[string]any `json:"result,omitempty"`
		Error   json.RawMessage        `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return body
	}
	if top.Result == nil {
		return body
	}

	caps, _ := top.Result["capabilities"].(map[string]any)
	if caps == nil {
		caps = make(map[string]any)
		top.Result["capabilities"] = caps
	}

	provider, _ := caps["executeCommandProvider"].(map[string]any)
	if provider == nil {
		provider = make(map[string]any)
		caps["executeCommandProvider"] = provider
	}

	existing, _ := provider["commands"].([]any)
	have := make(map[string]bool, len(existing))
	for _, c := range existing {
		if s, ok := c.(string); ok {
			have[s] = true
		}
	}
	for _, c := range []string{qgoextShowReferences, qgoextShowImplementations} {
		if !have[c] {
			existing = append(existing, c)
			have[c] = true
		}
	}
	provider["commands"] = existing

	out, err := json.Marshal(top)
	if err != nil {
		return body
	}
	return out
}
