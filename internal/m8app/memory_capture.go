package m8app

import (
	"context"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

type memoryCaptureQueue interface {
	EnqueueMemoryCaptureJob(context.Context, m8core.MemoryCaptureJob, int64) (bool, error)
	ClaimMemoryCaptureJob(context.Context, string, time.Duration) (m8core.MemoryCaptureJob, bool, error)
	CompleteMemoryCaptureJob(context.Context, string, int64, string) error
	CountActiveMemoryCaptureJobs(context.Context) (int64, error)
	GetMemoryCaptureCursor(context.Context, string) (m8core.MemoryCaptureCursor, error)
	SetMemoryCaptureCursor(context.Context, m8core.MemoryCaptureCursor) error
	ListUserMemorySourcesAfter(context.Context, string, int) ([]m8core.MemoryCaptureSource, error)
	ReclaimExpiredMemoryCaptureJobs(context.Context, time.Time) (int64, error)
}

func (s *MemoryOpsService) captureQueue() memoryCaptureQueue {
	if s == nil || s.store == nil {
		return nil
	}
	q, _ := s.store.(memoryCaptureQueue)
	return q
}

func (s *MemoryOpsService) EnqueueCaptureJob(ctx context.Context, job m8core.MemoryCaptureJob, highWater int64) (bool, error) {
	q := s.captureQueue()
	if q == nil {
		return false, ErrServiceUnavailable
	}
	return q.EnqueueMemoryCaptureJob(ctx, job, highWater)
}

func (s *MemoryOpsService) ClaimCaptureJob(ctx context.Context, owner string, lease time.Duration) (m8core.MemoryCaptureJob, bool, error) {
	q := s.captureQueue()
	if q == nil {
		return m8core.MemoryCaptureJob{}, false, ErrServiceUnavailable
	}
	return q.ClaimMemoryCaptureJob(ctx, owner, lease)
}

func (s *MemoryOpsService) CompleteCaptureJob(ctx context.Context, jobID string, fence int64, errCode string) error {
	q := s.captureQueue()
	if q == nil {
		return ErrServiceUnavailable
	}
	return q.CompleteMemoryCaptureJob(ctx, jobID, fence, errCode)
}

func (s *MemoryOpsService) CountActiveCaptureJobs(ctx context.Context) (int64, error) {
	q := s.captureQueue()
	if q == nil {
		return 0, ErrServiceUnavailable
	}
	return q.CountActiveMemoryCaptureJobs(ctx)
}

func (s *MemoryOpsService) CaptureCursor(ctx context.Context, subjectID string) (m8core.MemoryCaptureCursor, error) {
	q := s.captureQueue()
	if q == nil {
		return m8core.MemoryCaptureCursor{}, ErrServiceUnavailable
	}
	return q.GetMemoryCaptureCursor(ctx, subjectID)
}

func (s *MemoryOpsService) ListCaptureSourcesAfter(ctx context.Context, afterMessageID string, limit int) ([]m8core.MemoryCaptureSource, error) {
	q := s.captureQueue()
	if q == nil {
		return nil, ErrServiceUnavailable
	}
	return q.ListUserMemorySourcesAfter(ctx, afterMessageID, limit)
}

func (s *MemoryOpsService) ReclaimExpiredCaptureJobs(ctx context.Context, now time.Time) (int64, error) {
	q := s.captureQueue()
	if q == nil {
		return 0, ErrServiceUnavailable
	}
	return q.ReclaimExpiredMemoryCaptureJobs(ctx, now)
}

func (s *MemoryOpsService) HasCaptureQueue() bool {
	return s.captureQueue() != nil
}
