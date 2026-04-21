package proxy

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestWriteReadFrameRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"textDocument/hover"}`)
	if err := writeFrame(&buf, body); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	got, err := readFrame(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("roundtrip mismatch\nwant %s\n got %s", body, got)
	}
}

func TestReadFrameIgnoresContentType(t *testing.T) {
	raw := "Content-Type: application/vscode-jsonrpc; charset=utf-8\r\n" +
		"Content-Length: 2\r\n" +
		"\r\n" +
		"{}"
	got, err := readFrame(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if string(got) != "{}" {
		t.Fatalf("got %q want %q", got, "{}")
	}
}

func TestReadFrameMissingLength(t *testing.T) {
	raw := "Content-Type: application/vscode-jsonrpc\r\n\r\n{}"
	_, err := readFrame(bufio.NewReader(strings.NewReader(raw)))
	if err == nil {
		t.Fatal("expected error for missing Content-Length")
	}
}

func TestReadFrameRejectsMalformedHeader(t *testing.T) {
	raw := "bogus-line-no-colon\r\nContent-Length: 2\r\n\r\n{}"
	_, err := readFrame(bufio.NewReader(strings.NewReader(raw)))
	if err == nil {
		t.Fatal("expected error for malformed header")
	}
}
