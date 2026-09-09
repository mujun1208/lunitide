package sqlite

import (
	"context"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

// FindOfficePublishedVersion reads the durable request receipt after enforcing
// task ownership. It is used before repeating an expensive native calculation.
func (s *Store) FindOfficePublishedVersion(ctx context.Context, taskID, key string) (domain.Version, error) {
	if !officeKey(key) {
		return domain.Version{}, domain.ErrInvalid
	}
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return domain.Version{}, err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT m.version_id FROM office_version_metadata m JOIN artifact_versions v ON v.id=m.version_id JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id WHERE b.task_id=? AND m.idempotency_key=?`, taskID, key).Scan(&id)
	if err != nil {
		return domain.Version{}, officeError(err)
	}
	return s.GetOfficeVersion(ctx, id)
}
