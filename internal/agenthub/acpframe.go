package agenthub

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const maxACPFrameBytes = 4 << 20

func EncodeACPFrame(body []byte) []byte {
	if bytes.IndexByte(body, '\n') >= 0 || bytes.IndexByte(body, '\r') >= 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, body); err == nil {
			body = compact.Bytes()
		}
	}
	out := make([]byte, 0, len(body)+1)
	out = append(out, body...)
	return append(out, '\n')
}

func DecodeACPFrame(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > maxACPFrameBytes {
			return nil, fmt.Errorf("acp frame too large")
		}
		line = append(line, part...)
		if err == nil {
			break
		}
		if err == io.EOF {
			if len(line) == 0 {
				return nil, err
			}
			break
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
	line = bytes.TrimRight(line, "\r\n")
	if bytes.HasPrefix(bytes.TrimSpace(line), []byte("Content-Length:")) {
		return nil, fmt.Errorf("acp frame is Content-Length, want NDJSON")
	}
	if !json.Valid(line) {
		return nil, fmt.Errorf("acp frame is not JSON-RPC")
	}
	return line, nil
}
