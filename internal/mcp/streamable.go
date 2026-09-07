package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/buildinfo"
)

var ErrRemoteProtocol = errors.New("mcp: invalid Streamable HTTP response")
var ErrRemoteSessionExpired = errors.New("mcp: remote session expired; reconnect before the next call")

// RemoteSession lasts only within the credential lease. It never retries a
// tools/call POST: a lost response does not prove the operation did not run.
// Legacy GET endpoints retain their existing protocol and retry policy.
type RemoteSession struct {
	client                        *Client
	bearer                        []byte
	legacy                        bool
	identity, sessionID, protocol string
	nextID                        int64
}

// Discover negotiates standard MCP at /mcp paths, retaining the product's
// existing GET catalogue protocol for older configured endpoints. A missing
// legacy catalogue can also be a standard endpoint at a custom path.
func (c *Client) Discover(ctx context.Context, bearer []byte) (*RemoteSession, []ToolInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, c.TotalTimeout)
	defer cancel()
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, nil, err
	}
	if u.RawQuery == "" && !pathCredentialURL(u) && !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/mcp") {
		tools, legacyErr := c.ListToolsAuthenticated(ctx, bearer)
		if legacyErr == nil {
			s := &RemoteSession{client: c, bearer: bearer, legacy: true, identity: "legacy-get-v1|" + c.BaseURL}
			return s, tools, nil
		}
		var status *HTTPStatusError
		if !errors.As(legacyErr, &status) || (status.StatusCode != 404 && status.StatusCode != 405) {
			return nil, nil, legacyErr
		}
	}
	s := &RemoteSession{client: c, bearer: bearer}
	if err := s.initialize(ctx); err != nil {
		s.Close()
		var status *HTTPStatusError
		if u.RawQuery == "" && !pathCredentialURL(u) && errors.As(err, &status) && (status.StatusCode == 404 || status.StatusCode == 405) {
			tools, legacyErr := c.ListToolsAuthenticated(ctx, bearer)
			if legacyErr == nil {
				return &RemoteSession{client: c, bearer: bearer, legacy: true, identity: "legacy-get-v1|" + c.BaseURL}, tools, nil
			}
		}
		return nil, nil, err
	}
	tools, err := s.listTools(ctx)
	if err != nil {
		s.Close()
		return nil, nil, err
	}
	return s, tools, nil
}

func (s *RemoteSession) Identity() string { return s.identity }
func (s *RemoteSession) IsLegacy() bool   { return s.legacy }

func (s *RemoteSession) initialize(ctx context.Context) error {
	var answer struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	err := s.roundtrip(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "lunitide", "version": buildinfo.Version},
	}, &answer)
	if err != nil {
		return err
	}
	if (answer.ProtocolVersion != "2025-11-25" && answer.ProtocolVersion != "2025-06-18" && answer.ProtocolVersion != "2025-03-26") || strings.TrimSpace(answer.ServerInfo.Name) == "" || strings.TrimSpace(answer.ServerInfo.Version) == "" || len(answer.ServerInfo.Name) > 512 || len(answer.ServerInfo.Version) > 128 {
		return ErrRemoteProtocol
	}
	s.protocol = answer.ProtocolVersion
	identity, _ := json.Marshal(answer)
	s.identity = "streamable-http|" + s.client.BaseURL + "|" + string(identity)
	_, err = s.post(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, 0, true)
	return err
}

func (s *RemoteSession) listTools(ctx context.Context) ([]ToolInfo, error) {
	var out []ToolInfo
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 16; page++ {
		var answer struct {
			Tools      []ToolInfo `json:"tools"`
			NextCursor string     `json:"nextCursor"`
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := s.roundtrip(ctx, "tools/list", params, &answer); err != nil {
			return nil, err
		}
		out = append(out, answer.Tools...)
		if len(out) > 512 {
			return nil, ErrResponseTooLarge
		}
		if answer.NextCursor == "" {
			return out, nil
		}
		if len(answer.NextCursor) > 4096 || seen[answer.NextCursor] {
			return nil, ErrRemoteProtocol
		}
		seen[answer.NextCursor] = true
		cursor = answer.NextCursor
	}
	return nil, ErrResponseTooLarge
}

func (s *RemoteSession) Call(ctx context.Context, tool string, argsJSON []byte) (map[string]any, error) {
	if tool == "" {
		return nil, ErrRemoteProtocol
	}
	if s.legacy {
		result, err := s.client.InvokeAuthenticated(ctx, InvokeInput{Tool: tool, ArgsJSON: argsJSON}, s.bearer)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if json.Unmarshal(result.Data, &out) != nil || out == nil {
			out = map[string]any{"data": string(result.Data)}
		}
		return out, nil
	}
	args := map[string]any{}
	if len(argsJSON) > 0 && (json.Unmarshal(argsJSON, &args) != nil || args == nil) {
		return nil, ErrRemoteProtocol
	}
	var answer struct {
		Content    []json.RawMessage `json:"content"`
		Structured json.RawMessage   `json:"structuredContent"`
		IsError    bool              `json:"isError"`
	}
	if err := s.roundtrip(ctx, "tools/call", map[string]any{"name": tool, "arguments": args}, &answer); err != nil {
		return nil, err
	}
	out := map[string]any{"isError": answer.IsError}
	var texts []string
	for _, raw := range answer.Content {
		var item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type == "" {
			return nil, ErrRemoteProtocol
		}
		if item.Type == "text" {
			texts = append(texts, item.Text)
		}
	}
	// Preserve resource links and image/audio blocks for artifact consumers.
	if len(answer.Content) > 0 {
		out["content"] = answer.Content
	}
	if len(texts) == 1 {
		out["text"] = texts[0]
	} else if len(texts) > 1 {
		out["texts"] = texts
	}
	if len(answer.Structured) > 0 {
		out["structured"] = answer.Structured
	}
	return out, nil
}

func (s *RemoteSession) roundtrip(ctx context.Context, method string, params, into any) error {
	s.nextID++
	data, err := s.post(ctx, map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method, "params": params}, s.nextID, false)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, into) != nil {
		return ErrRemoteProtocol
	}
	return nil
}

