package m8core

import (
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Canonical kinds, authorities, and evidence source kinds are closed sets
// from the Memory Fabric PRD. SQL CHECK clauses use the same literals.
const (
	MemoryKindProfile       = "profile"
	MemoryKindPreference    = "preference"
	MemoryKindGoal          = "goal"
	MemoryKindConstraint    = "constraint"
	MemoryKindDecision      = "decision"
	MemoryKindProcedure     = "procedure"
	MemoryKindEpisode       = "episode"
	MemoryKindObservation   = "observation"
	MemoryKindWorking       = "working"
	MemoryAuthorityExplicit = "user_explicit"
	MemoryOriginNative      = "native"
	MemorySourceDirect      = "user_direct_entry"
	MemorySourceUserMessage = "user_message"
	MemoryStabilityDurable  = "durable"
)

// CanonicalMemoryWrite is one explicit create of a current fact version.
type CanonicalMemoryWrite struct {
	SubjectID      string
	ScopeKind      string
	ScopeID        string
	Kind           string
	Text           string
	OperationID    string
	IdempotencyKey string
	SourceKind     string
	SourceRef      string
	StartByte      *int64
	EndByte        *int64
	QuoteDigest    string
}

// CanonicalMemoryResult is the durable identity of a created or replayed item.
type CanonicalMemoryResult struct {
	FactID  string
	Version int64
	Replay  bool
}

// CanonicalMemoryRecord is one authorized current-head view of a fact.
type CanonicalMemoryRecord struct {
	FactID    string
	Version   int64
	Revision  int64
	Kind      string
	ScopeKind string
	ScopeID   string
	Text      string
	Forgotten bool
	UpdatedAt string
}

// MemoryReviewWrite inserts a quiet-review assessment for later resolve.
type MemoryReviewWrite struct {
	CandidateID    string
	Kind           string
	ScopeKind      string
	ScopeID        string
	Novelty        string
	ReasonCodes    []string
	ConflictFactID string
	Text           string
}

// MemoryReviewRecord is one unresolved review row.
type MemoryReviewRecord struct {
	ReviewID       string
	Kind           string
	Novelty        string
	ReasonCodes    []string
	ConflictFactID string
	Text           string
	ScopeKind      string
	ScopeID        string
	CreatedAt      string
}

// MemoryPurgeCounts is the scoped snapshot shown before confirmation.
type MemoryPurgeCounts struct {
	Facts           int64
	Candidates      int64
	SearchDocuments int64
	Embeddings      int64
}

// MemoryPurgePrepareResult is the one-shot confirmation grant.
type MemoryPurgePrepareResult struct {
	Counts             MemoryPurgeCounts
	SnapshotDigest     string
	ConfirmationToken  string
	ExpiresAt          string
	OperationID        string
	DatabaseRevision   int64
	Replay             bool
}

// MemoryUndoResult is the capture-batch undo receipt.
type MemoryUndoResult struct {
	ForgottenFactIDs []string
	DatabaseRevision int64
	Replay           bool
}

// ClassifyMemoryKind picks a deterministic kind. Ambiguous text is episode.
func ClassifyMemoryKind(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return MemoryKindEpisode
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "喜欢") || strings.Contains(lower, "偏好") || strings.Contains(lower, "prefer") || strings.Contains(lower, "always use"):
		return MemoryKindPreference
	case strings.Contains(lower, "不要") && (strings.Contains(lower, "以后") || strings.Contains(lower, "永远")):
		return MemoryKindConstraint
	case strings.Contains(lower, "目标") || strings.Contains(lower, "goal"):
		return MemoryKindGoal
	}
	return MemoryKindEpisode
}

// MemoryScope is the canonical identity+scope key for v2 facts.
type MemoryScope struct {
	SubjectID string
	Kind      string // user|workspace|project|expert|session
	ID        string
}

// MemoryV2Settings is the user-visible capture/recall policy.
type MemoryV2Settings struct {
	SubjectID             string
	Revision              int64
	CaptureMode           string
	LastNonOffCaptureMode string
	PersonalMemoryEnabled bool
	ProjectMemoryEnabled  bool
}

