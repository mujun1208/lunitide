package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m7app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// subagentFakeAdapter drives the sub-session loop: first Complete answers
// one read-only tool call, the second answers the final report.
type subagentFakeAdapter struct {
	calls     int
	seenTools []string
	seenMsgs  []gateway.Message
}

func (a *subagentFakeAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	a.calls++
	if a.calls == 1 {
		for _, t := range req.Tools {
			a.seenTools = append(a.seenTools, t.Name)
		}
		a.seenMsgs = req.Messages
		return gateway.Response{
			Message: gateway.Message{ToolCalls: []gateway.ToolCall{{
				ID: "sub-1", Name: "workspace.list", Arguments: json.RawMessage(`{"path":"."}`),
			}}},
			Usage: gateway.Usage{TotalTokens: 10},
		}, nil
	}
	return gateway.Response{
		Message: gateway.Message{Content: "research report: surveyed the workspace"},
		Usage:   gateway.Usage{TotalTokens: 5},
	}, nil
}

func (a *subagentFakeAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}

func (a *subagentFakeAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}

func newSubagentChatEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "sub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(m7app.NewSubagentService(repo), nil, nil)
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tools.Close() })
	e.SetToolRuntime(tools)
	return e
}

const subTestSession = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func subTestPolicy() subagentChatPolicy { return defaultSubagentChatPolicy() }

func TestSubagentToolDefinitionsTiers(t *testing.T) {
	e := newSubagentChatEngine(t)
	defs := e.subagentToolDefinitions(executionModeApproval, subTestPolicy())
	if len(defs) != 2 || defs[0].Name != "subagent.spawn" || defs[1].Name != "subagent.join" {
		t.Fatalf("explicit tier defs = %+v", defs)
	}
	// Plan mode never exposes delegation.
	if got := e.subagentToolDefinitions(executionModePlan, subTestPolicy()); len(got) != 0 {
		t.Fatalf("plan tier exposed tools: %+v", got)
	}
	// Disabled hides the tools.
	if err := e.SetDelegationMode(delegationDisabled); err != nil {
		t.Fatal(err)
	}
	if got := e.subagentToolDefinitions(executionModeApproval, subagentChatPolicy{DelegationMode: delegationDisabled}); len(got) != 0 {
		t.Fatalf("disabled tier exposed tools: %+v", got)
	}
	// Invalid mode is refused fail-closed.
	if err := e.SetDelegationMode(delegationMode("bogus")); err == nil {
		t.Fatal("invalid delegation mode accepted")
	}
}

func TestSubagentSpawnRunsReadOnlySessionAndReportsOnce(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &subagentFakeAdapter{}
	out, err := e.invokeSubagentTool(context.Background(), adapter, nil, "model-x", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"survey the workspace","profile":"explore"}`), subTestPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		SubagentID  string `json:"subagentId"`
		Status      string `json:"status"`
		Summary     string `json:"summary"`
		SpentTokens int64  `json:"spentTokens"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != "completed" || !strings.Contains(res.Summary, "research report") {
		t.Fatalf("spawn result = %+v", res)
	}
	if res.SpentTokens != 15 {
		t.Fatalf("spentTokens = %d, want 15", res.SpentTokens)
	}
	// The sub-session must be read-only: no workspace.write offered.
	for _, name := range adapter.seenTools {
		if name == "workspace.write" {
			t.Fatal("sub-session was offered workspace.write")
		}
		if name == "html.gen" {
			t.Fatal("sub-session was offered html.gen")
		}
	}
	if len(adapter.seenTools) == 0 {
		t.Fatal("sub-session received no tools")
	}
	// System prompt pins read-only semantics and the purpose is the user turn.
	if len(adapter.seenMsgs) < 2 || adapter.seenMsgs[0].Role != gateway.RoleSystem || !strings.Contains(adapter.seenMsgs[0].Content, "read-only") {
		t.Fatalf("sub-session messages = %+v", adapter.seenMsgs)
	}
	// The run is durable and terminal: join re-reads the same single report.
	joined, err := e.invokeSubagentTool(context.Background(), adapter, nil, "model-x", subTestSession, "subagent.join", json.RawMessage(`{"subagentId":"`+res.SubagentID+`"}`), subTestPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joined, "research report") {
		t.Fatalf("join result = %s", joined)
	}
}

