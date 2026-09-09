package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// Stream failures happen after HTTP headers have arrived. Do not retry them:
// output and tool intent may already have been received by the caller.
func classifyStreamError(err error) *Error {
	code := classify(err).Code
	var policyError *networkpolicy.Error
	if errors.As(err, &policyError) && policyError.Code == networkpolicy.CodeResponseTooLarge {
		// Preserve only known local operations, never arbitrary error text.
		switch policyError.Op {
		case "read response":
			code = "RESPONSE_BODY_TOO_LARGE"
		case "read SSE line":
			code = "RESPONSE_LINE_TOO_LARGE"
		case "read SSE event":
			code = "RESPONSE_EVENT_TOO_LARGE"
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		code = "CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "TIMEOUT"
	case errors.Is(err, io.ErrUnexpectedEOF):
		code = "STREAM_INCOMPLETE"
	}
	return safeError(code, StageStream, 0, "upstream stream interrupted")
}

// Decode only machine-readable error classes. Provider messages can echo
// credentials, prompts or documents and must never enter failure receipts.
func streamEventError(eventType, data string) *Error {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal([]byte(data), &envelope)
	if eventType != "error" && (len(envelope.Error) == 0 || string(envelope.Error) == "null") {
		return nil
	}
	var detail struct {
		Type string          `json:"type"`
		Code json.RawMessage `json:"code"`
	}
	_ = json.Unmarshal(envelope.Error, &detail)
	var code string
	_ = json.Unmarshal(detail.Code, &code)
	class := "UPSTREAM_STREAM_FAILED"
	for _, value := range []string{code, detail.Type} {
		switch strings.ToLower(value) {
		case "invalid_request_error", "invalid_request":
			class = "STREAM_BAD_REQUEST"
		case "authentication_error", "invalid_api_key":
			class = "STREAM_AUTHENTICATION_FAILED"
		case "permission_error", "permission_denied":
			class = "STREAM_ACCESS_DENIED"
		case "not_found_error", "model_not_found":
			class = "STREAM_NOT_FOUND"
		case "rate_limit_error", "rate_limit_exceeded", "insufficient_quota":
			class = "STREAM_RATE_LIMITED"
		case "api_error", "server_error", "internal_server_error":
			class = "STREAM_UNAVAILABLE"
		case "overloaded_error":
			class = "STREAM_OVERLOADED"
		}
		if class != "UPSTREAM_STREAM_FAILED" {
			break
		}
	}
	// An in-band error is not an HTTP failure. Keep its class without
	// inventing an HTTP status that was never received on the wire.
	return safeError(class, StageStream, 0, "upstream reported a stream error")
}
