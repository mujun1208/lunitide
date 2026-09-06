package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type planExecutionEngine struct {
	mu      sync.Mutex
	workers map[string]*planExecutionWorker
	closed  bool
	wg      sync.WaitGroup
}
type planExecutionWorker struct {
	cancel    context.CancelFunc
	ready     chan struct{}
	execution agentrun.PlanExecution
	err       error
}

var defaultPlanExecutionBudget = agentrun.Budget{MaxModelTurns: 16, MaxToolCalls: 64, MaxTokens: 50000, MaxCostMicros: 1000000, MaxWallClockSeconds: 300, MaxOutputBytes: 1 << 20, MaxRetries: 0, MaxNoProgress: 3, HardCeiling: true}

func (e *Engine) startPlanExecution(ctx context.Context, id string) (agentrun.PlanExecution, error) {
	return e.launchPlanExecution(ctx, id, 0, false)
}
func (e *Engine) launchPlanExecution(ctx context.Context, id string, expectedVersion int64, acknowledgeUncertain bool) (agentrun.PlanExecution, error) {
	if e.agentRuns == nil || e.tools == nil || e.providers == nil {
		return agentrun.PlanExecution{}, errors.New("plan executor unavailable")
	}
	if ex, err := e.agentRuns.GetPlanExecution(ctx, id); err == nil && expectedVersion == 0 {
		return ex, nil
	} else if err != nil && !errors.Is(err, agentrun.ErrNotFound) {
		return ex, err
	}
	x := &e.planExecutions
	x.mu.Lock()
	if x.closed {
		x.mu.Unlock()
		return agentrun.PlanExecution{}, errors.New("plan executor is stopping")
	}
	if old := x.workers[id]; old != nil {
		x.mu.Unlock()
		if expectedVersion > 0 {
			return agentrun.PlanExecution{}, agentrun.ErrVersionConflict
		}
		select {
		case <-old.ready:
			return old.execution, old.err
		case <-ctx.Done():
			return agentrun.PlanExecution{}, ctx.Err()
		}
	}
	if len(x.workers) >= 8 {
		x.mu.Unlock()
		return agentrun.PlanExecution{}, errors.New("plan executor capacity reached")
	}
	if x.workers == nil {
		x.workers = map[string]*planExecutionWorker{}
	}
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(defaultPlanExecutionBudget.MaxWallClockSeconds)*time.Second)
	w := &planExecutionWorker{cancel: cancel, ready: make(chan struct{})}
	x.workers[id] = w
	x.wg.Add(1)
	x.mu.Unlock()
	created := false
	providers, err := e.providers.List(ctx, provider.Filter{})
	if err == nil {
		chosen, ok := e.resolvePreferredChatModel(providers)
		if !ok {
			err = errors.New("no configured model for plan execution")
		} else {
			if expectedVersion > 0 {
				w.execution, created, err = e.agentRuns.RetryPlanExecution(ctx, id, chosen.Provider.ID, chosen.Model.ModelID, defaultPlanExecutionBudget, expectedVersion, acknowledgeUncertain)
			} else {
				w.execution, created, err = e.agentRuns.StartPlanExecution(ctx, id, chosen.Provider.ID, chosen.Model.ModelID, defaultPlanExecutionBudget)
			}
		}
	}
	w.err = err
	close(w.ready)
	if err != nil || !created || workCtx.Err() != nil {
		if created && workCtx.Err() != nil {
			e.failPlanExecution(w.execution, "cancelled", workCtx.Err())
		}
		e.finishPlanWorker(id, w)
		return w.execution, err
	}
	go func() {
		defer e.finishPlanWorker(id, w)
		defer func() {
			if recovered := recover(); recovered != nil {
				e.failPlanExecution(w.execution, "failed", fmt.Errorf("plan executor panic: %v", recovered))
			}
		}()
		execErr := e.executePlanTask(workCtx, w.execution)
		if execErr != nil {
			status := "failed"
			if errors.Is(execErr, context.Canceled) {
				status = "cancelled"
			} else if errors.Is(execErr, context.DeadlineExceeded) {
				status = "interrupted"
			}
			e.failPlanExecution(w.execution, status, execErr)
		}
	}()
	return w.execution, nil
}

