package mcp6

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Describe results must flow into ReadyToolSnapshot after a successful
// register probe, and describe failures must never block readiness.
func TestDescribeCachesIntoSnapshot(t *testing.T) {
	describeCalls := 0
	r := newTestRegistry(func(context.Context, *Endpoint) error { return nil }, nil)
	r.SetDescribeFunc(func(_ context.Context, e *Endpoint) (map[string]ToolSchema, error) {
		describeCalls++
		if strings.HasPrefix(e.URL, "https://fail.example.com") {
			return nil, errors.New("catalogue unreachable")
		}
		return map[string]ToolSchema{
			"searchDocs": {Description: "Search project docs", InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)},
		}, nil
	})

	ok, err := r.Register(context.Background(), EndpointInput{Transport: "https", URL: "https://ok.example.com/mcp", AuthRef: "secretref:pool/a", Pin: validPin()})
	if err != nil || ok.State != StateReady {
		t.Fatalf("register ok endpoint: state=%s err=%v", ok.State, err)
	}
	snapshot := r.ReadyToolSnapshot()
	if len(snapshot) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot[0].Description != "Search project docs" {
		t.Fatalf("description = %q", snapshot[0].Description)
	}
	if len(snapshot[0].Schema) == 0 || snapshot[0].Schema[0] != '{' {
		t.Fatalf("schema = %s", snapshot[0].Schema)
	}
	if describeCalls != 1 {
		t.Fatalf("describeCalls = %d", describeCalls)
	}

	// Degraded endpoints stay out of the snapshot entirely.
	bad, err := r.Register(context.Background(), EndpointInput{Transport: "https", URL: "https://fail.example.com/mcp", AuthRef: "secretref:pool/a", Pin: validPin()})
	if err != nil || bad.State != StateReady {
		t.Fatalf("register fail-describe endpoint: state=%s err=%v", bad.State, err)
	}
	if got := len(r.ReadyToolSnapshot()); got != 2 {
		t.Fatalf("snapshot after second register = %d (describe failure must not demote)", got)
	}
	second := r.ReadyToolSnapshot()
	for _, entry := range second {
		if entry.EndpointID == bad.ID && entry.Tool == "searchDocs" {
			if entry.Description != "" || len(entry.Schema) != 0 {
				t.Fatalf("failed describe must leave zero schema, got %+v", entry)
			}
		}
	}
}

// Revoked endpoints disappear from the snapshot even with a warm cache.
func TestSnapshotExcludesRevoked(t *testing.T) {
	r := newTestRegistry(func(context.Context, *Endpoint) error { return nil }, nil)
	e, _ := r.Register(context.Background(), EndpointInput{Transport: "https", URL: "https://example.com/mcp", AuthRef: "secretref:pool/a", Pin: validPin()})
	if _, err := r.Revoke(e.ID, ReasonManual); err != nil {
		t.Fatal(err)
	}
	if got := r.ReadyToolSnapshot(); len(got) != 0 {
		t.Fatalf("snapshot after revoke = %+v", got)
	}
}

// The schema cache is display-only: invoking a tool whose cached schema
// exists but whose pin digest is missing must still answer drift.
func TestSchemaCacheCannotBypassPin(t *testing.T) {
	r := newTestRegistry(func(context.Context, *Endpoint) error { return nil }, nil)
	r.SetDescribeFunc(func(context.Context, *Endpoint) (map[string]ToolSchema, error) {
		return map[string]ToolSchema{"unpinned": {Description: "ghost tool"}}, nil
	})
	e, _ := r.Register(context.Background(), EndpointInput{Transport: "https", URL: "https://example.com/mcp", AuthRef: "secretref:pool/a", Pin: validPin()})
	_, err := r.Invoke(context.Background(), e.ID, "unpinned", map[string]any{})
	if !errors.Is(err, ErrCapabilityDrift) {
		t.Fatalf("want ErrCapabilityDrift, got %v", err)
	}
	if !strings.Contains(err.Error(), "unpinned") {
		t.Fatalf("drift error must name the tool: %v", err)
	}
}