// MemoryFeatureFlags are internal rollout gates. They never appear in the UI.
type MemoryFeatureFlags struct {
	Write         bool
	Read          bool
	AutoCapture   bool
	HybridRecall  bool
	Consolidation bool
}

// MemoryBehavior is the single policy used by chat, workers, recall, and UI.
type MemoryBehavior struct {
	AllowRecall       bool
	AllowAutoCapture  bool
	AllowWorking      bool
	AllowExplicitSave bool
	AllowManagement   bool
}

// CurrentProductFlags keeps today's auto/manual/off settings effective
// before the v2 schema rollout flags exist. Later migrations default flags
// to off and open them per cohort.
func CurrentProductFlags() MemoryFeatureFlags {
	return MemoryFeatureFlags{Write: true, Read: true, AutoCapture: true, HybridRecall: true, Consolidation: true}
}

// SettingsToV2 projects the legacy settings row onto the v2 policy shape.
// MemoryEnabled=false is treated as captureMode=off. Missing captureMode
// stays auto when memory is enabled.
func SettingsToV2(st MemorySettings) MemoryV2Settings {
	mode := st.CaptureMode
	if mode == "" {
		if st.MemoryEnabled {
			mode = "auto"
		} else {
			mode = "off"
		}
	}
	if !st.MemoryEnabled {
		mode = "off"
	}
	last := mode
	if last != "auto" && last != "manual" {
		last = "auto"
	}
	return MemoryV2Settings{
		SubjectID:             st.SubjectID,
		CaptureMode:           mode,
		LastNonOffCaptureMode: last,
		PersonalMemoryEnabled: true,
		ProjectMemoryEnabled:  true,
	}
}

// ResolveMemoryBehavior is the only place that decides capture/recall/save.
func ResolveMemoryBehavior(settings MemoryV2Settings, scopeKind string, flags MemoryFeatureFlags) MemoryBehavior {
	out := MemoryBehavior{AllowManagement: true}
	if settings.CaptureMode == "off" {
		return out
	}
	scopeOn := true
	switch scopeKind {
	case "user", "session":
		scopeOn = settings.PersonalMemoryEnabled
	case "project", "expert", "workspace":
		scopeOn = settings.ProjectMemoryEnabled
	}
	if !scopeOn {
		return out
	}
	out.AllowExplicitSave = flags.Write
	out.AllowRecall = flags.Read
	if settings.CaptureMode == "auto" {
		out.AllowAutoCapture = flags.AutoCapture && flags.Write
		out.AllowWorking = flags.Write
	}
	return out
}

const (
	MemoryRecallRRFK          = 60
	MemoryRecallMMRLambda     = 0.7
	MemoryRecallFTSLimit      = 40
	MemoryRecallDenseLimit    = 40
	MemoryRecallTemporalLimit = 20
	MemoryRecallDeadline      = 150 * time.Millisecond
	MemoryRecallDenseBudget   = 100 * time.Millisecond
	MemoryArchiveMaxBytes     = 64 << 20
	MemoryArchiveMaxRecords   = 100000
	MemoryArchiveMaxJSONDepth = 32
)

// HybridRecallQuery is one scoped retrieval request.
type HybridRecallQuery struct {
	SubjectID      string
	ScopeKind      string
	ScopeID        string
	Query          string
	AsOf           *time.Time
	TopK           int
	BudgetTokens   int64
	Companion      bool
	ModelID        string
	QueryVector    []float32
	EmbeddingSpace string
	Kind           string
}

// HybridRecallHit is one fused candidate after RRF/MMR.
type HybridRecallHit struct {
	FactID         string
	Version        int64
	Kind           string
	Text           string
	KeywordScore   float64
	DenseScore     float64
	TemporalScore  float64
	RelationScore  float64
	FeedbackScore  float64
	FusedScore     float64
	TokenCount     int64
	TokenMode      string
	Adopted        bool
	ReasonCode     string
	SerializedText string
}

// HybridRecallResult is the bounded inject corpus plus honest degradation.
type HybridRecallResult struct {
	TraceID         string
	Hits            []HybridRecallHit
	DenseIncomplete bool
	Degraded        string
	TokenMode       string
	BudgetTokens    int64
	UsedTokens      int64
}