func TestSubagentSpawnBudgetInheritanceAndGuards(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &subagentFakeAdapter{}
	// Explicit budget rides through as the sub-session MaxTokens (capped).
	if _, err := e.invokeSubagentTool(context.Background(), adapter, nil, "m", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"p","budgetTokens":2000}`), subTestPolicy()); err != nil {
		t.Fatal(err)
	}
	if adapter.seenMsgs == nil {
		t.Fatal("no sub-session started")
	}
	// Out-of-window budgets are refused before any spawn.
	if _, err := e.invokeSubagentTool(context.Background(), adapter, nil, "m", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"p","budgetTokens":10}`), subTestPolicy()); err == nil {
		t.Fatal("budget below window accepted")
	}
	// Empty purpose is refused.
	if _, err := e.invokeSubagentTool(context.Background(), adapter, nil, "m", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":""}`), subTestPolicy()); err == nil {
		t.Fatal("empty purpose accepted")
	}
}

func TestSubagentSpawnQuotaFailsClosed(t *testing.T) {
	e := newSubagentChatEngine(t)
	// Exhaust the frozen concurrency quota (max 4 live per root) directly
	// through the service, then verify the chat tool surfaces the guard.
	for i := 0; i < m7flow.SubagentMaxConcurrent; i++ {
		if _, err := e.m7subagent.Spawn(context.Background(), m7app.SpawnInput{
			RootRunID: subTestSession, Purpose: "hold",
			ReadCaps: defaultSubagentProfileCaps(), BudgetTokens: 1000,
			DeadlineMS: subagentDeadlineMS, IdempotencyKey: string(rune('a' + i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &subagentFakeAdapter{}
	_, err := e.invokeSubagentTool(context.Background(), adapter, nil, "m", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"one too many"}`), subTestPolicy())
	if err == nil || !errors.Is(err, m7app.ErrSubagentQuota) {
		t.Fatalf("quota guard not surfaced: %v", err)
	}
	if adapter.calls != 0 {
		t.Fatal("sub-session ran despite quota refusal")
	}
}

// parallelFakeAdapter delays every Complete call slightly and tracks the
// peak number of overlapping calls so parallel spawning is observable
// without wall-clock assertions.
type parallelFakeAdapter struct {
	active int64
	peak   int64
}

func (a *parallelFakeAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	cur := atomic.AddInt64(&a.active, 1)
	for {
		peak := atomic.LoadInt64(&a.peak)
		if cur <= peak || atomic.CompareAndSwapInt64(&a.peak, peak, cur) {
			break
		}
	}
	time.Sleep(120 * time.Millisecond)
	atomic.AddInt64(&a.active, -1)
	return gateway.Response{
		Message: gateway.Message{Content: "parallel report"},
		Usage:   gateway.Usage{TotalTokens: 5},
	}, nil
}

func (a *parallelFakeAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}

func (a *parallelFakeAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}

func TestParallelSubagentSpawnsOverlapInOneTurn(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &parallelFakeAdapter{}
	calls := []gateway.ToolCall{
		{ID: "s1", Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"task one"}`)},
		{ID: "s2", Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"task two"}`)},
		{ID: "j1", Name: "subagent.join", Arguments: json.RawMessage(`{"subagentId":"01ARZ3NDEKTSV4RRFFQ69G5FAVX"}`)},
	}
	futures := startSubagentFutures(context.Background(), e, adapter, nil, "m", subTestSession, calls, subTestPolicy())
	// Only spawn calls are pre-started; join stays inline.
	if len(futures) != 2 {
		t.Fatalf("pre-started futures = %d, want 2", len(futures))
	}
	// Consume in original call order exactly like the chat tool loop.
	for _, id := range []string{"s1", "s2"} {
		res := <-futures[id]
		if res.err != nil || !strings.Contains(res.summary, "parallel report") {
			t.Fatalf("future %s = %q err %v", id, res.summary, res.err)
		}
	}
	if peak := atomic.LoadInt64(&adapter.peak); peak < 2 {
		t.Fatalf("same-turn spawns did not overlap, peak concurrency = %d", peak)
	}
}

func TestParallelSubagentFuturesBoundedAtThree(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &parallelFakeAdapter{}
	calls := make([]gateway.ToolCall, 5)
	for i := range calls {
		calls[i] = gateway.ToolCall{ID: "p" + string(rune('0'+i)), Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"p"}`)}
	}
	futures := startSubagentFutures(context.Background(), e, adapter, nil, "m", subTestSession, calls, subTestPolicy())
	if len(futures) != maxParallelSubagentSpawns {
		t.Fatalf("pre-started futures = %d, want %d", len(futures), maxParallelSubagentSpawns)
	}
	// Drain every future so no goroutine outlives the store cleanup.
	for _, ch := range futures {
		<-ch
	}
}

