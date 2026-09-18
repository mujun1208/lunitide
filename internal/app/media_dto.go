package app

import "github.com/lunitide/lunitide/internal/domain/media"

func publicScopeID(kind, id string) any {
	if kind == "user" {
		return nil
	}
	if id == "" {
		return nil
	}
	return id
}

func nullIfBlank(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func mediaSnapshotDTO(snap media.Snapshot) map[string]any {
	var asset any
	if snap.AssetID == "" {
		asset = nil
	} else {
		asset = snap.AssetID
	}
	return map[string]any{
		"mediaSessionId":     snap.MediaSessionID,
		"scopeKind":          snap.ScopeKind,
		"scopeId":            publicScopeID(snap.ScopeKind, snap.ScopeID),
		"origin":             snap.Origin,
		"phase":              snap.Phase,
		"verificationStatus": snap.VerificationStatus,
		"verificationSource": snap.VerificationSource,
		"assetId":            asset,
		"playbackEpoch":      snap.PlaybackEpoch,
		"autoAdvance":        snap.AutoAdvance,
		"positionMs":         snap.PositionMs,
		"durationMs":         snap.DurationMs,
		"volume":             snap.Volume,
		"muted":              snap.Muted,
		"queueRevision":      snap.QueueRevision,
		"revision":           snap.Revision,
		"updatedAt":          snap.UpdatedAt,
	}
}

func mediaOperationDTO(op media.Operation) map[string]any {
	parent := any(nil)
	if op.ParentOperationID != "" {
		parent = op.ParentOperationID
	}
	root := any(nil)
	if op.RootOperationID != "" {
		root = op.RootOperationID
	} else if op.OperationID != "" {
		root = op.OperationID
	}
	return map[string]any{
		"operationId":        op.OperationID,
		"mediaSessionId":     op.MediaSessionID,
		"parentOperationId":  parent,
		"rootOperationId":    root,
		"action":             op.Action,
		"phase":              op.Phase,
		"verificationStatus": op.VerificationStatus,
		"verificationSource": op.VerificationSource,
		"errorCode":          nullIfBlank(op.ErrorCode),
		"revision":           op.Revision,
	}
}

func mediaAssetDTO(asset media.Asset) map[string]any {
	return map[string]any{
		"assetId":    asset.AssetID,
		"sourceKind": asset.SourceKind,
		"kind":       asset.Kind,
		"title":      asset.Title,
		"mime":       asset.MIME,
		"size":       asset.Size,
		"state":      asset.State,
		"revision":   asset.Revision,
	}
}
