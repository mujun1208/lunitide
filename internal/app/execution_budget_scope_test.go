package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

// budgetScopeHarness wires a real store, a real session and the production
// execution budget, so admission runs against persisted usage rather than a
// stub. completionTokens is what upstream reports per call: the settled output
// is what accumulates against the policy.
func budgetScopeHarness(t *testing.T, completionTokens int) (llmadapter.Adapter, string) {
	a, sessionID, _ := budgetScopeHarnessWithStore(t, completionTokens)
	return a, sessionID
}

func budgetScopeHarnessWithStore(t *testing.T, completionTokens int) (llmadapter.Adapter, string, *storage.Store) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "budget-scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	projects := projectapp.New(store, store)
	proj, err := projects.Create(context.Background(), "budget-scope-project", "test", struct {
		Name string `json:"name"`
	}{"Parent"}, project.Project{Name: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	sess, err := sessions.Create(context.Background(), "budget-scope-session", "test", struct {
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
	}{proj.ID, "月伴对话"}, session.Session{ProjectID: proj.ID, Title: "月伴对话"})
	if err != nil {
		t.Fatal(err)
	}

	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetCallAttemptStore(&memCalls{})
	AttachProductionExecutionBudget(e, store.AgentRuntimeRepository())
	e.SetCompileProfile(glmPreflightProfile())
	body := fmt.Sprintf(
		`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":4,"completion_tokens":%d,"total_tokens":%d}}`,
		completionTokens, completionTokens+4,
	)
	inner := llmadapter.NewOpenAI(roundTripConnector{rt: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, llmadapter.Options{MaxAttempts: 1})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return inner, nil
	})
	a, err := e.adapter(context.Background(), provider.Provider{
		ID:       ulid.Make().String(),
		Protocol: provider.ProtocolOpenAICompatible,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, sess.ID, store
}

// TestProbeBudgetLedger is a diagnostic: it reports what the ledger actually
// records for one settled call, so the scoping tests reason about measured
// usage instead of assumed usage.
func TestProbeBudgetLedger(t *testing.T) {
	for _, tc := range []struct {
		name             string
		completionTokens int
	}{
		{"within the reserved cap", 1_000},
		{"overruns the reserved cap", 40_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, sessionID, store := budgetScopeHarnessWithStore(t, tc.completionTokens)
			ledger := agentrunapp.NewExecutionBudget(store.AgentRuntimeRepository())
			for call := 1; call <= 3; call++ {
				turnID := ulid.Make().String()
				ctx := budgetScopeTurn(sessionID, turnID)
				if _, err := a.Complete(ctx, []byte("k"), budgetScopeRequest()); err != nil {
					t.Fatalf("call %d: %v", call, err)
				}
				snap, err := ledger.Snapshot(context.Background(), agentrun.ExecutionScope{TaskID: turnID, ScopeID: turnID})
				if err != nil {
					t.Fatalf("snapshot after call %d: %v", call, err)
				}
				t.Logf("LEDGER call=%d consumedTotal=%d consumedOutput=%d reservedTotal=%d isolated=%d attempts=%d integrity=%s",
					call, snap.ConsumedTotal, snap.ConsumedOutput, snap.ReservedTotal, snap.IsolatedTotal, snap.Attempts, snap.Integrity)
			}
		})
	}
}

func budgetScopeRequest() llmadapter.Request {
	return llmadapter.Request{
		Model:     "glm-5.3",
		Mode:      "standard",
		MaxTokens: 32768,
		Messages:  []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我打开网站，搜索最新消息"}},
	}
}

func budgetScopeTurn(sessionID, turnID string) context.Context {
	return withContinuityScope(context.Background(), continuityScope{
		Owner: ownerScope(sessionID), Task: sessionID, Turn: turnID, Purpose: "chat",
	})
}

// Typed chat and 月伴 share one session for as long as the install lives.
// Each user turn gets a fresh allowance, so an earlier heavy turn cannot
// refuse the next one.
func TestSessionLifetimeDoesNotExhaustLaterTurns(t *testing.T) {
	a, sessionID := budgetScopeHarness(t, 40_000)
	req := budgetScopeRequest()
	for _, purpose := range []string{"chat", "companion"} {
		for turn := 1; turn <= 3; turn++ {
			ctx := withContinuityScope(context.Background(), continuityScope{
				Owner: ownerScope(sessionID), Task: sessionID, Turn: ulid.Make().String(), Purpose: purpose,
			})
			if _, err := a.Complete(ctx, []byte("k"), req); err != nil {
				t.Fatalf("%s turn %d of one long-lived session was refused: %v", purpose, turn, err)
			}
		}
	}
}

// A full tool loop that overruns the reserved output cap with thinking tokens
// must still be admitted, and every call must settle. A leaked reservation
// is what used to freeze the next call.
func TestFullToolLoopSettlesInsideOneTurn(t *testing.T) {
	const output = 40_000
	a, sessionID, store := budgetScopeHarnessWithStore(t, output)
	req := budgetScopeRequest()
	turnID := ulid.Make().String()
	ctx := budgetScopeTurn(sessionID, turnID)
	for call := 1; call <= maxToolLoopStepsHard; call++ {
		if _, err := a.Complete(ctx, []byte("k"), req); err != nil {
			t.Fatalf("tool step %d/%d was refused: %v", call, maxToolLoopStepsHard, err)
		}
	}
	snap, err := agentrunapp.NewExecutionBudget(store.AgentRuntimeRepository()).Snapshot(context.Background(), agentrun.ExecutionScope{TaskID: turnID, ScopeID: turnID})
	if err != nil {
		t.Fatal(err)
	}
	if snap.ReservedTotal != 0 {
		t.Fatalf("reservations leaked after settlement: reserved=%d", snap.ReservedTotal)
	}
	if snap.ConsumedOutput != int64(maxToolLoopStepsHard)*output {
		t.Fatalf("settled output=%d want %d", snap.ConsumedOutput, int64(maxToolLoopStepsHard)*output)
	}
}

// The per-turn allowance is still a runaway guard. Refusal must come after a
// normal tool loop, and only once settled usage actually fills the ceiling.
func TestOneTurnStillHitsItsOwnAllowance(t *testing.T) {
	const output = 40_000
	a, sessionID, store := budgetScopeHarnessWithStore(t, output)
	req := budgetScopeRequest()
	turnID := ulid.Make().String()
	ctx := budgetScopeTurn(sessionID, turnID)
	policy := defaultChatExecutionPolicy()
	limit := *policy.MaxOutputTokens
	var calls int
	for calls = 1; calls <= int(limit/output)+3; calls++ {
		_, err := a.Complete(ctx, []byte("k"), req)
		if errors.Is(err, agentrun.ErrExecutionBudget) {
			if calls <= maxToolLoopStepsHard {
				t.Fatalf("refused at call %d, inside a normal tool loop", calls)
			}
			snap, snapErr := agentrunapp.NewExecutionBudget(store.AgentRuntimeRepository()).Snapshot(context.Background(), agentrun.ExecutionScope{TaskID: turnID, ScopeID: turnID})
			if snapErr != nil {
				t.Fatal(snapErr)
			}
			if snap.ReservedTotal != 0 {
				t.Fatalf("refusal came from a leaked reservation, reserved=%d consumedOutput=%d", snap.ReservedTotal, snap.ConsumedOutput)
			}
			if snap.ConsumedOutput < int64(maxToolLoopStepsHard)*output {
				t.Fatalf("refusal before settled usage filled a tool loop: consumedOutput=%d", snap.ConsumedOutput)
			}
			return
		}
		if err != nil {
			t.Fatalf("call %d: %v", calls, err)
		}
	}
	t.Fatalf("runaway turn was never refused after %d calls", calls-1)
}

func TestCompanionTurnDoesNotUseExecutionBudget(t *testing.T) {
	const output = 40_000
	a, sessionID, store := budgetScopeHarnessWithStore(t, output)
	req := budgetScopeRequest()
	turnID := ulid.Make().String()
	ctx := withContinuityScope(context.Background(), continuityScope{
		Owner: ownerScope(sessionID), Task: sessionID, Turn: turnID, Purpose: "companion",
		SkipExecutionBudget: true,
	})
	for call := 1; call <= maxToolLoopStepsHard; call++ {
		if _, err := a.Complete(ctx, []byte("k"), req); err != nil {
			t.Fatalf("companion step %d was refused: %v", call, err)
		}
	}
	// A nested call replaces Purpose but stays inside the spoken turn.
	nested := withCallPurpose(ctx, "council")
	if _, err := a.Complete(nested, []byte("k"), req); err != nil {
		t.Fatalf("companion nested call was refused: %v", err)
	}
	_, err := agentrunapp.NewExecutionBudget(store.AgentRuntimeRepository()).Snapshot(context.Background(), agentrun.ExecutionScope{TaskID: turnID, ScopeID: turnID})
	if !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("voice turn opened an execution ledger: %v", err)
	}
}

func TestCompactionDoesNotConsumeTheTurnLedger(t *testing.T) {
	a, sessionID := budgetScopeHarness(t, 40_000)
	req := budgetScopeRequest()
	ctx := withContinuityScope(context.Background(), continuityScope{
		Owner: ownerScope(sessionID), Task: sessionID, Purpose: "compaction",
	})
	for call := 1; call <= 3; call++ {
		if _, err := a.Complete(ctx, []byte("k"), req); err != nil {
			t.Fatalf("compaction call %d was refused: %v", call, err)
		}
	}
}
