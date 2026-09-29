package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

// approvalResume is the decision the open tool stream is waiting for.
// The approve handler already executed the tool; the stream must not
// execute it a second time.
type approvalResume struct {
	approved bool
	result   toolruntime.Result
	err      error
}

func approvalWaitKey(sessionID, callID string) string {
	return sessionID + "\x00" + callID
}

func (e *Engine) registerApprovalWait(sessionID, callID string) chan approvalResume {
	ch := make(chan approvalResume, 1)
	e.approvalWaits.Store(approvalWaitKey(sessionID, callID), ch)
	return ch
}

func (e *Engine) receiveApprovalResume(ctx context.Context, ch chan approvalResume, sessionID, callID string) (approvalResume, error) {
	defer e.approvalWaits.Delete(approvalWaitKey(sessionID, callID))
	select {
	case resume := <-ch:
		return resume, nil
	case <-ctx.Done():
		return approvalResume{}, ctx.Err()
	}
}

func (e *Engine) waitApprovalResume(ctx context.Context, sessionID, callID string) (approvalResume, error) {
	return e.receiveApprovalResume(ctx, e.registerApprovalWait(sessionID, callID), sessionID, callID)
}

func (e *Engine) deliverApprovalWait(sessionID, callID string, approved bool, result toolruntime.Result, err error) bool {
	value, ok := e.approvalWaits.Load(approvalWaitKey(sessionID, callID))
	if !ok {
		return false
	}
	ch, ok := value.(chan approvalResume)
	if !ok {
		return false
	}
	select {
	case ch <- approvalResume{approved: approved, result: result, err: err}:
		return true
	default:
		return false
	}
}

func approvalResumeResult(resume approvalResume) (toolruntime.Result, error) {
	if resume.err != nil {
		return resume.result, resume.err
	}
	if !resume.approved {
		output := "ok:false\n用户拒绝了这次操作。"
		return toolruntime.Result{Output: output}, errors.New(output)
	}
	return resume.result, nil
}
