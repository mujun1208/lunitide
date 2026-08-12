package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

type EventEmitter func(bridge.Event) error

func (e *Engine) HandleStreaming(ctx context.Context, request bridge.Request, emit func(bridge.Event) error) bridge.Response {
	ctx = context.WithValue(ctx, streamParentKey{}, ctx)
	return e.Handle(context.WithValue(ctx, eventEmitterKey{}, EventEmitter(emit)), request)
}

type eventEmitterKey struct{}
type streamParentKey struct{}

type executionMode string

const (
	executionModeApproval   executionMode = "approval"
	executionModeAutoEdit   executionMode = "auto-edit"
	executionModePlan       executionMode = "plan"
	executionModeFullAccess executionMode = "full-access"
)

func handleChatStart(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ProviderID    string            `json:"providerId"`
		ModelID       string            `json:"modelId"`
		SessionID     string            `json:"sessionId"`
		Messages      []gateway.Message `json:"messages"`
		ExecutionMode executionMode     `json:"executionMode"`
		ContextRefs   []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"contextRefs"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.ProviderID) || len(p.ModelID) < 1 || len(p.ModelID) > 128 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
	}
	hasSession := ulidValid(p.SessionID)
	hasMessages := len(p.Messages) > 0
	if !hasSession && !hasMessages {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 需要提供 sessionId 或 messages", false)
	}
	if !validChatMessages(p.ModelID, p.Messages) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
	}
	for _, ref := range p.ContextRefs {
		if !validCanonicalULID(ref.ID) || (ref.Type != "attachment" && ref.Type != "skillResult") {
			return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start contextRefs 无效", false)
		}
	}
	mode, validMode := normalizeExecutionMode(p.ExecutionMode)
	if !validMode {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start executionMode 无效", false)
	}
	trustedMessages := append([]gateway.Message{{Role: gateway.RoleSystem, Content: executionModeInstruction(mode)}}, p.Messages...)

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
	var images []gateway.Image
	if hasSession && e.messageReader != nil {
		// Durable session path: assemble context from session history.
		// Dynamic context window: read from provider model config, fallback to 128000.
		contextWindow := int64(128000)
		safetyCeiling := int64(120000)
		tokenizerRevision := token.CanonicalTokenizerRevision
		for _, m := range item.Models {
			if m.ModelID == p.ModelID && m.ContextWindow > 0 {
				contextWindow = m.ContextWindow
				safetyCeiling = int64(float64(contextWindow) * 0.9375) // 93.75% safety ceiling
				break
			}
		}
		// Explicit messages sit outside restored durable history, so reserve the
		// current user/tool turn as well as authoritative system instructions.
		var explicitTokens int64
		for _, m := range trustedMessages {
			explicitTokens += token.EstimateTokens(m.Content)
		}
		providerInfo := contextapp.ProviderInfo{
			Provider:          string(item.Protocol),
			Model:             p.ModelID,
			ContextWindow:     contextWindow,
			SafetyCeiling:     safetyCeiling,
			ReservedOutput:    4096,
			SystemTokens:      explicitTokens,
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
			compactionResult := e.TriggerPreTurnCompaction(ctx, p.SessionID, item.ID, p.ModelID, tokenizerRevision, providerInfo.ContextWindow)
			if compactionResult.Err != nil {
				if errors.Is(compactionResult.Err, context.Canceled) || errors.Is(compactionResult.Err, context.DeadlineExceeded) {
					return bridge.Failure(request.ID, request.TraceID, "REQUEST_CANCELLED", "请求已取消", false)
				}
				return internalBridgeFailure(request, "COMPACTION_TRIGGER_FAILED", "上下文压缩检查失败", true, compactionResult.Err)
			}
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
			priorSummary, summaryErr := e.summaryReader.GetLatestCompactionSummary(ctx, p.SessionID)
			if summaryErr != nil {
				return internalBridgeFailure(request, "CONTEXT_SUMMARY_READ_FAILED", "上下文摘要暂时不可用", true, summaryErr)
			}
			if priorSummary != "" {
				envelope.AcceptedCheckpoint = &contextapp.ContextSource{
					Type:       contextapp.SourceCompactionSummary,
					ID:         "latest",
					Authority:  contextapp.AuthorityEvidence,
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
		capsuleContexts, capsuleErr := e.ListImportedHandoffCapsuleContexts(ctx, p.SessionID)
		if capsuleErr != nil {
			return internalBridgeFailure(request, "HANDOFF_CONTEXT_READ_FAILED", "交接上下文暂时不可用", true, capsuleErr)
		}
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

		// Attachment excerpts are opt-in per turn. Do not enumerate or resend
		// historical session attachments unless the renderer supplied at least
		// one explicit attachment ref. This keeps ordinary chat.start latency off
		// the attachment storage path and prevents implicit context injection.
		attachmentRefs := make(map[string]struct{})
		var orderedAttachmentRefs []string
		for _, ref := range p.ContextRefs {
			if ref.Type == "attachment" {
				if _, duplicate := attachmentRefs[ref.ID]; !duplicate {
					attachmentRefs[ref.ID] = struct{}{}
					orderedAttachmentRefs = append(orderedAttachmentRefs, ref.ID)
				}
			}
		}
		var imageRefs []string
		for _, refID := range orderedAttachmentRefs {
			candidate, getErr := e.GetAttachment(ctx, refID)
			if getErr != nil {
				if errors.Is(getErr, attachmentapp.ErrAttachmentNotFound) {
					return bridge.Failure(request.ID, request.TraceID, "CONTEXT_REF_NOT_FOUND", "显式上下文引用不存在或已删除", false)
				}
				return internalBridgeFailure(request, "ATTACHMENT_CONTEXT_READ_FAILED", "附件上下文暂时不可用", true, getErr)
			}
			if candidate.SessionID != p.SessionID {
				return bridge.Failure(request.ID, request.TraceID, "CONTEXT_REF_SCOPE_MISMATCH", "显式上下文引用不属于当前会话", false)
			}
			if strings.HasPrefix(candidate.MIME, "image/") {
				imageRefs = append(imageRefs, candidate.ID)
				continue
			}
			if !candidate.IsReadable() || candidate.ParsedText == "" {
				return bridge.Failure(request.ID, request.TraceID, "CONTEXT_REF_NOT_READABLE", "显式上下文引用尚未解析成功", false)
			}
			envelope.AttachmentExcerpts = append(envelope.AttachmentExcerpts, contextapp.ContextSource{Type: contextapp.SourceAttachmentExcerpt, ID: candidate.ID, Authority: contextapp.AuthorityEvidence, Content: candidate.OriginalName + "\n" + candidate.ParsedText, Provenance: "attachment:" + candidate.ID + ":project:" + candidate.ProjectID})
		}

		// Assemble the context envelope with full priority ordering and
		// selection trace (ADR-005 §3).
		result, assembleErr := contextapp.AssembleEnvelope(ctx, e.messageReader, p.SessionID, envelope)
		if assembleErr != nil {
			return internalBridgeFailure(request, "CONTEXT_ASSEMBLY_FAILED", "上下文装配暂时不可用", true, assembleErr)
		}
		messages, err = combineDurableProviderMessages(result.Messages, trustedMessages, providerInfo)
		if err != nil {
			if errors.Is(err, errCombinedContextOverBudget) {
				return bridge.Failure(request.ID, request.TraceID, "CONTEXT_BUDGET_EXCEEDED", "最终上下文超过模型输入预算", false)
			}
			return internalBridgeFailure(request, "CONTEXT_SEQUENCE_INVALID", "上下文序列无效", true, err)
		}
		// Images are expensive and model-dependent. Unlike parsed text, do not
		// silently resend every historical image on every turn: only explicitly
		// referenced images enter the multimodal request.
		if len(imageRefs) > 0 {
			if len(imageRefs) > attachmentapp.MaxVisionImages {
				return bridge.Failure(request.ID, request.TraceID, "ATTACHMENT_IMAGE_READ_FAILED", "图片附件读取或校验失败", false)
			}
			total := 0
			for _, imageID := range imageRefs {
				image, visionErr := e.GetVisionImage(ctx, imageID, p.SessionID)
				if visionErr != nil {
					retryable := !errors.Is(visionErr, attachmentapp.ErrAttachmentNotFound) && !errors.Is(visionErr, attachmentapp.ErrScopeMismatch) && !errors.Is(visionErr, attachmentapp.ErrUnsupportedMIME) && !errors.Is(visionErr, attachmentapp.ErrImageIntegrity) && !errors.Is(visionErr, attachmentapp.ErrImageBudget)
					return internalBridgeFailure(request, "ATTACHMENT_IMAGE_READ_FAILED", "图片附件读取或校验失败", retryable, visionErr)
				}
				total += len(image.Data)
				if total > attachmentapp.MaxVisionBatchBytes {
					return bridge.Failure(request.ID, request.TraceID, "ATTACHMENT_IMAGE_READ_FAILED", "图片附件读取或校验失败", false)
				}
				images = append(images, gateway.Image{MIME: image.MIME, Data: image.Data})
			}
		}
	} else {
		// Legacy path: use directly provided messages.
		if !hasMessages {
			return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start 无有效消息（Session 上下文装配器不可用，需提供 messages）", false)
		}
		messages = trustedMessages
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
	req := gateway.Request{Model: p.ModelID, Messages: messages, Images: images, MaxTokens: 4096, MaxAttempts: 1}
	if mode != executionModePlan && e.tools != nil {
		req.Tools = engineToolDefinitions()
	}
	go e.runStream(streamCtx, streamID, state, item, req, emit, p.SessionID, mode)
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

var errCombinedContextOverBudget = errors.New("combined provider context exceeds effective input budget")

func validChatMessages(model string, messages []gateway.Message) bool {
	totalBytes := len(model)
	for _, m := range messages {
		totalBytes += len(m.Content)
		// Public chat.start input is renderer-controlled; system and tool roles
		// remain available only to trusted engine-owned assembly mechanisms.
		if (m.Role != gateway.RoleUser && m.Role != gateway.RoleAssistant) || strings.TrimSpace(m.Content) == "" || len(m.Content) > 16*1024 || totalBytes > 48*1024 {
			return false
		}
	}
	return true
}

func normalizeExecutionMode(mode executionMode) (executionMode, bool) {
	if mode == "" {
		return executionModeApproval, true
	}
	switch mode {
	case executionModeApproval, executionModeAutoEdit, executionModePlan, executionModeFullAccess:
		return mode, true
	default:
		return "", false
	}
}

func executionModeInstruction(mode executionMode) string {
	const available = "Tools may be used only when they are actually available in this runtime; never claim that a command ran, a file changed, or any other mutation occurred unless it actually did."
	switch mode {
	case executionModePlan:
		return "Execution mode: plan. Planning only: analyze and provide a proposed plan. Do not invoke tools, execute commands, create, edit, or delete files, or perform any other mutation. Do not claim that execution or mutation occurred."
	case executionModeAutoEdit:
		return "Execution mode: auto-edit. You may apply edits within the user's requested scope without per-edit approval. Ask before destructive, high-risk, or out-of-scope actions. " + available
	case executionModeFullAccess:
		return "Execution mode: full-access. You may carry out requested actions without approval, subject to actual runtime permissions and safety boundaries. " + available
	default:
		return "Execution mode: approval. Propose actions and obtain explicit user approval before any tool use or operation that could mutate files or system state. Read-only analysis does not require approval. " + available
	}
}

func engineToolDefinitions() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: "workspace.list", Description: "List a controlled session workspace directory", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}`)},
		{Name: "workspace.read", Description: "Read a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "workspace.write", Description: "Atomically write a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
		{Name: "command.run", Description: "Run one allowlisted argv command in the controlled workspace", Schema: []byte(`{"type":"object","properties":{"argv":{"type":"array","items":{"type":"string"},"minItems":2,"maxItems":2}},"required":["argv"],"additionalProperties":false}`)},
	}
}

func combineDurableProviderMessages(history []contextapp.Message, explicit []gateway.Message, info contextapp.ProviderInfo) ([]gateway.Message, error) {
	combined := make([]gateway.Message, 0, len(history)+len(explicit))
	for _, m := range explicit {
		if m.Role == gateway.RoleSystem {
			combined = append(combined, m)
		}
	}
	for _, m := range history {
		combined = append(combined, gateway.Message{Role: gatewayRole(m.Role), Content: m.Content})
	}
	for _, m := range explicit {
		if m.Role != gateway.RoleSystem {
			combined = append(combined, m)
		}
	}
	// History counts can predate normalization or synthetic concatenation.
	// Enforce the final provider budget only from exact visible contents.
	var used int64
	for _, m := range combined {
		used += token.EstimateTokens(m.Content)
	}
	providerSequence := make([]contextapp.Message, len(combined))
	for i, m := range combined {
		providerSequence[i] = contextapp.Message{Role: string(m.Role), Content: m.Content}
	}
	if err := contextapp.ValidateProviderSequence(providerSequence); err != nil {
		return nil, err
	}
	ceiling := info.ContextWindow
	if info.SafetyCeiling > 0 && info.SafetyCeiling < ceiling {
		ceiling = info.SafetyCeiling
	}
	if used > ceiling-info.ReservedOutput-info.ToolSchemaTokens-info.SafetyMargin {
		return nil, errCombinedContextOverBudget
	}
	return combined, nil
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

func handleChatToolApprove(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID  string `json:"sessionId"`
		CallID     string `json:"callId"`
		ArgsDigest string `json:"argsDigest"`
		Approved   bool   `json:"approved"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.SessionID) || p.CallID == "" || len(p.CallID) > 128 || len(p.ArgsDigest) != 64 || e.tools == nil {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.tool.approve 参数无效", false)
	}
	r, err := e.tools.Decide(ctx, p.SessionID, p.CallID, p.ArgsDigest, p.Approved)
	if err != nil {
		return bridge.Failure(request.ID, request.TraceID, "TOOL_APPROVAL_CONSUMED", err.Error(), false)
	}
	status := "rejected"
	if p.Approved {
		status = "executed"
	}
	result := map[string]any{"callId": p.CallID, "status": status, "resultDigest": r.Digest, "summary": r.Output}
	if p.Approved && r.Artifact != nil && r.Artifact.Kind == "html" && len([]byte(r.Artifact.Content)) <= 180<<10 {
		result["artifact"] = map[string]string{"kind": r.Artifact.Kind, "path": r.Artifact.Path, "content": r.Artifact.Content}
	}
	return bridge.Success(request.ID, result)
}

func (e *Engine) runStream(ctx context.Context, id string, state *streamState, p provider.Provider, req gateway.Request, emit EventEmitter, sessionID string, modes ...executionMode) {
	const maxThinkingChunkBytes = 16 * 1024
	const maxThinkingTotalBytes = 256 * 1024
	const thinkingFlushBytes = 4 * 1024
	const thinkingFlushInterval = 50 * time.Millisecond
	var seq uint64
	var assistantText strings.Builder
	var thinkingText strings.Builder
	var pendingThinking string
	var pendingThinkingSince time.Time
	var streamResult gateway.Response
	mode := executionModeApproval
	if len(modes) > 0 {
		mode = modes[0]
	}
	rawSend := func(event bridge.Event) error {
		seq++
		event.Version = bridge.Version
		event.Kind = "event"
		event.ID = ulid.Make().String()
		event.StreamID = id
		event.Sequence = seq
		return emit(event)
	}
	flushThinking := func(force bool) error {
		for pendingThinking != "" && (force || len(pendingThinking) >= thinkingFlushBytes) {
			chunk := truncateUTF8Bytes(pendingThinking, maxThinkingChunkBytes)
			if err := rawSend(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: chunk}}); err != nil {
				return err
			}
			pendingThinking = pendingThinking[len(chunk):]
			pendingThinkingSince = time.Now()
			if !force && len(pendingThinking) < thinkingFlushBytes {
				break
			}
		}
		if pendingThinking == "" {
			pendingThinkingSince = time.Time{}
		}
		return nil
	}
	send := func(event bridge.Event) error {
		// Keep flushing and event emission on the stream callback goroutine: emit
		// may be synchronous, and this preserves thinking-before-answer ordering.
		if event.Type != bridge.EventThinking {
			if err := flushThinking(true); err != nil {
				return err
			}
		}
		return rawSend(event)
	}
	err := e.withProviderLease(ctx, p, secretlease.OperationChat, func(op context.Context, credential []byte) error {
		a, adapterErr := e.adapter(op, p)
		if adapterErr != nil {
			return adapterErr
		}
		seen := map[string]bool{}
		var result gateway.Response
		var streamErr error
		toolsFallbackUsed := false
		for step := 0; step < 6; step++ {
			result, streamErr = a.Stream(op, credential, req, func(d gateway.Delta) error {
				if d.Reasoning != "" && thinkingText.Len() < maxThinkingTotalBytes {
					reasoning := truncateUTF8Bytes(d.Reasoning, maxThinkingTotalBytes-thinkingText.Len())
					thinkingText.WriteString(reasoning)
					if pendingThinking == "" && reasoning != "" {
						pendingThinkingSince = time.Now()
					}
					pendingThinking += reasoning
					force := !pendingThinkingSince.IsZero() && time.Since(pendingThinkingSince) >= thinkingFlushInterval
					if err := flushThinking(force); err != nil {
						return err
					}
				}
				if d.Text != "" {
					assistantText.WriteString(d.Text)
					if err := send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: d.Text}}); err != nil {
						return err
					}
				}
				return nil
			})
			var gatewayErr *gateway.Error
			if streamErr != nil && !toolsFallbackUsed && assistantText.Len() == 0 && thinkingText.Len() == 0 && len(req.Tools) > 0 && errors.As(streamErr, &gatewayErr) && gatewayErr.HTTPStatus == 400 {
				// Some compatible text models reject function definitions. Retry once
				// as plain chat while preserving messages and attachment context.
				req.Tools = nil
				toolsFallbackUsed = true
				continue
			}
			if streamErr != nil || len(result.Message.ToolCalls) == 0 {
				break
			}
			req.Messages = append(req.Messages, result.Message)
			for _, call := range result.Message.ToolCalls {
				if seen[call.ID] {
					return errors.New("duplicate tool call id")
				}
				seen[call.ID] = true
				digest := toolruntime.Digest(call.Name, call.Arguments)
				if digest == "" {
					return errors.New("invalid tool arguments")
				}
				if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest}}); err != nil {
					return err
				}
				r, toolErr := e.tools.Execute(op, toolruntime.Mode(mode), sessionID, call.Name, call.Arguments, false)
				if errors.Is(toolErr, toolruntime.ErrApprovalRequired) {
					if _, prepareErr := e.tools.Prepare(op, id, sessionID, call.ID, call.Name, call.Arguments, toolruntime.Mode(mode), 10*time.Minute); prepareErr != nil {
						return prepareErr
					}
					if sendErr := send(bridge.Event{Type: bridge.EventApprovalRequired, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: "approval required"}}); sendErr != nil {
						return sendErr
					}
					return nil
				}
				summary := r.Output
				if toolErr != nil {
					summary = toolErr.Error()
				}
				if len(summary) > 4096 {
					summary = summary[:4096]
				}
				toolEvent := &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}
				if toolErr == nil && r.Artifact != nil && r.Artifact.Kind == "html" && len([]byte(r.Artifact.Content)) <= 180<<10 {
					toolEvent.Artifact = &bridge.ArtifactEvent{Kind: r.Artifact.Kind, Path: r.Artifact.Path, Content: r.Artifact.Content}
				}
				if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: toolEvent}); err != nil {
					return err
				}
				req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
			}
		}
		streamResult = result
		if streamErr == nil && result.Usage.TotalTokens > 0 {
			if sendErr := send(bridge.Event{Type: bridge.EventUsage, Usage: &bridge.UsageEvent{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.TotalTokens}}); sendErr != nil {
				return sendErr
			}
		}
		return streamErr
	})
	// Successful upstream completion must claim finalization before any durable
	// side effect. This is the linearization point against stream.cancel.
	var messageID string
	finalizationClaimed := false
	if err == nil {
		finalizationClaimed = e.claimStreamFinalization(state)
	}
	if err == nil && finalizationClaimed && sessionID != "" && e.messages != nil {
		text := assistantText.String()
		if text != "" {
			usage := messageapp.AssistantUsage{
				Provider:     string(p.Protocol),
				Model:        req.Model,
				OutputTokens: int64(streamResult.Usage.OutputTokens),
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
		terminal.Error = chatStreamError(err)
	}
	if send(terminal) != nil {
		state.cancel()
	}
	e.finishTerminal(id, state)
}

func truncateUTF8Bytes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	end := limit
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end]
}

