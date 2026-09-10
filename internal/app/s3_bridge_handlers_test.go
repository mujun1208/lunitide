package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/connectorapp"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/widgetapp"
)

func TestBackupProbeRejectsTraversalAndReadsFixture(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	bad := e.Handle(context.Background(), validRequest("backup.probe", `{"directory":"..\\escape"}`))
	if !bad.OK {
		t.Fatalf("probe should return closed result not schema fail: %#v", bad.Error)
	}
	raw, _ := json.Marshal(bad.Payload)
	if !strings.Contains(string(raw), `"ok":false`) {
		t.Fatalf("traversal must not look verified: %s", raw)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"format":"lunitide-directory-backup-v1","files":[{"path":"lunitide.db"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"directory": dir})
	ok := e.Handle(context.Background(), validRequest("backup.probe", string(body)))
	if !ok.OK {
		t.Fatalf("%#v", ok.Error)
	}
	out := mustDecodePayload[struct {
		OK     bool   `json:"ok"`
		Format string `json:"format"`
		Files  int    `json:"files"`
	}](t, ok.Payload)
	if !out.OK || out.Format != "lunitide-directory-backup-v1" || out.Files != 1 {
		t.Fatalf("probe %+v", out)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"format":"lunitide-directory-backup-v1","files":[{"path":"../escape"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	escaped := e.Handle(context.Background(), validRequest("backup.probe", string(body)))
	rawEscape, _ := json.Marshal(escaped.Payload)
	if strings.Contains(string(rawEscape), `"ok":true`) {
		t.Fatalf("manifest traversal must not look verified: %s", rawEscape)
	}
}

func TestWidgetAndItemBridgeRoundTrip(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetWidgetStore(widgetapp.NewFileStore(filepath.Join(t.TempDir(), "w.json")))
	created := e.Handle(context.Background(), validRequest("widget.create", `{"owner":"u1","id":"w1","title":"timer","widgets":[{"id":"a","kind":"timer"}]}`))
	if !created.OK {
		t.Fatalf("%#v", created.Error)
	}
	html := e.Handle(context.Background(), validRequest("widget.create", `{"owner":"u1","id":"bad","title":"x","widgets":[{"id":"a","kind":"html"}]}`))
	if html.OK {
		t.Fatal("html kind must fail schema")
	}
	listed := e.Handle(context.Background(), validRequest("widget.query", `{"owner":"u1"}`))
	if !listed.OK {
		t.Fatalf("%#v", listed.Error)
	}
	query := mustDecodePayload[struct {
		Items []struct {
			ID      string `json:"id"`
			Widgets []struct {
				Kind string `json:"kind"`
			} `json:"widgets"`
		} `json:"items"`
	}](t, listed.Payload)
	if len(query.Items) != 1 || len(query.Items[0].Widgets) != 1 || query.Items[0].Widgets[0].Kind != "timer" {
		t.Fatalf("query must keep registered widget kinds: %+v", query)
	}
	upsert := e.Handle(context.Background(), validRequest("item.upsert", `{"id":"i1","type":"bill","title":"rent"}`))
	if !upsert.OK {
		t.Fatalf("%#v", upsert.Error)
	}
	archived := e.Handle(context.Background(), validRequest("item.archive", `{"id":"i1"}`))
	out := mustDecodePayload[struct {
		Status string `json:"status"`
	}](t, archived.Payload)
	if out.Status != "stopped" {
		t.Fatalf("archive %+v %#v", out, archived.Error)
	}
	if !e.itemTriggerDue("missing-file-path") {
		t.Fatal("unknown spec must not block file-set triggers")
	}
	if e.itemTriggerDue("i1") {
		t.Fatal("archived item must not stay due")
	}
}

func TestWidgetBridgePersistsWidgetState(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetWidgetStore(widgetapp.NewFileStore(filepath.Join(t.TempDir(), "w.json")))
	created := e.Handle(context.Background(), validRequest("widget.create", `{"owner":"u1","id":"w1","title":"倒计时","widgets":[{"id":"a","kind":"timer","state":{"seconds":119,"running":true,"updatedAt":"2026-09-10T12:00:00Z"}}]}`))
	if !created.OK {
		t.Fatalf("create %#v", created.Error)
	}
	listed := e.Handle(context.Background(), validRequest("widget.query", `{"owner":"u1","id":"w1"}`))
	if !listed.OK {
		t.Fatalf("query %#v", listed.Error)
	}
	query := mustDecodePayload[struct {
		Items []struct {
			Revision int `json:"revision"`
			Widgets  []struct {
				ID    string `json:"id"`
				Kind  string `json:"kind"`
				State struct {
					Seconds int  `json:"seconds"`
					Running bool `json:"running"`
				} `json:"state"`
			} `json:"widgets"`
		} `json:"items"`
	}](t, listed.Payload)
	if len(query.Items) != 1 || len(query.Items[0].Widgets) != 1 || query.Items[0].Widgets[0].State.Seconds != 119 || !query.Items[0].Widgets[0].State.Running {
		t.Fatalf("query must return persisted state: %+v", query)
	}
	updated := e.Handle(context.Background(), validRequest("widget.update", `{"owner":"u1","id":"w1","revision":1,"title":"倒计时","widgets":[{"id":"a","kind":"timer","state":{"seconds":60,"running":false}}]}`))
	if !updated.OK {
		t.Fatalf("update %#v", updated.Error)
	}
	again := e.Handle(context.Background(), validRequest("widget.query", `{"owner":"u1","id":"w1"}`))
	next := mustDecodePayload[struct {
		Items []struct {
			Widgets []struct {
				State struct {
					Seconds int  `json:"seconds"`
					Running bool `json:"running"`
				} `json:"state"`
			} `json:"widgets"`
		} `json:"items"`
	}](t, again.Payload)
	if next.Items[0].Widgets[0].State.Seconds != 60 || next.Items[0].Widgets[0].State.Running {
		t.Fatalf("update must persist paused state: %+v", next)
	}
}

func TestPausedConnectorDoesNotAllowBackgroundTrigger(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetConnectorStore(connectorapp.NewFileStore(filepath.Join(t.TempDir(), "c.json")))
	if _, err := e.connectors.Put(connectorapp.Recipe{ID: "im-text", Scope: "im", CredentialRef: "cred-1"}); err != nil {
		t.Fatal(err)
	}
	if !e.triggerSpecAllowed("im-text") {
		t.Fatal("ready connector may run background work")
	}
	if _, err := e.RevokeConnectorCredential(context.Background(), "im-text", ""); err != nil {
		t.Fatal(err)
	}
	if e.triggerSpecAllowed("im-text") {
		t.Fatal("revoked connector must not start background work")
	}
	if !e.triggerSpecAllowed(`C:\inbox`) {
		t.Fatal("file-set path must stay allowed")
	}
}

func TestConnectorRecipeListNeverMarksCommercialReady(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetConnectorStore(connectorapp.NewFileStore(filepath.Join(t.TempDir(), "c.json")))
	resp := e.Handle(context.Background(), validRequest("connector.recipe.list", `{}`))
	if !resp.OK {
		t.Fatalf("%#v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), `"id":"ifind"`) && strings.Contains(string(raw), `"status":"ready"`) {
		if strings.Contains(string(raw), `"id":"ifind","scope":"quotes","revision":0,"status":"ready"`) {
			t.Fatalf("ifind must not be ready: %s", raw)
		}
	}
	out := mustDecodePayload[struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}](t, resp.Payload)
	for _, item := range out.Items {
		if (item.ID == "ifind" || item.ID == "tianyancha" || item.ID == "sec") && item.Status == "ready" {
			t.Fatalf("commercial feed marked ready: %+v", item)
		}
	}
	if len(out.Items) == 0 {
		t.Fatal("catalog must be visible")
	}
	if _, err := e.connectors.Put(connectorapp.Recipe{ID: "im-text", Scope: "im", CredentialRef: "cred-1"}); err != nil {
		t.Fatal(err)
	}
	after := e.Handle(context.Background(), validRequest("connector.recipe.list", `{}`))
	listed := mustDecodePayload[struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}](t, after.Payload)
	foundIM, foundIfind := false, false
	for _, item := range listed.Items {
		if item.ID == "im-text" && item.Status == "ready" {
			foundIM = true
		}
		if item.ID == "ifind" {
			foundIfind = true
			if item.Status == "ready" {
				t.Fatal("ifind must stay closed after a user recipe is saved")
			}
		}
	}
	if !foundIM || !foundIfind {
		t.Fatalf("saving IM must not hide the closed catalog: %+v", listed.Items)
	}
}

func TestConnectorRecipeRevokePausesBackgroundAndInFlight(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetConnectorStore(connectorapp.NewFileStore(filepath.Join(t.TempDir(), "c.json")))
	if _, err := e.connectors.Put(connectorapp.Recipe{ID: "im-text", Scope: "im", CredentialRef: "cred-1"}); err != nil {
		t.Fatal(err)
	}
	resp := e.Handle(context.Background(), validRequest("connector.recipe.revoke", `{"id":"im-text"}`))
	if !resp.OK {
		t.Fatalf("revoke %#v", resp.Error)
	}
	out := mustDecodePayload[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Paused bool   `json:"paused"`
	}](t, resp.Payload)
	if out.Status == "ready" || !out.Paused {
		t.Fatalf("revoke must pause: %+v", out)
	}
	if e.triggerSpecAllowed("im-text") {
		t.Fatal("revoked recipe must not allow background work")
	}
}

func TestRevokeConnectorCancelsInFlightOperations(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetConnectorStore(connectorapp.NewFileStore(filepath.Join(t.TempDir(), "c.json")))
	ops := &memToolOps{}
	e.SetToolOperationStore(ops)
	owner := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	op := modelfit.ToolOperation{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", OwnerScope: owner, ToolName: "web.fetch",
		ExternalID: "im-text", State: modelfit.OpRunning, ExpectedVersion: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := ops.PutToolOperationIntent(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if _, err := e.connectors.Put(connectorapp.Recipe{ID: "im-text", Scope: "im", CredentialRef: "cred-1"}); err != nil {
		t.Fatal(err)
	}
	got, err := e.RevokeConnectorCredential(context.Background(), "im-text", owner)
	if err != nil || got.Status != "missing_credential" || !got.Paused {
		t.Fatalf("revoke %+v %v", got, err)
	}
	listed, err := ops.ListToolOperations(context.Background(), owner, 10)
	if err != nil || len(listed) != 1 || listed[0].State != modelfit.OpCancelled || listed[0].CancellationRequestedAt == "" {
		t.Fatalf("in-flight connector work must stop: %+v %v", listed, err)
	}
}