func (s *RemoteSession) request(ctx context.Context, method string, body io.Reader) (*http.Response, error) {
	target, err := url.Parse(s.client.BaseURL)
	if err != nil {
		return nil, ErrRemoteProtocol
	}
	queryCredential := target.RawQuery != ""
	pathCredential := pathCredentialURL(target)
	urlCredential := queryCredential || pathCredential
	if len(s.bearer) > 16384 || strings.ContainsAny(string(s.bearer), "\r\n\x00") {
		return nil, ErrRemoteProtocol
	}
	if urlCredential {
		if err := ValidateBaseURL(s.client.BaseURL); err != nil {
			return nil, err
		}
		if len(s.bearer) == 0 {
			return nil, &HTTPStatusError{StatusCode: 401}
		}
		if pathCredential {
			// The provider's tokens are opaque ASCII identifiers, not paths.
			// Reject delimiters instead of allowing credentials to alter routing.
			for _, b := range s.bearer {
				if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.') {
					return nil, ErrRemoteProtocol
				}
			}
			target.Path = "/mcp/token=" + string(s.bearer)
			target.RawPath = ""
		} else {
			q := target.Query()
			for name := range q {
				q.Set(name, string(s.bearer))
			}
			target.RawQuery = q.Encode()
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if s.protocol != "" {
		req.Header.Set("MCP-Protocol-Version", s.protocol)
	}
	if s.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", s.sessionID)
	}
	if len(s.bearer) > 0 {
		if !urlCredential {
			req.Header.Set("Authorization", "Bearer "+string(s.bearer))
		}
		defer req.Header.Del("Authorization")
	}
	// Uses the same TLS, origin allowlist, redirect refusal and timeouts as
	// the existing client. Error bodies are never copied into logs or chat.
	resp, err := s.client.HTTP.Do(req)
	if urlCredential && err != nil {
		var detail *url.Error
		if errors.As(err, &detail) {
			err = &url.Error{Op: detail.Op, URL: "[MCP endpoint]", Err: detail.Err}
		}
	}
	return resp, err
}

func (s *RemoteSession) post(ctx context.Context, message any, id int64, notification bool) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, s.client.TotalTimeout)
	defer cancel()
	data, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	resp, err := s.request(ctx, http.MethodPost, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 && s.sessionID != "" {
		return nil, ErrRemoteSessionExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode}
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return nil, ErrEncodingBlocked
	}
	if received := resp.Header.Get("Mcp-Session-Id"); received != "" {
		if len(received) > 4096 {
			return nil, ErrRemoteProtocol
		}
		for _, c := range received {
			if c < 0x21 || c > 0x7e {
				return nil, ErrRemoteProtocol
			}
		}
		if s.sessionID != "" && received != s.sessionID {
			return nil, ErrRemoteProtocol
		}
		s.sessionID = received
	}
	if notification {
		if resp.StatusCode != 202 && resp.StatusCode != 204 {
			return nil, ErrRemoteProtocol
		}
		return nil, nil
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	limited := &io.LimitedReader{R: resp.Body, N: MaxResponseBytes + 1}
	if contentType == "text/event-stream" {
		return readRemoteSSE(limited, id)
	}
	if contentType != "application/json" {
		return nil, ErrRemoteProtocol
	}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	result, matched, err := remoteRPCResult(raw, id)
	if err == nil && !matched {
		err = ErrRemoteProtocol
	}
	return result, err
}

func remoteRPCResult(raw []byte, id int64) (json.RawMessage, bool, error) {
	var answer struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &answer) != nil || answer.JSONRPC != "2.0" {
		return nil, false, ErrRemoteProtocol
	}
	if len(answer.ID) == 0 && answer.Method != "" {
		return nil, false, nil
	} // server notification
	var received int64
	if json.Unmarshal(answer.ID, &received) != nil || received != id || answer.Method != "" {
		return nil, false, ErrRemoteProtocol
	}
	if answer.Error != nil {
		return nil, true, fmt.Errorf("%w: JSON-RPC code %d", ErrRemoteProtocol, answer.Error.Code)
	}
	if len(answer.Result) == 0 || bytes.Equal(answer.Result, []byte("null")) {
		return nil, true, ErrRemoteProtocol
	}
	return answer.Result, true, nil
}

func readRemoteSSE(body *io.LimitedReader, id int64) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), MaxResponseBytes+1)
	var data []byte
	for scanner.Scan() {
		if body.N <= 0 {
			return nil, ErrResponseTooLarge
		}
		line := scanner.Text()
		if line == "" {
			if len(data) == 0 {
				continue
			}
			result, matched, err := remoteRPCResult(bytes.TrimSpace(data), id)
			if err != nil || matched {
				return result, err
			}
			data = nil
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
			data = append(data, '\n')
		}
	}
	if body.N <= 0 {
		return nil, ErrResponseTooLarge
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, ErrRemoteProtocol
}

func (s *RemoteSession) Close() {
	if s == nil {
		return
	}
	defer func() { s.bearer = nil; s.sessionID = "" }()
	if s.legacy || s.sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if resp, err := s.request(ctx, http.MethodDelete, nil); err == nil {
		resp.Body.Close()
	}
}
