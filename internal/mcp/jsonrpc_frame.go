package mcp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Official MCP SDKs speak LSP-style Content-Length frames and often write
// the JSON body with no trailing newline. Line scanners then stall or treat
// the header as protocol garbage. Read both frames and newline JSON; write
// Content-Length so curated stdio servers can complete initialize.
func writeJSONRPCFrame(w *bufio.Writer, payload []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
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
