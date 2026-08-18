package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// localTrustHost reports whether the connector target is a loopback or
// RFC1918/ULA literal the user explicitly configured, meaning credentials
// over plain HTTP stay inside the trusted local network.
func localTrustHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func marshalBounded(v any, max int) (*bytes.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, safeError("INVALID_REQUEST", StageDecode, 0, "invalid request")
	}
	if len(b) > max {
		return nil, safeError("REQUEST_TOO_LARGE", StageDecode, 0, "request exceeds size budget")
	}
	return bytes.NewReader(b), nil
}

func strictJSON(r io.Reader, dst any) error {
	return decodeJSON(r, dst, true)
}

// compatibleJSON preserves JSON framing validation while allowing additive
// fields returned by OpenAI-compatible providers.
func compatibleJSON(r io.Reader, dst any) error {
	return decodeJSON(r, dst, false)
}

func decodeJSON(r io.Reader, dst any, disallowUnknown bool) error {
	d := json.NewDecoder(r)
	if disallowUnknown {
		d.DisallowUnknownFields()
	}
	if err := d.Decode(dst); err != nil {
		if code := networkpolicy.ErrorCode(err); code != "" {
			return classify(err)
		}
		return safeError("MALFORMED_RESPONSE", StageDecode, 0, "upstream returned malformed JSON")
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return safeError("MALFORMED_RESPONSE", StageDecode, 0, "upstream returned trailing JSON")
	}
	return nil
}

func safeError(code string, stage Stage, status int, message string) *Error {
	return &Error{Code: code, Stage: stage, HTTPStatus: status, Message: message}
}

func classify(err error) *Error {
	if err == nil {
		return nil
	}
	var ge *Error
	if errors.As(err, &ge) {
		return ge
	}
	code := string(networkpolicy.ErrorCode(err))
	if code == "" {
		code = "CONNECTION_FAILED"
	}
	return safeError(code, StageConnect, 0, "upstream connection failed")
}

func statusError(status int) *Error {
	return safeError("HTTP_"+strconv.Itoa(status), StageHTTP, status, http.StatusText(status))
}

// statusErrorReason extends statusError with a bounded, single-line reason
// extracted from the upstream error body so 4xx diagnostics name the real
// cause (schema keyword rejected, tools unsupported, ...) instead of a bare
// status text.
func statusErrorReason(status int, reason string) *Error {
	msg := http.StatusText(status)
	if reason != "" {
		msg += ": " + reason
	}
	return safeError("HTTP_"+strconv.Itoa(status), StageHTTP, status, msg)
}

// boundedReason reads at most 4 KiB of an error response body and extracts
// a single-line reason bounded to 200 runes. OpenAI-style error wrappers
// ({"error":{"message":...}} / {"message":...}) are unwrapped; anything
// else falls back to the raw text. Control characters are stripped so the
// result is safe for logs and the persisted fallback notice.
func boundedReason(r io.Reader) string {
	if r == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil || len(raw) == 0 {
		return ""
	}
	reason := ""
	var probe struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &probe) == nil {
		if probe.Error.Message != "" {
			reason = probe.Error.Message
		} else if probe.Message != "" {
			reason = probe.Message
		}
	}
	if reason == "" {
		reason = string(raw)
	}
	reason = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20:
			return -1
		default:
			return r
		}
	}, reason)
	reason = strings.Join(strings.Fields(reason), " ")
	if runes := []rune(reason); len(runes) > 200 {
		reason = string(runes[:200])
	}
	return reason
}

// schemaCoreKeys is the JSON Schema subset every targeted
// OpenAI-compatible provider accepts inside function parameters.
// sanitizeToolSchema drops everything else (additionalProperties,
// minimum/maximum, minLength/maxLength, maxItems/minItems, pattern,
// format, ...) before the one-shot compatibility retry.
var schemaCoreKeys = map[string]bool{
	"type":        true,
	"title":       true,
	"description": true,
	"properties":  true,
	"items":       true,
	"required":    true,
	"enum":        true,
}

