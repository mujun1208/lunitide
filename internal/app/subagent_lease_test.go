package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
)

// Mirrors the real DPAPI lease lifecycle without touching actual credentials.
type subagentZeroingLease struct {
	secret []byte
	active bool
	calls  int
}

func (l *subagentZeroingLease) WithLease(_ context.Context, _ secretlease.Request, callback func([]byte) error) error {
	l.calls++
	l.secret = []byte("isolated-test-credential")
	l.active = true
	defer func() { clear(l.secret); l.active = false }()
	return callback(l.secret)
}

type subagentLeaseAdapter struct {
	subagentFakeAdapter
	lease *subagentZeroingLease
	calls int
}

func (a *subagentLeaseAdapter) Complete(ctx context.Context, cred []byte, req llmadapter.Request) (llmadapter.Response, error) {
	a.calls++
	if !a.lease.active || string(cred) != "isolated-test-credential" {
		return llmadapter.Response{}, errors.New("used a credential after its lease ended")
	}
	if req.Model != "child-model" {
		return llmadapter.Response{}, errors.New("wrong override model")
	}
	if err := ctx.Err(); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: "override provider completed inside lease"}}, nil
}

func TestSubagentProviderOverrideRunsInsideCredentialLease(t *testing.T) {
	e := newSubagentChatEngine(t)
	e.providers = compactionProviderService{}
	lease := &subagentZeroingLease{}
	e.leases = lease
	child := &subagentLeaseAdapter{lease: lease}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return child, nil })
	policy := subTestPolicy()
	policy.Overrides = map[string]subagentProfileOverride{"research": {ProviderID: compactionProviderID, ModelID: "child-model"}}
	parent := &subagentFakeAdapter{}
	out, err := e.invokeSubagentTool(context.Background(), parent, []byte("parent-test-key"), "parent-model", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"use the configured child provider","profile":"research"}`), policy)
	if err != nil || !strings.Contains(out, "override provider completed inside lease") {
		t.Fatalf("override failed: %s %v", out, err)
	}
	if lease.calls != 1 || lease.active || child.calls != 1 || parent.calls != 0 {
		t.Fatalf("lifetime/call counts: lease=%+v child=%d parent=%d", lease, child.calls, parent.calls)
	}
	for _, b := range lease.secret {
		if b != 0 {
			t.Fatal("test credential not cleared after child finished")
		}
	}
}

func TestSubagentMissingOverrideProviderDoesNotSilentlyUseParent(t *testing.T) {
	e := newSubagentChatEngine(t)
	e.providers = providerRepositoryStub{}
	parent := &subagentFakeAdapter{}
	policy := subTestPolicy()
	policy.Overrides = map[string]subagentProfileOverride{"research": {ProviderID: compactionProviderID, ModelID: "child-model"}}
	if _, err := e.invokeSubagentTool(context.Background(), parent, nil, "parent-model", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"configured provider is missing","profile":"research"}`), policy); err == nil {
		t.Fatal("missing provider silently accepted")
	}
	if parent.calls != 0 {
		t.Fatal("missing provider silently called parent provider")
	}
}
