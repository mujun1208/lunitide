package officeapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

type officeOperation struct {
	id     string
	cancel context.CancelFunc
}

type officeRecovery struct {
	done chan struct{}
	err  error
}

// RecoverOnce coalesces recovery per verified organization. Failed attempts
// are removable so a cancelled startup can be retried by the next request.
func (s *Service) RecoverOnce(ctx context.Context) error {
	scope := domain.Scope(ctx)
	s.mu.Lock()
	if previous, ok := s.recoveries[scope]; ok {
		s.mu.Unlock()
		select {
		case <-previous.done:
			return previous.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.recoveries == nil {
		s.recoveries = map[string]*officeRecovery{}
	}
	state := &officeRecovery{done: make(chan struct{})}
	s.recoveries[scope] = state
	s.mu.Unlock()
	err := s.Recover(ctx)
	s.mu.Lock()
	state.err = err
	if err != nil {
		delete(s.recoveries, scope)
	}
	close(state.done)
	s.mu.Unlock()
	return err
}

func officeActive(status string) bool {
	switch status {
	case "queued", "planning", "running", "validating", "cancelling":
		return true
	}
	return false
}

func operationCheckpoint(previous json.RawMessage, runID, phase, state string) json.RawMessage {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(previous, &fields)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	fields["officeOperation"] = encode(map[string]string{"runId": runID, "phase": phase, "state": state})
	return encode(fields)
}

// Execute records the durable business operation around the shared runtime.
// A successful operation means only that this action completed, independently
// of file quality and acceptance. Cancellation never starts another chat run.
func (s *Service) Execute(ctx context.Context, taskID, phase string, fn func(context.Context) error) (result error) {
	if fn == nil {
		return domain.ErrInvalid
	}
	switch phase {
	case "running", "validating", "import", "patch", "restore", "sync", "export", "generate", "validate":
	default:
		return domain.ErrInvalid
	}
	if err := s.RecoverOnce(ctx); err != nil {
		return err
	}
	task, err := s.Store.GetOfficeTask(ctx, taskID)
	if err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	runID := ulid.Make().String()
	s.mu.Lock()
	if _, exists := s.runs[taskID]; exists {
		s.mu.Unlock()
		cancel()
		return domain.ErrBusy
	}
	if s.runs == nil {
		s.runs = map[string]officeOperation{}
	}
	s.runs[taskID] = officeOperation{id: runID, cancel: cancel}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		if s.runs[taskID].id == runID {
			delete(s.runs, taskID)
		}
		s.mu.Unlock()
	}()
	if officeActive(task.Status) {
		return domain.ErrBusy
	}
	task.RunID = runID
	task.Status = "queued"
	task.Checkpoint = operationCheckpoint(task.Checkpoint, task.RunID, phase, "queued")
	task, err = s.Store.UpdateOfficeTask(run, task, task.Revision)
	if err != nil {
		return err
	}
	inputDigest := digest(encode(map[string]any{"taskId": task.ID, "revision": task.Revision, "phase": phase, "runId": runID}))
	// Finishing uses the same verified organization context even if the caller
	// disconnected. Its deadline bounds storage cleanup and never extends work.
	defer func() {
		if recovered := recover(); recovered != nil {
			result = errors.New("办公操作发生内部错误，已保留现有文件")
		}
		if result == nil && run.Err() != nil {
			result = run.Err()
		}
		finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		finishErr := s.finishOperation(finishCtx, taskID, runID, phase, inputDigest, result)
		if finishErr != nil {
			result = errors.Join(result, fmt.Errorf("办公操作状态保存失败: %w", finishErr))
		}
	}()
	task.Status = "running"
	task.Checkpoint = operationCheckpoint(task.Checkpoint, runID, phase, "running")
	task, err = s.Store.UpdateOfficeTask(run, task, task.Revision)
	if err != nil {
		return err
	}
	if phase == "validating" || phase == "validate" {
		task.Status = "validating"
		task.Checkpoint = operationCheckpoint(task.Checkpoint, runID, phase, "validating")
		task, err = s.Store.UpdateOfficeTask(run, task, task.Revision)
		if err != nil {
			return err
		}
	}
	if err = s.Store.AppendOfficeStepReceipt(run, domain.StepReceipt{TaskID: taskID, RunID: runID, StepKey: "operation:" + phase, IdempotencyKey: runID, InputDigest: inputDigest, State: "started"}); err != nil {
		return err
	}
	return fn(run)
}

func (s *Service) finishOperation(ctx context.Context, taskID, runID, phase, inputDigest string, operationErr error) error {
	for attempts := 0; attempts < 4; attempts++ {
		task, err := s.Store.GetOfficeTask(ctx, taskID)
		if err != nil {
			return err
		}
		if task.RunID != runID {
			return domain.ErrConflict
		}
		state := "succeeded"
		if operationErr != nil {
			state = "failed"
		}
		if errors.Is(operationErr, context.Canceled) || task.Status == "cancelling" || task.Status == "cancelled" {
			state = "cancelled"
		}
		if !officeActive(task.Status) && task.Status != state {
			return domain.ErrConflict
		}
		detail := map[string]string{"phase": phase, "status": state}
		if operationErr != nil {
			msg := []rune(operationErr.Error())
			if len(msg) > 1000 {
				msg = msg[:1000]
			}
			detail["error"] = string(msg)
		}
		finisher, ok := s.Store.(domain.OperationFinisher)
		if !ok {
			return errors.New("office store does not support atomic operation completion")
		}
		task.Status = state
		task.Checkpoint = operationCheckpoint(task.Checkpoint, runID, phase, state)
		_, err = finisher.FinishOfficeTask(ctx, task, task.Revision, domain.StepReceipt{TaskID: taskID, RunID: runID, StepKey: "operation:" + phase, IdempotencyKey: runID, InputDigest: inputDigest, State: state, Result: encode(detail)})
		if errors.Is(err, domain.ErrConflict) {
			continue
		}
		return err
	}
	return domain.ErrConflict
}

// Cancel persists the request before signaling the worker. A late worker may
// still append evidence to its original version but cannot publish a new head.
func (s *Service) Cancel(ctx context.Context, taskID string) error {
	requestedRunID := ""
	requested := false
	for attempts := 0; attempts < 4; attempts++ {
		task, err := s.Store.GetOfficeTask(ctx, taskID)
		if err != nil {
			return err
		}
		if !requested {
			requestedRunID = task.RunID
			requested = true
		} else if task.RunID != requestedRunID {
			return domain.ErrConflict
		}
		if !officeActive(task.Status) {
			return nil
		}
		if task.Status != "cancelling" {
			task.Status = "cancelling"
			task.Checkpoint = operationCheckpoint(task.Checkpoint, task.RunID, "cancel", "cancelling")
			if _, err = s.Store.UpdateOfficeTask(ctx, task, task.Revision); errors.Is(err, domain.ErrConflict) {
				continue
			} else if err != nil {
				return err
			}
		}
		s.mu.Lock()
		operation := s.runs[taskID]
		s.mu.Unlock()
		if operation.cancel != nil && operation.id == task.RunID {
			operation.cancel()
			return nil
		}
		task, err = s.Store.GetOfficeTask(ctx, taskID)
		if err != nil {
			return err
		}
		if task.Status != "cancelling" {
			return nil
		}
		if task.RunID != requestedRunID {
			return domain.ErrConflict
		}
		task.Status = "cancelled"
		task.Checkpoint = operationCheckpoint(task.Checkpoint, task.RunID, "cancel", "cancelled")
		_, err = s.Store.UpdateOfficeTask(ctx, task, task.Revision)
		if errors.Is(err, domain.ErrConflict) {
			continue
		}
		return err
	}
	return domain.ErrConflict
}

// Recover requires the identity boundary's scope. Its bounded active batches
// drain as rows are marked interrupted; recent drafts cannot hide older runs.
func (s *Service) Recover(ctx context.Context) error {
	loader, ok := s.Store.(interface {
		ListOfficeActiveTasks(context.Context, int) ([]domain.Task, error)
	})
	if !ok {
		return errors.New("office store does not support durable recovery")
	}
	for {
		tasks, err := loader.ListOfficeActiveTasks(ctx, 500)
		if err != nil {
			return err
		}
		changed := 0
		for _, task := range tasks {
			s.mu.Lock()
			_, owned := s.runs[task.ID]
			if owned {
				s.mu.Unlock()
				continue
			}
			task.Status = "interrupted"
			task.Checkpoint = operationCheckpoint(task.Checkpoint, task.RunID, "recover", "interrupted")
			_, err = s.Store.UpdateOfficeTask(ctx, task, task.Revision)
			s.mu.Unlock()
			if errors.Is(err, domain.ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
			changed++
		}
		if changed == 0 || len(tasks) < 500 {
			return nil
		}
	}
}
