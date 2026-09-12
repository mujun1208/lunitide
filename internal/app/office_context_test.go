package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestOfficeChatInstructionForbidsInventedMetricsAndSpecRebuild(t *testing.T) {
	if !strings.Contains(officeChatInstruction, "schemaVersion may be 1 or 2") {
		t.Fatal("chat must know v2 generate")
	}
	if !strings.Contains(officeChatInstruction, "savings") || !strings.Contains(officeChatInstruction, "not be reconstructed from Spec") {
		t.Fatal("chat must forbid invented savings and spec rebuild of imports")
	}
	if !strings.Contains(officeChatInstruction, "templateId") || !strings.Contains(officeChatInstruction, "brand-pitch") {
		t.Fatal("chat must map Studio styles to templateId")
	}
	if !strings.Contains(officeChatInstruction, "kind=pptx") || !strings.Contains(officeChatInstruction, "research-report") || !strings.Contains(officeChatInstruction, "ops-ledger") {
		t.Fatal("chat must not copy PPT style ids onto Word/Excel")
	}
}

func TestOfficeChatEvidenceIncludesSelectedPptStyle(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "style-evidence")
	r := officeCall(t, e, "office.task.update", "style-upd", map[string]any{
		"taskId": task.ID, "expectedRevision": task.Revision, "title": task.Title, "goal": task.Goal, "styleId": "brand-pitch",
	})
	if !r.OK {
		t.Fatalf("update: %+v", r.Error)
	}
	sources, err := e.officeChatEvidence(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range sources {
		if strings.Contains(source.Content, "templateId=brand-pitch") && strings.Contains(source.Content, "kind=pptx") {
			found = true
		}
	}
	if !found {
		t.Fatalf("style missing from chat evidence: %+v", sources)
	}
}

func TestOfficeChatEvidenceIncludesBriefFacts(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "brief-evidence")
	r := officeCall(t, e, "office.task.update", "brief-upd", map[string]any{
		"taskId": task.ID, "expectedRevision": task.Revision, "title": task.Title, "goal": task.Goal,
		"brief": map[string]any{
			"audience": "客户", "purpose": "方案汇报", "targetLength": 8,
			"facts": []map[string]any{{"factId": "orders", "value": "1280", "unit": "单", "locked": true}},
		},
	})
	if !r.OK {
		t.Fatalf("update: %+v", r.Error)
	}
	var page struct {
		Task struct {
			Brief struct {
				Audience     string `json:"audience"`
				Purpose      string `json:"purpose"`
				TargetLength int    `json:"targetLength"`
			} `json:"brief"`
		} `json:"task"`
	}
	if err := decodeResponsePayload(r.Payload, &page); err != nil || page.Task.Brief.Audience != "客户" || page.Task.Brief.TargetLength != 8 {
		t.Fatalf("snapshot brief: %+v %v", page.Task.Brief, err)
	}
	sources, err := e.officeChatEvidence(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range sources {
		if strings.Contains(source.Content, "factId=orders") && strings.Contains(source.Content, "1280") && strings.Contains(source.Content, "客户") {
			found = true
		}
	}
	if !found {
		t.Fatalf("brief facts missing from chat evidence: %+v", sources)
	}
}