// Overlapping the reads must not reorder them: the tool protocol pairs each
// result with its call by position as well as by id, so a concurrency bug
// here would hand the model another call's output.
func TestSubagentToolCallsKeepCallOrder(t *testing.T) {
	e := newSubagentChatEngine(t)
	profile, _, ok := resolveSubagentProfile(subTestPolicy(), "explore")
	if !ok {
		t.Fatal("explore profile missing")
	}
	allowed := map[string]bool{"workspace.list": true, "workspace.read": true}
	calls := []gateway.ToolCall{
		{ID: "a", Name: "workspace.list", Arguments: json.RawMessage(`{"path":"."}`)},
		{ID: "b", Name: "workspace.write", Arguments: json.RawMessage(`{"path":"x","content":"y"}`)},
		{ID: "c", Name: "workspace.list", Arguments: json.RawMessage(`{"path":"."}`)},
		{ID: "d", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"missing.txt"}`)},
	}
	msgs := e.runSubagentToolCalls(context.Background(), subTestSession, profile, allowed, calls)
	if len(msgs) != len(calls) {
		t.Fatalf("got %d messages for %d calls", len(msgs), len(calls))
	}
	for i, m := range msgs {
		if m.ToolCallID != calls[i].ID {
			t.Fatalf("message %d carries id %q, want %q", i, m.ToolCallID, calls[i].ID)
		}
		if m.Role != gateway.RoleTool {
			t.Fatalf("message %d role = %q", i, m.Role)
		}
	}
	// A tool outside the profile is refused rather than silently executed,
	// and the refusal stays on its own call.
	if !strings.Contains(msgs[1].Content, "not allowed") {
		t.Fatalf("write call was not refused: %q", msgs[1].Content)
	}
}

// neverFinishesAdapter keeps calling tools for as long as it is offered any,
// which is how a subagent burns its whole step budget. Once the tools are
// taken away it can only answer in prose.
type neverFinishesAdapter struct {
	calls       int
	toollessReq bool
}

func (a *neverFinishesAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	a.calls++
	if len(req.Tools) == 0 {
		a.toollessReq = true
		return gateway.Response{
			Message: gateway.Message{Content: "partial findings: read 3 files, the config still needs checking"},
			Usage:   gateway.Usage{TotalTokens: 7},
		}, nil
	}
	return gateway.Response{
		Message: gateway.Message{ToolCalls: []gateway.ToolCall{{
			ID: "loop", Name: "workspace.list", Arguments: json.RawMessage(`{"path":"."}`),
		}}},
		Usage: gateway.Usage{TotalTokens: 3},
	}, nil
}

func (a *neverFinishesAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}

func (a *neverFinishesAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}

// Hitting the step ceiling used to throw away every tool result the subagent
// had already paid for and hand the parent an error. The findings have to
// survive instead.
func TestSubagentOutOfStepsStillReportsWhatItFound(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &neverFinishesAdapter{}
	profile, _, ok := resolveSubagentProfile(subTestPolicy(), "explore")
	if !ok {
		t.Fatal("explore profile missing")
	}
	report, spent, err := e.executeSubagentLoop(context.Background(), adapter, nil, "model-x", subTestSession, "survey", subagentDefaultBudgetTokens, profile)
	if err != nil {
		t.Fatalf("exhausted subagent returned an error instead of a report: %v", err)
	}
	if !strings.Contains(report, "partial findings") {
		t.Fatalf("report = %q", report)
	}
	if !adapter.toollessReq {
		t.Fatal("the closing call still offered tools, so the model could keep looping")
	}
	// Every step plus the closing call is billed, so the parent's budget
	// accounting stays honest.
	if want := int64(profile.MaxSteps*3 + 7); spent != want {
		t.Fatalf("spent = %d, want %d", spent, want)
	}
}
