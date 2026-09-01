// automation.* bridge handlers (P2-3 resident automation): CRUD + manual
// trigger + run history + heartbeat status. The headless executor reuses
// the single chat kernel through HandleStreaming with an event collector -
// never a second execution core - so scheduled runs hit the same durable
// session pipeline (history, tools, approval policy) as interactive chat.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/scheduler"
)

const isolatedAutomationTitle = "新对话"

func automationUnavailable(r bridge.Request) bridge.Response {
	return r.Fail("FEATURE_DISABLED", "自动化调度器未初始化", false)
}

// handleAutomationJobList answers all jobs (executionMode normalized).
func handleAutomationJobList(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	jobs, err := e.automation.Store().ListJobs()
	if err != nil {
		return r.Fail("AUTOMATION_STORE_FAILED", "任务列表读取失败", true)
	}
	type jobView struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Cron          string `json:"cron"`
		Prompt        string `json:"prompt"`
		ProviderID    string `json:"providerId"`
		ModelID       string `json:"modelId"`
		SessionID     string `json:"sessionId"`
		ExecutionMode string `json:"executionMode,omitempty"`
		SessionMode   string `json:"sessionMode,omitempty"`
		RunOnce       bool   `json:"runOnce,omitempty"`
		WebhookURL    string `json:"webhookUrl,omitempty"`
		Enabled       bool   `json:"enabled"`
		LastRunAt     string `json:"lastRunAt,omitempty"`
		CreatedAt     string `json:"createdAt"`
		UpdatedAt     string `json:"updatedAt"`
	}
	out := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		v := jobView{ID: j.ID, Name: j.Name, Cron: j.Cron, Prompt: j.Prompt,
			ProviderID: j.ProviderID, ModelID: j.ModelID, SessionID: j.SessionID,
			ExecutionMode: j.ExecutionMode, SessionMode: j.SessionMode, RunOnce: j.RunOnce,
			WebhookURL: j.WebhookURL, Enabled: j.Enabled,
			CreatedAt: j.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: j.UpdatedAt.UTC().Format(time.RFC3339)}
		if !j.LastRunAt.IsZero() {
			v.LastRunAt = j.LastRunAt.UTC().Format(time.RFC3339)
		}
		out = append(out, v)
	}
	return r.Ok(map[string]any{"jobs": out})
}

