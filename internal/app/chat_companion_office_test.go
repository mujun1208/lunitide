package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
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
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("slide-builder", "ppt deck", `{}`)}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	var equip *bridge.EquipEvent
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","messages":[{"role":"user","content":"帮我做一份路演PPT"}]}`
	resp := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(ev bridge.Event) error {
		if ev.Type == bridge.EventEquip && ev.Equip != nil {
			equip = ev.Equip
		}
		return nil
	})
	if !resp.OK {
		t.Fatalf("chat.start failed: %#v", resp)
	}
	_ = capturedChatRequest(t, requests)
	if equip == nil {
		t.Fatal("expected an equip event for an intent-matched PPT turn")
	}
	if len(equip.Experts) == 0 || equip.Experts[0] != "PPT专家" {
		t.Fatalf("equip experts = %#v", equip.Experts)
	}
}

func TestCompanionDoesNotEmitEquipEvent(t *testing.T) {
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("slide-builder", "ppt deck", `{}`)}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
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
