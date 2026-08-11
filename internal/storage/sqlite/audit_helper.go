package sqlite

import (
	"context"
	"encoding/json"
	"time"
)

// appendAudit writes an audit event for a P3/P4 operation.
// It is best-effort: audit write failure does not block the primary operation
// (the primary write has already succeeded by the time this is called).
func (s *Store) appendAudit(ctx context.Context, action, aggregateID, actor string, metadata map[string]any) {
	var metaJSON string
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			metaJSON = "{}"
		} else {
			metaJSON = string(b)
		}
	} else {
		metaJSON = "{}"
	}
	id, err := s.newULID(time.Now().UTC())
	if err != nil {
		return
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`,
		id, action, aggregateID, actor, metaJSON, formatTime(time.Now().UTC()))
}