// handleAutomationJobSet creates or updates one job.
func handleAutomationJobSet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Cron          string `json:"cron"`
		Prompt        string `json:"prompt"`
		ProviderID    string `json:"providerId"`
		ModelID       string `json:"modelId"`
		SessionID     string `json:"sessionId"`
		ExecutionMode string `json:"executionMode"`
		SessionMode   string `json:"sessionMode"`
		RunOnce       bool   `json:"runOnce"`
		WebhookURL    string `json:"webhookUrl"`
		Enabled       bool   `json:"enabled"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Name == "" || len([]rune(p.Name)) > 64 ||
		p.ModelID == "" || len(p.ModelID) > 128 || len(p.ID) > 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.job.set 参数无效", false)
	}
	if err := scheduler.ParseSchedule(p.Cron); err != nil {
		return r.Fail("AUTOMATION_CRON_INVALID", "cron 表达式无效（需 5 字段或 at:RFC3339）", false)
	}
	if p.ExecutionMode != "" {
		if _, ok := normalizeExecutionMode(executionMode(p.ExecutionMode)); !ok {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "executionMode 无效", false)
		}
	}
	if err := scheduler.ValidateWebhookURL(p.WebhookURL); err != nil {
		return r.Fail("AUTOMATION_WEBHOOK_INVALID", "webhook 地址无效（需 https 且不允许内网/IP 地址）", false)
	}
	now := time.Now().UTC()
	job := scheduler.Job{
		Name: p.Name, Cron: p.Cron, Prompt: p.Prompt,
		ProviderID: p.ProviderID, ModelID: p.ModelID, SessionID: p.SessionID,
		ExecutionMode: p.ExecutionMode, SessionMode: p.SessionMode, RunOnce: p.RunOnce,
		WebhookURL: p.WebhookURL, Enabled: p.Enabled,
		CreatedAt: now, UpdatedAt: now,
	}
	if p.ID != "" {
		existing, ok, err := e.automation.Store().GetJob(p.ID)
		if err != nil {
			return r.Fail("AUTOMATION_STORE_FAILED", "任务读取失败", true)
		}
		if !ok {
			return r.Fail("AUTOMATION_JOB_NOT_FOUND", "任务不存在", false)
		}
		job.ID = p.ID
		job.CreatedAt = existing.CreatedAt
		job.LastRunAt = existing.LastRunAt
	} else {
		job.ID = ulid.Make().String()
	}
	if err := e.automation.Store().PutJob(job); err != nil {
		if errors.Is(err, scheduler.ErrInvalid) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.job.set 参数无效", false)
		}
		return r.Fail("AUTOMATION_STORE_FAILED", "任务保存失败", true)
	}
	return r.Ok(map[string]any{"id": job.ID, "createdAt": job.CreatedAt.Format(time.RFC3339)})
}

// handleAutomationJobDelete removes one job.
func handleAutomationJobDelete(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ID) != 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.job.delete 参数无效", false)
	}
	if err := e.automation.Store().DeleteJob(p.ID); err != nil {
		return r.Fail("AUTOMATION_STORE_FAILED", "任务删除失败", true)
	}
	return r.Ok(map[string]any{"deleted": true})
}

// handleAutomationJobTrigger fires one job immediately (manual run-now).
func handleAutomationJobTrigger(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ID) != 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.job.trigger 参数无效", false)
	}
	if err := e.automation.TriggerNow(p.ID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return r.Fail("AUTOMATION_JOB_NOT_FOUND", "任务不存在", false)
		}
		return r.Fail("AUTOMATION_JOB_RUNNING", "任务正在执行，请稍后再试", false)
	}
	return r.Ok(map[string]any{"triggered": true})
}

// handleAutomationRunList answers the newest-first run history.
func handleAutomationRunList(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		JobID string `json:"jobId"`
		Limit int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.JobID != "" && len(p.JobID) != 26) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.run.list 参数无效", false)
	}
	if p.Limit < 1 || p.Limit > 100 {
		p.Limit = 50
	}
	runs, err := e.automation.Store().ListRuns(p.JobID, p.Limit)
	if err != nil {
		return r.Fail("AUTOMATION_STORE_FAILED", "运行历史读取失败", true)
	}
	type runView struct {
		ID          string `json:"id"`
		JobID       string `json:"jobId"`
		JobName     string `json:"jobName"`
		State       string `json:"state"`
		Trigger     string `json:"trigger"`
		Summary     string `json:"summary,omitempty"`
		TotalTokens int64  `json:"totalTokens"`
		Error       string `json:"error,omitempty"`
		StartedAt   string `json:"startedAt"`
		FinishedAt  string `json:"finishedAt,omitempty"`
	}
	out := make([]runView, 0, len(runs))
	for _, run := range runs {
		v := runView{ID: run.ID, JobID: run.JobID, JobName: run.JobName,
			State: run.State, Trigger: run.Trigger, Summary: run.Summary,
			TotalTokens: run.TotalTokens, Error: run.Error,
			StartedAt: run.StartedAt.UTC().Format(time.RFC3339)}
		if !run.FinishedAt.IsZero() {
			v.FinishedAt = run.FinishedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, v)
	}
	return r.Ok(map[string]any{"runs": out})
}

// handleAutomationStatus answers the scheduler heartbeat snapshot.
func handleAutomationStatus(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	s := e.automation.Snapshot()
	return r.Ok(map[string]any{
		"running":       s.Running,
		"startedAt":     stampOrEmpty(s.StartedAt),
		"lastHeartbeat": stampOrEmpty(s.LastHeartbeat),
		"nextFire":      s.NextFire,
		"runningJobs":   s.RunningJobs,
	})
}

func stampOrEmpty(t time.Time) string {
	if t.IsZero() {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return t.UTC().Format(time.RFC3339)
}

// AutomationHeadlessExecutor answers the executor that drives one scheduled
// job through the durable chat pipeline (chat.start via HandleStreaming
// with an event collector). Envelope deadline must stay inside
// bridge.ChatStartDeadlineMS — 600000 was rejected as 请求超时参数无效.
func (e *Engine) AutomationHeadlessExecutor() scheduler.Executor {
	return func(ctx context.Context, job scheduler.Job) scheduler.Outcome {
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(bridge.ChatStartDeadlineMS)*time.Millisecond)
		defer cancel()
		isolatedID := ""
		if strings.TrimSpace(job.SessionMode) == "isolated" {
			isolatedID = e.isolatedAutomationSession(runCtx, job.SessionID)
		}
		payloadMap := automationChatStartPayload(job, isolatedID)
		payload, err := json.Marshal(payloadMap)
		if err != nil {
			return scheduler.Outcome{Err: err}
		}
		var text strings.Builder
		var tokens int64
		var streamErr *bridge.StreamError
		req := bridge.Request{
			Version: bridge.Version, Kind: "request",
			ID: ulid.Make().String(), TraceID: ulid.Make().String(),
			Method: "chat.start", SentAt: time.Now().UTC(),
			Payload: payload, DeadlineMS: bridge.ChatStartDeadlineMS,
		}
		resp := e.HandleStreaming(runCtx, req, func(ev bridge.Event) error {
			switch {
			case ev.Delta != nil:
				text.WriteString(ev.Delta.Text)
			case ev.Usage != nil:
				tokens += int64(ev.Usage.TotalTokens)
			case ev.Error != nil:
				streamErr = ev.Error
			}
			return nil
		})
		if !resp.OK {
			code, message := "AUTOMATION_RUN_FAILED", "无头执行失败"
			if resp.Error != nil {
				code, message = resp.Error.Code, resp.Error.Message
			}
			return scheduler.Outcome{TotalTokens: tokens, Err: errors.New(message + " (" + code + ")")}
		}
		if streamErr != nil {
			return scheduler.Outcome{TotalTokens: tokens, Err: errors.New(streamErr.Message + " (" + streamErr.Code + ")")}
		}
		return scheduler.Outcome{Summary: text.String(), TotalTokens: tokens}
	}
}

// automationChatStartPayload builds chat.start for a scheduled fire.
// Isolated jobs never reuse the bound sessionId; they use a fresh session
// when one was created, otherwise messages-only so the main chat stays clean.
func automationChatStartPayload(job scheduler.Job, isolatedSessionID string) map[string]any {
	mode := job.ExecutionMode
	if mode == "" {
		mode = "auto-edit"
	}
	payload := map[string]any{
		"providerId":    job.ProviderID,
		"modelId":       job.ModelID,
		"messages":      []map[string]string{{"role": "user", "content": job.Prompt}},
		"executionMode": mode,
	}
	if strings.TrimSpace(job.SessionMode) == "isolated" {
		if isolatedSessionID != "" {
			payload["sessionId"] = isolatedSessionID
		}
		return payload
	}
	payload["sessionId"] = job.SessionID
	return payload
}

// isolatedAutomationSession creates a placeholder-titled chat in the bound
// session's project. The sidebar already hides 「新对话」, so each fire stays
// off the 对话 list. Returns empty when sessions storage is unavailable.
func (e *Engine) isolatedAutomationSession(ctx context.Context, boundSessionID string) string {
	if e == nil || !sessionServiceAvailable(e.sessions) {
		return ""
	}
	title, err := session.NormalizeTitle(isolatedAutomationTitle)
	if err != nil {
		return ""
	}
	projectID := e.projectIDForSession(ctx, boundSessionID)
	if projectID == "" {
		return ""
	}
	created, err := e.sessions.Create(ctx, ulid.Make().String(), "automation", map[string]string{
		"projectId": projectID,
		"title":     title,
	}, session.Session{ProjectID: projectID, Title: title})
	if err != nil {
		return ""
	}
	if _, dirErr := e.sessionOutputDir(created.ID); dirErr != nil {
		_ = dirErr
	}
	return created.ID
}
