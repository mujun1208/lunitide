package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/oklog/ulid/v2"
)

type continuationIdentity struct {
	ProviderDeploymentRef string
	ModelRequested        string
	ModelReturned         string
	RuntimeEpoch          string
	OwnerScope            string
	TaskRef               string
	CodecVersion          string
	OperationRefs         []string
}

func pinContinuationIdentity(cp *chatTurnCheckpoint, pin continuationIdentity) {
	if cp == nil {
		return
	}
	env := cp.Continuation
	if env == nil {
		env = &modelfit.ContinuationEnvelope{SchemaVersion: "1"}
	}
	if env.ProviderDeploymentRef == "" {
		env.ProviderDeploymentRef = strings.TrimSpace(pin.ProviderDeploymentRef)
	}
	if env.ModelRequested == "" {
		env.ModelRequested = strings.TrimSpace(pin.ModelRequested)
	}
	if env.ModelReturned == "" {
		env.ModelReturned = strings.TrimSpace(pin.ModelReturned)
	}
	if env.RuntimeEpoch == "" {
		env.RuntimeEpoch = strings.TrimSpace(pin.RuntimeEpoch)
	}
	if env.OwnerScope == "" {
		env.OwnerScope = strings.TrimSpace(pin.OwnerScope)
	}
	if env.TaskRef == "" {
		env.TaskRef = strings.TrimSpace(pin.TaskRef)
	}
	if env.CodecVersion == "" {
		env.CodecVersion = strings.TrimSpace(pin.CodecVersion)
	}
	for _, ref := range pin.OperationRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		found := false
		for _, existing := range env.OperationRefs {
			if existing == ref {
				found = true
				break
			}
		}
		if !found {
			env.OperationRefs = append(env.OperationRefs, ref)
		}
	}
	cp.Continuation = env
}

const maxObtainedProtocolBytes = 256 * 1024

func completenessOf(cp chatTurnCheckpoint) modelfit.Completeness {
	if cp.Continuation != nil && cp.Continuation.Completeness != "" {
		return cp.Continuation.Completeness
	}
	return modelfit.ClassifyCompleteness(modelfit.ProtocolEvidence{
		HasGoal:            cp.Goal != "",
		HasToolReceipts:    len(cp.LastTools) > 0,
		HasMessageGroup:    cp.Continuation != nil && len(cp.Continuation.MessageGroupRefs) > 0,
		HasObtainedPrivate: cp.Continuation != nil && cp.Continuation.Protocol.ReasoningContent != "",
	})
}

func protocolMessagesFromAdapter(msgs []llmadapter.Message) []modelfit.ProtocolMessage {
	out := make([]modelfit.ProtocolMessage, 0, len(msgs))
	for _, m := range msgs {
		pm := modelfit.ProtocolMessage{
			Role:             string(m.Role),
			Content:          m.Content,
			ReasoningContent: m.ReasoningContent,
			ToolCallID:       m.ToolCallID,
		}
		for _, c := range m.ToolCalls {
			pm.ToolCalls = append(pm.ToolCalls, modelfit.ProtocolToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments})
		}
		out = append(out, pm)
	}
	return out
}

func rememberMessageGroups(cp *chatTurnCheckpoint, msgs []llmadapter.Message) []modelfit.MessageGroup {
	if cp == nil {
		return nil
	}
	groups := modelfit.ExtractMessageGroups(protocolMessagesFromAdapter(msgs))
	var refs []string
	var complete []modelfit.MessageGroup
	for i := range groups {
		if !groups[i].Complete {
			continue
		}
		if groups[i].ID == "" {
			groups[i].ID = ulid.Make().String()
		}
		refs = append(refs, groups[i].ID)
		complete = append(complete, groups[i])
	}
	if len(refs) == 0 {
		return nil
	}
	if cp.Continuation == nil {
		cp.Continuation = &modelfit.ContinuationEnvelope{SchemaVersion: "1"}
	}
	cp.Continuation.MessageGroupRefs = refs
	return complete
}

func obtainedProtocolReasoning(resp llmadapter.Response, streamed string, disable bool) string {
	if s := strings.TrimSpace(resp.Reasoning); s != "" {
		return s
	}
	if s := strings.TrimSpace(resp.Message.ReasoningContent); s != "" {
		return s
	}
	if disable {
		return ""
	}
	return streamed
}

