package agenthub

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxACPFrameBytes = 4 << 20

func EncodeACPFrame(body []byte) []byte {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	out := make([]byte, 0, len(header)+len(body))
	out = append(out, header...)
	return append(out, body...)
}

func DecodeACPFrame(r *bufio.Reader) ([]byte, error) {
	var length int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			if length <= 0 {
				return nil, fmt.Errorf("acp frame missing Content-Length")
			}
			break
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("acp frame header %q", trimmed)
		}
		if !strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 || n > maxACPFrameBytes {
			return nil, fmt.Errorf("acp frame length %q", value)
		}
		length = n
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}
