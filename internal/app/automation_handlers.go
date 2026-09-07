// automation.* bridge handlers (P2-3 resident automation): CRUD + manual
// trigger + run history + heartbeat status. The headless executor reuses
// the single chat kernel through HandleStreaming with an event collector -
// never a second execution core - so scheduled runs hit the same durable
// session pipeline (history, tools, approval policy) as interactive chat.
package app

import (
	"context"
	"crypto/sha256"
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
func handleAutomationJobList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
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
		Timezone      string `json:"timezone,omitempty"`
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
		Revision      string `json:"revision"`
	}
	out := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		scope, err := e.authorizeAutomationSession(ctx, j.SessionID)
		if err != nil {
			if dataScopeAccessError(err) {
				continue
			}
			return r.Fail("DATA_SCOPE_UNAVAILABLE", "无法确认自动化任务所属范围", true)
		}
		scope()
		v := jobView{ID: j.ID, Name: j.Name, Cron: j.Cron, Timezone: j.Timezone, Prompt: j.Prompt,
			ProviderID: j.ProviderID, ModelID: j.ModelID, SessionID: j.SessionID,
			ExecutionMode: j.ExecutionMode, SessionMode: j.SessionMode, RunOnce: j.RunOnce,
			WebhookURL: j.WebhookURL, Enabled: j.Enabled,
			CreatedAt: j.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: j.UpdatedAt.UTC().Format(time.RFC3339Nano), Revision: scheduler.JobRevision(j)}
		if !j.LastRunAt.IsZero() {
			v.LastRunAt = j.LastRunAt.UTC().Format(time.RFC3339)
		}
		out = append(out, v)
	}
	return r.Ok(map[string]any{"jobs": out})
}

// handleAutomationJobSet creates or updates one job.
func handleAutomationJobSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		ExpectedRevision string  `json:"expectedRevision"`
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		Cron             string  `json:"cron"`
		Timezone         *string `json:"timezone"`
		Prompt           string  `json:"prompt"`
		ProviderID       string  `json:"providerId"`
		ModelID          string  `json:"modelId"`
		SessionID        string  `json:"sessionId"`
		ExecutionMode    string  `json:"executionMode"`
		SessionMode      string  `json:"sessionMode"`
		RunOnce          bool    `json:"runOnce"`
		WebhookURL       string  `json:"webhookUrl"`
		Enabled          bool    `json:"enabled"`
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
	scope, scopeErr := e.authorizeAutomationSession(ctx, p.SessionID)
	if scopeErr != nil {
		return r.Fail("DATA_SCOPE_DENIED", "当前组织无法使用该自动化会话", false)
	}
	defer scope()
	now := time.Now().UTC()
	job := scheduler.Job{
		Name: p.Name, Cron: p.Cron, Prompt: p.Prompt,
		ProviderID: p.ProviderID, ModelID: p.ModelID, SessionID: p.SessionID,
		ExecutionMode: p.ExecutionMode, SessionMode: p.SessionMode, RunOnce: p.RunOnce,
		WebhookURL: p.WebhookURL, Enabled: p.Enabled,
		CreatedAt: now, UpdatedAt: now,
	}
	if p.Timezone != nil {
		job.Timezone = strings.TrimSpace(*p.Timezone)
	}
	if p.ID != "" {
		oldScope, failure := e.authorizeAutomationJob(ctx, r, p.ID)
		if failure != nil {
			return *failure
		}
		defer oldScope()
		if len(p.ExpectedRevision) != 64 {
			return r.Fail("SETTINGS_VERSION_CONFLICT", "请读取最新任务版本后保存", false)
		}
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
		if p.Timezone == nil {
			job.Timezone = existing.Timezone
		}
	} else {
		job.ID = r.ID
		if r.IdempotencyKey != "" {
			// Every valid bridge key is stable, including keys that are not ULIDs.
			digest := sha256.Sum256([]byte("automation.job.create/v1/" + r.IdempotencyKey))
			var stable ulid.ULID
			copy(stable[:], digest[:16])
			job.ID = stable.String()
		}
	}
	saved, err := e.automation.Store().PutJobVersioned(job, p.ExpectedRevision)
	if errors.Is(err, scheduler.ErrJobConflict) {
		return r.Fail("SETTINGS_VERSION_CONFLICT", "任务已被修改，当前草稿已保留，请刷新后重试", false)
	}
	if err != nil {
		if errors.Is(err, scheduler.ErrInvalid) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.job.set 参数无效", false)
		}
		return r.Fail("AUTOMATION_STORE_FAILED", "任务保存失败", true)
	}
	return r.Ok(map[string]any{"id": saved.ID, "createdAt": saved.CreatedAt.Format(time.RFC3339Nano), "revision": scheduler.JobRevision(saved)})
}

