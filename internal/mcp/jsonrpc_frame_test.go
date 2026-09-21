package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
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

// A round trip through our own tolerant reader cannot prove the wire shape: it
// accepts Content-Length headers too, which is how LSP framing shipped and
// silenced every line-reading MCP server. Assert what the server sees.
func TestWriteJSONRPCFrameIsNewlineDelimitedForLineReaders(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if err := writeJSONRPCFrame(w, payload); err != nil {
		t.Fatal(err)
	}
	wire := buf.String()
	if strings.Contains(strings.ToLower(wire), "content-length") {
		t.Fatalf("MCP stdio takes newline JSON, not framing headers: %q", wire)
	}
	if !strings.HasSuffix(wire, "\n") {
		t.Fatalf("server line reader never terminates: %q", wire)
	}
	// What a line-reading server actually consumes: one line, valid JSON.
	line, err := bufio.NewReader(strings.NewReader(wire)).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &decoded); err != nil {
		t.Fatalf("first line is not a JSON-RPC message (%v): %q", err, line)
	}
	if decoded["method"] != "initialize" {
		t.Fatalf("first line lost the request: %q", line)
	}
	if strings.Count(wire, "\n") != 1 {
		t.Fatalf("one message must be one line: %q", wire)
	}
}

func TestWriteJSONRPCFrameRejectsEmbeddedNewline(t *testing.T) {
	w := bufio.NewWriter(&bytes.Buffer{})
	if err := writeJSONRPCFrame(w, []byte("{\"a\":1}\n{\"b\":2}")); err == nil {
		t.Fatal("embedded newline would split one message into two")
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
