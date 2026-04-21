package proxy

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestRewriteInitializeResponseAddsCommandsWhenMissing(t *testing.T) {
	input := []byte(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`)
	out := rewriteInitializeResponse(input)

	commands := extractCommands(t, out)
	wantAll := []string{qgoextShowReferences, qgoextShowImplementations}
	for _, w := range wantAll {
		if !slices.Contains(commands, w) {
			t.Errorf("missing command %q in %v", w, commands)
		}
	}
}

func TestRewriteInitializeResponsePreservesExisting(t *testing.T) {
	input := []byte(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"executeCommandProvider":{"commands":["gopls.run_tests","gopls.generate"]}}}}`)
	out := rewriteInitializeResponse(input)

	commands := extractCommands(t, out)
	wantAll := []string{"gopls.run_tests", "gopls.generate", qgoextShowReferences, qgoextShowImplementations}
	for _, w := range wantAll {
		if !slices.Contains(commands, w) {
			t.Errorf("missing command %q in %v", w, commands)
		}
	}
}

func TestRewriteInitializeResponseIdempotent(t *testing.T) {
	input := []byte(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`)
	out1 := rewriteInitializeResponse(input)
	out2 := rewriteInitializeResponse(out1)

	cmds1 := extractCommands(t, out1)
	cmds2 := extractCommands(t, out2)
	if len(cmds1) != len(cmds2) {
		t.Fatalf("re-applying duplicated commands: %v vs %v", cmds1, cmds2)
	}
}

func TestRewriteInitializeResponseErrorsUntouched(t *testing.T) {
	input := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`)
	out := rewriteInitializeResponse(input)
	if string(out) != string(input) {
		t.Fatalf("error body was modified:\n want %s\n  got %s", input, out)
	}
}

func extractCommands(t *testing.T, body []byte) []string {
	t.Helper()
	var parsed struct {
		Result struct {
			Capabilities struct {
				Provider struct {
					Commands []string `json:"commands"`
				} `json:"executeCommandProvider"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	return parsed.Result.Capabilities.Provider.Commands
}
