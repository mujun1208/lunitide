package media

import "errors"

var (
	ErrSessionV2Disabled   = errors.New("MEDIA_SESSION_V2_DISABLED")
	ErrScopeDenied         = errors.New("MEDIA_SCOPE_DENIED")
	ErrQueueConflict       = errors.New("MEDIA_QUEUE_REVISION_CONFLICT")
	ErrSessionNotFound     = errors.New("MEDIA_SESSION_NOT_FOUND")
	ErrAssetNotFound       = errors.New("MEDIA_ASSET_NOT_FOUND")
	ErrRevisionConflict    = errors.New("MEDIA_REVISION_CONFLICT")
	ErrIdempotencyConflict = errors.New("MEDIA_IDEMPOTENCY_CONFLICT")
	ErrAssetChanged        = errors.New("MEDIA_ASSET_CHANGED")
	ErrPlayerLeaseConflict = errors.New("MEDIA_PLAYER_LEASE_CONFLICT")
)
