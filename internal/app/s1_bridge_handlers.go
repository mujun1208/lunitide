package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/fileops"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/ocrapp"
)

func handleChatUsageGet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		TaskID    string `json:"taskId"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(request.Payload, &p) != nil || !looksLikeULID(p.SessionID) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.usage.get 参数无效", false)
	}
	out := map[string]any{
		"sessionId": p.SessionID, "collected": false, "integrity": "unknown",
		"inputTokens": 0, "outputTokens": 0, "attempts": []any{},
	}
	if e == nil || e.callAttempts == nil {
		return request.Ok(out)
	}
	attempts, err := e.callAttempts.ListCallAttempts(ctx, ownerScope(p.SessionID), p.TaskID, p.Limit)
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "用量账本暂时不可用", true)
	}
	sum, err := e.callAttempts.SumCallAttemptsByOwner(ctx, ownerScope(p.SessionID), p.TaskID)
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "用量账本暂时不可用", true)
	}
	items := make([]map[string]any, 0, len(attempts))
	cached, cacheWrite := 0, 0
	cacheReported := false
	for _, rec := range attempts {
		cached += rec.CachedInputTokens
		cacheWrite += rec.CacheWriteTokens
		if rec.CachedInputTokens > 0 || rec.CacheWriteTokens > 0 {
			cacheReported = true
		}
		integrity := rec.Integrity
		if integrity == "" {
			integrity = "unknown"
		}
		item := map[string]any{
			"callId": rec.CallID, "attemptId": rec.AttemptID, "purpose": rec.Purpose,
			"provider": rec.Provider, "model": rec.Model, "status": rec.Status,
			"integrity": integrity, "inputTokens": rec.InputTokens, "outputTokens": rec.OutputTokens,
			"cachedInputTokens": rec.CachedInputTokens, "cacheWriteInputTokens": rec.CacheWriteTokens,
		}
		if rec.PolicyVersion != "" || rec.BytesBefore > 0 || rec.BytesAfter > 0 {
			item["policyVersion"] = rec.PolicyVersion
			item["bytesBefore"] = rec.BytesBefore
			item["bytesAfter"] = rec.BytesAfter
		}
		if !rec.StartedAt.IsZero() && !rec.EndedAt.IsZero() && !rec.EndedAt.Before(rec.StartedAt) {
			item["startedAt"] = rec.StartedAt.UTC().Format(time.RFC3339)
			item["endedAt"] = rec.EndedAt.UTC().Format(time.RFC3339)
			item["durationMs"] = rec.EndedAt.Sub(rec.StartedAt).Milliseconds()
		}
		if rec.CostStatus == "unknown" {
			item["costStatus"] = "unknown"
		}
		items = append(items, item)
	}
	integrity := sum.Integrity
	if integrity == "" || len(attempts) == 0 {
		integrity = "unknown"
	}
	out["collected"] = len(attempts) > 0
	out["integrity"] = integrity
	out["inputTokens"] = sum.InputTokens
	out["outputTokens"] = sum.OutputTokens
	out["cachedInputTokens"] = cached
	out["cacheWriteInputTokens"] = cacheWrite
	out["cacheUsageReported"] = cacheReported
	out["attempts"] = items
	if len(attempts) > 0 {
		out["stablePrefixHash"] = typedDefaultStablePrefixHash()
	}
	return request.Ok(out)
}

func handleOCRRoutingGet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if len(request.Payload) > 0 && string(request.Payload) != "{}" && string(request.Payload) != "null" {
		var empty map[string]any
		if decodePayload(request.Payload, &empty) != nil || len(empty) > 0 {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "ocr.routing.get 参数无效", false)
		}
	}
	_ = ctx
	if e == nil || e.ocr == nil {
		r := ocrapp.Routing{PreferProvider: true}
		r.Revision = ocrapp.RoutingRevision(r)
		return request.Ok(ocrRoutingResult(r, ocrapp.HealthSnapshot{Local: ocrapp.LocalOCRReady()}))
	}
	r, err := e.ocr.Routing()
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "OCR 路由暂时不可用", true)
	}
	return request.Ok(ocrRoutingResult(r, e.ocr.HealthSnapshot()))
}

func handleOCRRoutingSet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ProviderID       string `json:"providerId"`
		ModelID          string `json:"modelId"`
		PreferProvider   bool   `json:"preferProvider"`
		ExpectedRevision string `json:"expectedRevision"`
	}
	if decodePayload(request.Payload, &p) != nil || len(p.ExpectedRevision) != 64 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "ocr.routing.set 参数无效", false)
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	if (p.ProviderID == "") != (p.ModelID == "") {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "providerId 与 modelId 必须同时填写", false)
	}
	if p.ProviderID != "" {
		if e.providers == nil {
			return request.Fail("STORAGE_UNAVAILABLE", "供应商数据暂时不可用", true)
		}
		items, err := e.providers.List(ctx, provider.Filter{})
		if err != nil {
			return request.Fail("STORAGE_UNAVAILABLE", "供应商数据暂时不可用", true)
		}
		found := false
		for _, item := range items {
			if item.ID != p.ProviderID {
				continue
			}
			for _, m := range item.Models {
				if m.ModelID == p.ModelID {
					found = true
					break
				}
			}
		}
		if !found {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "OCR 绑定的模型不在当前目录", false)
		}
	}
	if e == nil || e.ocr == nil {
		return request.Fail("CAPABILITY_NOT_READY", "OCR 路由尚未装配", false)
	}
	saved, err := e.ocr.SetRouting(ocrapp.Routing{ProviderID: p.ProviderID, ModelID: p.ModelID, PreferProvider: p.PreferProvider}, p.ExpectedRevision)
	if errors.Is(err, ocrapp.ErrRevisionConflict) {
		return request.Fail("SETTINGS_VERSION_CONFLICT", "OCR 路由已被修改，请载入最新版本", false)
	}
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "OCR 路由写入失败", true)
	}
	return request.Ok(ocrRoutingResult(saved, e.ocr.HealthSnapshot()))
}

func ocrRoutingResult(r ocrapp.Routing, health ocrapp.HealthSnapshot) map[string]any {
	if health.Local.Backend == "" {
		health.Local = ocrapp.LocalOCRReady()
	}
	out := map[string]any{
		"preferProvider": r.PreferProvider, "revision": r.Revision,
		"appliedRevision": r.Revision, "state": "applied",
		"localReady": map[string]any{"pdf": health.Local.PDF, "image": health.Local.Image, "backend": health.Local.Backend},
	}
	if r.ProviderID != "" {
		out["providerId"] = r.ProviderID
	}
	if r.ModelID != "" {
		out["modelId"] = r.ModelID
	}
	if health.LastFailure != nil {
		out["lastFailure"] = map[string]any{
			"class": health.LastFailure.Class, "operation": health.LastFailure.Operation,
			"until": health.LastFailure.Until.UTC().Format(time.RFC3339),
		}
	}
	return out
}

func handleOperationList(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(request.Payload, &p) != nil || !looksLikeULID(p.SessionID) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "operation.list 参数无效", false)
	}
	if e == nil || e.toolOps == nil {
		return request.Ok(map[string]any{"items": []any{}})
	}
	ops, err := e.toolOps.ListToolOperations(ctx, ownerScope(p.SessionID), p.Limit)
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "操作回执暂时不可用", true)
	}
	items := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		items = append(items, operationDTO(op))
	}
	return request.Ok(map[string]any{"items": items})
}

func handleOperationGet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID   string `json:"sessionId"`
		OperationID string `json:"operationId"`
	}
	if decodePayload(request.Payload, &p) != nil || !looksLikeULID(p.SessionID) || strings.TrimSpace(p.OperationID) == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "operation.get 参数无效", false)
	}
	op, fail := loadToolOperation(e, ctx, request, p.SessionID, p.OperationID)
	if fail != nil {
		return *fail
	}
	return request.Ok(operationDTO(op))
}

func handleOperationCancel(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID       string `json:"sessionId"`
		OperationID     string `json:"operationId"`
		ExpectedVersion int    `json:"expectedVersion"`
	}
	if decodePayload(request.Payload, &p) != nil || !looksLikeULID(p.SessionID) || p.OperationID == "" || p.ExpectedVersion < 1 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "operation.cancel 参数无效", false)
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	if e == nil || e.toolOps == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "操作回执暂时不可用", true)
	}
	if err := e.toolOps.RequestToolOperationCancel(ctx, ownerScope(p.SessionID), p.OperationID, p.ExpectedVersion, time.Now().UTC()); err != nil {
		if isToolOperationVersionError(err) {
			return request.Fail("INPUT_CHANGED", "操作版本已变化，请重新查询后再取消", false)
		}
		return request.Fail("STORAGE_UNAVAILABLE", "操作回执暂时不可用", true)
	}
	op, fail := loadToolOperation(e, ctx, request, p.SessionID, p.OperationID)
	if fail != nil {
		return *fail
	}
	return request.Ok(operationDTO(op))
}

func handleOperationResume(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID       string `json:"sessionId"`
		OperationID     string `json:"operationId"`
		ExpectedVersion int    `json:"expectedVersion"`
	}
	if decodePayload(request.Payload, &p) != nil || !looksLikeULID(p.SessionID) || p.OperationID == "" || p.ExpectedVersion < 1 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "operation.resume 参数无效", false)
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	op, fail := loadToolOperation(e, ctx, request, p.SessionID, p.OperationID)
	if fail != nil {
		return *fail
	}
	if op.ExpectedVersion != p.ExpectedVersion {
		return request.Fail("INPUT_CHANGED", "操作版本已变化，请重新查询后再恢复", false)
	}
	dto := operationDTO(op)
	executed := modelfit.ResumeMayExecute(op)
	dto["executed"] = executed
	if executed && op.EffectClass == modelfit.EffectRemoteTrackable {
		dto["resumeHint"] = "已按已有任务查询，未新建外部任务"
	}
	return request.Ok(dto)
}

func loadToolOperation(e *Engine, ctx context.Context, request bridge.Request, sessionID, operationID string) (modelfit.ToolOperation, *bridge.Response) {
	if e == nil || e.toolOps == nil {
		resp := request.Fail("STORAGE_UNAVAILABLE", "操作回执暂时不可用", true)
		return modelfit.ToolOperation{}, &resp
	}
	op, err := e.toolOps.GetToolOperation(ctx, ownerScope(sessionID), operationID)
	if err != nil {
		resp := request.Fail("INPUT_CHANGED", "操作不存在或已过期", false)
		return modelfit.ToolOperation{}, &resp
	}
	return op, nil
}

func isToolOperationVersionError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "version")
}

func operationDTO(op modelfit.ToolOperation) map[string]any {
	out := map[string]any{
		"id": op.ID, "toolName": op.ToolName, "effectClass": string(op.EffectClass),
		"state": string(op.State), "expectedVersion": max(op.ExpectedVersion, 1), "attempt": max(op.Attempt, 1),
		"resumeAction": string(modelfit.ResumeDecisionFor(op)),
		"resumeHint":   operationResumeHint(op),
	}
	if op.SessionID != "" {
		out["sessionId"] = op.SessionID
	}
	if op.TurnID != "" {
		out["turnId"] = op.TurnID
	}
	if op.ErrorKind != "" {
		out["errorKind"] = op.ErrorKind
	}
	if id := strings.TrimSpace(op.ExternalID); id != "" {
		out["externalId"] = id
	}
	return out
}

func operationResumeHint(op modelfit.ToolOperation) string {
	switch modelfit.ResumeDecisionFor(op) {
	case modelfit.ResumeShowExisting:
		return "已有完成结果，直接展示原产物，不要重做"
	case modelfit.ResumeQueryExisting:
		return "已有远端任务，请查询现有结果，不要重新生成或发送"
	case modelfit.ResumeVerifyUnknown:
		return "结果未确认，请先核实，不要自动重做"
	case modelfit.ResumeKeepStopped:
		return "已取消，不要复活旧任务"
	case modelfit.ResumeCompare:
		return "先比对源与目标，再恢复未完成部分，不要整批重做"
	case modelfit.ResumeReread:
		return "只读复核现有内容，不要当作失败重做"
	case modelfit.ResumeReobserve:
		return "重新观察当前界面，不要重放已执行的桌面动作"
	default:
		return "请先核实当前状态，不要自动重做"
	}
}

func handleFilesPlan(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	return handleFilesTool(e, ctx, request, "files.plan")
}

func handleFilesApply(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	return handleFilesTool(e, ctx, request, "files.apply")
}

func handleFilesStatus(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	return handleFilesTool(e, ctx, request, "files.status")
}

func handleFilesUndo(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	return handleFilesTool(e, ctx, request, "files.undo")
}

func handleFilesTool(e *Engine, ctx context.Context, request bridge.Request, name string) bridge.Response {
	var raw map[string]any
	if decodePayload(request.Payload, &raw) != nil {
		return request.Fail("BRIDGE_SCHEMA_INVALID", name+" 参数无效", false)
	}
	sessionID, _ := raw["sessionId"].(string)
	if !looksLikeULID(sessionID) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", name+" 参数无效", false)
	}
	delete(raw, "sessionId")
	args, err := json.Marshal(raw)
	if err != nil {
		return request.Fail("BRIDGE_SCHEMA_INVALID", name+" 参数无效", false)
	}
	out, execErr := e.executeFilesBridgeTool(ctx, sessionID, name, args)
	if execErr != nil && !filesToolReturnsStatus(name, execErr) {
		if errors.Is(execErr, fileops.ErrPlanNotFound) {
			return request.Fail("INPUT_CHANGED", "文件计划不存在", false)
		}
		if errors.Is(execErr, fileops.ErrOutsideRoot) {
			return request.Fail("SCOPE_DENIED", "路径超出工作区", false)
		}
		if errors.Is(execErr, fileops.ErrNothingToUndo) {
			return request.Fail("INPUT_CHANGED", "没有可撤销的步骤", false)
		}
		if errors.Is(execErr, fileops.ErrAlreadyUndone) {
			return request.Fail("INPUT_CHANGED", "已撤销的计划不能再次执行", false)
		}
		if os.IsNotExist(execErr) || errors.Is(execErr, errWorkspaceUnavailable) || errors.Is(execErr, fileops.ErrUnavailable) || strings.Contains(execErr.Error(), "workspace unavailable") {
			return request.Fail("DEPENDENCY_MISSING", "工作区尚未就绪", false)
		}
		if errors.Is(execErr, fileops.ErrInvalidRoot) {
			return request.Fail("DEPENDENCY_MISSING", fileops.UserMessage(execErr), false)
		}
		if errors.Is(execErr, errFilesArgs) {
			return request.Fail("BRIDGE_SCHEMA_INVALID", errFilesArgs.Error(), false)
		}
		if errors.Is(execErr, errUnknownFilesTool) {
			return request.Fail("BRIDGE_SCHEMA_INVALID", errUnknownFilesTool.Error(), false)
		}
		if name == "files.plan" {
			if errors.Is(execErr, fileops.ErrCollision) {
				return request.Fail("INPUT_CHANGED", "目标路径已存在，已停止，未覆盖", false)
			}
			if errors.Is(execErr, fileops.ErrCrossVolume) {
				return request.Fail("INPUT_CHANGED", "不支持跨卷移动，已停止，未复制", false)
			}
			msg := execErr.Error()
			if msg == "文件计划参数无效" || errors.Is(execErr, errFilesArgs) {
				return request.Fail("BRIDGE_SCHEMA_INVALID", msg, false)
			}
			return request.Fail("BRIDGE_SCHEMA_INVALID", fileops.UserMessage(execErr), false)
		}
		if errors.Is(execErr, fileops.ErrInputChanged) {
			return request.Fail("INPUT_CHANGED", "源文件在计划后已被修改，已停止执行，未覆盖原文件", false)
		}
		if errors.Is(execErr, fileops.ErrCollision) {
			return request.Fail("INPUT_CHANGED", "目标路径已存在，已停止，未覆盖", false)
		}
		if errors.Is(execErr, fileops.ErrUndoCollision) {
			return request.Fail("INPUT_CHANGED", "撤销目标已被修改，不能覆盖", false)
		}
		if errors.Is(execErr, fileops.ErrCrossVolume) {
			return request.Fail("INPUT_CHANGED", "不支持跨卷移动，已停止，未复制", false)
		}
		return request.Fail("OUTCOME_UNKNOWN", fileops.UserMessage(execErr), false)
	}
	if name == "files.plan" {
		var plan fileops.Plan
		if json.Unmarshal([]byte(out.Output), &plan) != nil {
			return request.Fail("OUTCOME_UNKNOWN", "文件计划结果无法解析", false)
		}
		return request.Ok(filePlanResult(plan))
	}
	var st fileops.Status
	if json.Unmarshal([]byte(out.Output), &st) != nil {
		return request.Fail("OUTCOME_UNKNOWN", "文件计划结果无法解析", false)
	}
	return request.Ok(fileStatusResult(st))
}

func filePlanResult(plan fileops.Plan) map[string]any {
	items := make([]map[string]any, 0, len(plan.Items))
	for _, item := range plan.Items {
		row := map[string]any{"id": item.ID, "action": string(item.Action)}
		if item.From != "" {
			row["from"] = item.From
		}
		if item.To != "" {
			row["to"] = item.To
		}
		if item.SourceDigest != "" {
			row["sourceDigest"] = item.SourceDigest
		}
		items = append(items, row)
	}
	out := map[string]any{"id": plan.ID, "root": plan.Root, "digest": plan.Digest, "items": items}
	if plan.Recipe != "" {
		out["recipe"] = plan.Recipe
	}
	if plan.CreatedAt != "" {
		out["createdAt"] = plan.CreatedAt
	}
	return out
}

func fileStatusResult(st fileops.Status) map[string]any {
	items := make([]map[string]any, 0, len(st.Items))
	for _, item := range st.Items {
		row := map[string]any{"id": item.ID, "state": item.State}
		if item.Error != "" {
			row["error"] = item.Error
		}
		items = append(items, row)
	}
	out := map[string]any{"planId": st.PlanID, "state": st.State, "items": items}
	if st.AppliedAt != "" {
		out["appliedAt"] = st.AppliedAt
	}
	if st.UndoneAt != "" {
		out["undoneAt"] = st.UndoneAt
	}
	return out
}

func filesToolReturnsStatus(name string, err error) bool {
	if name != "files.apply" && name != "files.undo" {
		return false
	}
	return errors.Is(err, fileops.ErrInputChanged) ||
		errors.Is(err, fileops.ErrCollision) ||
		errors.Is(err, fileops.ErrUndoCollision) ||
		errors.Is(err, fileops.ErrCrossVolume) ||
		errors.Is(err, fileops.ErrApplyPartial)
}
