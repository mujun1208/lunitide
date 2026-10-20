package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// Stream failures happen after HTTP headers have arrived. The retry decision
// is delivery-based, not error-based: the run loop may retry a stream fault
// only while nothing deliverable (text or tool fragment) has been emitted,
// which the readStream deliverable clock tracks; once a single deliverable
// delta went out, the no-retry contract holds and the fault surfaces.
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

// retryableStreamFault reports whether a zero-deliverable stream fault is
// worth a same-turn retry: transient upstream states only. Caller must also
// verify the stream delivered nothing — an error after the first emitted
// deliverable delta is never retried regardless of class.
func retryableStreamFault(e error) bool {
	var se *Error
	if !errors.As(e, &se) {
		return false
	}
	switch se.Code {
	case "REASONING_STALL", "TIMEOUT", "STREAM_INCOMPLETE", "STREAM_RATE_LIMITED", "STREAM_UNAVAILABLE", "STREAM_OVERLOADED":
		return true
	}
	return false
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
