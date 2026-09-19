package mcp

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestReadJSONRPCFrameContentLengthWithoutTrailingNewline(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	var buf bytes.Buffer
	buf.WriteString("Content-Length: ")
	buf.WriteString(strconv.Itoa(len(payload)))
	buf.WriteString("\r\n\r\n")
	buf.WriteString(payload)
	got, err := readJSONRPCFrame(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("frame = %q", got)
	}
}

func TestReadJSONRPCFrameAcceptsNewlineJSON(t *testing.T) {
	got, err := readJSONRPCFrame(bufio.NewReader(strings.NewReader("starting...\n{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{}}\n")))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"jsonrpc":"2.0","id":2,"result":{}}` {
		t.Fatalf("newline json = %q", got)
	}
}

func TestWriteJSONRPCFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	payload := []byte(`{"jsonrpc":"2.0","id":3,"result":{"name":"ddg"}}`)
	if err := writeJSONRPCFrame(w, payload); err != nil {
		t.Fatal(err)
	}
	got, err := readJSONRPCFrame(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round trip = %q", got)
	}
}
