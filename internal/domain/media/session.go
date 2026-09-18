package media

import "strconv"

type SessionPhase string

const (
	SessionIdle      SessionPhase = "idle"
	SessionPlaying   SessionPhase = "playing"
	SessionPaused    SessionPhase = "paused"
	SessionStalled   SessionPhase = "stalled"
	SessionEnded     SessionPhase = "ended"
	SessionUncertain SessionPhase = "uncertain"
	SessionFailed    SessionPhase = "failed"
	SessionStopped   SessionPhase = "stopped"
)

type SessionVerificationStatus string

const (
	SessionVerifyNone               SessionVerificationStatus = "none"
	SessionVerifyCommandDispatched  SessionVerificationStatus = "command_dispatched"
	SessionVerifyPlaying            SessionVerificationStatus = "verified_playing"
	SessionVerifyPaused             SessionVerificationStatus = "verified_paused"
	SessionVerifyEnded              SessionVerificationStatus = "verified_ended"
	SessionVerifyStopped            SessionVerificationStatus = "verified_stopped"
)

type OperationPhase string

const (
	OpRequested         OperationPhase = "requested"
	OpAwaitingApproval  OperationPhase = "awaiting_approval"
	OpDispatching       OperationPhase = "dispatching"
	OpVerifying         OperationPhase = "verifying"
	OpSucceeded         OperationPhase = "succeeded"
	OpUncertain         OperationPhase = "uncertain"
	OpFailed            OperationPhase = "failed"
	OpCancelled         OperationPhase = "cancelled"
)

type Snapshot struct {
	MediaSessionID     string
	OwnerSubjectID     string
	ScopeKind          string
	ScopeID            string
	Origin             string
	Phase              SessionPhase
	VerificationStatus SessionVerificationStatus
	VerificationSource string
	AssetID            string
	PlaybackEpoch      int64
	AutoAdvance        bool
	PositionMs         int64
	DurationMs         int64
	Volume             int
	Muted              bool
	QueueRevision      int64
	Revision           int64
	UpdatedAt          string
}

type Operation struct {
	OperationID        string
	MediaSessionID     string
	ParentOperationID  string
	RootOperationID    string
	Action             string
	Phase              OperationPhase
	VerificationStatus string
	VerificationSource string
	ErrorCode          string
	Revision           int64
	CreatedAt          string
	UpdatedAt          string
}

type Asset struct {
	AssetID      string
	SourceKind   string
	Kind         string
	Title        string
	MIME         string
	Size         int64
	State        string
	Revision     int64
	SourceRef    string
	FileIdentity string
}

func FileIdentity(size int64, mtimeNano int64) string {
	return strconv.FormatInt(size, 10) + ":" + strconv.FormatInt(mtimeNano, 10)
}

type Activity struct {
	ActivityID         string
	Domain             string
	Kind               string
	Phase              string
	Terminal           bool
	VerificationStatus string
	VerificationSource string
	Title              string
	CompletedUnits     *int
	TotalUnits         *int
	ErrorCode          string
	Retryable          bool
	RecoveryAction     string
	ScopeKind          string
	ScopeID            string
	CreatedAt          string
	UpdatedAt          string
	RootOperationID    string
	SortKey            string
}

type QueueItem struct {
	ItemID     string
	AssetID    string
	OrderIndex int
	State      string
}
