package mcp6

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeLease struct {
	secret []byte
}

func (f fakeLease) WithLease(ctx context.Context, authRef string, fn func(auth []byte) error) error {
	return fn(f.secret)
}

func validPin() CapabilityPin {
	return CapabilityPin{
		ServerIdentityDigest: strings.Repeat("a", 64),
		ToolSchemaDigests:    map[string]string{"searchDocs": strings.Repeat("b", 64)},
	}
}

func newTestRegistry(probe ProbeFunc, invoke InvokeFunc) *Registry {
	r := NewRegistry(probe, invoke, fakeLease{secret: []byte("topsecret")})
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	r.SetClock(func() time.Time { return base })
	return r
}

// M6-MCP-004: stdio stays disabled at the registration gate itself.
func TestRegisterRejectsStdio(t *testing.T) {
	r := newTestRegistry(nil, nil)
	_, err := r.Register(context.Background(), "stdio", "https://example.com/mcp", "secretref:pool/a", validPin())
	if !errors.Is(err, ErrStdioDisabled) {
		t.Fatalf("want ErrStdioDisabled, got %v", err)
	}
}

// M6-MCP-001: failed probe keeps the endpoint registered but degraded.
func TestRegisterDegradedOnProbeFailure(t *testing.T) {
	r := newTestRegistry(func(ctx context.Context, e *Endpoint) error {
		return errors.New("tls handshake timeout")
	}, nil)
	e, err := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	if e == nil || e.State != StateDegraded {
		t.Fatalf("want degraded endpoint, got %+v", e)
	}
	if !errors.Is(err, ErrHealthCheckFailed) {
		t.Fatalf("want ErrHealthCheckFailed, got %v", err)
	}
}

func TestRegisterRejectsInlineCredential(t *testing.T) {
	r := newTestRegistry(nil, nil)
	_, err := r.Register(context.Background(), "https", "https://example.com/mcp", "Bearer abc123", validPin())
	if err == nil || !strings.Contains(err.Error(), "secretref") {
		t.Fatalf("want secretref handle rejection, got %v", err)
	}
}

func TestRegisterRejectsBadPin(t *testing.T) {
	r := newTestRegistry(nil, nil)
	pin := validPin()
	pin.ServerIdentityDigest = "not-a-digest"
	if _, err := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", pin); err == nil {
		t.Fatal("want malformed pin rejection")
	}
	pin2 := CapabilityPin{ServerIdentityDigest: strings.Repeat("a", 64)} // no tools
	if _, err := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", pin2); err == nil {
		t.Fatal("want empty toolset rejection")
	}
}

// Happy path: register→ready→invoke returns pinned result with trace.
func TestInvokeHappyPath(t *testing.T) {
	r := newTestRegistry(nil, func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		if string(auth) != "topsecret" {
			return nil, errors.New("auth not delivered")
		}
		return map[string]any{"hits": 3}, nil
	})
	e, err := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	if err != nil || e.State != StateReady {
		t.Fatalf("register failed: %v %s", err, e.State)
	}
	res, err := r.Invoke(context.Background(), e.ID, "searchDocs", map[string]any{"q": "durable"})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if res.Result["hits"] != 3 || res.TraceID == "" || res.Bytes <= 0 {
		t.Fatalf("bad result: %+v", res)
	}
}

// M6-MCP-002: unpinned tool degrades the endpoint and invalidates the grant.
func TestInvokeUnpinnedToolDrifts(t *testing.T) {
	r := newTestRegistry(nil, func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		return map[string]any{}, nil
	})
	e, _ := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	_, err := r.Invoke(context.Background(), e.ID, "deleteEverything", nil)
	if !errors.Is(err, ErrCapabilityDrift) {
		t.Fatalf("want ErrCapabilityDrift, got %v", err)
	}
	if cur, _ := r.Get(e.ID); cur.State != StateDegraded {
		t.Fatalf("want degraded after drift, got %s", cur.State)
	}
	// Subsequent invoke answers not-ready (M6-MCP-001).
	if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); !errors.Is(err, ErrNotReady) {
		t.Fatalf("want ErrNotReady, got %v", err)
	}
}

// M6-MCP-003: upstream 401 revokes the endpoint immediately.
func TestInvokeCredentialRevocation(t *testing.T) {
	r := newTestRegistry(nil, func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		return nil, ErrCredentialRevoked
	})
	e, _ := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	_, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil)
	if !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("want ErrCredentialRevoked, got %v", err)
	}
	if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); !errors.Is(err, ErrEndpointRevoked) {
		t.Fatalf("want ErrEndpointRevoked after 401, got %v", err)
	}
}

