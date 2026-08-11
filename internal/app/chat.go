package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/oklog/ulid/v2"
)

type EventEmitter func(bridge.Event) error

func (e *Engine) HandleStreaming(ctx context.Context, request bridge.Request, emit func(bridge.Event) error) bridge.Response {
	ctx = context.WithValue(ctx, streamParentKey{}, ctx)
	return e.Handle(context.WithValue(ctx, eventEmitterKey{}, EventEmitter(emit)), request)
}

type eventEmitterKey struct{}
type streamParentKey struct{}

func handleChatStart(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ProviderID string            `json:"providerId"`
		ModelID    string            `json:"modelId"`
		SessionID  string            `json:"sessionId"`
		Messages   []gateway.Message `json:"messages"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.ProviderID) || len(p.ModelID) < 1 || len(p.ModelID) > 128 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
	}
	hasSession := ulidValid(p.SessionID)
	hasMessages := len(p.Messages) > 0
	if !hasSession && !hasMessages {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 需要提供 sessionId 或 messages", false)
	}

	item, err := e.providers.Get(ctx, p.ProviderID)
	if err != nil {
		return providerFailure(request, err)
	}
	if failure := providerReadyFailure(request, item); failure != nil {
		return *failure
	}
	if !storedModel(item, p.ModelID) {
		return bridge.Failure(request.ID, request.TraceID, "MODEL_NOT_FOUND", "模型不属于该供应商", false)
	}

	emit, ok := ctx.Value(eventEmitterKey{}).(EventEmitter)
	if !ok {
		return bridge.Failure(request.ID, request.TraceID, "STREAM_UNAVAILABLE", "流事件通道不可用", true)
	}

	var messages []gateway.Message
	if hasSession && e.messageReader != nil {
		// Durable session path: assemble context from session history.
		// Dynamic context window: read from provider model config, fallback to 128000.
		contextWindow := int64(128000)
		safetyCeiling := int64(120000)
		var tokenizerRevision string
		for _, m := range item.Models {
			if m.ModelID == p.ModelID && m.ContextWindow > 0 {
				contextWindow = m.ContextWindow
				safetyCeiling = int64(float64(contextWindow) * 0.9375) // 93.75% safety ceiling
				break
			}
		}
		// Estimate system instruction token cost from explicitly provided
		// messages (ADR-005 §3 priority 1: authoritative system/security/product
		// instructions). System messages are reserved before message selection.
		var systemTokens int64
		for _, m := range p.Messages {
			if m.Role == gateway.RoleSystem {
				systemTokens += token.EstimateTokens(m.Content)
			}
		}
		providerInfo := contextapp.ProviderInfo{
			Provider:          string(item.Protocol),
			Model:             p.ModelID,
			ContextWindow:     contextWindow,
			SafetyCeiling:     safetyCeiling,
			ReservedOutput:    4096,
			SystemTokens:      systemTokens,
			SafetyMargin:      1024,
			TokenizerRevision: tokenizerRevision,
		}

		// ADR-005 §5: Synchronous pre-turn compaction. When token usage exceeds
		// the high watermark, compaction is triggered and executed synchronously
		// BEFORE context assembly. This ensures the current turn benefits from
		// the compaction (summary is available for assembly). The flow is:
		// budget check → generate compaction candidate → validate → CAS activate
		// → re-assemble → send request.
		if e.compactionTrigger != nil && e.compactionExecutor != nil {
			e.TriggerPreTurnCompaction(ctx, p.SessionID, string(item.Protocol), p.ModelID, tokenizerRevision, providerInfo.ContextWindow)
		}

		// Build the ContextEnvelope (ADR-005 §3 seven-level priority).
		envelope := contextapp.ContextEnvelope{
			Provider:          providerInfo,
			MaxMessages:       256,
			RecentUserReserve: 0, // Use default: max(512, budget/10)
			SafetyMargin:      1024,
		}

		// Priority 3: Latest accepted compaction checkpoint summary.
		if e.summaryReader != nil {
			if priorSummary, _ := e.summaryReader.GetLatestCompactionSummary(ctx, p.SessionID); priorSummary != "" {
				envelope.AcceptedCheckpoint = &contextapp.ContextSource{
					Type:       contextapp.SourceCompactionSummary,
					ID:         "latest",
					Authority:  contextapp.AuthorityCheckpoint,
					Content:    priorSummary,
					Provenance: "session:" + p.SessionID + ":checkpoint:latest",
				}
			}
		}

		// Handoff capsules: provenance-linked summaries from other sessions,
		// imported as untrusted prior context (ADR-005 §5). Each active
		// capsule's source checkpoint summary is injected at checkpoint
		// authority but tagged with handoff provenance. Capsules whose source
		// checkpoint was deleted (deletion propagation) are skipped
		// fail-closed: their stale summary is never injected.
		capsuleContexts, _ := e.ListImportedHandoffCapsuleContexts(ctx, p.SessionID)
		for _, cc := range capsuleContexts {
			if cc.Checkpoint == nil {
				// Source checkpoint deleted: fail-closed, skip.
				continue
			}
			summaryContent := cc.Checkpoint.HumanSummary
			if summaryContent == "" {
				summaryContent = cc.Checkpoint.SummaryJSON
			}
			envelope.HandoffCapsules = append(envelope.HandoffCapsules, contextapp.ContextSource{
				Type:       contextapp.SourceHandoffCapsule,
				ID:         cc.Capsule.ID,
				Authority:  contextapp.AuthorityCheckpoint,
				Content:    summaryContent,
				Provenance: "handoff:capsule:" + cc.Capsule.ID + ":source-session:" + cc.Capsule.SourceSessionID,
			})
		}

		// Assemble the context envelope with full priority ordering and
		// selection trace (ADR-005 §3).
		result, assembleErr := contextapp.AssembleEnvelope(ctx, e.messageReader, p.SessionID, envelope)
		if assembleErr != nil {
			return bridge.Failure(request.ID, request.TraceID, "CONTEXT_ASSEMBLY_FAILED", "上下文装配失败: "+assembleErr.Error(), true)
		}
		assembled := result.Messages
		// Validate the assembled message sequence (ADR-005 §3: "never emits
		// an invalid provider message sequence").
		if seqErr := contextapp.ValidateProviderSequence(assembled); seqErr != nil {
			return bridge.Failure(request.ID, request.TraceID, "CONTEXT_SEQUENCE_INVALID", "上下文序列无效: "+seqErr.Error(), true)
		}
		messages = make([]gateway.Message, len(assembled))
		for i, m := range assembled {
			messages[i] = gateway.Message{
				Role:    gatewayRole(m.Role),
				Content: m.Content,
			}
		}
		// Prepend any explicitly provided messages (e.g., system instructions).
		if hasMessages {
			messages = append(p.Messages, messages...)
		}
	} else {
		// Legacy path: use directly provided messages.
		if !hasMessages {
			return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 无有效消息（Session 上下文装配器不可用，需提供 messages）", false)
		}
		totalBytes := len(p.ModelID)
		for _, m := range p.Messages {
			totalBytes += len(m.Content)
			if (m.Role != gateway.RoleSystem && m.Role != gateway.RoleUser && m.Role != gateway.RoleAssistant && m.Role != gateway.RoleTool) || strings.TrimSpace(m.Content) == "" || len(m.Content) > 16*1024 || totalBytes > 48*1024 {
				return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
			}
		}
		messages = p.Messages
	}

	if len(messages) == 0 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 无有效消息", false)
	}

	e.streamsMu.Lock()
	if len(e.streams) >= e.maxStreams {
		e.streamsMu.Unlock()
		return bridge.Failure(request.ID, request.TraceID, "STREAM_LIMIT_REACHED", "并发流数量已达上限", true)
	}
	streamID := ulid.Make().String()
	parent, _ := ctx.Value(streamParentKey{}).(context.Context)
	if parent == nil {
		parent = ctx
	}
	streamCtx, cancel := context.WithCancel(parent)
	state := &streamState{cancel: cancel}
	e.streams[streamID] = state
	e.streamsMu.Unlock()
	go e.runStream(streamCtx, streamID, state, item, gateway.Request{Model: p.ModelID, Messages: messages, MaxAttempts: 1}, emit, p.SessionID)
	return bridge.Success(request.ID, map[string]any{"streamId": streamID})
}

// gatewayRole converts a contextapp role string to a gateway.Role.
func gatewayRole(role string) gateway.Role {
	switch role {
	case "system":
		return gateway.RoleSystem
	case "assistant":
		return gateway.RoleAssistant
	case "tool":
		return gateway.RoleTool
	default:
		return gateway.RoleUser
	}
}

func handleStreamCancel(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		StreamID string `json:"streamId"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.StreamID) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "stream.cancel 参数无效", false)
	}
	ok := e.cancelStream(p.StreamID)
	return bridge.Success(request.ID, map[string]any{"cancelled": ok})
}

