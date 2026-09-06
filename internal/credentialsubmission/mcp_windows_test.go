//go:build windows

package credentialsubmission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/secret"
)

type mcpCredentialEngine struct {
	target  mcpCredentialTarget
	loseACK bool
	binds   int
}

func (e *mcpCredentialEngine) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	switch r.Method {
	case "internal.mcp.credential.resolve":
		return bridge.Success(r.ID, e.target), nil
	case "internal.mcp.credential.bind":
		var p struct {
			ExpectedVersion int64  `json:"expectedVersion"`
			Ref             string `json:"ref"`
			Env             string `json:"env"`
		}
		if json.Unmarshal(r.Payload, &p) != nil {
			return bridge.Response{}, errors.New("invalid")
		}
		if p.ExpectedVersion != e.target.SecurityVersion {
			return bridge.Failure(r.ID, r.TraceID, "CONFLICT", "changed", false), nil
		}
		e.binds++
		e.target.SecurityVersion++
		if p.Env == "" {
			e.target.AuthRef = p.Ref
		} else {
			e.target.EnvRefs[p.Env] = p.Ref
		}
		if e.loseACK {
			e.loseACK = false
			return bridge.Response{}, errors.New("fixture lost ACK after commit")
		}
		return bridge.Success(r.ID, map[string]any{"securityVersion": e.target.SecurityVersion}), nil
	}
	return bridge.Response{}, errors.New("unexpected method")
}
func mcpCredentialHost(t *testing.T) (*HostHandler, *mcpCredentialEngine, *memorySecrets) {
	c, secrets, _ := testCoordinator(t)
	engine := &mcpCredentialEngine{target: mcpCredentialTarget{EndpointID: "mcp-fixture", RuntimeID: "fixture", Transport: "https", URL: "https://fixture.invalid/mcp", State: "ready", EnvRefs: map[string]string{}}}
	h := &HostHandler{Coordinator: c, Secrets: secrets, Engine: engine, McpConfirm: func(context.Context, RevealTarget, string, bool) (bool, error) { return true, nil }}
	return h, engine, secrets
}
func callMcpCredential(t *testing.T, h *HostHandler, p mcpCredentialRequest) bridge.Response {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return h.HandleHost(context.Background(), bridge.Request{ID: "fixture-request", Method: "mcp.credential.set", Payload: raw})
}

func TestMcpCredentialLostACKRetryRotationAndRevoke(t *testing.T) {
	h, engine, secrets := mcpCredentialHost(t)
	engine.loseACK = true
	p := mcpCredentialRequest{EndpointID: engine.target.EndpointID, Credential: "fixture-old-token", RequestID: "01234567-0123-0123-0123-0123456789ab"}
	if res := callMcpCredential(t, h, p); res.OK {
		t.Fatal("lost ACK falsely succeeded")
	}
	if engine.binds != 1 {
		t.Fatal("missing commit")
	}
	if res := callMcpCredential(t, h, p); !res.OK {
		t.Fatalf("replay: %+v", res.Error)
	}
	if engine.binds != 1 {
		t.Fatal("duplicate commit")
	}
	files, err := os.ReadDir(h.Coordinator.root.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "mcp-credential-") {
			path, _ := h.Coordinator.root.FilePath(file.Name())
			raw, _ := os.ReadFile(path)
			if strings.Contains(string(raw), p.Credential) {
				t.Fatal("plaintext in journal")
			}
		}
	}
	different := p
	different.Credential = "fixture-different"
	if res := callMcpCredential(t, h, different); res.OK {
		t.Fatal("same ID accepted different credential")
	}
	p.ExpectedVersion = 1
	p.RequestID = "01234567-0123-0123-0123-0123456789ac"
	p.Credential = "fixture-new-token"
	if res := callMcpCredential(t, h, p); !res.OK {
		t.Fatalf("rotate: %+v", res.Error)
	}
	if err := h.ReconcileMcpCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if secrets.count() != 1 {
		t.Fatalf("retired key retained: %d", secrets.count())
	}
	p.ExpectedVersion = 2
	p.RequestID = "01234567-0123-0123-0123-0123456789ad"
	p.Credential = ""
	p.Remove = true
	if res := callMcpCredential(t, h, p); !res.OK {
		t.Fatalf("revoke: %+v", res.Error)
	}
	if err := h.ReconcileMcpCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if secrets.count() != 0 || engine.target.AuthRef != "" {
		t.Fatal("revoked key remains")
	}
}

func TestMcpCredentialConfirmationAndOrphanRecovery(t *testing.T) {
	h, engine, secrets := mcpCredentialHost(t)
	p := mcpCredentialRequest{EndpointID: engine.target.EndpointID, Credential: "fixture-token", RequestID: "01234567-0123-0123-0123-0123456789ab"}
	h.McpConfirm = func(context.Context, RevealTarget, string, bool) (bool, error) { return false, nil }
	if res := callMcpCredential(t, h, p); res.OK || engine.binds != 0 || secrets.count() != 0 {
		t.Fatal("denied Host confirmation changed state")
	}
	p.Credential = ""
	entry := mcpCredentialJournal{Request: p, Binding: secret.Ref{CredentialRef: "secretref:mcp/fixture/orphan", ProviderID: "mcp:fixture", Origin: "https://fixture.invalid", Protocol: "mcp"}, ExpiresAt: time.Now().Add(-time.Minute)}
	if err := secrets.Put(context.Background(), entry.Binding, []byte("orphan")); err != nil {
		t.Fatal(err)
	}
	if err := h.saveMcpJournal("mcp-credential-"+p.RequestID+".json", entry); err != nil {
		t.Fatal(err)
	}
	if err := h.ReconcileMcpCredentials(context.Background()); err != nil {
		t.Fatal(err)
	}
	if secrets.count() != 0 {
		t.Fatal("orphan not recovered")
	}
}
