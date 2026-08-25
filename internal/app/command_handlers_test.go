package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/canonpath"
	"github.com/lunitide/lunitide/internal/commandworker"
)

type commandJobOut struct {
	ID                string `json:"id"`
	RunID             string `json:"runId"`
	CommandSpecDigest string `json:"commandSpecDigest"`
	Status            string `json:"status"`
	ExitCode          *int64 `json:"exitCode"`
}

type commandReviewOut struct {
	CommandSpecDigest string `json:"commandSpecDigest"`
	ApprovalDigest    string `json:"approvalDigest"`
}

func cmdCall(e *Engine, method bridge.Method, payload map[string]any, key string) bridge.Response {
	body, _ := json.Marshal(payload)
	return e.Handle(context.Background(), agentRunRequest(method, string(body), key))
}

func cmdStartPayload(runID string, lease fsLeaseOut, extra map[string]any) map[string]any {
	p := map[string]any{
		"runId":           runID,
		"leaseId":         lease.ID,
		"fencingToken":    lease.FencingToken,
		"templateId":      "go.test",
		"templateVersion": "1.0.0",
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// approvedCmdStartPayload drives the durable command authorization contract:
// command.review.request -> review.decide -> command.start. The returned start
// payload pins both digests produced by the review request.
func approvedCmdStartPayload(t *testing.T, e *Engine, runID string, lease fsLeaseOut, extra map[string]any, key string) map[string]any {
	t.Helper()
	reviewPayload := cmdStartPayload(runID, lease, extra)
	delete(reviewPayload, "expectedSpecDigest")
	delete(reviewPayload, "approvalDigest")
	review := cmdCall(e, bridge.MethodCommandReviewRequest, reviewPayload, key+"-request")
	if !review.OK {
		t.Fatalf("command review request: code=%s msg=%s", review.Error.Code, review.Error.Message)
	}
	body, _ := json.Marshal(review.Payload)
	var reviewed commandReviewOut
	if err := json.Unmarshal(body, &reviewed); err != nil {
		t.Fatal(err)
	}
	if len(reviewed.CommandSpecDigest) != 64 || len(reviewed.ApprovalDigest) != 64 {
		t.Fatalf("command review digests=%+v", reviewed)
	}
	status, version := getRunStatus(t, e, runID)
	if status != "paused_review" {
		t.Fatalf("run status after command review request=%s", status)
	}
	decided := decideReview(t, e, runID, version, reviewed.ApprovalDigest, "approved", key+"-decide")
	if !decided.OK {
		t.Fatalf("command review decide: code=%s msg=%s", decided.Error.Code, decided.Error.Message)
	}
	startPayload := cmdStartPayload(runID, lease, extra)
	startPayload["expectedSpecDigest"] = reviewed.CommandSpecDigest
	startPayload["approvalDigest"] = reviewed.ApprovalDigest
	return startPayload
}

// awaitCommandJob polls command.get until the job reaches a terminal state.
func awaitCommandJob(t *testing.T, e *Engine, jobID string) commandJobOut {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res := cmdCall(e, bridge.MethodCommandGet, map[string]any{"jobId": jobID}, "")
		if !res.OK {
			t.Fatalf("get: code=%s msg=%s", res.Error.Code, res.Error.Message)
		}
		body, _ := json.Marshal(res.Payload)
		var job commandJobOut
		if err := json.Unmarshal(body, &job); err != nil {
			t.Fatal(err)
		}
		switch job.Status {
		case "completed", "failed", "cancelled", "outcome_unknown":
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never reached a terminal state", jobID)
	return commandJobOut{}
}

func TestCommandStartGetLifecycleCompletes(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	var gotSpec commandworker.Spec
	e.agentRuns.SetCommandRunner(func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error) {
		defer guard.Close()
		gotSpec = spec
		onOutput([]byte("ok\n"))
		return commandworker.Outcome{ExitCode: 0, OutputBytes: 3}, nil
	})
	lease := fsLease(t, e, t.TempDir(), []string{"read", "write", "execute"}, []string{"**"}, "cmd-life")
	run := startAgentRun(t, e, sessionID, "cmd-life-run")

	startPayload := approvedCmdStartPayload(t, e, run.ID, lease, map[string]any{"target": "./..."}, "cmd-life-review")
	res := cmdCall(e, bridge.MethodCommandStart, startPayload, "cmd-life-start")
	if !res.OK {
		t.Fatalf("start: code=%s msg=%s", res.Error.Code, res.Error.Message)
	}
	body, _ := json.Marshal(res.Payload)
	var started commandJobOut
	if err := json.Unmarshal(body, &started); err != nil {
		t.Fatal(err)
	}
	if started.Status != "running" || started.RunID != run.ID {
		t.Fatalf("started=%+v", started)
	}
	if len(started.CommandSpecDigest) != 64 || strings.ToLower(started.CommandSpecDigest) != started.CommandSpecDigest {
		t.Fatalf("spec digest wrong: %q", started.CommandSpecDigest)
	}

	finished := awaitCommandJob(t, e, started.ID)
	if finished.Status != "completed" || finished.ExitCode == nil || *finished.ExitCode != 0 {
		t.Fatalf("finished=%+v", finished)
	}
	// The worker spec must be shell-free: template argv plus the target, inside
	// the granted workspace root.
	if len(gotSpec.Args) != 3 || gotSpec.Args[0] != "test" || gotSpec.Args[1] != "-count=1" || gotSpec.Args[2] != "./..." {
		t.Fatalf("spec args=%v", gotSpec.Args)
	}
	if gotSpec.Timeout <= 0 || gotSpec.MaxOutputBytes <= 0 {
		t.Fatalf("spec limits=%+v", gotSpec)
	}
}

func TestCommandStartCwdMustMatchExecuteGrant(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	root := t.TempDir()
	for _, rel := range []string{"safe", "restricted", "project", "project/safe"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runnerCalled := make(chan commandworker.Spec, 1)
	e.agentRuns.SetCommandRunner(func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error) {
		defer guard.Close()
		runnerCalled <- spec
		return commandworker.Outcome{ExitCode: 0}, nil
	})

	t.Run("safe grant rejects root and unrelated cwd", func(t *testing.T) {
		lease := fsLease(t, e, root, []string{"execute"}, []string{"safe/**"}, "cmd-cwd-safe")
		for i, cwd := range []string{"", "restricted", "project"} {
			run := startAgentRun(t, e, sessionID, "cmd-cwd-denied-run-"+strconv.Itoa(i))
			res := cmdCall(e, bridge.MethodCommandReviewRequest, cmdStartPayload(run.ID, lease, map[string]any{"workDir": cwd}), "cmd-cwd-denied-review-"+strconv.Itoa(i))
			if res.OK || res.Error.Code != "FS_SCOPE_DENIED" {
				t.Fatalf("cwd %q: response=%#v", cwd, res)
			}
		}
	})

	t.Run("ancestor of nested grant is denied", func(t *testing.T) {
		lease := fsLease(t, e, root, []string{"execute"}, []string{"project/safe/**"}, "cmd-cwd-ancestor")
		run := startAgentRun(t, e, sessionID, "cmd-cwd-ancestor-run")
		res := cmdCall(e, bridge.MethodCommandReviewRequest, cmdStartPayload(run.ID, lease, map[string]any{"workDir": "project"}), "cmd-cwd-ancestor-review")
		if res.OK || res.Error.Code != "FS_SCOPE_DENIED" {
			t.Fatalf("response=%#v", res)
		}
	})

	t.Run("granted cwd starts with shell-free argv", func(t *testing.T) {
		lease := fsLease(t, e, root, []string{"execute"}, []string{"safe/**"}, "cmd-cwd-allowed")
		run := startAgentRun(t, e, sessionID, "cmd-cwd-allowed-run")
		payload := approvedCmdStartPayload(t, e, run.ID, lease, map[string]any{"workDir": "safe"}, "cmd-cwd-allowed-review")
		res := cmdCall(e, bridge.MethodCommandStart, payload, "cmd-cwd-allowed-start")
		if !res.OK {
			t.Fatalf("start: code=%s msg=%s", res.Error.Code, res.Error.Message)
		}
		select {
		case spec := <-runnerCalled:
			// The worker is pinned to the directory's real name, with short
			// components like RUNNER~1 expanded, while t.TempDir reports
			// whatever spelling TMP happens to carry. Comparing the two
			// directly only works where the profile name is short enough to
			// need no 8.3 alias — true on a developer laptop, false on CI.
			wantDir, err := canonpath.Canonical(filepath.Join(root, "safe"))
			if err != nil {
				t.Fatal(err)
			}
			if spec.Dir != wantDir {
				t.Fatalf("dir=%q, want %q", spec.Dir, wantDir)
			}
			if len(spec.Args) != 3 || spec.Args[0] != "test" || spec.Args[1] != "-count=1" || spec.Args[2] != "./..." {
				t.Fatalf("argv=%v", spec.Args)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("command runner was not called")
		}
	})
}

func TestCommandStartIdempotencyReplayAndConflict(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	var launches int
	started := make(chan struct{})
	release := make(chan struct{})
	e.agentRuns.SetCommandRunner(func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error) {
		defer guard.Close()
		launches++
		close(started)
		<-release
		return commandworker.Outcome{ExitCode: 0}, nil
	})
	lease := fsLease(t, e, t.TempDir(), []string{"read", "write", "execute"}, []string{"**"}, "cmd-idem")
	run := startAgentRun(t, e, sessionID, "cmd-idem-run")

	startPayload := approvedCmdStartPayload(t, e, run.ID, lease, nil, "cmd-idem-review")
	first := cmdCall(e, bridge.MethodCommandStart, startPayload, "cmd-idem-start")
	if !first.OK {
		t.Fatalf("first: code=%s msg=%s", first.Error.Code, first.Error.Message)
	}
	<-started
	replay := cmdCall(e, bridge.MethodCommandStart, startPayload, "cmd-idem-start")
	if !replay.OK {
		t.Fatalf("replay: code=%s msg=%s", replay.Error.Code, replay.Error.Message)
	}
	var a, b commandJobOut
	bodyA, _ := json.Marshal(first.Payload)
	bodyB, _ := json.Marshal(replay.Payload)
	if json.Unmarshal(bodyA, &a) != nil || json.Unmarshal(bodyB, &b) != nil || a.ID != b.ID {
		t.Fatalf("replay created a new job: %+v vs %+v", a, b)
	}
	if launches != 1 {
		t.Fatalf("idempotent replay launched %d workers", launches)
	}

	conflictPayload := cmdStartPayload(run.ID, lease, map[string]any{
		"target":             "./internal/...",
		"expectedSpecDigest": startPayload["expectedSpecDigest"],
		"approvalDigest":     startPayload["approvalDigest"],
	})
	conflict := cmdCall(e, bridge.MethodCommandStart, conflictPayload, "cmd-idem-start")
	if conflict.OK || conflict.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict=%#v", conflict)
	}
	close(release)
	awaitCommandJob(t, e, a.ID)
}

func TestCommandStartValidationFailures(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	e.agentRuns.SetCommandRunner(func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error) {
		defer guard.Close()
		t.Fatal("runner must not execute on validation failure")
		return commandworker.Outcome{}, nil
	})
	lease := fsLease(t, e, t.TempDir(), []string{"read", "write", "execute"}, []string{"**"}, "cmd-val")
	run := startAgentRun(t, e, sessionID, "cmd-val-run")
	approvedPayload := approvedCmdStartPayload(t, e, run.ID, lease, nil, "cmd-val-review")

	cases := []struct {
		name  string
		extra map[string]any
		code  string
	}{
		{"unknown template", map[string]any{"templateId": "rm.rf"}, "COMMAND_TEMPLATE_UNKNOWN"},
		{"version mismatch", map[string]any{"templateVersion": "9.9.9"}, "COMMAND_TEMPLATE_UNKNOWN"},
		{"target escape", map[string]any{"target": "../escape"}, "BRIDGE_SCHEMA_INVALID"},
		{"target whitespace", map[string]any{"target": "./...; rm"}, "BRIDGE_SCHEMA_INVALID"},
		{"target flag smuggling", map[string]any{"target": "-exec"}, "BRIDGE_SCHEMA_INVALID"},
		{"spec digest mismatch", map[string]any{"expectedSpecDigest": strings.Repeat("0", 64)}, "COMMAND_SPEC_MISMATCH"},
	}
	for i, tc := range cases {
		key := "cmd-val-case-" + strings.ReplaceAll(strings.ReplaceAll(tc.name, " ", "-"), ".", "-")
		payload := cmdStartPayload(run.ID, lease, tc.extra)
		payload["expectedSpecDigest"] = approvedPayload["expectedSpecDigest"]
		payload["approvalDigest"] = approvedPayload["approvalDigest"]
		if digest, ok := tc.extra["expectedSpecDigest"]; ok {
			payload["expectedSpecDigest"] = digest
		}
		res := cmdCall(e, bridge.MethodCommandStart, payload, key)
		if res.OK || res.Error.Code != tc.code {
			t.Fatalf("case %d (%s): code=%s want %s (%#v)", i, tc.name, res.Error.Code, tc.code, res)
		}
	}
}

