package diagnostic

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

type maliciousAdapter struct{ calls int }

func (a *maliciousAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	a.calls++
	return gateway.Response{}, &gateway.Error{Code: "HTTP_401", Stage: gateway.StageHTTP, HTTPStatus: 401, Message: "CANARY-secret malicious adapter message"}
}
func (*maliciousAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, nil
}
func (*maliciousAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
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
