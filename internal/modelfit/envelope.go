// Package modelfit holds shared DeepSeek/GLM continuity contracts.
// It stays free of app, storage, and desktop imports.
package modelfit

import "encoding/json"

type Completeness string

const (
	CompletenessNativeComplete Completeness = "native_complete"
	CompletenessStructuredOnly Completeness = "structured_only"
	CompletenessLegacyUnknown  Completeness = "legacy_unknown"
)

type ProtocolSource string

const (
	SourceProvider ProtocolSource = "provider"
)

type ProtocolEvidence struct {
	HasGoal            bool
	HasToolReceipts    bool
	HasMessageGroup    bool
	HasObtainedPrivate bool
	CodecQualified     bool
}

func ClassifyCompleteness(ev ProtocolEvidence) Completeness {
	if ev.CodecQualified && ev.HasMessageGroup && ev.HasObtainedPrivate {
		return CompletenessNativeComplete
	}
	if ev.HasGoal || ev.HasToolReceipts || ev.HasMessageGroup {
		return CompletenessStructuredOnly
	}
	return CompletenessLegacyUnknown
}

type ObtainedProtocol struct {
	ReasoningContent string
	Source           ProtocolSource
}

type ProtocolCapture struct {
	ReasoningContent string `json:"reasoningContent,omitempty"`
	Source           ProtocolSource `json:"source,omitempty"`
	Complete         bool   `json:"complete,omitempty"`
}

func CaptureProtocolFields(in ObtainedProtocol) ProtocolCapture {
	if in.Source != SourceProvider || in.ReasoningContent == "" {
		return ProtocolCapture{}
	}
	return ProtocolCapture{ReasoningContent: in.ReasoningContent, Source: SourceProvider, Complete: true}
}

type ContinuationEnvelope struct {
	SchemaVersion              string          `json:"schemaVersion,omitempty"`
	RecordRevision             int             `json:"recordRevision,omitempty"`
	OwnerScope                 string          `json:"ownerScope,omitempty"`
	TaskRef                    string          `json:"taskRef,omitempty"`
	SessionID                  string          `json:"sessionId,omitempty"`
	TurnID                     string          `json:"turnId,omitempty"`
	AutomationRunID            string          `json:"automationRunId,omitempty"`
	DispatchKey                string          `json:"dispatchKey,omitempty"`
	RuntimeEpoch               string          `json:"runtimeEpoch,omitempty"`
	WriterVersion              string          `json:"writerVersion,omitempty"`
	ToolSchemaRevision         string          `json:"toolSchemaRevision,omitempty"`
	PolicyRevision             string          `json:"policyRevision,omitempty"`
	ProviderDeploymentRef      string          `json:"providerDeploymentRef,omitempty"`
	ModelRequested             string          `json:"modelRequested,omitempty"`
	ModelReturned              string          `json:"modelReturned,omitempty"`
	CredentialGeneration       string          `json:"credentialGeneration,omitempty"`
	CodecVersion               string          `json:"codecVersion,omitempty"`
	ProfileRevision            string          `json:"profileRevision,omitempty"`
	EffectiveParameterDigest   string          `json:"effectiveParameterDigest,omitempty"`
	MessageGroupRefs           []string        `json:"messageGroupRefs,omitempty"`
	ProtocolPrivateRef         string          `json:"protocolPrivateRef,omitempty"`
	ProtocolDigest             string          `json:"protocolDigest,omitempty"`
	Completeness               Completeness    `json:"completeness,omitempty"`
	SourceRefs                 []string        `json:"sourceRefs,omitempty"`
	ArtifactRefs               []string        `json:"artifactRefs,omitempty"`
	OperationRefs              []string        `json:"operationRefs,omitempty"`
	PendingExternalRefs        []string        `json:"pendingExternalRefs,omitempty"`
	AuthorizationRef           string          `json:"authorizationRef,omitempty"`
	CancellationRequestedAt    string          `json:"cancellationRequestedAt,omitempty"`
	BudgetUsed                 int64           `json:"budgetUsed,omitempty"`
	RemainingLimit             int64           `json:"remainingLimit,omitempty"`
	LastCommittedEventSequence int64           `json:"lastCommittedEventSequence,omitempty"`
	RecoveryDecision           string          `json:"recoveryDecision,omitempty"`
	UpdatedAt                  string          `json:"updatedAt,omitempty"`
	Protocol                   ProtocolCapture `json:"protocol,omitempty"`
	extra                      map[string]json.RawMessage
}

func CompletenessFromCheckpoint(env *ContinuationEnvelope) Completeness {
	if env == nil || env.Completeness == "" {
		return CompletenessLegacyUnknown
	}
	return env.Completeness
}

func DecodeEnvelope(raw []byte) (ContinuationEnvelope, error) {
	var env ContinuationEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return ContinuationEnvelope{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ContinuationEnvelope{}, err
	}
	for _, key := range knownEnvelopeKeys() {
		delete(fields, key)
	}
	if len(fields) > 0 {
		env.extra = fields
	}
	return env, nil
}

func EncodeEnvelope(env ContinuationEnvelope) ([]byte, error) {
	type alias ContinuationEnvelope
	body, err := json.Marshal(alias(env))
	if err != nil {
		return nil, err
	}
	if len(env.extra) == 0 {
		return body, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	for k, v := range env.extra {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return json.Marshal(out)
}

func knownEnvelopeKeys() []string {
	return []string{
		"schemaVersion", "recordRevision", "ownerScope", "taskRef", "sessionId", "turnId",
		"automationRunId", "dispatchKey", "runtimeEpoch", "writerVersion", "toolSchemaRevision",
		"policyRevision", "providerDeploymentRef", "modelRequested", "modelReturned",
		"credentialGeneration", "codecVersion", "profileRevision", "effectiveParameterDigest",
		"messageGroupRefs", "protocolPrivateRef", "protocolDigest", "completeness", "sourceRefs",
		"artifactRefs", "operationRefs", "pendingExternalRefs", "authorizationRef",
		"cancellationRequestedAt", "budgetUsed", "remainingLimit", "lastCommittedEventSequence",
		"recoveryDecision", "updatedAt", "protocol",
	}
}
