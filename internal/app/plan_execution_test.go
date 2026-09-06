package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/planningapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

type planProvider struct{ chatAttachmentProvider }

func (p planProvider) List(ctx context.Context, _ provider.Filter) ([]provider.Provider, error) {
	v, err := p.Get(ctx, chatAttachmentProviderID)
	v.Models[0].Kind, v.Models[0].IsDefault, v.Models[0].KindDefault = provider.KindLLM, true, true
	return []provider.Provider{v}, err
}

type planAdapter struct {
	complete func(context.Context, llmadapter.Request) (llmadapter.Response, error)
}

func (a planAdapter) Complete(ctx context.Context, _ []byte, r llmadapter.Request) (llmadapter.Response, error) {
	return a.complete(ctx, r)
}
func (a planAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("unexpected stream")
}
func (a planAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}

func planExecutionFixture(t *testing.T, role string, fn func(context.Context, llmadapter.Request) (llmadapter.Response, error)) (*Engine, *storage.Store, agentorchestration.AgentRun) {
	t.Helper()
	e, sessionID, store := agentRunEngine(t)
	sess, err := store.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	plans := planningapp.New(store, store, nil)
	p, err := plans.CreatePlan(context.Background(), planning.Plan{ProjectID: sess.ProjectID, Name: "Execution"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := plans.CreateNode(context.Background(), planning.Node{PlanID: p.ID, Name: "Task", RiskLevel: planning.RiskLow, WorkerRole: role, Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = plans.Activate(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	coordinator, err := agentorchestration.New(store.AgentOrchestrationRepository(), agentorchestration.Limits{MaxDepth: 3, MaxConcurrency: 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := coordinator.CreateRoot(context.Background(), p.ID, n.ID, role, agentorchestration.Todo{ID: ulid.Make().String(), Title: "Write report", Description: "Write a real report."})
	if err != nil {
		t.Fatal(err)
	}
	e.SetAgentCoordinator(coordinator)
	e.planning = plans
	e.providers = planProvider{}
	e.leases = streamTestLease{}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return planAdapter{fn}, nil })
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.SetToolRuntime(tools)
	t.Cleanup(func() { _ = tools.Close() })
	t.Cleanup(e.StopPlanExecutions)
	return e, store, task
}

func planExecutionCall(id, name string, args any) llmadapter.Response {
	raw, _ := json.Marshal(args)
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: id, Name: name, Arguments: raw}}}}
}
func completeReport(_ context.Context, req llmadapter.Request) (llmadapter.Response, error) {
	if len(req.Messages) == 2 {
		return planExecutionCall("write", "workspace.write", map[string]string{"path": "report.md", "content": "真实产物\n"}), nil
	}
	var receipt struct {
		ReceiptID string `json:"receiptId"`
		OK        bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &receipt); err != nil {
		return llmadapter.Response{}, err
	}
	if !receipt.OK {
		return llmadapter.Response{}, errors.New("tool did not succeed")
	}
	return planExecutionCall("finish", "plan.finish", map[string]any{"summary": "报告已生成并验证", "artifacts": []string{"report.md"}, "receiptIds": []string{receipt.ReceiptID}}), nil
}
func waitPlanTerminal(t *testing.T, e *Engine, id string) agentrun.PlanExecution {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		ex, err := e.agentRuns.GetPlanExecution(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if ex.Terminal() {
			return ex
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("plan execution did not reach terminal state")
	return agentrun.PlanExecution{}
}
func TestPlanExecutionWritesVerifiesAndCommitsAllStates(t *testing.T) {
	var calls atomic.Int32
	e, s, task := planExecutionFixture(t, "implementer", func(ctx context.Context, r llmadapter.Request) (llmadapter.Response, error) {
		calls.Add(1)
		return completeReport(ctx, r)
	})
	started := e.Handle(context.Background(), validRequest(string(bridge.MethodPlanRunStart), `{"runId":"`+task.ID+`"}`))
	if !started.OK {
		t.Fatalf("start=%+v", started.Error)
	}
	ex := waitPlanTerminal(t, e, task.ID)
	sum := sha256.Sum256([]byte("真实产物\n"))
	if ex.Status != "succeeded" || len(ex.Artifacts) != 1 || ex.Artifacts[0].Digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("execution=%+v", ex)
	}
	node, err := s.GetNode(context.Background(), task.NodeID)
	if err != nil || node.Status != planning.NodeStatusCompleted {
		t.Fatalf("node=%+v err=%v", node, err)
	}
	plan, err := s.GetPlan(context.Background(), task.PlanID)
	if err != nil || plan.Status != planning.PlanStatusCompleted {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	err = s.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		run, err := tx.GetRun(ex.RunID)
		if err != nil {
			return err
		}
		if run.Status != agentrun.RunCompleted {
			t.Fatalf("runtime=%+v", run)
		}
		turns, err := tx.ListTurns(ex.RunID)
		if err != nil {
			return err
		}
		if len(turns) != 1 || turns[0].Status != agentrun.TurnCompleted {
			t.Fatalf("turns=%+v", turns)
		}
		steps, err := tx.ListSteps(turns[0].ID)
		if err != nil {
			return err
		}
		if len(steps) != 1 || steps[0].Status != agentrun.StepCompleted {
			t.Fatalf("steps=%+v", steps)
		}
		evidence, err := tx.ListEvidence(ex.RunID)
		if err != nil {
			return err
		}
		if len(evidence) != 1 || evidence[0].ContentDigest != ex.Artifacts[0].Digest {
			t.Fatalf("evidence=%+v", evidence)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.startPlanExecution(context.Background(), task.ID)
	if err != nil || replay.RunID != ex.RunID || calls.Load() != 2 {
		t.Fatalf("replay=%+v err=%v calls=%d", replay, err, calls.Load())
	}
}
func TestPlanExecutionRejectsProseAndUnprovenTesterCompletion(t *testing.T) {
	for _, role := range []string{"prose", "tester"} {
		t.Run(role, func(t *testing.T) {
			fn := completeReport
			if role == "prose" {
				fn = func(context.Context, llmadapter.Request) (llmadapter.Response, error) {
					return llmadapter.Response{Message: llmadapter.Message{Content: "完成了所有测试"}}, nil
				}
			}
			e, _, task := planExecutionFixture(t, role, fn)
			if _, err := e.startPlanExecution(context.Background(), task.ID); err != nil {
				t.Fatal(err)
			}
			ex := waitPlanTerminal(t, e, task.ID)
			if ex.Status != "failed" || len(ex.Artifacts) != 0 {
				t.Fatalf("execution=%+v", ex)
			}
		})
	}
}
func TestPlanExecutionCancellationStopsModelAndAcknowledges(t *testing.T) {
	entered := make(chan struct{})
	e, _, task := planExecutionFixture(t, "implementer", func(ctx context.Context, _ llmadapter.Request) (llmadapter.Response, error) {
		close(entered)
		<-ctx.Done()
		return llmadapter.Response{}, ctx.Err()
	})
	if _, err := e.startPlanExecution(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	response := e.Handle(context.Background(), validRequest(string(bridge.MethodPlanRunCancel), `{"runId":"`+task.ID+`"}`))
	if !response.OK {
		t.Fatalf("cancel=%+v", response.Error)
	}
	ex := waitPlanTerminal(t, e, task.ID)
	if ex.Status != "cancelled" {
		t.Fatalf("execution=%+v", ex)
	}
}
func TestPlanExecutionRestartMarksPreparedToolUnknownWithoutReplay(t *testing.T) {
	var calls atomic.Int32
	e, _, task := planExecutionFixture(t, "implementer", func(ctx context.Context, r llmadapter.Request) (llmadapter.Response, error) {
		calls.Add(1)
		return completeReport(ctx, r)
	})
	ex, created, err := e.agentRuns.StartPlanExecution(context.Background(), task.ID, chatAttachmentProviderID, "model", defaultPlanExecutionBudget)
	if err != nil || !created {
		t.Fatalf("start=%+v %v", ex, err)
	}
	if _, err = e.agentRuns.PreparePlanTool(context.Background(), task.ID, ex.RunID, "workspace.write", json.RawMessage(`{"path":"report.md","content":"unknown"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = e.agentRuns.RunRecoveryScanner(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = e.agentRuns.RecoverPlanExecutions(context.Background()); err != nil {
		t.Fatal(err)
	}
	ex = waitPlanTerminal(t, e, task.ID)
	if ex.Status != "outcome_unknown" || calls.Load() != 0 {
		t.Fatalf("execution=%+v calls=%d", ex, calls.Load())
	}
	if err = e.agentRuns.RecoverPlanExecutions(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPlanExecutionRetryKeepsHistoryAndRejectsOldWorker(t *testing.T) {
	e, s, task := planExecutionFixture(t, "implementer", completeReport)
	ctx := context.Background()
	old, _, err := e.agentRuns.StartPlanExecution(ctx, task.ID, chatAttachmentProviderID, "model", defaultPlanExecutionBudget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.agentRuns.PreparePlanTool(ctx, task.ID, old.RunID, "workspace.write", json.RawMessage(`{"path":"report.md","content":"unknown"}`)); err != nil {
		t.Fatal(err)
	}
	if err = e.agentRuns.RecoverPlanExecutions(ctx); err != nil {
		t.Fatal(err)
	}
	old = waitPlanTerminal(t, e, task.ID)
	if _, _, err = e.agentRuns.RetryPlanExecution(ctx, task.ID, chatAttachmentProviderID, "model", defaultPlanExecutionBudget, old.Version, false); err == nil {
		t.Fatal("uncertain operation retried without acknowledgment")
	}
	current, created, err := e.agentRuns.RetryPlanExecution(ctx, task.ID, chatAttachmentProviderID, "model", defaultPlanExecutionBudget, old.Version, true)
	if err != nil || !created {
		t.Fatalf("retry=%+v created=%v err=%v", current, created, err)
	}
	if current.RunID == old.RunID || current.SessionID == old.SessionID || current.Spec.PreviousRunID != old.RunID {
		t.Fatalf("new attempt reused old identity: %+v", current)
	}
	if _, err = e.agentRuns.FinishPlanExecution(ctx, task.ID, old.RunID, "failed", "", "late old worker", nil, nil); !errors.Is(err, agentrun.ErrVersionConflict) {
		t.Fatalf("late old finish err=%v", err)
	}
	if _, err = e.agentRuns.PreparePlanTool(ctx, task.ID, old.RunID, "workspace.write", json.RawMessage(`{}`)); !errors.Is(err, agentrun.ErrVersionConflict) {
		t.Fatalf("late tool err=%v", err)
	}
	if _, _, err = e.agentRuns.RetryPlanExecution(ctx, task.ID, chatAttachmentProviderID, "model", defaultPlanExecutionBudget, old.Version, true); !errors.Is(err, agentrun.ErrVersionConflict) {
		t.Fatalf("repeated retry err=%v", err)
	}
	if err = e.executePlanTask(ctx, current); err != nil {
		t.Fatal(err)
	}
	got := waitPlanTerminal(t, e, task.ID)
	if got.RunID != current.RunID || got.Status != "succeeded" {
		t.Fatalf("current=%+v", got)
	}
	if err = s.AgentRuntimeRepository().Transact(ctx, func(tx agentrun.Tx) error {
		r, err := tx.GetRun(old.RunID)
		if err != nil {
			return err
		}
		if r.Status != agentrun.RunOutcomeUnknown {
			t.Fatalf("old history modified: %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