func (e *Engine) saveTurnCheckpointAfterModel(sessionID string, turn *chatTurnCheckpoint, resp llmadapter.Response, streamed string, disable bool, live []llmadapter.Message) error {
	if turn == nil {
		return e.saveTurnCheckpoint(sessionID, chatTurnCheckpoint{})
	}
	if live != nil {
		turn.liveProtocol = live
	}
	attachObtainedProtocol(turn, obtainedProtocolReasoning(resp, streamed, disable))
	return e.saveTurnCheckpoint(sessionID, *turn)
}

func attachObtainedProtocol(cp *chatTurnCheckpoint, reasoning string) {
	if cp == nil {
		return
	}
	captured := modelfit.CaptureProtocolFields(modelfit.ObtainedProtocol{
		ReasoningContent: strings.TrimSpace(truncateUTF8Bytes(reasoning, maxObtainedProtocolBytes)),
		Source:           modelfit.SourceProvider,
	})
	if captured.ReasoningContent == "" {
		return
	}
	if cp.Continuation == nil {
		cp.Continuation = &modelfit.ContinuationEnvelope{SchemaVersion: "1"}
	}
	cp.Continuation.Protocol = captured
}

func seedContinuationFromScope(ctx context.Context, cp *chatTurnCheckpoint) {
	if cp == nil {
		return
	}
	scope := continuityScopeFrom(ctx)
	if strings.TrimSpace(scope.AutomationRunID) == "" && strings.TrimSpace(scope.DispatchKey) == "" {
		return
	}
	env := cp.Continuation
	if env == nil {
		env = &modelfit.ContinuationEnvelope{SchemaVersion: "1"}
	}
	if env.AutomationRunID == "" {
		env.AutomationRunID = strings.TrimSpace(scope.AutomationRunID)
	}
	if env.DispatchKey == "" {
		env.DispatchKey = strings.TrimSpace(scope.DispatchKey)
	}
	cp.Continuation = env
}

func attachContinuation(sessionID string, writerVersion string, cp *chatTurnCheckpoint) {
	if cp == nil {
		return
	}
	env := cp.Continuation
	if env == nil {
		env = &modelfit.ContinuationEnvelope{SchemaVersion: "1"}
	}
	if env.SchemaVersion == "" {
		env.SchemaVersion = "1"
	}
	if env.SessionID == "" {
		env.SessionID = sessionID
	}
	if env.TurnID == "" {
		env.TurnID = cp.StreamID
	}
	if env.WriterVersion == "" {
		env.WriterVersion = writerVersion
	}
	if env.RuntimeEpoch == "" {
		env.RuntimeEpoch = ulid.Make().String()
	}
	if env.Completeness == "" || env.Completeness == modelfit.CompletenessLegacyUnknown {
		env.Completeness = completenessOf(*cp)
		if env.Completeness == modelfit.CompletenessLegacyUnknown && (cp.Goal != "" || len(cp.LastTools) > 0) {
			env.Completeness = modelfit.CompletenessStructuredOnly
		}
	}
	if codec := modelfit.LookupCodec(env.CodecVersion); codec != nil {
		var groups []modelfit.MessageGroup
		if len(cp.liveProtocol) > 0 {
			groups = rememberMessageGroups(cp, cp.liveProtocol)
		}
		if len(groups) > 0 {
			if codec.Qualify(modelfit.QualifyInput{
				Family:  codec.Family(),
				Groups:  groups,
				Private: env.Protocol,
			}) {
				env.Completeness = modelfit.CompletenessNativeComplete
			} else if env.Completeness == modelfit.CompletenessNativeComplete {
				env.Completeness = modelfit.CompletenessStructuredOnly
			}
		} else if env.Completeness == modelfit.CompletenessNativeComplete &&
			env.ProtocolPrivateRef == "" &&
			(env.Protocol.Source != modelfit.SourceProvider || env.Protocol.ReasoningContent == "") {
			env.Completeness = modelfit.CompletenessStructuredOnly
		}
	} else if env.Completeness == modelfit.CompletenessNativeComplete {
		env.Completeness = modelfit.CompletenessStructuredOnly
	}
	cp.Continuation = env
}

func (c *chatTurnCheckpoint) UnmarshalJSON(raw []byte) error {
	type alias chatTurnCheckpoint
	var known alias
	if err := json.Unmarshal(raw, &known); err != nil {
		return err
	}
	*c = chatTurnCheckpoint(known)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range knownCheckpointKeys() {
		delete(fields, key)
	}
	if len(fields) > 0 {
		c.extra = fields
	}
	return nil
}