// M6-MCP-005: five consecutive transport failures open a 60 s window; a
// success resets the counter.
func TestBreakerOpensAfterFiveFailures(t *testing.T) {
	calls := 0
	r := newTestRegistry(nil, func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		calls++
		if calls <= 4 {
			return nil, errors.New("connection reset")
		}
		return map[string]any{}, nil // 5th call would succeed — breaker must already be open
	})
	e, _ := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	for i := 0; i < 4; i++ {
		if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); !errors.Is(err, ErrTransport) {
			t.Fatalf("call %d: want ErrTransport, got %v", i+1, err)
		}
	}
	if calls != 4 {
		t.Fatalf("want 4 upstream calls, got %d", calls)
	}
	// 5th failure trips the breaker.
	r.invoke = func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		return nil, errors.New("connection reset")
	}
	if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("want ErrCircuitOpen on 5th failure, got %v", err)
	}
	// While open, even before any further upstream call, invoke fails fast.
	r.invoke = func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		t.Fatal("upstream must not be called while breaker open")
		return nil, nil
	}
	if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("want fail-fast while open, got %v", err)
	}
}

func TestBreakerResetsOnSuccess(t *testing.T) {
	fail := true
	r := newTestRegistry(nil, func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		if fail {
			return nil, errors.New("timeout")
		}
		return map[string]any{"ok": true}, nil
	})
	e, _ := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	for i := 0; i < 3; i++ {
		_, _ = r.Invoke(context.Background(), e.ID, "searchDocs", nil)
	}
	fail = false
	if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); err != nil {
		t.Fatalf("success call failed: %v", err)
	}
	fail = true
	for i := 0; i < 3; i++ {
		_, _ = r.Invoke(context.Background(), e.ID, "searchDocs", nil)
	}
	// Counter restarted at 0 after the success: 3 failures + this one stay transport errors.
	if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); !errors.Is(err, ErrTransport) {
		t.Fatalf("want ErrTransport (counter was reset), got %v", err)
	}
}

// Secret zero-leak: lease bytes never appear in any returned error string.
func TestSecretNeverLeaksIntoErrors(t *testing.T) {
	r := newTestRegistry(nil, func(ctx context.Context, e *Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		return nil, errors.New("upstream said: topsecret is invalid")
	})
	e, _ := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	_, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil)
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("secret leaked into error: %s", err.Error())
	}
}

func TestRevokeIdempotentAndValidated(t *testing.T) {
	r := newTestRegistry(nil, nil)
	e, _ := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	if _, err := r.Revoke(e.ID, "because"); err == nil {
		t.Fatal("want invalid reason rejection")
	}
	first, err := r.Revoke(e.ID, ReasonDrift)
	if err != nil || first.State != StateRevoked {
		t.Fatalf("revoke failed: %v %+v", err, first)
	}
	second, err := r.Revoke(e.ID, ReasonManual)
	if err != nil || second.State != StateRevoked {
		t.Fatalf("revoke not idempotent: %v %+v", err, second)
	}
	if _, err := r.Invoke(context.Background(), e.ID, "searchDocs", nil); !errors.Is(err, ErrEndpointRevoked) {
		t.Fatalf("want ErrEndpointRevoked, got %v", err)
	}
}

func TestProbeRecoversDegraded(t *testing.T) {
	fail := true
	r := newTestRegistry(func(ctx context.Context, e *Endpoint) error {
		if fail {
			return errors.New("unreachable")
		}
		return nil
	}, nil)
	e, _ := r.Register(context.Background(), "https", "https://example.com/mcp", "secretref:pool/a", validPin())
	if e.State != StateDegraded {
		t.Fatalf("want degraded, got %s", e.State)
	}
	fail = false
	if _, err := r.Probe(context.Background(), e.ID); err != nil {
		t.Fatalf("probe recovery failed: %v", err)
	}
	if cur, _ := r.Get(e.ID); cur.State != StateReady {
		t.Fatalf("want ready after probe, got %s", cur.State)
	}
}

func TestGetUnknownEndpoint(t *testing.T) {
	r := newTestRegistry(nil, nil)
	if _, err := r.Get("01ARZ3NDEKTSV4RRFFQ69G5FAV"); !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("want ErrEndpointNotFound, got %v", err)
	}
}