// MemoryGeneration is one consolidation snapshot.
type MemoryGeneration struct {
	GenerationID       string
	ParentGenerationID string
	SubjectID          string
	ScopeKind          string
	ScopeID            string
	State              string
	SourceCutoffSeq    int64
	BuilderVersion     string
	MemberCount        int64
	Revision           int64
	CreatedAt          string
	ReadyAt            string
	ActivatedAt        string
	ErrorCode          string
}

// MemoryGenerationChange is one previewed member delta.
type MemoryGenerationChange struct {
	Change      string
	FactID      string
	FromVersion *int64
	ToVersion   *int64
	BeforeText  string
	AfterText   string
	ReasonCodes []string
}

// MemoryImportCounts is the preview census.
type MemoryImportCounts struct {
	Total      int `json:"total"`
	Accepted   int `json:"accepted"`
	Review     int `json:"review"`
	Conflicts  int `json:"conflicts"`
	Sensitive  int `json:"sensitive"`
	Tombstones int `json:"tombstones"`
}

// MemoryImportPreview is a durable, uncommitted archive inspection.
type MemoryImportPreview struct {
	PreviewID        string
	SourceArtifactID string
	ArchiveDigest    string
	ManifestDigest   string
	DatabaseRevision int64
	ExpiresAt        string
	Counts           MemoryImportCounts
	Warnings         []string
	OperationID      string
	Replay           bool
}

// MemoryImportCommit is the single-transaction ingest receipt.
type MemoryImportCommit struct {
	PreviewID        string
	State            string
	ImportedCount    int
	ReviewCount      int
	SkippedCount     int
	ConflictCount    int
	DatabaseRevision int64
	OperationID      string
	Replay           bool
}

// MemoryFabricExport is the fabric_v2 archive metadata returned by memory.export.
type MemoryFabricExport struct {
	Format           string `json:"format"`
	ArtifactID       string `json:"artifactId"`
	ArchiveDigest    string `json:"archiveDigest"`
	ManifestDigest   string `json:"manifestDigest"`
	DatabaseRevision int64  `json:"databaseRevision"`
	Counts           MemoryFabricExportCounts `json:"counts"`
}

// MemoryFabricExportCounts is the archive inventory, excluding indexes.
type MemoryFabricExportCounts struct {
	Records    int `json:"records"`
	Current    int `json:"current"`
	Tombstones int `json:"tombstones"`
}

const (
	MemoryFeedbackUsed          = "used"
	MemoryFeedbackUnused        = "unused"
	MemoryFeedbackHelpful       = "helpful"
	MemoryFeedbackContradicted  = "contradicted"
	MemoryFeedbackUserCorrected = "user_corrected"
)

// MemoryTotalBudget is min(1536, floor(available*8%)); Companion also caps at 512.
func MemoryTotalBudget(available int64, companion bool) int64 {
	if available <= 0 {
		return 0
	}
	n := available * 8 / 100
	if n > 1536 {
		n = 1536
	}
	if companion && n > 512 {
		n = 512
	}
	return n
}

// SerializeMemoryInject counts the labeled block that actually enters the prompt.
func SerializeMemoryInject(factID string, version int64, text string) string {
	return "[memory fact=" + factID + " version=" + strconv.FormatInt(version, 10) + "]\n" + text
}

// SkipGenericRecall drops greetings and weather questions that have no history deixis.
func SkipGenericRecall(query string) bool {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return true
	}
	trimmed := strings.TrimFunc(q, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
	switch trimmed {
	case "你好", "您好", "hello", "hi", "hey", "早上好", "晚上好", "谢谢", "thank you", "thanks", "ok", "好的":
		return true
	}
	weather := strings.Contains(q, "天气") || strings.Contains(q, "气温") || strings.Contains(q, "下雨") || strings.Contains(q, "weather") || strings.Contains(q, "forecast")
	if !weather {
		return false
	}
	deictic := strings.Contains(q, "上次") || strings.Contains(q, "之前") || strings.Contains(q, "以前") || strings.Contains(q, "还记得") || strings.Contains(q, "我这里")
	return !deictic
}

// ProfileOnlyRecall restricts retrieval to profile facts.
func ProfileOnlyRecall(query string) bool {
	q := strings.TrimSpace(query)
	return strings.Contains(q, "我这里") || strings.Contains(strings.ToLower(q), "where i live")
}
