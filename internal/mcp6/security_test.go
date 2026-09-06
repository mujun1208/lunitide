package mcp6

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func secureFixtureCatalogue() Catalogue {
	return Catalogue{Identity: "fixture@1/protocol", Tools: map[string]ToolSchema{"lookup": {Description: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
}

func TestSecureMcpRedactsEchoedCredentialsBeforeLeaseEnds(t *testing.T) {
	ctx := context.Background()
	catalog := secureFixtureCatalogue()
	r := NewRegistry(nil, nil, nil)
	secret := []byte("fixture-<secret>&token")
	env := []byte("fixture-env-credential")
	r.SetSecurityAdapters(func(ctx context.Context, _ *Endpoint, fn func(Credentials) error) error {
		return fn(Credentials{Context: ctx, Bearer: secret, Env: map[string][]byte{"TOKEN": env}})
	}, func(context.Context, *Endpoint, Credentials) (Catalogue, error) { return catalog, nil }, func(_ context.Context, _ *Endpoint, _ string, _ map[string]any, c Credentials) (map[string]any, error) {
		return map[string]any{"echo": string(c.Bearer), "nested": map[string]any{"credential": string(c.Env["TOKEN"])}}, nil
	})
	if _, err := r.Register(ctx, EndpointInput{ID: "redaction-fixture", Transport: "https", URL: "https://fixture.invalid", Pin: BootstrapPin("fixture")}); err != nil {
		t.Fatal(err)
	}
	result, err := r.Invoke(ctx, "redaction-fixture", "lookup", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "fixture-") || !strings.Contains(string(raw), "REDACTED") {
		t.Fatalf("credential echo escaped redaction: %s", raw)
	}
}

func TestSecureMcpPinDriftAndCredentialRotation(t *testing.T) {
	ctx := context.Background()
	catalog := secureFixtureCatalogue()
	var calls, leases int
	r := NewRegistry(nil, nil, nil)
	lease := func(_ context.Context, e *Endpoint, fn func(Credentials) error) error {
		leases++
		if e.AuthRef != "secretref:fixture/new" && e.AuthRef != "secretref:fixture/old" {
			return ErrCredentialRevoked
		}
		return fn(Credentials{Bearer: []byte("fixture-secret")})
	}
	r.SetSecurityAdapters(lease, func(context.Context, *Endpoint, Credentials) (Catalogue, error) { return catalog, nil }, func(_ context.Context, e *Endpoint, _ string, _ map[string]any, c Credentials) (map[string]any, error) {
		if err := catalog.Verify(e.Pin); err != nil {
			return nil, err
		}
		if e.AuthRef == "secretref:fixture/old" {
			return nil, ErrCredentialRevoked
		}
		calls++
		return map[string]any{"ok": true}, nil
	})
	in := EndpointInput{ID: "fixture", Transport: "https", URL: "https://fixture.invalid", AuthRef: "secretref:fixture/old", Pin: BootstrapPin("fixture")}
	ep, err := r.Register(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	pin := ep.Pin
	if _, err = r.Invoke(ctx, in.ID, "lookup", nil); !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("401=%v", err)
	}
	in.AuthRef = "secretref:fixture/new"
	in.Pin = pin
	in.SecurityVersion = 1
	if _, err = r.Register(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Invoke(ctx, in.ID, "lookup", nil); err != nil {
		t.Fatal(err)
	}
	catalog.Tools["lookup"] = ToolSchema{InputSchema: json.RawMessage(`{"type":"object","required":["write"]}`)}
	if _, err = r.Invoke(ctx, in.ID, "lookup", nil); !errors.Is(err, ErrCapabilityDrift) {
		t.Fatalf("drift=%v", err)
	}
	if _, err = r.Probe(ctx, in.ID); !errors.Is(err, ErrEndpointRevoked) {
		t.Fatalf("automatic repin=%v", err)
	}
	if calls != 1 || leases != 5 {
		t.Fatalf("calls=%d leases=%d", calls, leases)
	}
}

func TestSecureMcpCatalogueCanonicalAndIdentityBound(t *testing.T) {
	c := secureFixtureCatalogue()
	p, err := c.Pin()
	if err != nil {
		t.Fatal(err)
	}
	c.Tools["lookup"] = ToolSchema{Description: "lookup", InputSchema: json.RawMessage("{\n \"type\" : \"object\" }")}
	if err = c.Verify(p); err != nil {
		t.Fatal(err)
	}
	c.Identity = "fixture@2/protocol"
	if !errors.Is(c.Verify(p), ErrCapabilityDrift) {
		t.Fatal("identity change accepted")
	}
	c = secureFixtureCatalogue()
	c.Tools["extra"] = c.Tools["lookup"]
	if !errors.Is(c.Verify(p), ErrCapabilityDrift) {
		t.Fatal("added unreviewed tool accepted")
	}
}

func TestMcpScopeRevocationDropsLateSuccess(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	started := make(chan struct{})
	var reached atomic.Bool
	r := NewRegistry(nil, nil, nil)
	catalog := secureFixtureCatalogue()
	r.SetSecurityAdapters(func(ctx context.Context, _ *Endpoint, fn func(Credentials) error) error {
		return fn(Credentials{Context: ctx})
	}, func(context.Context, *Endpoint, Credentials) (Catalogue, error) { return catalog, nil }, func(ctx context.Context, _ *Endpoint, _ string, _ map[string]any, _ Credentials) (map[string]any, error) {
		close(started)
		<-ctx.Done()
		reached.Store(true)
		return map[string]any{"late": "success"}, nil
	})
	r.SetInvokeScope(func(context.Context, *Endpoint) (context.Context, func(), error) { return ctx, func() {}, nil })
	ep, err := r.Register(context.Background(), EndpointInput{Transport: "https", URL: "https://fixture.invalid", Pin: BootstrapPin("fixture")})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		result, err := r.Invoke(context.Background(), ep.ID, "lookup", nil)
		if result != nil {
			done <- errors.New("late result delivered")
			return
		}
		done <- err
	}()
	<-started
	denied := errors.New("capability withdrawn")
	cancel(denied)
	if err := <-done; !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if !reached.Load() {
		t.Fatal("adapter not exercised")
	}
}
