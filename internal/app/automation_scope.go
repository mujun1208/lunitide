package app

import (
	"context"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/bridge"
)

func (e *Engine) authorizeAutomationSession(ctx context.Context, sessionID string) (func(), error) {
	raw, _ := json.Marshal(map[string]string{"sessionId": sessionID})
	return e.authorizeDataRequest(ctx, "session.get", raw)
}

func (e *Engine) authorizeAutomationJob(ctx context.Context, request bridge.Request, id string) (func(), *bridge.Response) {
	job, ok, err := e.automation.Store().GetJob(id)
	if err != nil {
		r := request.Fail("AUTOMATION_STORE_FAILED", "任务读取失败", true)
		return nil, &r
	}
	if !ok {
		r := request.Fail("AUTOMATION_JOB_NOT_FOUND", "任务不存在", false)
		return nil, &r
	}
	release, err := e.authorizeAutomationSession(ctx, job.SessionID)
	if err != nil {
		if !dataScopeAccessError(err) {
			r := request.Fail("DATA_SCOPE_UNAVAILABLE", "任务所属范围暂时无法确认，请重试", true)
			return nil, &r
		}
		r := request.Fail("DATA_SCOPE_DENIED", "当前组织无法访问该自动化任务", false)
		return nil, &r
	}
	return release, nil
}
