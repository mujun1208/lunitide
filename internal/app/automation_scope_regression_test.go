package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

func TestAutomationHistoryFiltersScopeAndFinalStateBeforeLimit(t *testing.T) {
	ctx := context.Background()
	e, personalID, db := agentRunEngine(t)
	e.SetDataScopeStore(db)
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(db.OrgStorage()), nil), m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")))
	e.SetM9OrgAdminService(admin)
	foreign, err := admin.CreateOrg(ctx, "Foreign")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.projects.Create(ctx, ulid.Make().String(), "test", "foreign", project.Project{Name: "Foreign", OrgID: foreign.OrgID})
	if err != nil {
		t.Fatal(err)
	}
	f, err := e.sessions.Create(ctx, ulid.Make().String(), "test", "foreign", session.Session{ProjectID: p.ID, Title: "Foreign"})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	journal, err := scheduler.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	engine := scheduler.New(journal, nil, quietAutomationNotifier{})
	t.Cleanup(engine.Close)
	e.SetAutomationScheduler(engine)
	var own []scheduler.Run
	for i := 0; i < 3; i++ {
		r := schedulerRunForOpen(personalID)
		r.State = scheduler.RunRunning
		if err := journal.AppendRun(r); err != nil {
			t.Fatal(err)
		}
		r.State = scheduler.RunSucceeded
		r.Summary = fmt.Sprintf("own-%d", i)
		if err := journal.AppendRun(r); err != nil {
			t.Fatal(err)
		}
		own = append(own, r)
	}
	for i := 0; i < 12; i++ {
		r := schedulerRunForOpen(f.ID)
		r.Summary = "foreign secret"
		if err := journal.AppendRun(r); err != nil {
			t.Fatal(err)
		}
	}
	read := func() []scheduler.Run {
		t.Helper()
		res := e.Handle(ctx, automationRequest("automation.run.list", `{"limit":2}`))
		if !res.OK {
			t.Fatalf("list: %+v", res)
		}
		var body struct {
			Runs []scheduler.Run `json:"runs"`
		}
		if err := json.Unmarshal(mustJSON(res.Payload), &body); err != nil {
			t.Fatal(err)
		}
		return body.Runs
	}
	runs := read()
	if len(runs) != 2 || runs[0].ID != own[2].ID || runs[1].ID != own[1].ID || runs[0].State != scheduler.RunSucceeded {
		t.Fatalf("own history hidden or duplicate: %+v", runs)
	}
	if _, err := admin.Switch(ctx, foreign.OrgID); err != nil {
		t.Fatal(err)
	}
	for _, r := range read() {
		if r.SessionID != f.ID {
			t.Fatalf("foreign scope leaked personal record: %+v", r)
		}
	}
	if err := admin.SelectPersonal(ctx); err != nil {
		t.Fatal(err)
	}
	// Reopening the journal and revisiting the page preserve historical rows.
	reopened, err := scheduler.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	other := scheduler.New(reopened, nil, quietAutomationNotifier{})
	t.Cleanup(other.Close)
	e.SetAutomationScheduler(other)
	if got := read(); len(got) != 2 || got[0].ID != own[2].ID {
		t.Fatalf("history lost on reopen: %+v", got)
	}
}

