package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/scheduler"
)

func newAutomationEngine(t *testing.T) (*Engine, *scheduler.Scheduler, *sync.Map) {
	t.Helper()
	e := NewEngine(nil, "test")
	store, err := scheduler.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var calls sync.Map
	s := scheduler.New(store, func(_ context.Context, j scheduler.Job) scheduler.Outcome {
		calls.Store(j.ID, true)
		return scheduler.Outcome{Summary: "done", TotalTokens: 7}
	}, nil)
	e.SetAutomationScheduler(s)
	return e, s, &calls
}

func automationRequest(method, payload string) bridge.Request {
	return bridge.Request{Version: bridge.Version, Kind: "request", ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Method: method, SentAt: time.Now().UTC(), Payload: json.RawMessage(payload), DeadlineMS: 3000}
}

const automationJobPayload = `{"name":"每日站会摘要","cron":"30 8 * * 1-5","prompt":"汇总昨天待办","providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAE","modelId":"gpt-test","sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAF","executionMode":"auto-edit","enabled":true}`

func TestAutomationJobLifecycleThroughBridge(t *testing.T) {
	e, s, calls := newAutomationEngine(t)
	ctx := context.Background()
	// status heartbeat before start still answers (not running yet).
	status := e.Handle(ctx, automationRequest("automation.status", "{}"))
	if !status.OK {
		t.Fatalf("status failed: %+v", status)
	}
	// create
	created := e.Handle(ctx, automationRequest("automation.job.set", automationJobPayload))
	if !created.OK {
		t.Fatalf("create failed: %+v", created)
	}
	var createdPayload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil || len(createdPayload.ID) != 26 {
		t.Fatalf("created id invalid: %+v %v", createdPayload, err)
	}
	// list shows it
	listed := e.Handle(ctx, automationRequest("automation.job.list", "{}"))
	if !listed.OK || !strings.Contains(string(mustJSON(listed.Payload)), "每日站会摘要") {
		t.Fatalf("list missing job: %+v", listed.Payload)
	}
	// update flips name and stays one row
	updated := strings.Replace(automationJobPayload, "每日站会摘要", "周报生成", 1)
	_ = e.Handle(ctx, automationRequest("automation.job.set", `{"id":"`+createdPayload.ID+`",`+updated[1:]))
	listed2 := e.Handle(ctx, automationRequest("automation.job.list", "{}"))
	raw := string(mustJSON(listed2.Payload))
	if strings.Contains(raw, "每日站会摘要") || !strings.Contains(raw, "周报生成") {
		t.Fatalf("update wrong: %s", raw)
	}
	// manual trigger executes headlessly and lands two run rows
	triggered := e.Handle(ctx, automationRequest("automation.job.trigger", `{"id":"`+createdPayload.ID+`"}`))
	if !triggered.OK {
		t.Fatalf("trigger failed: %+v", triggered)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		runsResp := e.Handle(ctx, automationRequest("automation.run.list", `{"limit":10}`))
		if runsResp.OK && strings.Contains(string(mustJSON(runsResp.Payload)), `"succeeded"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ran := calls.Load(createdPayload.ID); !ran {
		t.Fatal("executor never called")
	}
	// delete
	deleted := e.Handle(ctx, automationRequest("automation.job.delete", `{"id":"`+createdPayload.ID+`"}`))
	if !deleted.OK {
		t.Fatalf("delete failed: %+v", deleted)
	}
	listed3 := e.Handle(ctx, automationRequest("automation.job.list", "{}"))
	if strings.Contains(string(mustJSON(listed3.Payload)), "周报生成") {
		t.Fatal("job survived delete")
	}
	_ = s
}

func TestAutomationBridgeValidation(t *testing.T) {
	e, _, _ := newAutomationEngine(t)
	ctx := context.Background()
	// bad cron
	badCron := e.Handle(ctx, automationRequest("automation.job.set", strings.Replace(automationJobPayload, `"30 8 * * 1-5"`, `"每天"`, 1)))
	if badCron.OK || badCron.Error == nil || badCron.Error.Code != "AUTOMATION_CRON_INVALID" {
		t.Fatalf("bad cron = %+v", badCron)
	}
	// bad execution mode
	badMode := e.Handle(ctx, automationRequest("automation.job.set", strings.Replace(automationJobPayload, `"auto-edit"`, `"yolo"`, 1)))
	if badMode.OK {
		t.Fatal("bad mode accepted")
	}
	// unknown trigger target
	missing := e.Handle(ctx, automationRequest("automation.job.trigger", `{"id":"01ARZ3NDEKTSV4RRFFQ69G5FAQ"}`))
	if missing.OK || missing.Error == nil || missing.Error.Code != "AUTOMATION_JOB_NOT_FOUND" {
		t.Fatalf("missing trigger = %+v", missing)
	}
	// malformed run list
	if r := e.Handle(ctx, automationRequest("automation.run.list", `{"jobId":"short"}`)); r.OK {
		t.Fatal("short jobId accepted")
	}
}

func TestAutomationDisabledWithoutScheduler(t *testing.T) {
	e := NewEngine(nil, "test")
	resp := e.Handle(context.Background(), automationRequest("automation.job.list", "{}"))
	if resp.OK || resp.Error == nil || resp.Error.Code != "FEATURE_DISABLED" {
		t.Fatalf("expected FEATURE_DISABLED: %+v", resp)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