// chatStreamError deliberately maps only stable error classes. In particular,
// gateway Error.Message may originate in an adapter, so it must never be
// copied into a renderer-visible stream event.
func chatStreamError(err error) *bridge.StreamError {
	streamError := func(code, message string, retryable bool) *bridge.StreamError {
		return &bridge.StreamError{Code: code, Message: message, Retryable: retryable}
	}
	if errors.Is(err, messageapp.ErrAssistantResponseTooLarge) {
		return streamError("ASSISTANT_RESPONSE_TOO_LARGE", "assistant 响应超过 16384 code points", false)
	}
	if errors.Is(err, messageapp.ErrMessageStorageQuotaReached) {
		return streamError("MESSAGE_STORAGE_QUOTA_REACHED", "消息存储配额已满", false)
	}
	if errors.Is(err, context.DeadlineExceeded) || networkpolicy.ErrorCode(err) == networkpolicy.CodeTimeout {
		return streamError("UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true)
	}

	var gatewayErr *gateway.Error
	if errors.As(err, &gatewayErr) {
		if gatewayErr.Code == "REQUEST_TOO_LARGE" || gatewayErr.HTTPStatus == 413 {
			return streamError("REQUEST_TOO_LARGE", "请求内容过大，请减少附件或上下文后重试", false)
		}
		if gatewayErr.Code == "TIMEOUT" || gatewayErr.Code == "OUTCOME_UNKNOWN" {
			return streamError("UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true)
		}
		switch gatewayErr.HTTPStatus {
		case 400:
			return streamError("UPSTREAM_BAD_REQUEST", "供应商拒绝了请求，请检查模型、附件和上下文", false)
		case 401:
			return streamError("PROVIDER_AUTHENTICATION_FAILED", "供应商身份验证失败，请检查凭据", false)
		case 403:
			return streamError("PROVIDER_ACCESS_DENIED", "供应商拒绝访问，请检查模型权限", false)
		case 429:
			return streamError("PROVIDER_RATE_LIMITED", "供应商请求过于频繁，请稍后重试", true)
		}
		if gatewayErr.HTTPStatus >= 500 && gatewayErr.HTTPStatus <= 599 {
			return streamError("UPSTREAM_UNAVAILABLE", "供应商服务暂时不可用，请稍后重试", true)
		}
	}
	return streamError("UPSTREAM_FAILED", "模型请求失败", true)
}

func ulidValid(s string) bool { _, err := ulid.ParseStrict(s); return err == nil }
