package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

// proxyIDPrefix is the string prefix used on JSON-RPC IDs for requests the
// proxy itself originates (as opposed to those it merely forwards from the
// editor). Editors use integer IDs; using a string namespace guarantees we
// never collide with the editor's ID space on the gopls connection.
const proxyIDPrefix = "qgoext-"

// Proxy brokers LSP traffic between an editor (over stdio) and a real gopls
// child process (also over stdio). The default behaviour is to blind-forward
// every message in both directions. Later layers can install interceptors
// for specific methods.
type Proxy struct {
	// editor side (bound to os.Stdin / os.Stdout).
	editorIn  *bufio.Reader
	editorOut io.Writer

	// gopls side (bound to the child's stdout / stdin).
	goplsIn  *bufio.Reader
	goplsOut io.Writer

	// Serialization of outbound writes. Each stream needs a dedicated mutex
	// because multiple goroutines may write to it (forwarded frames and our
	// own originated messages share the gopls pipe).
	editorWriteMu sync.Mutex
	goplsWriteMu  sync.Mutex

	// Pending sub-requests the proxy itself sent to gopls, keyed by our own
	// string-namespaced ID. The receiver goroutine delivers the raw response
	// on the channel and deletes the entry.
	pendingMu sync.Mutex
	pending   map[string]chan *message

	// Monotonic counter for proxy-originated IDs.
	nextID atomic.Uint64

	symbols  *symbolsCache
	throttle *refThrottle
	resolved *resolveCache
	prewarm  *prewarmer

	// Parent context for out-of-band workers (prewarm, cache fills). Set by
	// Run; nil before that.
	bgCtx context.Context
}

// New builds a Proxy wired to the four streams. Construction does not
// perform any IO; call Run to start the forwarding loops.
func New(editorIn io.Reader, editorOut io.Writer, goplsIn io.Reader, goplsOut io.Writer) *Proxy {
	return &Proxy{
		editorIn:  bufio.NewReader(editorIn),
		editorOut: editorOut,
		goplsIn:   bufio.NewReader(goplsIn),
		goplsOut:  goplsOut,
		pending:   make(map[string]chan *message),
		symbols:   newSymbolsCache(),
		throttle:  newRefThrottle(),
		resolved:  newResolveCache(),
		prewarm:   newPrewarmer(),
	}
}

// Run blocks until either side closes the connection or ctx is cancelled.
// Both forwarding loops are supervised in parallel; whichever returns first
// tears the proxy down.
func (p *Proxy) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p.bgCtx = ctx

	errCh := make(chan error, 2)
	go func() { errCh <- p.pumpEditorToGopls(ctx) }()
	go func() { errCh <- p.pumpGoplsToEditor(ctx) }()

	select {
	case err := <-errCh:
		cancel()
		// drain the second to avoid leaks
		<-errCh
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pumpEditorToGopls reads frames from the editor and forwards them to gopls.
// Before forwarding, it peeks at notifications that affect proxy state
// (didChange, didClose) so we can invalidate caches.
func (p *Proxy) pumpEditorToGopls(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		body, err := readFrame(p.editorIn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("editor read: %w", err)
		}
		p.observeFromEditor(body)
		if p.interceptFromEditor(ctx, body) {
			continue
		}
		if err := p.sendToGopls(body); err != nil {
			return fmt.Errorf("forward to gopls: %w", err)
		}
	}
}

// pumpGoplsToEditor reads frames from gopls and forwards them to the editor,
// with one exception: responses to IDs this proxy originated are routed to
// the matching pending channel and never reach the editor.
func (p *Proxy) pumpGoplsToEditor(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		body, err := readFrame(p.goplsIn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("gopls read: %w", err)
		}
		// Peek at the ID. If it is one of ours, swallow and dispatch.
		var peek message
		if err := json.Unmarshal(body, &peek); err == nil && peek.isResponse() {
			if id, ok := stringID(peek.ID); ok && p.takePending(id, &peek) {
				continue
			}
		}
		if err := p.sendToEditor(body); err != nil {
			return fmt.Errorf("forward to editor: %w", err)
		}
	}
}

func (p *Proxy) sendToEditor(body []byte) error {
	p.editorWriteMu.Lock()
	defer p.editorWriteMu.Unlock()
	return writeFrame(p.editorOut, body)
}

func (p *Proxy) sendToGopls(body []byte) error {
	p.goplsWriteMu.Lock()
	defer p.goplsWriteMu.Unlock()
	return writeFrame(p.goplsOut, body)
}

// call sends a request originated by the proxy itself to gopls and returns
// the raw response when it arrives or ctx is cancelled.
func (p *Proxy) call(ctx context.Context, method string, params any) (*message, error) {
	id := proxyIDPrefix + strconv.FormatUint(p.nextID.Add(1), 10)
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	idJSON, _ := json.Marshal(id)
	body, err := json.Marshal(message{
		JSONRPC: "2.0",
		ID:      idJSON,
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ch := make(chan *message, 1)
	p.pendingMu.Lock()
	p.pending[id] = ch
	p.pendingMu.Unlock()

	if err := p.sendToGopls(body); err != nil {
		p.forgetPending(id)
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		p.forgetPending(id)
		return nil, ctx.Err()
	}
}

func (p *Proxy) takePending(id string, resp *message) bool {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	ch, ok := p.pending[id]
	if !ok {
		return false
	}
	delete(p.pending, id)
	ch <- resp
	return true
}

func (p *Proxy) forgetPending(id string) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	delete(p.pending, id)
}

// sendResultResponse sends a success response back to the editor, preserving
// the original request ID. Non-fatal write errors are swallowed.
func (p *Proxy) sendResultResponse(id json.RawMessage, result any) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		p.sendErrorResponse(id, -32603, fmt.Sprintf("marshal result: %v", err))
		return
	}
	body, err := json.Marshal(message{JSONRPC: "2.0", ID: id, Result: resultJSON})
	if err != nil {
		return
	}
	_ = p.sendToEditor(body)
}

// sendErrorResponse sends a JSON-RPC error response back to the editor.
func (p *Proxy) sendErrorResponse(id json.RawMessage, code int, msg string) {
	errObj := struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: msg}
	errJSON, _ := json.Marshal(errObj)
	body, err := json.Marshal(message{JSONRPC: "2.0", ID: id, Error: errJSON})
	if err != nil {
		return
	}
	_ = p.sendToEditor(body)
}

// stringID returns the string form of a JSON-RPC ID if it is a string, along
// with ok=true. Integer IDs are rejected (ok=false) because proxy-originated
// IDs are always strings under proxyIDPrefix.
func stringID(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}