func TestAutomationIsolatedResultRemainsReadableAndCannotBeReclaimedAsDraft(t *testing.T) {
	ctx := context.Background()
	e, boundID, db := agentRunEngine(t)
	e.SetDataScopeStore(db)
	sessions := e.sessions.(*sessionapp.Service)
	sessions.SetDeleter(db)
	bound, err := sessions.Get(ctx, boundID)
	if err != nil {
		t.Fatal(err)
	}
	actualID := e.isolatedAutomationSession(ctx, boundID)
	if actualID == "" || actualID == boundID {
		t.Fatal("isolated execution reused or lost its session")
	}
	actual, err := sessions.Get(ctx, actualID)
	if err != nil || actual.ProjectID != bound.ProjectID {
		t.Fatalf("isolated ownership: %+v %v", actual, err)
	}
	draft, err := sessions.Create(ctx, ulid.Make().String(), "desktop-host", "ordinary draft", session.Session{ProjectID: bound.ProjectID, Title: "新对话"})
	if err != nil {
		t.Fatal(err)
	}
	freed, err := sessions.ReclaimEmptyDrafts(ctx, bound.ProjectID, 100)
	if err != nil || freed != 1 {
		t.Fatalf("reclaim should remove only ordinary draft: %d %v", freed, err)
	}
	if _, err := sessions.Get(ctx, draft.ID); err == nil {
		t.Fatal("ordinary unused draft remains")
	}
	if _, err := sessions.Get(ctx, actualID); err != nil {
		t.Fatalf("automation execution was erased: %v", err)
	}
	messages, err := messageapp.New(db, db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	e.messages = messages
	appendRequest := validRequest("message.append", fmt.Sprintf(`{"sessionId":%q,"text":"已完成产物，请继续分析"}`, actualID))
	appendRequest.IdempotencyKey = ulid.Make().String()
	if res := e.Handle(ctx, appendRequest); !res.OK {
		t.Fatalf("continuation append: %+v", res)
	}
	if res := e.Handle(ctx, validRequest("message.list", fmt.Sprintf(`{"sessionId":%q}`, actualID))); !res.OK || !strings.Contains(string(mustJSON(res.Payload)), "已完成产物") {
		t.Fatalf("execution history cannot be opened: %+v", res)
	}
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(db.OrgStorage()), nil), m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")))
	e.SetM9OrgAdminService(admin)
	foreign, err := admin.CreateOrg(ctx, "Foreign")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Switch(ctx, foreign.OrgID); err != nil {
		t.Fatal(err)
	}
	if res := e.Handle(ctx, validRequest("message.list", fmt.Sprintf(`{"sessionId":%q}`, actualID))); res.OK || res.Error.Code != "DATA_SCOPE_DENIED" {
		t.Fatalf("foreign organization accessed isolated chat: %+v", res)
	}
}

func TestAutomationCancelUsesSchedulerOwnershipWithRealScope(t *testing.T) {
	ctx := context.Background()
	e, sessionID, db := agentRunEngine(t)
	e.SetDataScopeStore(db)
	journal, err := scheduler.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	engine := scheduler.New(journal, func(ctx context.Context, _ scheduler.Job) scheduler.Outcome {
		close(entered)
		<-ctx.Done()
		return scheduler.Outcome{Err: ctx.Err()}
	}, quietAutomationNotifier{})
	t.Cleanup(engine.Close)
	e.SetAutomationScheduler(engine)
	job := scheduler.Job{ID: ulid.Make().String(), Name: "Scoped execution", Cron: "0 8 * * *", ProviderID: ulid.Make().String(), ModelID: "model", SessionID: sessionID, Prompt: "Test", Enabled: true, ExecutionMode: "auto-edit", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := journal.PutJob(job); err != nil {
		t.Fatal(err)
	}
	if err := engine.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("execution never started")
	}
	runs, err := journal.LatestRuns(job.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("active run: %+v %v", runs, err)
	}
	request := automationRequest("automation.run.cancel", fmt.Sprintf(`{"jobId":%q,"runId":%q}`, job.ID, runs[0].ID))
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(db.OrgStorage()), nil), m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")))
	e.SetM9OrgAdminService(admin)
	foreign, err := admin.CreateOrg(ctx, "Foreign")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Switch(ctx, foreign.OrgID); err != nil {
		t.Fatal(err)
	}
	if denied := e.Handle(ctx, request); denied.OK || denied.Error.Code != "DATA_SCOPE_DENIED" {
		t.Fatalf("foreign organization cancelled personal execution: %+v", denied)
	}
	if err := admin.SelectPersonal(ctx); err != nil {
		t.Fatal(err)
	}
	response := e.Handle(ctx, request)
	if !response.OK || !strings.Contains(string(mustJSON(response.Payload)), `"cancellationRequested":true`) {
		t.Fatalf("scheduler id mistaken for agent run: %+v", response)
	}
}