// handleAutomationJobDelete removes one job.
func handleAutomationJobDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ID) != 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.job.delete 参数无效", false)
	}
	scope, failure := e.authorizeAutomationJob(ctx, r, p.ID)
	if failure != nil {
		return *failure
	}
	defer scope()
	if err := e.automation.Store().DeleteJob(p.ID); err != nil {
		return r.Fail("AUTOMATION_STORE_FAILED", "任务删除失败", true)
	}
	return r.Ok(map[string]any{"deleted": true})
}

// handleAutomationJobTrigger fires one job immediately (manual run-now).
func handleAutomationJobTrigger(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ID) != 26 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.job.trigger 参数无效", false)
	}
	scope, failure := e.authorizeAutomationJob(ctx, r, p.ID)
	if failure != nil {
		return *failure
	}
	defer scope()
	if err := e.automation.TriggerNow(p.ID); err != nil {
		if errors.Is(err, scheduler.ErrPersistence) {
			return r.Fail("AUTOMATION_STORE_FAILED", "执行记录无法保存，任务未启动", true)
		}
		if strings.Contains(err.Error(), "not found") {
			return r.Fail("AUTOMATION_JOB_NOT_FOUND", "任务不存在", false)
		}
		return r.Fail("AUTOMATION_JOB_RUNNING", "任务正在执行，请稍后再试", false)
	}
	return r.Ok(map[string]any{"triggered": true})
}

func handleAutomationRunCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	var p struct {
		JobID string `json:"jobId"`
		RunID string `json:"runId"`
	}
	if decodePayload(r.Payload, &p) != nil || !ulidValid(p.JobID) || !ulidValid(p.RunID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.run.cancel 参数无效", false)
	}
	scope, failure := e.authorizeAutomationJob(ctx, r, p.JobID)
	if failure != nil {
		return *failure
	}
	defer scope()
	return r.Ok(map[string]any{"cancellationRequested": e.automation.CancelRun(p.JobID, p.RunID)})
}

// handleAutomationRunList answers the newest-first run history.
func handleAutomationRunList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
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
		ID             string      `json:"id"`
		JobID          string      `json:"jobId"`
		JobName        string      `json:"jobName"`
		SessionID      string      `json:"sessionId,omitempty"`
		Session        *sessionDTO `json:"session,omitempty"`
		State          string      `json:"state"`
		Trigger        string      `json:"trigger"`
		Summary        string      `json:"summary,omitempty"`
		TotalTokens    int64       `json:"totalTokens"`
		Error          string      `json:"error,omitempty"`
		StartedAt      string      `json:"startedAt"`
		FinishedAt     string      `json:"finishedAt,omitempty"`
		OutcomeUnknown bool        `json:"outcomeUnknown,omitempty"`
		Cancelled      bool        `json:"cancelled,omitempty"`
	}
	out := make([]runView, 0, len(runs))
	seen := map[string]bool{}
	for _, run := range runs {
		if seen[run.ID] {
			continue
		}
		seen[run.ID] = true
		if run.SessionID != "" {
			scope, err := e.authorizeAutomationSession(ctx, run.SessionID)
			if err != nil {
				if dataScopeAccessError(err) {
					continue
				}
				return r.Fail("DATA_SCOPE_UNAVAILABLE", "历史记录范围无法确认", true)
			}
			scope()
		} else if e.dataScope.store != nil || e.m9org != nil {
			scope, failure := e.authorizeAutomationJob(ctx, r, run.JobID)
			if failure != nil {
				if failure.Error.Code == "AUTOMATION_JOB_NOT_FOUND" || failure.Error.Code == "DATA_SCOPE_DENIED" {
					continue
				}
				return *failure
			}
			scope()
		}
		v := runView{ID: run.ID, JobID: run.JobID, JobName: run.JobName,
			State: run.State, SessionID: run.SessionID, Trigger: run.Trigger, Summary: run.Summary,
			TotalTokens: run.TotalTokens, Error: run.Error, OutcomeUnknown: run.OutcomeUnknown, Cancelled: run.Cancelled,
			StartedAt: run.StartedAt.UTC().Format(time.RFC3339)}
		if getter, ok := e.sessions.(sessionGetter); ok && sessionServiceAvailable(e.sessions) && run.SessionID != "" {
			if actual, err := getter.Get(ctx, run.SessionID); err == nil {
				dto := newSessionDTO(actual)
				v.Session = &dto
			}
		}
		if !run.FinishedAt.IsZero() {
			v.FinishedAt = run.FinishedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, v)
	}
	return r.Ok(map[string]any{"runs": out})
}

