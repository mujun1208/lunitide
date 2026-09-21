package mcp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The MCP stdio transport is newline-delimited JSON: one message per line, no
// embedded newlines, no framing headers. Writing LSP-style Content-Length
// headers instead leaves every line-reading server (every official
// @modelcontextprotocol server) waiting forever, because the header line is not
// JSON and the body never ends in a newline — the handshake then dies on the
// client deadline. Reads stay tolerant of both shapes; writes must be newlines.
func writeJSONRPCFrame(w *bufio.Writer, payload []byte) error {
	if i := bytes.IndexAny(payload, "\r\n"); i >= 0 {
		return fmt.Errorf("%w: embedded newline at %d", ErrStdioProtocol, i)
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

func readJSONRPCFrame(r *bufio.Reader) ([]byte, error) {
	var ancillary int
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if err == io.EOF && len(bytes.TrimSpace(line)) > 0 {
				trimmed := bytes.TrimSpace(line)
				if looksLikeJSONRPC(trimmed) {
					return trimmed, nil
				}
			}
			return nil, err
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if length, ok := parseContentLength(trimmed); ok {
			for {
				header, err := r.ReadBytes('\n')
				if err != nil {
					return nil, err
				}
				if len(bytes.TrimSpace(header)) == 0 {
					break
				}
			}
			if length < 1 || length > StdioMaxLineBytes {
				return nil, fmt.Errorf("%w: content-length", ErrStdioProtocol)
			}
			body := make([]byte, length)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, err
			}
			return bytes.TrimSpace(body), nil
		}
		if looksLikeJSONRPC(trimmed) {
			return trimmed, nil
		}
		ancillary += len(trimmed)
		if ancillary > 8<<20 {
			return nil, fmt.Errorf("%w: ancillary overflow", ErrStdioProtocol)
		}
	}
}

func parseContentLength(header []byte) (int, bool) {
	lower := bytes.ToLower(header)
	const prefix = "content-length:"
	if !bytes.HasPrefix(lower, []byte(prefix)) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(header[len(prefix):])))
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

func looksLikeJSONRPC(raw []byte) bool {
	return len(raw) > 0 && (raw[0] == '{' || raw[0] == '[')
}
