package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/scheduler"
)

type quietAutomationNotifier struct{}

func (quietAutomationNotifier) Notify(string, string) error { return nil }

func TestAutomationCancelBridgeStopsOnlyNamedRun(t *testing.T) {
	store, err := scheduler.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	s := scheduler.New(store, func(ctx context.Context, _ scheduler.Job) scheduler.Outcome {
		close(entered)
		<-ctx.Done()
		return scheduler.Outcome{Err: ctx.Err(), Summary: "已经完成的部分"}
	}, quietAutomationNotifier{})
	t.Cleanup(s.Close)
	e := NewEngine(nil, "test")
	e.SetAutomationScheduler(s)
	created := e.Handle(context.Background(), automationRequest("automation.job.set", automationJobPayload))
	if !created.OK {
		t.Fatalf("create: %+v", created)
	}
	var result struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(mustJSON(created.Payload), &result)
	if err = s.TriggerNow(result.ID); err != nil {
		t.Fatal(err)
	}
	<-entered
	runs, _ := store.ListRuns(result.ID, 1)
	payload, _ := json.Marshal(map[string]string{"jobId": result.ID, "runId": runs[0].ID})
	response := e.Handle(context.Background(), automationRequest("automation.run.cancel", string(payload)))
	if !response.OK || !strings.Contains(string(mustJSON(response.Payload)), `"cancellationRequested":true`) {
		t.Fatalf("cancel: %+v", response)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(s.Snapshot().RunningJobs) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	listed := e.Handle(context.Background(), automationRequest("automation.run.list", `{}`))
	if !listed.OK || !strings.Contains(string(mustJSON(listed.Payload)), `"cancelled":true`) || !strings.Contains(string(mustJSON(listed.Payload)), "已经完成的部分") {
		t.Fatalf("lost receipt: %+v", listed)
	}
}

func TestAutomationTimezonePersistsAndOlderClientsPreserveIt(t *testing.T) {
	e, s, _ := newAutomationEngine(t)
	payload := `{"timezone":"Asia/Shanghai",` + automationJobPayload[1:]
	created := e.Handle(context.Background(), automationRequest("automation.job.set", payload))
	if !created.OK {
		t.Fatalf("create: %+v", created)
	}
	var result struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(mustJSON(created.Payload), &result)
	job, _, _ := s.Store().GetJob(result.ID)
	if job.Timezone != "Asia/Shanghai" {
		t.Fatal(job.Timezone)
	}
	// An older client omits timezone while toggling/editing another field.
	updated := e.Handle(context.Background(), automationRequest("automation.job.set", `{"id":"`+job.ID+`","expectedRevision":"`+scheduler.JobRevision(job)+`",`+automationJobPayload[1:]))
	if !updated.OK {
		t.Fatalf("update: %+v", updated)
	}
	stored, _, _ := s.Store().GetJob(job.ID)
	if stored.Timezone != "Asia/Shanghai" {
		t.Fatal("old client reset timezone")
	}
	listed := e.Handle(context.Background(), automationRequest("automation.job.list", `{}`))
	if !listed.OK || !strings.Contains(string(mustJSON(listed.Payload)), `"timezone":"Asia/Shanghai"`) {
		t.Fatalf("missing timezone: %+v", listed)
	}
}