func TestOfficeChatEvidenceDoesNotInventBriefDefaults(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "brief-evidence-raw")
	sources, err := e.officeChatEvidence(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	blob := ""
	for _, source := range sources {
		blob += source.Content
	}
	if strings.Contains(blob, "audience=管理层") || strings.Contains(blob, "purpose=经营汇报") {
		t.Fatalf("invented brief defaults in chat evidence: %s", blob)
	}
	if !strings.Contains(blob, "audience unset") {
		t.Fatalf("empty audience not marked unset: %s", blob)
	}
	r := officeCall(t, e, "office.task.update", "brief-conf-ev", map[string]any{
		"taskId": task.ID, "expectedRevision": task.Revision, "title": task.Title, "goal": task.Goal,
		"brief": map[string]any{"confidentiality": "内部"},
	})
	if !r.OK {
		t.Fatalf("update: %+v", r.Error)
	}
	sources, err = e.officeChatEvidence(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	blob = ""
	for _, source := range sources {
		blob += source.Content
	}
	if !strings.Contains(blob, "confidentiality=内部") {
		t.Fatalf("authored confidentiality missing: %s", blob)
	}
	if strings.Contains(blob, "机密") {
		t.Fatal("invented stricter classification")
	}
}

func TestOfficeTaskSnapshotDoesNotInventBriefDefaults(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "brief-raw")
	r := officeCall(t, e, "office.task.get", "brief-get", map[string]any{"taskId": task.ID})
	if !r.OK {
		t.Fatalf("get: %+v", r.Error)
	}
	var page struct {
		Task struct {
			Brief struct {
				Audience        string `json:"audience"`
				Purpose         string `json:"purpose"`
				TargetLength    int    `json:"targetLength"`
				Confidentiality string `json:"confidentiality"`
			} `json:"brief"`
		} `json:"task"`
	}
	if err := decodeResponsePayload(r.Payload, &page); err != nil {
		t.Fatal(err)
	}
	if page.Task.Brief.Audience != "" || page.Task.Brief.Purpose != "" || page.Task.Brief.TargetLength != 0 || page.Task.Brief.Confidentiality != "" {
		t.Fatalf("invented brief defaults: %+v", page.Task.Brief)
	}
}

func TestOfficeTaskUpdatePersistsConfidentialityWithoutInventingAudience(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "brief-conf")
	r := officeCall(t, e, "office.task.update", "brief-conf-upd", map[string]any{
		"taskId": task.ID, "expectedRevision": task.Revision, "title": task.Title, "goal": task.Goal,
		"brief": map[string]any{"confidentiality": "内部"},
	})
	if !r.OK {
		t.Fatalf("update: %+v", r.Error)
	}
	var page struct {
		Task struct {
			Brief struct {
				Audience        string `json:"audience"`
				Confidentiality string `json:"confidentiality"`
			} `json:"brief"`
		} `json:"task"`
	}
	if err := decodeResponsePayload(r.Payload, &page); err != nil || page.Task.Brief.Confidentiality != "内部" {
		t.Fatalf("confidentiality: %+v %v", page.Task.Brief, err)
	}
	if page.Task.Brief.Audience == "管理层" {
		t.Fatal("update invented audience")
	}
}

func TestOfficeChatEvidenceIncludesTaskBrand(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "brand-evidence")
	r := officeCall(t, e, "office.task.update", "brand-upd", map[string]any{
		"taskId": task.ID, "expectedRevision": task.Revision, "title": task.Title, "goal": task.Goal,
		"brand": map[string]any{
			"brandId": "task-teal",
			"colors":  map[string]any{"navy": "112233"},
			"fonts":   map[string]any{"latin": "Georgia", "east": "SimSun"},
			"asset": map[string]any{
				"sourceUrl": "https://example.invalid/brand", "license": "client-granted",
				"digest": "abababababababababababababababababababababababababababababababab", "commercial": true,
			},
		},
	})
	if !r.OK {
		t.Fatalf("update: %+v", r.Error)
	}
	var page struct {
		Task struct {
			BrandID string `json:"brandId"`
		} `json:"task"`
	}
	if err := decodeResponsePayload(r.Payload, &page); err != nil || page.Task.BrandID != "task-teal" {
		t.Fatalf("snapshot brand: %+v %v", page.Task, err)
	}
	sources, err := e.officeChatEvidence(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range sources {
		if strings.Contains(source.Content, "brandId=task-teal") && strings.Contains(source.Content, "do not mix classic colors") {
			found = true
		}
	}
	if !found {
		t.Fatalf("brand missing from chat evidence: %+v", sources)
	}
}

