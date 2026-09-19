package mcp

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Community MCP servers often echo the request id as a string ("1") and
// publish serverInfo.version as a JSON number. Both are legal JSON-RPC;
// rejecting them quarantines otherwise usable keyless servers.
func jsonRPCIDMatches(raw json.RawMessage, id int64) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var number int64
	if json.Unmarshal(raw, &number) == nil {
		return number == id
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return false
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 32 {
		return false
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	return err == nil && parsed == id
}

func coerceJSONText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return strings.TrimSpace(number.String())
	}
	return ""
}

type mcpInitializeResult struct {
	ProtocolVersion json.RawMessage `json:"protocolVersion"`
	ServerInfo      struct {
		Name    json.RawMessage `json:"name"`
		Version json.RawMessage `json:"version"`
	} `json:"serverInfo"`
}

func (r mcpInitializeResult) name() string     { return coerceJSONText(r.ServerInfo.Name) }
func (r mcpInitializeResult) version() string  { return coerceJSONText(r.ServerInfo.Version) }
func (r mcpInitializeResult) protocol() string { return coerceJSONText(r.ProtocolVersion) }