// sanitizeToolSchema returns schema reduced to schemaCoreKeys (recursively).
// On any decode failure or when nothing changes it returns the original.
func sanitizeToolSchema(schema json.RawMessage) json.RawMessage {
	var doc any
	if len(schema) == 0 || json.Unmarshal(schema, &doc) != nil {
		return schema
	}
	cleaned, changed := sanitizeSchemaValue(doc)
	if !changed {
		return schema
	}
	out, err := json.Marshal(cleaned)
	if err != nil {
		return schema
	}
	return out
}

func sanitizeSchemaValue(v any) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		changed := false
		for k, child := range t {
			if !schemaCoreKeys[k] {
				changed = true
				continue
			}
			if k == "properties" {
				if props, ok := child.(map[string]any); ok {
					clean := make(map[string]any, len(props))
					for name, pv := range props {
						cp, c := sanitizeSchemaValue(pv)
						clean[name] = cp
						changed = changed || c
					}
					out[k] = clean
					continue
				}
			}
			cv, c := sanitizeSchemaValue(child)
			out[k] = cv
			changed = changed || c
		}
		return out, changed
	case []any:
		out := make([]any, len(t))
		changed := false
		for i, item := range t {
			cv, c := sanitizeSchemaValue(item)
			out[i] = cv
			changed = changed || c
		}
		return out, changed
	default:
		return v, false
	}
}

// wireNames is the per-request bidirectional tool-name mapping. Internal
// tool names (workspace.list, mcp_<endpoint>_<tool>, ...) carry dots and
// possibly other runes that strict OpenAI-compatible and Anthropic
// providers reject (pattern ^[a-zA-Z0-9_-]+$), so every request maps
// internal names onto provider-safe wire names and every parsed tool call
// maps them back. A nil *wireNames is valid: wire() still sanitizes
// deterministically and original() is the identity.
type wireNames struct {
	toWire   map[string]string
	fromWire map[string]string
}

const (
	openAIToolNameMax    = 64
	anthropicToolNameMax = 128
)

// sanitizeToolName maps name onto the provider-safe alphabet: every rune
// outside [a-zA-Z0-9_-] becomes '_'.
func sanitizeToolName(name string) string {
	changed := false
	for _, r := range name {
		if !providerToolRune(r) {
			changed = true
			break
		}
	}
	if !changed {
		return name
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if providerToolRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func providerToolRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
}

func fnv32(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// buildWireNames precomputes the mapping for the declared tools. Names that
// sanitize unchanged and fit the budget keep their exact form (most
// providers already accept those); truncated or colliding names get a
// deterministic hash suffix so the mapping stays reversible.
func buildWireNames(tools []ToolDefinition, maxLen int) *wireNames {
	wn := &wireNames{toWire: make(map[string]string, len(tools)), fromWire: make(map[string]string, len(tools))}
	used := make(map[string]bool, len(tools))
	for _, t := range tools {
		if t.Name == "" {
			continue
		}
		wire := sanitizeToolName(t.Name)
		if len(wire) > maxLen || used[wire] {
			base := wire
			if len(base) > maxLen-7 {
				base = base[:maxLen-7]
			}
			for i := 2; ; i++ {
				wire = fmt.Sprintf("%s_%05x", base, fnv32(fmt.Sprintf("%s#%d", t.Name, i))&0xfffff)
				if !used[wire] {
					break
				}
			}
		}
		used[wire] = true
		wn.toWire[t.Name] = wire
		wn.fromWire[wire] = t.Name
	}
	return wn
}

func (wn *wireNames) wire(name string) string {
	if wn != nil {
		if w, ok := wn.toWire[name]; ok {
			return w
		}
	}
	return sanitizeToolName(name)
}

func (wn *wireNames) original(name string) string {
	if wn != nil {
		if o, ok := wn.fromWire[name]; ok {
			return o
		}
	}
	return name
}

func doWithSecret(c Connector, req *http.Request, name, prefix string, secret []byte) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, safeError("MALFORMED_REQUEST", StageConnect, 0, "malformed request")
	}
	// When no secret is configured (e.g. local model servers like LM Studio,
	// Ollama that run on HTTP without an API key), allow plain HTTP.
	// A non-empty secret still requires HTTPS for transport security —
	// except on loopback/private hosts the user explicitly configured
	// (LM Studio / Ollama / vLLM on a LAN box): the credential never
	// leaves the trusted network. Public HTTP endpoints keep failing closed.
	if len(secret) > 0 && !strings.EqualFold(req.URL.Scheme, "https") && !localTrustHost(req.URL.Hostname()) {
		return nil, safeError("HTTPS_REQUIRED", StageConnect, 0, "credentials require HTTPS")
	}
	// Go strings cannot be guaranteed to be zeroed. Minimize lifetime and remove
	// the header immediately after Do returns.
	if len(secret) > 0 {
		req.Header.Set(name, prefix+string(secret))
		defer req.Header.Del(name)
	}
	return c.Do(req)
}

