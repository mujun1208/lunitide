package mcp6

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestRevokeCancelsInflightAndRejectsRetainedTool(t *testing.T) {
	started := make(chan struct{})
	r := newTestRegistry(nil, func(ctx context.Context, _ *Endpoint, _ string, _ map[string]any, _ []byte) (map[string]any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	var evicted atomic.Int64
	r.SetRevokeHook(func(string) { evicted.Add(1) })
	ep, err := r.Register(context.Background(), EndpointInput{Transport: "stdio", Command: "npx", Args: []string{"fixture"}, Pin: validPin()})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := r.Invoke(context.Background(), ep.ID, "searchDocs", nil); done <- err }()
	<-started
	if _, err := r.Revoke(ep.ID, ReasonManual); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrEndpointRevoked) {
		t.Fatalf("in-flight call: %v", err)
	}
	if _, err := r.Invoke(context.Background(), ep.ID, "searchDocs", nil); !errors.Is(err, ErrEndpointRevoked) {
		t.Fatalf("retained tool: %v", err)
	}
	if evicted.Load() != 1 {
		t.Fatal("connection teardown hook not invoked")
	}
}

func TestRegisterRetriesDegradedEndpointAndSnapshotsCannotMutateRegistry(t *testing.T) {
	failing := true
	r := newTestRegistry(func(context.Context, *Endpoint) error {
		if failing {
			return errors.New("offline")
		}
		return nil
	}, nil)
	input := EndpointInput{ID: "fixture", Transport: "stdio", Command: "npx", Args: []string{"fixture"}, Pin: validPin()}
	ep, err := r.Register(context.Background(), input)
	if !errors.Is(err, ErrHealthCheckFailed) || ep.State != StateDegraded {
		t.Fatalf("first probe: %+v %v", ep, err)
	}
	failing = false
	ep, err = r.Register(context.Background(), input)
	if err != nil || ep.State != StateReady {
		t.Fatalf("reconnect: %+v %v", ep, err)
	}
	ep.Args[0] = "tampered"
	ep.Pin.ToolSchemaDigests["other"] = "tampered"
	input.Pin.ToolSchemaDigests["other2"] = "tampered"
	current, _ := r.Get(ep.ID)
	if current.Args[0] != "fixture" || len(current.Pin.ToolSchemaDigests) != 1 {
		t.Fatal("returned snapshot changed authorization")
	}
}