func TestCommandCancelFirstTerminalWins(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	started := make(chan struct{}, 1)
	e.agentRuns.SetCommandRunner(func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error) {
		defer guard.Close()
		started <- struct{}{}
		<-ctx.Done()
		return commandworker.Outcome{}, ctx.Err()
	})
	lease := fsLease(t, e, t.TempDir(), []string{"read", "write", "execute"}, []string{"**"}, "cmd-cancel")
	run := startAgentRun(t, e, sessionID, "cmd-cancel-run")

	startPayload := approvedCmdStartPayload(t, e, run.ID, lease, nil, "cmd-cancel-review")
	res := cmdCall(e, bridge.MethodCommandStart, startPayload, "cmd-cancel-start")
	if !res.OK {
		t.Fatalf("start: code=%s msg=%s", res.Error.Code, res.Error.Message)
	}
	body, _ := json.Marshal(res.Payload)
	var job commandJobOut
	if json.Unmarshal(body, &job) != nil {
		t.Fatal("start payload decode")
	}
	<-started

	cancel := cmdCall(e, bridge.MethodCommandCancel, map[string]any{"jobId": job.ID}, "cmd-cancel-1")
	if !cancel.OK {
		t.Fatalf("cancel: code=%s msg=%s", cancel.Error.Code, cancel.Error.Message)
	}
	body, _ = json.Marshal(cancel.Payload)
	var cancelled commandJobOut
	if err := json.Unmarshal(body, &cancelled); err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}

	// A second cancel observes the terminal row (first-terminal-wins).
	again := cmdCall(e, bridge.MethodCommandCancel, map[string]any{"jobId": job.ID}, "cmd-cancel-2")
	if again.OK || again.Error.Code != "COMMAND_JOB_TERMINAL" {
		t.Fatalf("again=%#v", again)
	}

	// The racing completion path must not overwrite the cancelled state.
	time.Sleep(100 * time.Millisecond)
	final := cmdCall(e, bridge.MethodCommandGet, map[string]any{"jobId": job.ID}, "")
	body, _ = json.Marshal(final.Payload)
	var after commandJobOut
	if !final.OK || json.Unmarshal(body, &after) != nil || after.Status != "cancelled" {
		t.Fatalf("after=%+v ok=%v", after, final.OK)
	}
}

