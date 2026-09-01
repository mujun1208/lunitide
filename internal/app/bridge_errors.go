package app

import (
	"log"

	"github.com/lunitide/lunitide/internal/bridge"
)

// internalBridgeFailure preserves full diagnostics in the engine log while
// exposing only a stable public code/message. The trace ID is also returned as
// the correlation ID so the client can identify the corresponding server log.
func internalBridgeFailure(r bridge.Request, code, publicMessage string, retryable bool, err error) bridge.Response {
	log.Printf("bridge internal failure: correlation_id=%s request_id=%s method=%s code=%s err=%v", r.TraceID, r.ID, r.Method, code, err)
	return r.Fail(code, publicMessage, retryable)
}