func (e *Engine) runStream(ctx context.Context, id string, state *streamState, p provider.Provider, req gateway.Request, emit EventEmitter, sessionID string) {
	var seq uint64
	var assistantText strings.Builder
	var streamResult gateway.Response
	send := func(event bridge.Event) error {
		seq++
		event.Version = bridge.Version
		event.Kind = "event"
		event.ID = ulid.Make().String()
		event.StreamID = id
		event.Sequence = seq
		return emit(event)
	}
	err := e.withProviderLease(ctx, p, secretlease.OperationChat, func(op context.Context, credential []byte) error {
		a, adapterErr := e.adapter(op, p)
		if adapterErr != nil {
			return adapterErr
		}
		result, streamErr := a.Stream(op, credential, req, func(d gateway.Delta) error {
			if d.Text != "" {
				assistantText.WriteString(d.Text)
				if err := send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: d.Text}}); err != nil {
					return err
				}
			}
			return nil
		})
		streamResult = result
		if streamErr == nil && result.Usage.TotalTokens > 0 {
			if sendErr := send(bridge.Event{Type: bridge.EventUsage, Usage: &bridge.UsageEvent{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.TotalTokens}}); sendErr != nil {
				return sendErr
			}
		}
		return streamErr
	})
	// On stream success with a durable session, persist the assistant response.
	var messageID string
	if err == nil && sessionID != "" && e.messages != nil {
		text := assistantText.String()
		if text != "" {
			usage := messageapp.AssistantUsage{
				Provider:    string(p.Protocol),
				Model:       req.Model,
				TotalTokens: int64(streamResult.Usage.TotalTokens),
			}
			msg, appendErr := e.messages.AppendAssistant(ctx, id, "engine", sessionID, text, usage)
			if appendErr != nil {
				err = appendErr
			} else {
				messageID = msg.ID
			}
		}
	}
	terminal := bridge.Event{Type: e.selectTerminal(id, state, err)}
	if terminal.Type == bridge.EventCompleted && messageID != "" {
		terminal.Completed = &bridge.CompletedEvent{MessageID: messageID}
	}
	if terminal.Type == bridge.EventFailed {
		code, message, retryable := "UPSTREAM_FAILED", "模型请求失败", true
		if errors.Is(err, messageapp.ErrAssistantResponseTooLarge) {
			code, message, retryable = "ASSISTANT_RESPONSE_TOO_LARGE", "assistant 响应超过 16384 code points", false
		} else if errors.Is(err, messageapp.ErrMessageStorageQuotaReached) {
			code, message, retryable = "MESSAGE_STORAGE_QUOTA_REACHED", "消息存储配额已满", false
		}
		terminal.Error = &bridge.StreamError{Code: code, Message: message, Retryable: retryable}
	}
	if send(terminal) != nil {
		state.cancel()
	}
	e.finishTerminal(id, state)
}

func ulidValid(s string) bool { _, err := ulid.ParseStrict(s); return err == nil }
