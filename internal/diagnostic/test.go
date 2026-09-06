// Package diagnostic provides secret-safe provider connectivity diagnostics.
package diagnostic

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func Test(ctx context.Context, a llmadapter.Adapter, secret []byte, model string) llmadapter.TestResult {
	start := time.Now()
	_, err := a.Complete(ctx, secret, llmadapter.Request{Model: model, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "Hi"}}, MaxTokens: 1, MaxAttempts: 1})
	latency := time.Since(start)
	if err == nil {
		return llmadapter.TestResult{OK: true, Stage: llmadapter.StageHTTP, HTTPStatus: 200, Latency: latency, SanitizedMessage: "Connection successful"}
	}
	var ge *llmadapter.Error
	if !errors.As(err, &ge) {
		ge = &llmadapter.Error{Code: "GATEWAY_ERROR", Stage: llmadapter.StageDecode, Message: "Gateway test failed"}
	}
	// Never trust Error.Message from an adapter implementation.
	safe := &llmadapter.Error{Code: ge.Code, Stage: ge.Stage, HTTPStatus: ge.HTTPStatus, Message: diagnosticMessage(ge.Code, ge.Stage)}
	return llmadapter.TestResult{OK: false, Stage: safe.Stage, HTTPStatus: safe.HTTPStatus, Latency: latency, Error: safe, SanitizedMessage: safe.Message}
}

func diagnosticMessage(code string, stage llmadapter.Stage) string {
	if stage == llmadapter.StageHTTP {
		switch code {
		case "HTTP_401", "HTTP_403":
			return "Authentication failed"
		case "HTTP_404":
			return "Provider endpoint was not found"
		case "HTTP_429":
			return "Provider rate limit reached"
		}
	}
	if stage == llmadapter.StageConnect {
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
	if stage == llmadapter.StageDecode && code == "MALFORMED_RESPONSE" {
		return "Provider returned an invalid response"
	}
	return "Provider connection test failed"
}