func TestOfficeShellBypassedForBoundTaskAndLegacyPipelines(t *testing.T) {
	if officeShellBypassed("workspace.read", "01ARZ3NDEKTSV4RRFFQ69G5FAV", nil) {
		t.Fatal("non-shell tool blocked")
	}
	if !officeShellBypassed("command.run", "01ARZ3NDEKTSV4RRFFQ69G5FAV", &chatTurnCheckpoint{Goal: "随便看看"}) {
		t.Fatal("office-bound command.run must use office.generate")
	}
	if !officeShellBypassed("run_terminal_cmd", "", &chatTurnCheckpoint{PptActive: true}) {
		t.Fatal("ppt pipeline still allows shell bypass")
	}
	if officeShellBypassed("command.run", "", &chatTurnCheckpoint{Goal: "跑一下单元测试"}) {
		t.Fatal("plain coding command blocked")
	}
	if officeShellBypassed("command.run", "", &chatTurnCheckpoint{Goal: "跑一下表格相关的单元测试"}) {
		t.Fatal("coding verification mentioning a spreadsheet blocked")
	}
	for _, goal := range []string{"帮我做一个Excel表格", "参考材料写一份Word", "参考文档，帮我做一个10页的PDF"} {
		if !officeShellBypassed("command.run", "", &chatTurnCheckpoint{Goal: goal}) {
			t.Fatalf("office file goal still allows shell: %s", goal)
		}
	}
}

func TestOfficeManagedWriteBlockedForExcelWordPDF(t *testing.T) {
	excel := json.RawMessage(`{"path":"半年财报.xlsx","content":"not-a-workbook"}`)
	if !officeManagedBypass("workspace.write", "", &chatTurnCheckpoint{Goal: "做一份半年财报"}, excel) {
		t.Fatal("excel workspace.write must use excel.gen")
	}
	word := json.RawMessage(`{"path":"周报.docx","content":"PK"}`)
	if !officeManagedBypass("workspace.write", "", &chatTurnCheckpoint{Goal: "写一份周报"}, word) {
		t.Fatal("word workspace.write must use docx.gen")
	}
	pdf := json.RawMessage(`{"path":"方案.pdf","content":"%PDF"}`)
	if !officeManagedBypass("workspace.write", "", &chatTurnCheckpoint{Goal: "参考文档，帮我做一个10页的PDF"}, pdf) {
		t.Fatal("pdf workspace.write must use pdf.gen")
	}
	note := json.RawMessage(`{"path":"notes.md","content":"ok"}`)
	if officeManagedBypass("workspace.write", "", &chatTurnCheckpoint{Goal: "做一份半年财报"}, note) {
		t.Fatal("non-office write blocked")
	}
	if officeManagedBypass("workspace.write", "", &chatTurnCheckpoint{Goal: "跑一下单元测试"}, excel) {
		t.Fatal("coding turn blocked from writing a spreadsheet")
	}
}

func TestOfficeChatBindingKeepsOlderTaskAndPendingApprovalDestination(t *testing.T) {
	e, store := officeEngineFixture(t)
	a := officeCreatedTask(t, e, "task-a")
	b, err := store.CreateOfficeTask(context.Background(), domain.Task{SessionID: a.SessionID, Title: "Task B"}, "task-b")
	if err != nil {
		t.Fatal(err)
	}
	ctx := withOfficeTask(context.Background(), a.ID)
	if err = e.validateOfficeChatTask(ctx, a.SessionID, a.ID); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"name": "当前任务.docx", "spec": content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "A", Blocks: []content.Block{{Type: "paragraph", Text: "归属旧任务A"}}}})
	if _, _, _, err = e.executeOfficeTool(ctx, a.SessionID, "office.generate", args); err != nil {
		t.Fatal(err)
	}
	va, _ := store.ListOfficeVersions(ctx, a.ID, "")
	vb, _ := store.ListOfficeVersions(ctx, b.ID, "")
	if len(va) != 1 || len(vb) != 0 {
		t.Fatalf("bound task drifted: A=%d B=%d", len(va), len(vb))
	}
	// Pending approval persists the bound destination; later decisions do not
	// require the original stream context or the same visible route.
	bound := officeBoundToolArgs(ctx, "office.generate", args)
	var p map[string]any
	_ = json.Unmarshal(bound, &p)
	if p["taskId"] != a.ID {
		t.Fatal("approval arguments lost task", p)
	}
	if _, _, _, err = e.executeOfficeTool(context.Background(), a.SessionID, "office.generate", bound); err != nil {
		t.Fatal(err)
	}
	va, _ = store.ListOfficeVersions(ctx, a.ID, "")
	vb, _ = store.ListOfficeVersions(ctx, b.ID, "")
	if len(va) != 2 || len(vb) != 0 {
		t.Fatalf("approval destination drifted: A=%d B=%d", len(va), len(vb))
	}
	p["taskId"] = b.ID
	wrong, _ := json.Marshal(p)
	if _, _, _, err = e.executeOfficeTool(ctx, a.SessionID, "office.generate", wrong); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-bound task accepted: %v", err)
	}
	other := officeCreatedTask(t, e, "another-session")
	if err = e.validateOfficeChatTask(ctx, other.SessionID, a.ID); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-session binding accepted: %v", err)
	}
}

