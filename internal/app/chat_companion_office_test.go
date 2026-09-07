package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/oklog/ulid/v2"
)

func TestCompanionTaskWorkflowInjectionPpt(t *testing.T) {
	got := companionTaskWorkflowInjection("帮我做一份个人介绍的PPT")
	if got == "" {
		t.Fatal("ppt task must inject the companion office pipeline")
	}
	if !strings.Contains(got, "pptx.gen") {
		t.Fatalf("ppt lite must name pptx.gen: %q", got)
	}
	if !strings.Contains(got, "流水线") {
		t.Fatalf("ppt lite must enforce the pipeline: %q", got)
	}
	// The full nine-step blob is too heavy for the voice TTFT budget; the
	// companion lane must stay compact.
	if len(got) > 1400 {
		t.Fatalf("companion office lite too large (%d bytes): %q", len(got), got)
	}
}

func TestCompanionTaskWorkflowInjectionReportAndExcel(t *testing.T) {
	if got := companionTaskWorkflowInjection("帮我写一份调研报告"); !strings.Contains(got, "docx.gen") {
		t.Fatalf("report lite must name docx.gen: %q", got)
	}
	if got := companionTaskWorkflowInjection("做一个半年财报表格"); !strings.Contains(got, "excel.gen") {
		t.Fatalf("excel lite must name excel.gen: %q", got)
	}
}

func TestCompanionTaskWorkflowInjectionSkipsIdle(t *testing.T) {
	for _, idle := range []string{"", "你好", "今晚天气", "继续聊", "打开网页"} {
		if got := companionTaskWorkflowInjection(idle); got != "" {
			t.Fatalf("non-office turn %q must not inject office pipeline: %q", idle, got)
		}
	}
}

func TestChatEmitsTurnEquipEvent(t *testing.T) {
	requests := make(chan llmadapter.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("slide-builder", "ppt deck", `{}`)}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	events := make(chan bridge.Event, 128)
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","messages":[{"role":"user","content":"帮我做一份路演PPT"}]}`
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(ev bridge.Event) error {
		events <- ev
		return nil
	})
	if !resp.OK {
		t.Fatalf("chat.start failed: %#v", resp)
	}
	_ = capturedChatRequest(t, requests)
	var equip *bridge.EquipEvent
	for _, ev := range collectFramedChatEvents(t, resp, events) {
		if ev.Type == bridge.EventEquip {
			equip = ev.Equip
		}
	}
	if equip == nil {
		t.Fatal("expected an equip event for an intent-matched PPT turn")
	}
	if len(equip.Experts) == 0 || equip.Experts[0] != "PPT专家" {
		t.Fatalf("equip experts = %#v", equip.Experts)
	}
}

// Exercise the actual chat.start producer, not only a synthetic event payload:
// an unframed equip used to poison the host pipe before skill creation began.
func collectFramedChatEvents(t *testing.T, response bridge.Response, events <-chan bridge.Event) []bridge.Event {
	t.Helper()
	encoded, _ := json.Marshal(response.Payload)
	var start struct {
		StreamID string `json:"streamId"`
	}
	if err := json.Unmarshal(encoded, &start); err != nil || start.StreamID == "" {
		t.Fatalf("invalid start: %#v", response)
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	var result []bridge.Event
	for {
		select {
		case ev := <-events:
			if ev.Version != bridge.Version || ev.Kind != "event" || ev.StreamID != start.StreamID || ev.Sequence != uint64(len(result)+1) {
				t.Fatalf("invalid stream envelope: %#v", ev)
			}
			if _, err := ulid.ParseStrict(ev.ID); err != nil {
				t.Fatalf("invalid event id: %v", err)
			}
			result = append(result, ev)
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed || ev.Type == bridge.EventCancelled {
				return result
			}
		case <-timer.C:
			t.Fatal("chat did not finish")
		}
	}
}

func TestSkillCreationChatStartAndNextTurnKeepValidEventStream(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	stub := &skillCreateRecordingStub{}
	e.skills = stub
	adapter := &skillCreateAdapter{}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	for _, text := range []string{"创建一个帮我读取文件的技能", "谢谢，继续聊一下"} {
		events := make(chan bridge.Event, 128)
		payload, _ := json.Marshal(map[string]any{"providerId": chatAttachmentProviderID, "modelId": "model", "executionMode": "full-access", "messages": []map[string]string{{"role": "user", "content": text}}})
		response := e.HandleStreaming(context.Background(), validRequest("chat.start", string(payload)), func(ev bridge.Event) error { events <- ev; return nil })
		if !response.OK {
			t.Fatalf("chat failed: %#v", response)
		}
		frames := collectFramedChatEvents(t, response, events)
		if terminal := frames[len(frames)-1]; terminal.Type != bridge.EventCompleted {
			t.Fatalf("skill chat did not complete: %s %#v", terminal.Type, terminal.Error)
		}
	}
	if stub.created.Name != "folder-reader" {
		t.Fatalf("skill not created: %#v", stub.created)
	}
}

func TestEquipDisplayLimitsDoNotChangeExecutionLabels(t *testing.T) {
	labels := []string{strings.Repeat("😀", 40), "专家2", "专家3"}
	shown := equipDisplayLabels(labels, 2, 32)
	if len(shown) != 2 || shown[0] != strings.Repeat("😀", 15)+"…" || shown[1] != "专家2" {
		t.Fatalf("invalid chip labels: %#v", shown)
	}
	if len(labels) != 3 || labels[0] != strings.Repeat("😀", 40) {
		t.Fatal("display clipping changed execution equipment")
	}
}

func TestCompanionDoesNotEmitEquipEvent(t *testing.T) {
	requests := make(chan llmadapter.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("slide-builder", "ppt deck", `{}`)}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	var sawEquip bool
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","companion":true,"messages":[{"role":"user","content":"帮我做一份路演PPT"}]}`
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(ev bridge.Event) error {
		if ev.Type == bridge.EventEquip {
			sawEquip = true
		}
		return nil
	})
	if !resp.OK {
		t.Fatalf("companion chat.start failed: %#v", resp)
	}
	_ = capturedChatRequest(t, requests)
	if sawEquip {
		t.Fatal("companion voice lane must not emit the text equip chip event")
	}
}
