// Package diagnostic provides secret-safe provider connectivity diagnostics.
package diagnostic

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/gateway"
)

func Test(ctx context.Context, a gateway.Adapter, secret []byte, model string) gateway.TestResult {
	start := time.Now()
	_, err := a.Complete(ctx, secret, gateway.Request{Model: model, Messages: []gateway.Message{{Role: gateway.RoleUser, Content: "Hi"}}, MaxTokens: 1, MaxAttempts: 1})
	latency := time.Since(start)
	if err == nil {
		return gateway.TestResult{OK: true, Stage: gateway.StageHTTP, HTTPStatus: 200, Latency: latency, SanitizedMessage: "Connection successful"}
	}
	var ge *gateway.Error
	if !errors.As(err, &ge) {
		ge = &gateway.Error{Code: "GATEWAY_ERROR", Stage: gateway.StageDecode, Message: "Gateway test failed"}
	}
	// Never trust Error.Message from an adapter implementation.
	safe := &gateway.Error{Code: ge.Code, Stage: ge.Stage, HTTPStatus: ge.HTTPStatus, Message: diagnosticMessage(ge.Code, ge.Stage)}
	return gateway.TestResult{OK: false, Stage: safe.Stage, HTTPStatus: safe.HTTPStatus, Latency: latency, Error: safe, SanitizedMessage: safe.Message}
}

func diagnosticMessage(code string, stage gateway.Stage) string {
	if stage == gateway.StageHTTP {
		switch code {
		case "HTTP_401", "HTTP_403":
			return "Authentication failed"
		case "HTTP_404":
			return "Provider endpoint was not found"
		case "HTTP_429":
			return "Provider rate limit reached"
		}
	}
	if stage == gateway.StageConnect {
		switch code {
		case "TIMEOUT":
			return "Provider connection timed out"
		case "CANCELLED":
			return "Connection test was cancelled"
		case "TLS_ERROR":
			return "Secure connection failed"
		case "HTTPS_REQUIRED":
			return "Provider credentials require HTTPS"
		case "OUTCOME_UNKNOWN":
			return "Provider outcome is unknown"
		}
	}
	if stage == gateway.StageDecode && code == "MALFORMED_RESPONSE" {
		return "Provider returned an invalid response"
	}
	return "Provider connection test failed"
}