func TestOfficeChatStartStreamsAndPersistsRealBoundDelivery(t *testing.T) {
	e, store := officeEngineFixture(t)
	a := officeCreatedTask(t, e, "stream-a")
	b, err := store.CreateOfficeTask(context.Background(), domain.Task{SessionID: a.SessionID, Title: "Another task"}, "stream-b")
	if err != nil {
		t.Fatal(err)
	}
	e.providers = chatAttachmentProvider{}
	e.leases = streamTestLease{}
	sourceBytes, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "Reference", Blocks: []content.Block{{Type: "paragraph", Text: "Original source"}}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.officeStudio.Import(context.Background(), a.ID, "", "bound-reference.docx", sourceBytes, "", 0, "reference-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.officeStudio.Import(context.Background(), b.ID, "", "other-task-secret.docx", sourceBytes, "", 0, "reference-b"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	adapter := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
		calls++
		if calls == 1 {
			found := false
			for _, message := range req.Messages {
				if strings.Contains(message.Content, "other-task-secret") {
					t.Error("other task source leaked")
				}
				if strings.Contains(message.Content, "bound-reference.docx") {
					found = strings.Contains(message.Content, source.ID)
					if message.Role == llmadapter.RoleSystem {
						t.Error("source promoted to system authority")
					}
				}
				if message.Role == llmadapter.RoleSystem && strings.Contains(message.Content, "九步流水线") {
					t.Error("legacy Office pipeline injected")
				}
			}
			if !found {
				t.Error("bound source catalog missing from real model request")
			}
			if !routedRequestHasTool(req, "office.generate") {
				t.Error("Office generator missing in real chat tools")
			}
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "bound-office-call", Name: "office.generate", Arguments: json.RawMessage(`{"name":"当前任务.docx","spec":{"schemaVersion":1,"kind":"docx","title":"任务A","blocks":[{"type":"paragraph","text":"真实工具生成的文件"}]}}`)}}}}, nil
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "文件已生成，可以查看。"}}, nil
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	payload, _ := json.Marshal(map[string]any{"providerId": chatAttachmentProviderID, "modelId": "model", "sessionId": a.SessionID, "officeTaskId": a.ID, "executionMode": "full-access", "messages": []map[string]string{{"role": "user", "content": "生成一份包含任务A说明的文档。"}}})
	events := make(chan bridge.Event, 256)
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", string(payload)), func(event bridge.Event) error { events <- event; return nil })
	if !response.OK {
		t.Fatalf("start: %+v", response.Error)
	}
	frames := collectFramedChatEvents(t, response, events)
	if frames[len(frames)-1].Type != bridge.EventCompleted {
		t.Fatalf("terminal: %+v", frames[len(frames)-1])
	}
	delivered := false
	for _, frame := range frames {
		if frame.Tool != nil && frame.Tool.Name == "office.generate" && frame.Tool.Artifact != nil {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("real file card missing from stream")
	}
	va, _ := store.ListOfficeVersions(context.Background(), a.ID, "")
	vb, _ := store.ListOfficeVersions(context.Background(), b.ID, "")
	if len(va) != 2 || len(vb) != 1 {
		t.Fatalf("stream task drifted: A=%d B=%d", len(va), len(vb))
	}
	index := e.loadSessionArtifactsByMessage(a.SessionID)
	bound := false
	for _, files := range index {
		for _, file := range files {
			if strings.HasSuffix(file.Path, ".docx") {
				bound = bound || file.OfficeTaskID == a.ID
			}
		}
	}
	if !bound {
		t.Fatal("durable chat file lost Office task binding")
	}
}
