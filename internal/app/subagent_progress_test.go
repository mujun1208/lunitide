package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestSubagentProgressIsWholeBoundedJSON(t *testing.T) {
	for _, text := range []string{strings.Repeat("中文查询", 1000), strings.Repeat("\n\t\"<>&", 1000)} {
		p := subagentProgress{ID: subTestSession, Status: "running", Stage: "searching", Profile: text, Purpose: text, Tool: text, Detail: text}
		got := p.JSONSummary()
		if len(got) > 512 || !json.Valid([]byte(got)) || !utf8.ValidString(got) {
			t.Fatalf("invalid progress: bytes=%d, %q", len(got), got)
		}
		var decoded subagentProgress
		if err := json.Unmarshal([]byte(got), &decoded); err != nil || decoded.ID != p.ID || decoded.Status != p.Status {
			t.Fatalf("lost identity: %+v, %v", decoded, err)
		}
	}
}

func TestSubagentParallelProgressBeforeResultsAndDistinctRuns(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &parallelFakeAdapter{entered: make(chan struct{}, 2), release: make(chan struct{})}
	type update struct {
		callID   string
		progress subagentProgress
	}
	updates := make(chan update, 20)
	calls := []llmadapter.ToolCall{
		{ID: "one", Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"查询一个来源","profile":"research"}`)},
		{ID: "two", Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"查询另一个来源","profile":"research"}`)},
	}
	futures := startSubagentFutures(context.Background(), e, adapter, nil, "m", subTestSession, calls, subTestPolicy(), func(id string, p subagentProgress) { updates <- update{id, p} })
	released := false
	defer func() {
		if !released {
			close(adapter.release)
		}
		for _, ch := range futures {
			for range ch {
			}
		}
	}()
	ids := map[string]string{}
	for len(ids) < 2 {
		select {
		case event := <-updates:
			if event.progress.Stage != "thinking" {
				continue
			}
			if _, err := ulid.ParseStrict(event.progress.ID); err != nil {
				t.Fatal(err)
			}
			if event.progress.Status != "running" {
				t.Fatalf("non-running early event: %+v", event)
			}
			ids[event.callID] = event.progress.ID
		case <-time.After(10 * time.Second):
			t.Fatal("no live progress until results")
		}
	}
	if ids["one"] == ids["two"] {
		t.Fatal("two actual tasks shared a run ID")
	}
	for _, ch := range futures {
		select {
		case <-ch:
			t.Fatal("completed before adapter released")
		default:
		}
	}
	close(adapter.release)
	released = true
	for id, ch := range futures {
		res := <-ch
		if res.err != nil || !strings.Contains(res.summary, ids[id]) {
			t.Fatalf("future %s=%s %v", id, res.summary, res.err)
		}
	}
	terminal := map[string]bool{}
	for len(terminal) < 2 {
		select {
		case event := <-updates:
			if event.progress.Status == "completed" {
				terminal[event.callID] = true
			}
		case <-time.After(10 * time.Second):
			t.Fatal("missing terminal progress")
		}
	}
}

type subagentOutcomeAdapter struct {
	subagentFakeAdapter
	mode    string
	entered chan struct{}
}

func (a *subagentOutcomeAdapter) Complete(ctx context.Context, _ []byte, _ llmadapter.Request) (llmadapter.Response, error) {
	if a.entered != nil {
		a.entered <- struct{}{}
	}
	switch a.mode {
	case "cancel":
		<-ctx.Done()
		return llmadapter.Response{}, ctx.Err()
	case "panic":
		panic("isolated adapter failure")
	default:
		return llmadapter.Response{}, errors.New("provider unavailable")
	}
}

func TestSubagentFailedAndCancelledRunsAreTerminalAndReleaseQuota(t *testing.T) {
	for _, mode := range []string{"failure", "panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			e := newSubagentChatEngine(t)
			adapter := &subagentOutcomeAdapter{mode: mode, entered: make(chan struct{}, 1)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			updates := make(chan subagentProgress, 20)
			calls := []llmadapter.ToolCall{{ID: "sub", Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"检查失败收尾"}`)}}
			futures := startSubagentFutures(ctx, e, adapter, nil, "m", subTestSession, calls, subTestPolicy(), func(_ string, p subagentProgress) { updates <- p })
			select {
			case <-adapter.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("adapter never entered")
			}
			if mode == "cancel" {
				cancel()
			}
			var result subagentFutureResult
			select {
			case result = <-futures["sub"]:
			case <-time.After(10 * time.Second):
				t.Fatal("run did not terminate")
			}
			if result.err != nil {
				t.Fatal(result.err)
			}
			want := m7flow.SagFailed
			if mode == "cancel" {
				want = m7flow.SagCancelled
			}
			var final struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(result.summary), &final); err != nil || final.Status != want {
				t.Fatalf("status %+v %v", final, err)
			}
			runs, _, err := e.m7subagent.Tree(context.Background(), subTestSession, "", 100)
			if err != nil || len(runs) != 1 || runs[0].Status != want || runs[0].CompletedAt == nil {
				t.Fatalf("durable outcome=%+v %v", runs, err)
			}
			found := false
			for len(updates) > 0 {
				if (<-updates).Status == want {
					found = true
				}
			}
			if !found {
				t.Fatal("missing actual terminal progress")
			}
			for i := 0; i < m7flow.SubagentMaxConcurrent; i++ {
				if _, err := e.m7subagent.Spawn(context.Background(), m7app.SpawnInput{RootRunID: subTestSession, Purpose: "quota released", ReadCaps: defaultSubagentProfileCaps(), BudgetTokens: 1000, DeadlineMS: subagentDeadlineMS, IdempotencyKey: ulid.Make().String()}); err != nil {
					t.Fatalf("terminal run held quota: %v", err)
				}
			}
		})
	}
}

func TestSubagentProgressReportsRealToolExecution(t *testing.T) {
	e := newSubagentChatEngine(t)
	if _, err := e.tools.Execute(context.Background(), toolruntime.FullAccess, subTestSession, "workspace.write", json.RawMessage(`{"path":"seed.txt","content":"test fixture"}`), false); err != nil {
		t.Fatal(err)
	}
	updates := []subagentProgress{}
	ctx := withSubagentObserver(context.Background(), func(p subagentProgress) { updates = append(updates, p) })
	if _, err := e.invokeSubagentTool(ctx, &subagentFakeAdapter{}, nil, "m", subTestSession, "subagent.spawn", json.RawMessage(`{"purpose":"read files","profile":"explore"}`), subTestPolicy()); err != nil {
		t.Fatal(err)
	}
	started, finished := false, false
	for _, p := range updates {
		if p.Tool == "workspace.list" {
			started = started || p.Detail == "正在执行"
			finished = finished || p.Detail == "工具已完成"
		}
	}
	if !started || !finished {
		t.Fatalf("missing real tool phases: %+v", updates)
	}
}
