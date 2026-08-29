package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestLooksLikePptTask(t *testing.T) {
	if !looksLikePptTask("请 PPT专家做一份个人介绍") || !looksLikePptTask("帮我生成 pptx") {
		t.Fatal("PPT asks must start the pipeline")
	}
	if looksLikePptTask("做好了没有") || looksLikePptTask("还要多久") {
		t.Fatal("status follow-ups are not a new PPT job")
	}
	if looksLikePptTask("打开桌面协议") {
		t.Fatal("unrelated tasks must not start the PPT pipeline")
	}
}

func TestPptGenBlockedUntilResearch(t *testing.T) {
	turn := &chatTurnCheckpoint{PptActive: true}
	if blocked, msg := pptGenBlocked(turn, "pptx.gen"); !blocked || !strings.Contains(msg, "拦住") {
		t.Fatalf("empty pipeline must block pptx.gen: %v %q", blocked, msg)
	}
	if blocked, _ := pptGenBlocked(turn, "web.search"); blocked {
		t.Fatal("research tools must not be blocked")
	}
	notePptTools(turn, []string{"web.search"})
	if pptPipelineReady(turn) {
		t.Fatal("one search is not enough")
	}
	notePptTools(turn, []string{"web.fetch"})
	if !pptPipelineReady(turn) {
		t.Fatal("two web passes must unlock pptx.gen")
	}
	if blocked, _ := pptGenBlocked(turn, "pptx.gen"); blocked {
		t.Fatal("ready pipeline still blocked")
	}
}

func TestPptStageNudgeVisibleInThinking(t *testing.T) {
	turn := &chatTurnCheckpoint{PptActive: true}
	req := gateway.Request{Model: "m"}
	var thinking []string
	if !shouldContinuePptTurn(turn, false) {
		t.Fatal("active PPT turn must keep going until pptx.gen")
	}
	ok := nudgePptWorkflow(&req, turn, func(event bridge.Event) error {
		if event.Thinking != nil {
			thinking = append(thinking, event.Thinking.Text)
		}
		return nil
	})
	if !ok || turn.PptNudges != 1 || len(thinking) == 0 {
		t.Fatalf("nudge = %v nudges=%d thinking=%v", ok, turn.PptNudges, thinking)
	}
	if !strings.Contains(strings.Join(thinking, ""), "PPT 流程") {
		t.Fatalf("stage must be visible in 思考: %v", thinking)
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != gateway.RoleSystem {
		t.Fatal("stage nudge must be a system message")
	}
	turn.PptGenerated = true
	if shouldContinuePptTurn(turn, false) {
		t.Fatal("finished pptx.gen must stop nudging")
	}
}

func TestStartPptWorkflowInjectsPipeline(t *testing.T) {
	req := gateway.Request{Model: "m", Messages: []gateway.Message{{Role: gateway.RoleUser, Content: "做一份介绍 PPT"}}}
	turn := &chatTurnCheckpoint{Goal: "做一份介绍 PPT"}
	var banners []string
	startPptWorkflow(&req, turn, func(event bridge.Event) error {
		if event.Thinking != nil {
			banners = append(banners, event.Thinking.Text)
		}
		return nil
	})
	if !turn.PptActive || !strings.Contains(req.Messages[len(req.Messages)-1].Content, "九步") {
		t.Fatalf("pipeline not injected: active=%v msgs=%v", turn.PptActive, req.Messages)
	}
	if len(banners) == 0 || !strings.Contains(banners[0], "思考") {
		t.Fatalf("clarify stage missing from 思考: %v", banners)
	}
	startPptWorkflow(&req, turn, func(bridge.Event) error { return nil })
	count := 0
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "九步流水线") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("pipeline injected twice: %d", count)
	}
}

func TestBlockedPptGenResult(t *testing.T) {
	r := blockedPptGenResult(pptGenBlockedMsg)
	if r.Output == "" || !strings.Contains(r.Output, "ok:false") {
		t.Fatalf("blocked result = %#v", r)
	}
	var _ toolruntime.Result = r
}
