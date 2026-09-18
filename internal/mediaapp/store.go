package mediaapp

import (
	"context"

	"github.com/lunitide/lunitide/internal/domain/media"
)

type Store interface {
	InsertMediaAsset(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sourceKind, sourceRef, mime, kind, title string, size int64) (string, error)
	GetMediaAsset(ctx context.Context, ownerSubjectID, assetID string) (media.Asset, error)
	ListMediaAssetRecords(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sourceKind string, limit int) ([]media.Asset, error)
	ListMediaQueueAssets(ctx context.Context, ownerSubjectID, sessionID string) ([]media.Asset, error)
	MarkMediaAssetState(ctx context.Context, ownerSubjectID, assetID, state string) error
	CreateMediaSession(ctx context.Context, ownerSubjectID, scopeKind, scopeID, origin, assetID, operationKey string) (media.Snapshot, media.Operation, error)
	GetMediaSessionForOwner(ctx context.Context, ownerSubjectID, sessionID string) (media.Snapshot, error)
	ListMediaSessions(ctx context.Context, ownerSubjectID, scopeKind, scopeID string, limit int) ([]media.Snapshot, error)
	ApplyMediaSessionCommand(ctx context.Context, ownerSubjectID, sessionID, action, operationID string, expectedRevision int64, positionMs, volume int) (media.Snapshot, media.Operation, error)
	ApplyMediaQueueCommand(ctx context.Context, ownerSubjectID, sessionID, action, itemID, operationID string, beforeItemID *string, expectedQueueRevision int64) (media.Snapshot, media.Operation, error)
	ReplaceMediaQueue(ctx context.Context, ownerSubjectID, scopeKind, scopeID, sessionID string, expectedQueueRevision int64, assetIDs []string) (int64, error)
	GetMediaOperationByID(ctx context.Context, ownerSubjectID, operationID string) (media.Operation, error)
	ListMediaOperations(ctx context.Context, ownerSubjectID, scopeKind, scopeID string, limit int) ([]media.Operation, error)
	AttachMediaPlayerLease(ctx context.Context, sessionID, windowInstanceID string, navigationEpoch int64, tokenDigest string, expiresAt string) (int64, error)
	VerifyMediaPlayerLease(ctx context.Context, sessionID, windowInstanceID, token string, generation int64) error
	NextMediaPlayerCommand(ctx context.Context, sessionID string, generation int64) (operationID, desired string, err error)
	AckMediaPlayerCommandForOwner(ctx context.Context, ownerSubjectID, sessionID, operationID, event string, positionMs, durationMs int64) error
}
