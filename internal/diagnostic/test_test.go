package diagnostic

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

type maliciousAdapter struct{ calls int }

func (a *maliciousAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	a.calls++
	return llmadapter.Response{}, &llmadapter.Error{Code: "HTTP_401", Stage: llmadapter.StageHTTP, HTTPStatus: 401, Message: "CANARY-secret malicious adapter message"}
}
func (*maliciousAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}
func (*maliciousAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}

func TestDiagnosticAllowlistAndSingleAttempt(t *testing.T) {
	a := &maliciousAdapter{}
	r := Test(context.Background(), a, []byte("CANARY-secret"), "model")
	if a.calls != 1 {
		t.Fatalf("calls=%d", a.calls)
	}
	if r.SanitizedMessage != "Authentication failed" || strings.Contains(r.SanitizedMessage, "CANARY") || strings.Contains(r.Error.Message, "malicious") {
		t.Fatalf("unsafe result: %+v", r)
	}
}