func (e *Engine) finishPlanWorker(id string, w *planExecutionWorker) {
	w.cancel()
	x := &e.planExecutions
	x.mu.Lock()
	if x.workers[id] == w {
		delete(x.workers, id)
	}
	x.mu.Unlock()
	x.wg.Done()
}
func (e *Engine) failPlanExecution(ex agentrun.PlanExecution, status string, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	failure := cause.Error()
	if len(failure) > 4096 {
		failure = string([]rune(failure)[:min(len([]rune(failure)), 1000)])
	}
	if _, err := e.agentRuns.FinishPlanExecution(ctx, ex.PlanRunID, ex.RunID, status, "", failure, nil, nil); err != nil {
		log.Printf("plan execution %s finalization failed: %v", ex.PlanRunID, err)
	}
}

// StopPlanExecutions is called before closing tools or storage. The context
// cancellation reaches the provider and command host; terminal writes finish
// before the backing database is closed.
func (e *Engine) StopPlanExecutions() {
	x := &e.planExecutions
	x.mu.Lock()
	x.closed = true
	for _, w := range x.workers {
		w.cancel()
	}
	x.mu.Unlock()
	x.wg.Wait()
}

func (e *Engine) cancelPlanExecutionWorkers(ids map[string]bool) {
	x := &e.planExecutions
	x.mu.Lock()
	for id, w := range x.workers {
		if ids[id] {
			w.cancel()
		}
	}
	x.mu.Unlock()
}

const planExecutionPrompt = `你正在执行一个项目计划任务。仅使用已提供的工具，在本次任务的隔离会话工作区内工作，不得声称操作了用户其他目录。工具权限和命令白名单由引擎校验。
实际完成任务并将非空产物写入工作区。每个工具结果包含 receiptId；完成时调用 plan.finish，给出准确摘要、产物相对路径、能够证明工作的成功 receiptId。测试任务必须包含实际成功 command.run 回执，并生成测试报告。不能把工具失败、文字计划或未经验证的主张当成已完成。若无法完成，直接说明失败原因。`

