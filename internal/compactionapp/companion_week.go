package compactionapp

import (
	"context"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
)

const CompanionWeekReason = "companion-weekly:"

type CompanionWeek struct {
	SessionID        string
	StartSeq, EndSeq int64
	Start, End       time.Time
}

type CompanionArchive struct {
	CheckpointID string
	Period       string
	Summary      string
}

type CompanionArchiveStore interface {
	ListDueCompanionWeeks(context.Context, time.Time) ([]CompanionWeek, error)
	SearchCompanionArchives(context.Context, string, string, int) ([]CompanionArchive, error)
}

// WeekStart follows the computer's local calendar; Monday starts a new week.
func WeekStart(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return t.AddDate(0, 0, -(int(t.Weekday())+6)%7)
}

func (t *Trigger) TriggerCompanionWeek(ctx context.Context, week CompanionWeek, provider, model string) (ManualTriggerResult, error) {
	return t.triggerRange(ctx, week.SessionID, provider, model, week.StartSeq, week.EndSeq, 1, compaction.TriggerAutomatic, CompanionWeekReason+week.Start.Format("2006-01-02"))
}