func adapterMessagesFromProtocol(msgs []modelfit.ProtocolMessage) []llmadapter.Message {
	out := make([]llmadapter.Message, 0, len(msgs))
	for _, m := range msgs {
		am := llmadapter.Message{
			Role:             llmadapter.Role(m.Role),
			Content:          m.Content,
			ReasoningContent: m.ReasoningContent,
			ToolCallID:       m.ToolCallID,
		}
		for _, c := range m.ToolCalls {
			am.ToolCalls = append(am.ToolCalls, llmadapter.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments})
		}
		out = append(out, am)
	}
	return out
}

func (e *Engine) nativeReplayMessages(sessionID string) []llmadapter.Message {
	if e == nil || sessionID == "" {
		return nil
	}
	cp := e.loadTurnCheckpoint(sessionID)
	env := cp.Continuation
	if env == nil || env.Completeness != modelfit.CompletenessNativeComplete {
		return nil
	}
	codec := modelfit.LookupCodec(env.CodecVersion)
	if codec == nil {
		return nil
	}
	var groups []modelfit.MessageGroup
	if e.messageGroups != nil && looksLikeULID(sessionID) && looksLikeULID(cp.StreamID) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		loaded, err := e.messageGroups.ListCompleteProtocolMessageGroups(ctx, ownerScope(sessionID), sessionID, cp.StreamID)
		if err == nil {
			groups = loaded
		}
	}
	if len(groups) == 0 && len(cp.liveProtocol) > 0 {
		groups = rememberMessageGroups(&cp, cp.liveProtocol)
	}
	if len(groups) == 0 {
		return nil
	}
	priv := env.Protocol
	if priv.ReasoningContent == "" && env.ProtocolPrivateRef != "" && e.messageGroups != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		blob, _, err := e.messageGroups.GetProtocolPrivate(ctx, ownerScope(sessionID), env.ProtocolPrivateRef)
		if err == nil {
			if plain, openErr := modelfit.OpenProtocolPrivate(blob, protocolPrivateKey(env.CredentialGeneration)); openErr == nil {
				priv.ReasoningContent = string(plain)
				priv.Source = modelfit.SourceProvider
			}
		}
	}
	msgs, err := codec.EncodeReplay(groups, priv)
	if err != nil {
		return nil
	}
	return adapterMessagesFromProtocol(msgs)
}

func protocolPrivateKey(generation string) []byte {
	sum := sha256.Sum256([]byte("lunitide-protocol-private|" + generation))
	return sum[:]
}

func (e *Engine) persistProtocolPrivate(sessionID string, env *modelfit.ContinuationEnvelope) error {
	if e == nil || e.messageGroups == nil || env == nil || env.Protocol.ReasoningContent == "" {
		return nil
	}
	ref := env.ProtocolPrivateRef
	if ref == "" {
		ref = ulid.Make().String()
	}
	blob, digest, err := modelfit.SealProtocolPrivate([]byte(env.Protocol.ReasoningContent), protocolPrivateKey(env.CredentialGeneration))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := e.messageGroups.PutProtocolPrivate(ctx, ownerScope(sessionID), ref, blob, digest, env.CredentialGeneration); err != nil {
		return err
	}
	env.ProtocolPrivateRef = ref
	env.ProtocolDigest = digest
	return nil
}

func continuationExport(env *modelfit.ContinuationEnvelope) *modelfit.ContinuationEnvelope {
	if env == nil {
		return nil
	}
	out := *env
	out.Protocol = modelfit.RedactProtocolCapture(env.Protocol)
	return &out
}

func (c chatTurnCheckpoint) MarshalJSON() ([]byte, error) {
	type alias chatTurnCheckpoint
	body, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	if len(c.extra) == 0 {
		return body, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	for k, v := range c.extra {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return json.Marshal(out)
}

func knownCheckpointKeys() []string {
	return []string{
		"status", "goal", "streamId", "injected", "queueDeliveries", "lastTools", "toolFailed",
		"capabilityWork", "pptActive", "pptStage", "pptTools", "pptNudges", "pptGenerated",
		"docxActive", "docxKind", "docxStage", "docxTools", "docxNudges", "docxGenerated",
		"docxChars", "persistDraft", "persistFailed", "persistUsage", "updatedAt", "continuation",
	}
}