// handleAutomationStatus answers the scheduler heartbeat snapshot.
func handleAutomationStatus(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.automation == nil {
		return automationUnavailable(r)
	}
	s := e.automation.Snapshot()
	visible := map[string]bool{}
	jobs, err := e.automation.Store().ListJobs()
	if err != nil {
		return r.Fail("AUTOMATION_STORE_FAILED", "任务状态读取失败", true)
	}
	for _, job := range jobs {
		scope, err := e.authorizeAutomationSession(ctx, job.SessionID)
		if err != nil {
			if dataScopeAccessError(err) {
				continue
			}
			return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认", true)
		}
		scope()
		visible[job.ID] = true
	}
	for id := range s.NextFire {
		if !visible[id] {
			delete(s.NextFire, id)
		}
	}
	running := []string{}
	for _, id := range s.RunningJobs {
		if visible[id] {
			running = append(running, id)
		}
	}
	s.RunningJobs = running
	return r.Ok(map[string]any{
		"running":       s.Running,
		"startedAt":     stampOrEmpty(s.StartedAt),
		"lastHeartbeat": stampOrEmpty(s.LastHeartbeat),
		"nextFire":      s.NextFire,
		"runningJobs":   s.RunningJobs,
		"lastError":     s.LastError,
	})
}

func stampOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// AutomationHeadlessExecutor answers the executor that drives one scheduled
// job through the durable chat pipeline (chat.start via HandleStreaming
// with an event collector). The envelope bounds only startup; the scheduler
// context owns the complete run, including all model and tool passes.
func (e *Engine) AutomationHeadlessExecutor() scheduler.Executor {
	return func(ctx context.Context, job scheduler.Job) scheduler.Outcome {
		runCtx := ctx
		scope, scopeErr := e.authorizeAutomationSession(runCtx, job.SessionID)
		if scopeErr != nil {
			return scheduler.Outcome{Err: scopeErr, NotStarted: true}
		}
		isolatedID := ""
		if strings.TrimSpace(job.SessionMode) == "isolated" {
			isolatedID = e.isolatedAutomationSession(runCtx, job.SessionID)
		}
		scope()
		if strings.TrimSpace(job.SessionMode) == "isolated" && isolatedID == "" {
			return scheduler.Outcome{Err: errors.New("独立自动化会话创建失败，任务未执行"), NotStarted: true}
		}
		payloadMap := automationChatStartPayload(job, isolatedID)
		payload, err := json.Marshal(payloadMap)
		if err != nil {
			return scheduler.Outcome{Err: err, NotStarted: true}
		}

		req := bridge.Request{
			Version: bridge.Version, Kind: "request",
			ID: ulid.Make().String(), TraceID: ulid.Make().String(),
			Method: "chat.start", SentAt: time.Now().UTC(),
			Payload: payload, DeadlineMS: bridge.ChatStartDeadlineMS,
		}
		out := e.runHeadlessStream(runCtx, req)
		out.SessionID = job.SessionID
		if isolatedID != "" {
			out.SessionID = isolatedID
		}
		return out
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
