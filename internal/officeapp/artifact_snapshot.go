package officeapp

import (
	"context"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

// SnapshotOfficeArtifact returns the immutable Office blob identity.
// SHA256 is the stored blob bytes, never extracted or paginated text.
func (s *Service) SnapshotOfficeArtifact(ctx context.Context, taskID, versionID string) (agentrun.ArtifactSnapshot, error) {
	v, raw, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return agentrun.ArtifactSnapshot{}, err
	}
	return agentrun.ArtifactSnapshot{
		ID:             v.ID,
		Path:           v.Name,
		SHA256:         v.SHA256,
		ContentRef:     "office-version:" + v.ID,
		Bytes:          int64(len(raw)),
		SourceIdentity: v.ContentRef,
	}, nil
}