func TestCommandGetNotFound(t *testing.T) {
	e, _, _ := agentRunEngine(t)
	res := cmdCall(e, bridge.MethodCommandGet, map[string]any{"jobId": "01ARZ3NDEKTSV4RRFFQ69G5FAV"}, "")
	if res.OK || res.Error.Code != "COMMAND_JOB_NOT_FOUND" {
		t.Fatalf("res=%#v", res)
	}
}

func TestCommandStartRequiresRunningRun(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	e.agentRuns.SetCommandRunner(func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error) {
		defer guard.Close()
		t.Fatal("runner must not execute for a non-running run")
		return commandworker.Outcome{}, nil
	})
	lease := fsLease(t, e, t.TempDir(), []string{"read", "write", "execute"}, []string{"**"}, "cmd-state")
	run := startAgentRun(t, e, sessionID, "cmd-state-run")
	startPayload := approvedCmdStartPayload(t, e, run.ID, lease, nil, "cmd-state-review")
	_, version := getRunStatus(t, e, run.ID)

	cancelRun := e.Handle(context.Background(), agentRunRequest(bridge.MethodAgentRunCancel,
		`{"runId":"`+run.ID+`","expectedVersion":`+strconv.FormatInt(version, 10)+`}`, "cmd-state-cancel-run"))
	if !cancelRun.OK {
		t.Fatalf("cancel run: code=%s msg=%s", cancelRun.Error.Code, cancelRun.Error.Message)
	}
	res := cmdCall(e, bridge.MethodCommandStart, startPayload, "cmd-state-start")
	if res.OK || res.Error.Code != "COMMAND_JOB_TRANSITION_INVALID" {
		t.Fatalf("res=%#v", res)
	}
}
