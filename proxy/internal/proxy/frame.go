package proxy

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// readFrame reads a single LSP-framed JSON payload from r.
// The LSP framing is: one or more "Header: value\r\n" lines terminated by an
// empty "\r\n" line, followed by exactly Content-Length bytes of body.
//
// Returns io.EOF when the peer closed cleanly with no pending frame.
func readFrame(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("malformed header line: %q", line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if strings.EqualFold(name, "Content-Length") {
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length %q: %w", value, err)
			}
			contentLength = n
		}
		// other headers (Content-Type, etc.) are ignored
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// writeFrame writes an LSP-framed JSON payload to w.
// The caller is responsible for holding any serialization lock.
func writeFrame(w io.Writer, body []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}
