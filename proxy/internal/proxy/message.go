package proxy

import "encoding/json"

// message is the wire-level shape of an LSP / JSON-RPC message.
//
// Both requests and responses (and notifications) are accepted; fields are
// optional depending on kind. We keep params/result/error as raw bytes so
// the proxy can forward them opaquely when it does not need to look inside.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func (m *message) isRequest() bool      { return m.Method != "" && len(m.ID) > 0 }
func (m *message) isNotification() bool { return m.Method != "" && len(m.ID) == 0 }
func (m *message) isResponse() bool     { return m.Method == "" && len(m.ID) > 0 }
