package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func TestSchedulerImportDoesNotExecuteAndPreservesUnknownDisable(t *testing.T) {
	root := t.TempDir()
	jsonStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{
		ID: ulid.Make().String(), Name: "日报", Cron: "0 8 * * *", Prompt: "生成日报",
		ProviderID: ulid.Make().String(), ModelID: "glm-4", SessionID: ulid.Make().String(),
		Enabled: false, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := jsonStore.PutJob(job); err != nil {
		t.Fatal(err)
	}
	run := Run{
		ID: ulid.Make().String(), JobID: job.ID, JobName: job.Name, State: RunFailed,
		Trigger: "cron", OutcomeUnknown: true, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
		Error: "上次执行被中断，结果不确定；已停用任务，请核对后重新启用",
	}
	if err := jsonStore.AppendRun(run); err != nil {
		t.Fatal(err)
	}
	fired := 0
	sqlStore, manifest, err := ImportJSON(root, func() { fired++ })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	if fired != 0 {
		t.Fatal("import must not trigger execution")
	}
	if manifest.ImportedJobs != 1 || manifest.ImportedRuns != 1 || manifest.Cutover {
		t.Fatalf("manifest %+v", manifest)
	}
	if CurrentWriter(root) != WriterJSON {
		t.Fatal("import without cutover must keep JSON writer")
	}
	jobs, err := sqlStore.ListJobs()
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].Enabled {
		t.Fatalf("unknown-disabled job rewritten: %+v %v", jobs, err)
	}
	runs, err := sqlStore.LatestRuns(job.ID)
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID || !runs[0].OutcomeUnknown || runs[0].State == RunRunning {
		t.Fatalf("unknown run rewritten: %+v %v", runs, err)
	}

	if err := Cutover(root, manifest); err != nil {
		t.Fatal(err)
	}
	if CurrentWriter(root) != WriterSQLite {
		t.Fatal("cutover must switch single writer")
	}
	if err := jsonStore.PutJob(job); err == nil {
		t.Fatal("JSON writer must refuse after sqlite cutover")
	}
}

func TestSchedulerCutoverBlockedOnCountMismatch(t *testing.T) {
	root := t.TempDir()
	jsonStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := Job{
		ID: ulid.Make().String(), Name: "简报", Cron: "0 9 * * *", Prompt: "生成简报",
		ProviderID: ulid.Make().String(), ModelID: "glm-4", SessionID: ulid.Make().String(),
		Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := jsonStore.PutJob(job); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SourceJobs: 2, SourceRuns: 0, ImportedJobs: 1, ImportedRuns: 0}
	if err := Cutover(root, manifest); err == nil {
		t.Fatal("mismatch must block cutover")
	}
	if CurrentWriter(root) != WriterJSON {
		t.Fatal("failed cutover must keep JSON writer")
	}
}

func TestLiveSQLPutJobRefusesBeforeCutover(t *testing.T) {
	root := t.TempDir()
	store, err := OpenLiveSQL(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.PutJob(validJob("live-sql", "* * * * *")); err == nil {
		t.Fatal("live SQLite PutJob must refuse while JSON is the writer")
	}
}

func TestSchedulerWriterFileIsJSONByDefault(t *testing.T) {
	if CurrentWriter(filepath.Join(t.TempDir(), "missing")) != WriterJSON {
		t.Fatal("absent writer marker is JSON")
	}
}

func TestSQLStoreBindRunSessionKeepsSameRunID(t *testing.T) {
	root := t.TempDir()
	store, err := OpenSQL(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	job := validJob("bind-sql", "* * * * *")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	runID := "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	if err := store.AppendRun(Run{
		ID: runID, JobID: job.ID, JobName: job.Name, SessionID: job.SessionID,
		State: RunRunning, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	isolated := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	if err := store.BindRunSession(runID, isolated); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestRuns(job.ID)
	if err != nil || len(latest) != 1 || latest[0].ID != runID || latest[0].SessionID != isolated || latest[0].State != RunRunning {
		t.Fatalf("same dispatch minted a second run: %+v %v", latest, err)
	}
	if err := store.BindRunSession(runID, isolated); err != nil {
		t.Fatal(err)
	}
	again, err := store.LatestRuns(job.ID)
	if err != nil || len(again) != 1 || again[0].ID != runID {
		t.Fatalf("rebind created another run: %+v %v", again, err)
	}
}

func TestSchedulerRecoverStaysOffAfterSQLiteCutover(t *testing.T) {
	root := t.TempDir()
	jsonStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := validJob("cutover-recover", "*/5 * * * *")
	if err := jsonStore.PutJob(job); err != nil {
		t.Fatal(err)
	}
	sqlStore, manifest, err := ImportJSON(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	if err := Cutover(root, manifest); err != nil {
		t.Fatal(err)
	}
	got := make(chan RunContext, 1)
	s := New(sqlStore, nil, noopNotifier{})
	s.SetContextualExecutor(func(_ context.Context, _ Job, rc RunContext) Outcome {
		got <- rc
		return Outcome{}
	})
	t.Cleanup(s.Close)
	if err := s.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case rc := <-got:
		if rc.Recover {
			t.Fatal("cutover without classified decision must keep Recover false")
		}
		if rc.DispatchKey != job.ID+":"+rc.RunID {
			t.Fatalf("dispatch key %+v", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor was not invoked")
	}

	s.SetRecoverDecision("native_continue")
	got2 := make(chan RunContext, 1)
	s.SetContextualExecutor(func(_ context.Context, _ Job, rc RunContext) Outcome {
		got2 <- rc
		return Outcome{}
	})
	if err := s.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case rc := <-got2:
		if !rc.Recover {
			t.Fatal("sqlite + native_continue must allow classified Recover")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("classified executor was not invoked")
	}

	jsonOnly, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonOnly.PutJob(job); err != nil {
		t.Fatal(err)
	}
	js := New(jsonOnly, nil, noopNotifier{})
	js.SetRecoverDecision("native_continue")
	got3 := make(chan RunContext, 1)
	js.SetContextualExecutor(func(_ context.Context, _ Job, rc RunContext) Outcome {
		got3 <- rc
		return Outcome{}
	})
	t.Cleanup(js.Close)
	if err := js.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case rc := <-got3:
		if rc.Recover {
			t.Fatal("JSON writer must keep Recover false even with a classified decision")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("json executor was not invoked")
	}
}

func TestOpenAutomationRepositoryFollowsWriterMarker(t *testing.T) {
	root := t.TempDir()
	repo, closer, err := OpenAutomationRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	closer()
	if _, ok := repo.(*SQLStore); ok {
		t.Fatal("absent marker must open JSON store")
	}
	jsonStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := validJob("open-repo", "*/5 * * * *")
	if err := jsonStore.PutJob(job); err != nil {
		t.Fatal(err)
	}
	sqlStore, manifest, err := ImportJSON(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlStore.Close()
	if err := Cutover(root, manifest); err != nil {
		t.Fatal(err)
	}
	repo, closer, err = OpenAutomationRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closer)
	if _, ok := repo.(*SQLStore); !ok {
		t.Fatal("cutover marker must open SQLite store")
	}
	if err := jsonStore.PutJob(job); err == nil {
		t.Fatal("JSON must stay read-only after cutover")
	}
}
