package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestSystemDiagnosticsIdentifiesStorageAndAuditFailure(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetStorageReadiness(store)
	e.SetAuditChainVerifier(store)
	e.SetToolRuntime(runtime)
	check := func() map[string]diagnosticComponent {
		t.Helper()
		r := validRequest("system.diagnostics", `{}`)
		resp := e.Handle(ctx, r)
		if !resp.OK {
			t.Fatalf("%+v", resp.Error)
		}
		var out struct {
			TraceID    string                `json:"traceId"`
			Components []diagnosticComponent `json:"components"`
		}
		raw, _ := json.Marshal(resp.Payload)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if out.TraceID != r.TraceID {
			t.Fatal("trace correlation lost")
		}
		items := map[string]diagnosticComponent{}
		for _, item := range out.Components {
			items[item.ID] = item
		}
		return items
	}
	items := check()
	if items["storage"].State != "healthy" || items["audit"].State != "healthy" || items["tool_policy"].State != "healthy" {
		t.Fatalf("healthy check: %+v", items)
	}
	if items["voice_local"].State != "not_configured" || items["computer"].State != "not_configured" {
		t.Fatal("unconfigured runtimes claimed ready")
	}
	e.SetAuditChainVerifier(stubAuditVerifier{err: audit.ErrChainBroken})
	if check()["audit"].Code != "AUDIT_CHAIN_BROKEN" {
		t.Fatal("audit tamper not identified")
	}
	store.Close()
	if check()["storage"].State != "degraded" {
		t.Fatal("closed database reported healthy")
	}
}