func planExecutionTools() []llmadapter.ToolDefinition {
	allow := map[string]bool{"workspace.list": true, "workspace.read": true, "workspace.search": true, "workspace.write": true, "workspace.edit": true, "command.run": true}
	defs := filterToolDefs(engineToolDefinitions(), allow)
	return append(defs, llmadapter.ToolDefinition{Name: "plan.finish", Description: "Submit completed work for independent artifact and receipt validation. Must refer to actual nonempty files and successful receiptIds returned by tools.", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"summary":{"type":"string","minLength":1,"maxLength":16000},"artifacts":{"type":"array","minItems":1,"maxItems":32,"items":{"type":"string","minLength":1,"maxLength":512}},"receiptIds":{"type":"array","minItems":1,"maxItems":64,"items":{"type":"string"}}},"required":["summary","artifacts","receiptIds"]}`)})
}

func (e *Engine) executePlanTask(ctx context.Context, ex agentrun.PlanExecution) error {
	p, err := e.providers.Get(ctx, ex.Spec.ProviderID)
	if err != nil {
		return err
	}
	return e.withProviderLease(ctx, p, secretlease.OperationChat, func(op context.Context, credential []byte) error {
		a, err := e.adapter(op, p)
		if err != nil {
			return err
		}
		req := llmadapter.Request{Model: ex.Spec.ModelID, MaxTokens: 4096, MaxAttempts: 1, Messages: []llmadapter.Message{{Role: llmadapter.RoleSystem, Content: planExecutionPrompt}, {Role: llmadapter.RoleUser, Content: fmt.Sprintf("角色：%s\n任务：%s\n说明：%s", ex.Spec.Role, ex.Spec.Title, ex.Spec.Description)}}, Tools: planExecutionTools()}
		allowed := toolNameSet(req.Tools)
		seen := map[string]bool{}
		for turn := int64(0); turn < ex.Spec.Budget.MaxModelTurns; turn++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := e.agentRuns.ChargePlanExecution(op, ex.PlanRunID, ex.RunID, agentrun.Usage{ModelTurns: 1}); err != nil {
				return err
			}
			resp, err := e.completeMaybeRotate(op, a, credential, req)
			if err != nil {
				return err
			}
			if err = e.agentRuns.ChargePlanExecution(op, ex.PlanRunID, ex.RunID, agentrun.Usage{Tokens: int64(resp.Usage.TotalTokens), OutputBytes: int64(len(resp.Message.Content))}); err != nil {
				return err
			}
			if len(resp.Message.ToolCalls) == 0 {
				return errors.New("任务未提交可验证的产物与工具回执，未标记完成")
			}
			req.Messages = append(req.Messages, resp.Message)
			for _, call := range resp.Message.ToolCalls {
				if !allowed[call.Name] || call.ID == "" || seen[call.ID] {
					return errors.New("plan returned an invalid or repeated tool call")
				}
				seen[call.ID] = true
				if call.Name == "plan.finish" {
					if len(resp.Message.ToolCalls) != 1 {
						return errors.New("plan.finish must be the only call in its turn")
					}
					return e.verifyPlanArtifacts(op, ex, call.Arguments)
				}
				prepared, err := e.agentRuns.PreparePlanTool(op, ex.PlanRunID, ex.RunID, call.Name, call.Arguments)
				if err != nil {
					return err
				}
				var output toolruntime.Result
				toolErr := op.Err()
				dispatched := toolErr == nil
				if toolErr == nil {
					output, toolErr = e.tools.Execute(op, toolruntime.AutoEdit, ex.SessionID, call.Name, call.Arguments, true)
				}
				rctx, rcancel := context.WithTimeout(context.WithoutCancel(op), 3*time.Second)
				uncertain := dispatched && op.Err() != nil && (call.Name == "workspace.write" || call.Name == "workspace.edit" || call.Name == "command.run")
				receiptErr := e.agentRuns.ReceiptPlanTool(rctx, ex.PlanRunID, prepared.ID, output.Output, toolErr, uncertain)
				rcancel()
				if receiptErr != nil {
					return receiptErr
				}
				if err = ctx.Err(); err != nil {
					return err
				}
				if err = e.agentRuns.ChargePlanExecution(op, ex.PlanRunID, ex.RunID, agentrun.Usage{OutputBytes: int64(len(output.Output))}); err != nil {
					return err
				}
				body := map[string]any{"receiptId": prepared.ID, "ok": toolErr == nil, "output": output.Output}
				if toolErr != nil {
					body["error"] = toolErr.Error()
				}
				encoded, err := json.Marshal(body)
				if err != nil {
					return err
				}
				req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: string(encoded)})
			}
		}
		return errors.New("plan model-turn budget exhausted without verified completion")
	})
}

func (e *Engine) verifyPlanArtifacts(ctx context.Context, ex agentrun.PlanExecution, raw json.RawMessage) error {
	var finish struct {
		Summary    string   `json:"summary"`
		Artifacts  []string `json:"artifacts"`
		ReceiptIDs []string `json:"receiptIds"`
	}
	if decodePayload(raw, &finish) != nil || strings.TrimSpace(finish.Summary) == "" || len(finish.Summary) > 16384 || len(finish.Artifacts) < 1 || len(finish.Artifacts) > 32 || len(finish.ReceiptIDs) < 1 || len(finish.ReceiptIDs) > 64 {
		return errors.New("invalid plan completion evidence")
	}
	artifacts := make([]agentrun.PlanArtifact, 0, len(finish.Artifacts))
	seen := map[string]bool{}
	for _, path := range finish.Artifacts {
		if path == "" || len(path) > 512 || seen[path] {
			return errors.New("invalid or repeated artifact path")
		}
		seen[path] = true
		args, err := json.Marshal(map[string]string{"path": path})
		if err != nil {
			return err
		}
		read, err := e.tools.Execute(ctx, toolruntime.AutoEdit, ex.SessionID, "workspace.read", args, true)
		if err != nil {
			return fmt.Errorf("verify artifact %s: %w", path, err)
		}
		if len(read.Output) == 0 {
			return errors.New("empty artifact cannot prove task completion")
		}
		sum := sha256.Sum256([]byte(read.Output))
		artifacts = append(artifacts, agentrun.PlanArtifact{Path: path, Digest: hex.EncodeToString(sum[:]), Bytes: int64(len(read.Output))})
	}
	_, err := e.agentRuns.FinishPlanExecution(ctx, ex.PlanRunID, ex.RunID, "succeeded", finish.Summary, "", artifacts, finish.ReceiptIDs)
	return err
}
