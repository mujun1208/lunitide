package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func (s *Store) RecordCanonicalFeedback(ctx context.Context, subjectID, factID string, version int64, turnID, outcome string) error {
	return recordCanonicalFeedback(ctx, s.db, subjectID, factID, version, turnID, outcome)
}

func (r *AgentRuntimeRepository) RecordCanonicalFeedback(ctx context.Context, subjectID, factID string, version int64, turnID, outcome string) error {
	return recordCanonicalFeedback(ctx, r.db, subjectID, factID, version, turnID, outcome)
}

func recordCanonicalFeedback(ctx context.Context, db *sql.DB, subjectID, factID string, version int64, turnID, outcome string) error {
	turnID = strings.TrimSpace(turnID)
	if subjectID == "" || factID == "" || version < 1 || len(turnID) < 1 || len(turnID) > 128 {
		return fmt.Errorf("feedback missing required fields")
	}
	switch outcome {
	case m8core.MemoryFeedbackUsed, m8core.MemoryFeedbackUnused, m8core.MemoryFeedbackHelpful, m8core.MemoryFeedbackContradicted, m8core.MemoryFeedbackUserCorrected:
	default:
		return fmt.Errorf("feedback outcome invalid")
	}
	var headSubject string
	err := db.QueryRowContext(ctx, `SELECT h.subject_id FROM memory_content_versions v
		JOIN memory_fact_heads h ON h.fact_id=v.fact_id
		WHERE v.fact_id=? AND v.fact_version=?`, factID, version).Scan(&headSubject)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.ErrNotFound
	}
	if err != nil {
		return err
	}
	if headSubject != subjectID {
		return m8core.ErrImportScopeDenied
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx, `INSERT INTO memory_feedback_events(id,trace_id,fact_id,fact_version,turn_id,outcome,created_at)
		VALUES(?,?,?,?,?,?,?)`, ulid.Make().String(), turnID, factID, version, turnID, outcome, now)
	return err
}
