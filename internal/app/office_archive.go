package app

import (
	"context"
	"log"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

// Archive existing generator deliveries without making chat completion wait
// for Office indexing. The session index is still retryable via task.sync.
func (e *Engine) archiveOfficeTurn(ctx context.Context, sessionID string) {
	if e.officeStudio == nil || !e.officeCapabilities().Studio {
		return
	}
	org, _, err := e.boundOrgState(ctx)
	if err != nil {
		return
	}
	taskID := officeTaskContextID(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(domain.WithScope(context.Background(), org), 30*time.Second)
		defer cancel()
		if err := e.archiveOfficeTurnNow(ctx, sessionID, taskID); err != nil {
			log.Printf("office archive deferred: session=%s task=%s; retry from Office Studio", sessionID, taskID)
		}
	}()
}

func (e *Engine) archiveOfficeTurnNow(ctx context.Context, sessionID, taskID string) error {
	if !validCanonicalULID(sessionID) || e.officeStudio == nil {
		return domain.ErrInvalid
	}
	if taskID != "" {
		task, err := e.officeStudio.Store.GetOfficeTask(ctx, taskID)
		if err != nil {
			return err
		}
		if task.SessionID != sessionID {
			return domain.ErrScope
		}
		return e.syncOfficeArtifacts(ctx, task)
	}
	// Mutable updated_at must never determine chat ownership. Without an
	// explicit binding, select the latest created task and let message/card
	// boundaries exclude historical files belonging to earlier tasks.
	snapshotStore, ok := e.officeStudio.Store.(domain.SnapshotStore)
	if !ok {
		return domain.ErrInvalid
	}
	tasks, err := snapshotStore.ReadOfficeTaskListSnapshot(ctx, sessionID, "")
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	task := tasks[0].Task
	for _, candidate := range tasks[1:] {
		if officeTaskCreatedBefore(task, candidate.Task) {
			task = candidate.Task
		}
	}
	return e.syncOfficeArtifacts(ctx, task)
}