func retryableBeforeConnect(err error) bool {
	if err != nil {
		c := networkpolicy.ErrorCode(err)
		return c == networkpolicy.CodeConnectionRefused || c == networkpolicy.CodeDNSError || c == networkpolicy.CodeTLSError
	}
	return false
}

func retryableStatus(status int) bool {
	return status == 429 || status == 500 || status == 502 || status == 503 || status == 504
}
func attempts(o Options, in Request, stream bool) int {
	n := o.MaxAttempts
	if in.MaxAttempts > 0 {
		n = in.MaxAttempts
	}
	if n < 1 || stream {
		return 1
	}
	return n
}
func uncertain(err error) *Error {
	if networkpolicy.ErrorCode(err) == networkpolicy.CodeTimeout {
		return safeError("OUTCOME_UNKNOWN", StageConnect, 0, "upstream outcome is unknown")
	}
	return classify(err)
}

const maxRetryDelay = 30 * time.Second

func retryDelay(resp *http.Response, attempt int, base time.Duration) time.Duration {
	if resp != nil {
		r := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if n, err := strconv.Atoi(r); err == nil && n >= 0 {
			if n > int(maxRetryDelay/time.Second) {
				return maxRetryDelay
			}
			return time.Duration(n) * time.Second
		}
		if t, err := http.ParseTime(r); err == nil {
			if d := time.Until(t); d > 0 {
				if d > maxRetryDelay {
					return maxRetryDelay
				}
				return d
			}
		}
	}
	if base <= 0 {
		return 0
	}
	d := base
	for i := 1; i < attempt; i++ {
		if d >= maxRetryDelay/2 {
			return maxRetryDelay
		}
		d *= 2
	}
	if d > maxRetryDelay {
		return maxRetryDelay
	}
	return d
}

func waitRetry(ctx context.Context, d time.Duration) error {
	if d > maxRetryDelay {
		d = maxRetryDelay
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 || d >= remaining {
			return classify(context.DeadlineExceeded)
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return classify(ctx.Err())
	case <-t.C:
		return nil
	}
}

func sseData(event []byte) (string, string) {
	var typ string
	var data []string
	for _, line := range strings.Split(string(event), "\n") {
		if strings.HasPrefix(line, "event:") {
			typ = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return typ, strings.Join(data, "\n")
}

func normalizeUsage(input, output, total int) Usage {
	if total == 0 {
		total = input + output
	}
	return Usage{InputTokens: input, OutputTokens: output, TotalTokens: total}
}

func validUsage(input, output, total int) bool {
	const maxTokens = 1 << 30
	return input >= 0 && output >= 0 && total >= 0 && input <= maxTokens && output <= maxTokens && total <= maxTokens
}
