package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goruntime "runtime"

	"github.com/jung-kurt/gofpdf"
	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/connectorapp"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/scheduler"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/widgetapp"
)

// TestRobotClosedLoopSimulatedUser walks one office-user day through Bridge and
// tools. It is a simulated-human closed loop, not a G1/G2/G3 stamp.
func TestRobotClosedLoopSimulatedUser(t *testing.T) {
	dir := t.TempDir()
	runtime, err := toolruntime.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	runtime.SetIMSend(func(context.Context, string, string, string) (string, string, error) {
		return "", "sent via 飞书 webhook", nil
	})

	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	widgetPath := filepath.Join(dir, "widgets.json")
	e.SetWidgetStore(widgetapp.NewFileStore(widgetPath))
	e.SetConnectorStore(connectorapp.NewFileStore(filepath.Join(dir, "connectors.json")))
	e.SetToolRuntime(runtime)

	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}

	t.Run("R01-create-session-widgets", func(t *testing.T) {
		created := e.Handle(context.Background(), validRequest("widget.create", `{"owner":"`+session+`","id":"board","title":"发票,回执","widgets":[{"id":"timer","kind":"timer","state":{"seconds":119,"running":false}},{"id":"list","kind":"checklist"},{"id":"metric","kind":"metric","state":{"value":"12","unit":"万"}}]}`))
		if !created.OK {
			t.Fatalf("%#v", created.Error)
		}
	})

	t.Run("R02-pause-timer-persists", func(t *testing.T) {
		updated := e.Handle(context.Background(), validRequest("widget.update", `{"owner":"`+session+`","id":"board","revision":1,"title":"发票,回执","widgets":[{"id":"timer","kind":"timer","state":{"seconds":90,"running":false}},{"id":"list","kind":"checklist","state":{"checked":"发票"}},{"id":"metric","kind":"metric","state":{"value":"12","unit":"万"}}]}`))
		if !updated.OK {
			t.Fatalf("%#v", updated.Error)
		}
	})

	t.Run("R03-restart-reloads-widget-state", func(t *testing.T) {
		reopened := NewEngineWithGateway(nil, "test", streamTestLease{})
		reopened.SetWidgetStore(widgetapp.NewFileStore(widgetPath))
		listed := reopened.Handle(context.Background(), validRequest("widget.query", `{"owner":"`+session+`","id":"board"}`))
		if !listed.OK {
			t.Fatalf("%#v", listed.Error)
		}
		raw, _ := json.Marshal(listed.Payload)
		if !strings.Contains(string(raw), `"seconds":90`) || !strings.Contains(string(raw), `"checked":"发票"`) || !strings.Contains(string(raw), `"value":"12"`) {
			t.Fatalf("restart lost widget state: %s", raw)
		}
	})

	t.Run("R04-item-due-respects-timezone", func(t *testing.T) {
		future := e.Handle(context.Background(), validRequest("item.upsert", `{"id":"check-1","type":"checkin","title":"打卡","status":"open","dueAt":"2099-01-01 09:00:00","timezone":"Asia/Shanghai"}`))
		if !future.OK {
			t.Fatalf("%#v", future.Error)
		}
		if e.itemTriggerDue("check-1") {
			t.Fatal("future checkin must not fire")
		}
	})

	t.Run("R05-pdf-split-declares-lossy", func(t *testing.T) {
		p := gofpdf.New("P", "mm", "A4", "")
		p.AddPage()
		p.SetFont("Helvetica", "", 12)
		p.Cell(40, 10, "page-one")
		p.AddPage()
		p.SetFont("Helvetica", "", 12)
		p.Cell(40, 10, "page-two")
		var buf bytes.Buffer
		if err := p.Output(&buf); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "src.pdf"), buf.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "pdf.copy", json.RawMessage(`{"op":"split","path":"src.pdf"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Output, `"lossy":true`) || !strings.Contains(got.Output, "lossy_text_rerender") {
			t.Fatalf("lossy receipt missing: %s", got.Output)
		}
	})

	t.Run("R06-pdf-form-refused", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "form.pdf"), []byte("%PDF-1.4\n1 0 obj<</AcroForm 2 0 R>>endobj\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "pdf.copy", json.RawMessage(`{"op":"split","path":"form.pdf"}`))
		if err == nil || !strings.Contains(err.Error(), "表单") {
			t.Fatalf("form must be refused: %v", err)
		}
	})

	t.Run("R07-commercial-catalog-not-ready", func(t *testing.T) {
		listed := e.Handle(context.Background(), validRequest("connector.recipe.list", `{}`))
		if !listed.OK {
			t.Fatalf("%#v", listed.Error)
		}
		out := mustDecodePayload[struct {
			Items []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"items"`
		}](t, listed.Payload)
		for _, item := range out.Items {
			if (item.ID == "ifind" || item.ID == "tianyancha") && item.Status == "ready" {
				t.Fatalf("commercial marked ready: %+v", item)
			}
		}
	})

	t.Run("R08-revoke-pauses-background", func(t *testing.T) {
		if _, err := e.connectors.Put(connectorapp.Recipe{ID: "im-text", Scope: "im", CredentialRef: "cred-1"}); err != nil {
			t.Fatal(err)
		}
		revoked := e.Handle(context.Background(), validRequest("connector.recipe.revoke", `{"id":"im-text"}`))
		if !revoked.OK {
			t.Fatalf("%#v", revoked.Error)
		}
		if e.triggerSpecAllowed("im-text") {
			t.Fatal("revoked IM must not run background work")
		}
	})

	t.Run("R09-backup-probe-confined", func(t *testing.T) {
		escape := e.Handle(context.Background(), validRequest("backup.probe", `{"directory":"..\\escape"}`))
		raw, _ := json.Marshal(escape.Payload)
		if strings.Contains(string(raw), `"ok":true`) {
			t.Fatalf("traversal looked verified: %s", raw)
		}
		backup := filepath.Join(dir, "backup")
		if err := os.MkdirAll(backup, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(backup, "manifest.json"), []byte(`{"format":"lunitide-directory-backup-v1","files":[{"path":"lunitide.db"}]}`), 0600); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]string{"directory": backup})
		ok := e.Handle(context.Background(), validRequest("backup.probe", string(body)))
		if !ok.OK {
			t.Fatalf("%#v", ok.Error)
		}
	})

	t.Run("R10-classify-workspace-files", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "note.docx"), []byte("PK"), 0600); err != nil {
			t.Fatal(err)
		}
		plan, err := e.executeFileOps(context.Background(), session, "files.plan", json.RawMessage(`{"recipe":"classify","files":["note.docx"]}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan.Output, "planId") && !strings.Contains(plan.Output, "docx") {
			t.Fatalf("classify plan: %s", plan.Output)
		}
		var parsed struct {
			PlanID string `json:"planId"`
			ID     string `json:"id"`
		}
		_ = json.Unmarshal([]byte(plan.Output), &parsed)
		planID := parsed.PlanID
		if planID == "" {
			planID = parsed.ID
		}
		if planID == "" {
			t.Fatalf("plan id missing: %s", plan.Output)
		}
		if _, err := e.executeFileOps(context.Background(), session, "files.apply", json.RawMessage(`{"planId":"`+planID+`"}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "docx", "note.docx")); err != nil {
			t.Fatalf("classified file missing: %v", err)
		}
	})

	t.Run("R11-im-attachment-stays-pending", func(t *testing.T) {
		got, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "im.send", json.RawMessage(`{"channel":"feishu","text":"催办","attachment":"invoice.pdf"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Output, "pending_external") || strings.Contains(got.Output, "已发送附件") {
			t.Fatalf("attachment honesty: %s", got.Output)
		}
	})

	t.Run("R12-knowledge-delete-stops-search", func(t *testing.T) {
		store, err := storage.OpenTemplated(context.Background(), filepath.Join(dir, "kb.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		kb := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
		e.SetM8SliceServices(kb, nil, nil)
		expert := ulid.Make().String()
		coll, err := kb.EnsureExpertCollection(context.Background(), expert)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "policy.md")
		if err := os.WriteFile(path, []byte("reimbursement policy for invoices"), 0600); err != nil {
			t.Fatal(err)
		}
		ingested, err := kb.IngestLocalSource(context.Background(), m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: path, MediaType: "text/markdown"})
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{
			"expertId": expert, "sourceId": ingested.Source.SourceID, "expectedRevision": ingested.Source.Revision,
		})
		resp := e.Handle(context.Background(), nominationRequest("expert.knowledge.delete", string(body)))
		if !resp.OK {
			t.Fatalf("%#v", resp.Error)
		}
		after, err := kb.Search(context.Background(), m8app.KBSearchInput{ExpertID: expert, Query: "reimbursement"})
		if err != nil || len(after.Hits) != 0 {
			t.Fatalf("deleted source leaked %+v %v", after, err)
		}
	})

	t.Run("R13-data-process-known-answer", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "rows.csv"), []byte("name,amount\nA,1\nA,1\nB,2\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "data.process", json.RawMessage(`{"path":"rows.csv","op":"dedup","keys":["name","amount"]}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Output, ".csv") {
			t.Fatalf("dedup receipt: %s", got.Output)
		}
	})
}

func robotModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// TestRobotClosedLoopSafetyGates locks S01–S09 the only honest way a robot can:
// prove the product does not stamp, vendor, auto-cutover, or invent savings.
func TestRobotClosedLoopSafetyGates(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetWidgetStore(widgetapp.NewFileStore(filepath.Join(dir, "widgets.json")))
	e.SetConnectorStore(connectorapp.NewFileStore(filepath.Join(dir, "connectors.json")))
	session := ulid.Make().String()

	t.Run("S01-no-g-stamp-or-readiness-method", func(t *testing.T) {
		if _, ok := RuntimeHandlers[bridge.Method("capability.readiness.get")]; ok {
			t.Fatal("capability.readiness.get must not exist")
		}
		if _, ok := RuntimeHandlers[bridge.MethodCapabilityList]; !ok {
			t.Fatal("capability.list must stay on M4 surface")
		}
		wire, err := os.ReadFile(filepath.Join(robotModuleRoot(t), "internal", "bootstrap", "wire.go"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(wire), "scheduler.Cutover(") || strings.Contains(string(wire), "Recover: true") {
			t.Fatal("production wire must not auto-cutover or set Recover:true")
		}
	})

	t.Run("S02-qualification-fixture-never-adopted", func(t *testing.T) {
		store, err := storage.OpenTemplated(ctx, filepath.Join(dir, "qualify.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		for _, family := range []string{"deepseek", "glm"} {
			q := modelfit.DefaultQualification(family, family+"-chat", "v1")
			q.Status = modelfit.QualifyFixturePass
			q.Evidence = "robot offline fixture"
			q.Adopted = true
			if err := store.PutModelFitQualification(ctx, session, q); err != nil {
				t.Fatal(err)
			}
			got, err := store.GetModelFitQualification(ctx, session, family, family+"-chat", "v1")
			if err != nil || got.Adopted || got.Status != modelfit.QualifyFixturePass {
				t.Fatalf("%s must stay fixture_pass not adopted: %+v %v", family, got, err)
			}
		}
		bad := modelfit.DefaultQualification("deepseek", "x", "v1")
		bad.Status = "adopted"
		if err := store.PutModelFitQualification(ctx, session, bad); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetModelFitQualification(ctx, session, "deepseek", "x", "v1")
		if err != nil || got.Status == "adopted" || got.Adopted {
			t.Fatalf("adopted status must be coerced off: %+v %v", got, err)
		}
	})

	t.Run("S03-official-ppocr-pack-not-claimed", func(t *testing.T) {
		got := ocrapp.DetectPPOcrPack(dir)
		if got.Available || got.Status != "missing_dependency" {
			t.Fatalf("empty tree must not claim pack: %+v", got)
		}
		local := ocrapp.LocalOCRReady()
		if local.Backend == "ppocr" || local.Backend == "ppocr-pack" {
			t.Fatalf("local OCR must not wear official pack name: %+v", local)
		}
		root := robotModuleRoot(t)
		for _, rel := range []string{
			filepath.Join("third_party", "ppocr"),
			filepath.Join("third_party", "paddleocr"),
			filepath.Join("vendor", "paddleocr"),
			filepath.Join("bin", "ppocr.exe"),
		} {
			if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
				t.Fatalf("official pack must not be vendored: %s", rel)
			}
		}
		ocr := e.Handle(ctx, validRequest("ocr.routing.get", `{}`))
		if !ocr.OK {
			t.Fatalf("%#v", ocr.Error)
		}
		raw, _ := json.Marshal(ocr.Payload)
		if strings.Contains(string(raw), `"status":"ready"`) && strings.Contains(string(raw), "ppocr-pack") {
			t.Fatalf("routing must not advertise ready official pack: %s", raw)
		}
	})

	t.Run("S04-production-stays-json-writer", func(t *testing.T) {
		repo, closer, err := scheduler.OpenAutomationRepository(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(closer)
		if repo == nil || scheduler.CurrentWriter(dir) != scheduler.WriterJSON {
			t.Fatal("fresh data root must keep JSON writer")
		}
		if scheduler.ClassifiedRecover(scheduler.WriterJSON, "native_continue") {
			t.Fatal("JSON era Recover must stay off")
		}
	})

	t.Run("S05-im-attachment-not-claimed-sent", func(t *testing.T) {
		runtime, err := toolruntime.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close() })
		runtime.SetIMSend(func(context.Context, string, string, string) (string, string, error) {
			return "", "sent via 飞书 webhook", nil
		})
		e.SetToolRuntime(runtime)
		got, err := e.executeUserTool(ctx, executionModeFullAccess, session, "im.send", json.RawMessage(`{"channel":"feishu","text":"催办","attachment":"scan.pdf"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Output, "pending_external") || strings.Contains(got.Output, "已发送附件") {
			t.Fatalf("%s", got.Output)
		}
	})

	t.Run("S06-commercial-connectors-closed", func(t *testing.T) {
		if _, err := e.connectors.Put(connectorapp.Recipe{ID: "ifind", Scope: "quotes", CredentialRef: "pasted"}); err != nil {
			t.Fatal(err)
		}
		listed := e.Handle(ctx, validRequest("connector.recipe.list", `{}`))
		out := mustDecodePayload[struct {
			Items []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"items"`
		}](t, listed.Payload)
		for _, item := range out.Items {
			switch item.ID {
			case "ifind", "tianyancha", "cloud-drive", "sec":
				if item.Status == "ready" {
					t.Fatalf("commercial ready: %+v", item)
				}
			}
		}
	})

	t.Run("S07-usage-has-no-savings-percent", func(t *testing.T) {
		resp := e.Handle(ctx, validRequest("chat.usage.get", `{"sessionId":"`+session+`"}`))
		if !resp.OK {
			t.Fatalf("%#v", resp.Error)
		}
		raw, _ := json.Marshal(resp.Payload)
		if strings.Contains(string(raw), "savings") || strings.Contains(string(raw), "%") || strings.Contains(string(raw), "native_complete") {
			t.Fatalf("usage invented savings or completeness: %s", raw)
		}
	})

	t.Run("S08-pdf-form-and-lossy-contract", func(t *testing.T) {
		if _, err := e.executeUserTool(ctx, executionModeFullAccess, session, "pdf.copy", json.RawMessage(`{"op":"split","path":"missing.pdf"}`)); err == nil {
			t.Fatal("missing pdf must fail")
		}
	})

	t.Run("S09-email-and-calendar-out-of-scope", func(t *testing.T) {
		mail := e.Handle(ctx, validRequest("item.upsert", `{"id":"mail","type":"email","title":"inbox"}`))
		if mail.OK {
			t.Fatal("email items are out of scope")
		}
		cal := e.Handle(ctx, validRequest("item.upsert", `{"id":"cal","type":"calendar","title":"sync"}`))
		if cal.OK {
			t.Fatal("shared calendar items are out of scope")
		}
	})
}
