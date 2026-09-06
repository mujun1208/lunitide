package mcp6

import (
	"context"
	"errors"
	"github.com/lunitide/lunitide/internal/secret"
	"os"
	"testing"
	"time"
)

type fixtureLeaseSecrets struct {
	binding  secret.Ref
	bytes    []byte
	released []byte
}

func (s *fixtureLeaseSecrets) Put(context.Context, secret.Ref, []byte) error {
	return errors.New("unused")
}
func (s *fixtureLeaseSecrets) Delete(context.Context, secret.Ref) error { return errors.New("unused") }
func (s *fixtureLeaseSecrets) WithSecret(ctx context.Context, ref secret.Ref, fn func([]byte) error) error {
	if ref != s.binding {
		return os.ErrNotExist
	}
	copy := append([]byte(nil), s.bytes...)
	s.released = copy
	defer secret.Zero(copy)
	return fn(copy)
}
func TestMcpCredentialLeaseIsScopedBoundedAndZeroed(t *testing.T) {
	ep := &Endpoint{ID: "fixture", Transport: "https", URL: "https://fixture.invalid/mcp", AuthRef: "secretref:mcp/fixture/token"}
	binding, err := CredentialBinding(ep, ep.AuthRef)
	if err != nil {
		t.Fatal(err)
	}
	secrets := &fixtureLeaseSecrets{binding: binding, bytes: []byte("fixture-token")}
	lease := SecretCredentialLease(secrets)
	if err = lease(context.Background(), ep, func(c Credentials) error {
		deadline, ok := c.Context.Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Fatal("unbounded lease")
		}
		if string(c.Bearer) != "fixture-token" {
			t.Fatal("missing token")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, b := range secrets.released {
		if b != 0 {
			t.Fatal("credential copy retained")
		}
	}
	ep.ID = "another-endpoint"
	called := false
	err = lease(context.Background(), ep, func(Credentials) error { called = true; return nil })
	if !errors.Is(err, ErrCredentialRevoked) || called {
		t.Fatalf("cross-endpoint secret allowed: %v", err)
	}
}
