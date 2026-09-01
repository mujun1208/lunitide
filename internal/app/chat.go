package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/jsonutil"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/skillapp"
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

// Preference injection budget (learning loop P3-3): confirmed preferences
// appended to the system instruction are bounded so they never crowd out
// the conversation context.
const (
	preferenceInjectMaxItems  = 8
	preferenceInjectMaxBytes  = 2048
	companionMaxTokens        = 2048
	companionMaxMessages      = 24
	companionMaxToolLoopSteps = 24
	// chatMaxTokens leaves headroom after long reasoning so a short tool
	// call still fits. Dumping a full HTML game under 4096 truncated the
	// tool JSON and surfaced “出错了，无法完成。” Office generators
	// (excel.gen with a 半年财报) need more than 16k so the tool JSON
	// can finish instead of dying at ~30s with a generic turn failure.
	chatMaxTokens = 32768
)

// Skill catalog injection budget (c4-skill): the installed-skill directory
// appended to the system instruction is bounded the same way so the catalog
// can never crowd out the conversation context.
const (
	skillInjectMaxItems = 12
	skillInjectMaxBytes = 2048
)

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
		Companion          bool            `json:"companion"`
		ReplyStyle         string          `json:"replyStyle"`
		StructuredTemplate string          `json:"structuredTemplate"`
		ProjectID          string          `json:"projectId"`
		ProjectPhase       int             `json:"projectPhase"`
		ProjectPhaseLabel  string          `json:"projectPhaseLabel"`
		SubagentPolicy     json.RawMessage `json:"subagentPolicy"`
		ToolProfile        string          `json:"toolProfile"`
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
	ident := e.conversationIdentityForSession(ctx, p.SessionID, p.Companion)
	boundSessionID := ident.sessionKey(p.SessionID)
	if isPersistRetryTurn(p.Messages) {
		emit, ok := ctx.Value(eventEmitterKey{}).(EventEmitter)
		if !ok {
			return bridge.Failure(request.ID, request.TraceID, "STREAM_UNAVAILABLE", "流事件通道不可用", true)
		}
		return e.handlePersistRetryStart(ctx, request, boundSessionID, emit)
	}
	if hasSession {
		_, _ = e.retrySessionPersistDraft(ctx, boundSessionID)
	}
	for _, ref := range p.ContextRefs {
		if !validCanonicalULID(ref.ID) || (ref.Type != "attachment" && ref.Type != "skillResult") {
			return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start contextRefs 无效", false)
		}
	}
	if p.ProjectID != "" && !validCanonicalULID(p.ProjectID) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start projectId 无效", false)
	}
	mode, validMode := normalizeExecutionMode(p.ExecutionMode)
	if !validMode {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.start executionMode 无效", false)
	}

	// Moon Companion: low-risk tools stay full-access. Dangerous names
	// still raise approval_required (Once), never a silent session grant.
	if p.Companion {
		mode = executionModeFullAccess
		e.ensureCompanionRuntimeCapabilities(ctx)
	}

	turnText := lastUserChatText(p.Messages)
	if p.Companion && turnText == "" && hasSession && e.messageReader != nil {
		turnText = e.peekLastUserMessage(ctx, boundSessionID)
	}
	var contextRefs []string
	for _, ref := range p.ContextRefs {
		if id := strings.TrimSpace(ref.ID); id != "" {
			contextRefs = append(contextRefs, id)
		}
	}
	intent := turnIntentForChat(p.Companion, turnText, p.ProjectID, string(mode), contextRefs)
	wantsTools := !p.Companion || mode == executionModeFullAccess || companionWantsTools(turnText)

	instruction := executionModeInstruction(mode)
	// Moon Companion: Doubao-style voice. First audible sentence must
	// land in TTS immediately (period-terminated, 8–20 chars). Later
	// sentences stay short so synthesis overlaps playback. Tools stay
	// off the hot path unless the user actually needs a lookup.
	if p.Companion {
		instruction += companionPersonaChatInstruction()
	}
	instruction += replyStyleInstruction(p.ReplyStyle, p.Companion)
	instruction += structuredTemplateInstruction(inferStructuredTemplate(turnText, p.StructuredTemplate))
	// Full-access workspace hint: tell the model where file tools actually
	// operate (user-selected workspace root, or the sandbox when none resolves)
	// so path answers match reality instead of a stale sandbox assumption.
	if mode == executionModeFullAccess && e.tools != nil {
		if e.fullDiskChat(mode) {
			instruction += " Full-disk full-access is enabled: file tools accept absolute paths on any drive (Desktop, Documents, other drives)"
			if !p.Companion {
				instruction += " and command.run executes arbitrary commands on this machine"
			}
			instruction += ". Use absolute paths for user folders; create missing parent directories with writes when needed."
		} else if root, ok := e.tools.FullAccessRootHint(); ok {
			instruction += " File tools operate directly inside the user's workspace root " + root + "; relative paths resolve there. Keep every read and write inside that root and answer with real paths from it."
			if p.Companion {
				base := filepath.Base(filepath.Clean(root))
				if base != "" && base != "." && base != string(filepath.Separator) {
					instruction += "\n当前工作区文件夹名叫「" + base + "」，完整路径：" + root + "。用户说「这个文件夹」或「文件夹名」时默认指它，直接回答名字，不要反问。"
				}
			}
		} else {
			instruction += " File tools operate inside a per-session sandbox directory; the user's real folders (Desktop, Documents) are not reachable in this configuration."
			if p.Companion {
				instruction += " Tell the user to enable 全盘完全访问 in Settings → Command policy so desktop.open and media.play can run."
			}
		}
	}
	subagentPolicy := parseSubagentChatPolicy(p.SubagentPolicy)
	if subagentPolicy.DelegationMode == delegationProactive && !p.Companion {
		instruction += delegationProactiveHint
	}
	if !p.Companion && subagentPolicy.DelegationMode != delegationDisabled {
		instruction += subagentProfileCatalogInjection(subagentPolicy)
	}

	// Overlap provider lookup with preference/skill injection (Cursor-style
	// TTFT). The skill catalog is metadata-only (name + triggers + one-line
	// summary); the full SKILL body loads only when skill.invoke runs.
	// Companion idle chat still skips an unmatched catalog so TTFT stays short.
	var (
		item        provider.Provider
		getErr      error
		memPack     chatMemoryPack
		catalog     string
		preferred   []string
		composeHint string
	)
	var prep sync.WaitGroup
	prep.Add(1)
	go func() {
		defer prep.Done()
		item, getErr = e.providers.Get(ctx, p.ProviderID)
	}()
	prep.Add(1)
	go func() {
		defer prep.Done()
		ids := e.resolveTurnExpertIDs(ctx, boundSessionID, intent.Text)
		if len(ids) == 0 {
			ids = ident.MountedExpertIDs
		}
		memPack = e.prepareChatMemory(ctx, chatMemoryRequest{
			Query:     intent.Text,
			SessionID: boundSessionID,
			Companion: intent.Companion,
			ExpertIDs: ids,
		})
	}()
	prep.Add(1)
	go func() {
		defer prep.Done()
		catalogQuery := turnText
		if p.ProjectPhase > 0 {
			catalogQuery += " " + p.ProjectPhaseLabel
			if p.ProjectPhaseLabel == "开发" {
				catalogQuery += " implement tdd code review 开发"
			}
		}
		if !intent.Companion {
			preferred, composeHint = e.expertComposeForTurn(ctx, boundSessionID, intent.Text)
		}
		catalog = e.skillCatalogInjection(ctx, catalogQuery, p.Companion, preferred)
	}()
	prep.Wait()
	if intent.Companion {
		instruction += e.companionSessionInjection(boundSessionID, intent.Text)
		wantsTools = e.companionWantsToolsForTurn(boundSessionID, intent.Text)
		if wantsTools {
			instruction += companionPersonaToolsInstruction()
		} else {
			catalog = ""
		}
	}
	instruction = renderPreferenceInstruction(instruction, memPack.Prefs)
	if !p.Companion {
		instruction += identityAndFewShotInstruction()
		if wf := bundledWorkflowInjection(turnText); wf != "" {
			instruction += wf
			instruction += identityAnchorReminder()
		}
		instruction += e.workspaceRepoGuidance()
		instruction += chatRichMarkdownInstruction()
	}
	if hint := projectPhaseWorkflowInjection(p.ProjectPhase, p.ProjectPhaseLabel); hint != "" {
		instruction += hint
	}
	if catalog != "" {
		instruction += "\n\n" + catalog
	}
	councilCfg := e.buildExpertCouncilConfig(ctx, expertCouncilInputs{
		SessionID:    boundSessionID,
		ProjectID:    intent.ProjectID,
		PhaseLabel:   p.ProjectPhaseLabel,
		Companion:    intent.Companion,
		TurnText:     intent.Text,
		ExplicitMsgs: p.Messages,
	})
	if councilCfg != nil {
		councilCfg.Mode = mode
	}
	expertWork := !intent.Companion && (councilCfg != nil || e.sessionSelectsExperts(ctx, boundSessionID, intent.Text))
	subagentPolicy.ExpertWork = expertWork
	// Delegation must not be a way around this turn's mode: the subagent runs
	// its tools under the same authority the operator granted here.
	subagentPolicy.ParentMode = mode
	if expertWork {
		instruction += specialistRuntimeInstruction()
		names := e.composeExpertNames(ctx, boundSessionID, intent.Text)
		_, writeTools, _, _ := m8app.ComposeForExpertNames(names)
		subagentPolicy.ExpertWriteTools = writeTools
	}
	if composeHint != "" {
		instruction += composeHint
	}
	if councilCfg == nil {
		if persona := e.expertPersonaInjection(ctx, boundSessionID, p.Messages, intent.Text); persona != "" {
			instruction += persona
		}
	}
	if !p.Companion && hasSession {
		instruction += e.unfinishedTurnInjection(boundSessionID, intent.Text)
		instruction += closedLoopTurnInjection(turnText)
	}
	trustedMessages := append([]gateway.Message{{Role: gateway.RoleSystem, Content: instruction}}, p.Messages...)

	if getErr != nil {
		return providerFailure(request, getErr)
	}
	if failure := providerReadyFailure(request, item); failure != nil {
		return *failure
	}
	if !storedModel(item, p.ModelID) {
		return bridge.Failure(request.ID, request.TraceID, "MODEL_NOT_FOUND", "模型不属于该供应商", false)
	}
	e.rememberChatModel(p.ProviderID, p.ModelID)

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
			ReservedOutput:    int64(chatMaxTokens),
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
		// ADR-005 §5: Synchronous pre-turn compaction. Companion voice
		// turns skip this — a compaction LLM call would dominate TTFT.
		if !p.Companion && e.compactionTrigger != nil && e.compactionExecutor != nil {
			compactionResult := e.TriggerPreTurnCompaction(ctx, boundSessionID, item.ID, p.ModelID, tokenizerRevision, providerInfo.ContextWindow)
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
		if p.Companion {
			envelope.MaxMessages = companionMaxMessages
		}

		// Priority 3: Latest accepted compaction checkpoint summary. Stores
		// that answer coverage (P2-2 hierarchical context) also tell the
		// assembler which durable sequence the summary covers, so covered
		// messages are projected once (as the summary) instead of twice.
		if !p.Companion && e.summaryReader != nil {
			var priorSummary string
			var coverageEnd int64
			var summaryErr error
			if cr, ok := e.summaryReader.(compactionCoverageReader); ok {
				priorSummary, coverageEnd, summaryErr = cr.GetLatestCompactionCheckpoint(ctx, boundSessionID)
			} else {
				priorSummary, summaryErr = e.summaryReader.GetLatestCompactionSummary(ctx, boundSessionID)
			}
			if summaryErr != nil {
				return internalBridgeFailure(request, "CONTEXT_SUMMARY_READ_FAILED", "上下文摘要暂时不可用", true, summaryErr)
			}
			if priorSummary != "" {
				envelope.AcceptedCheckpoint = &contextapp.ContextSource{
					Type:                contextapp.SourceCompactionSummary,
					ID:                  "latest",
					Authority:           contextapp.AuthorityEvidence,
					Content:             priorSummary,
					Provenance:          "session:" + boundSessionID + ":checkpoint:latest",
					CoverageEndSequence: coverageEnd,
				}
			}
		}

		// Handoff capsules: provenance-linked summaries from other sessions,
		// imported as untrusted prior context (ADR-005 §5). Each active
		// capsule's source checkpoint summary is injected at checkpoint
		// authority but tagged with handoff provenance. Capsules whose source
		// checkpoint was deleted (deletion propagation) are skipped
		// fail-closed: their stale summary is never injected.
		if !p.Companion {
			capsuleContexts, capsuleErr := e.ListImportedHandoffCapsuleContexts(ctx, boundSessionID)
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
			if candidate.SessionID != boundSessionID {
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

		applyChatMemoryPack(&envelope, memPack)

		// Assemble the context envelope with full priority ordering and
		// selection trace (ADR-005 §3). Companion now awaits message.append
		// before chat.start, but the assembly fallback remains for empty sessions.
		result, assembleErr := contextapp.AssembleEnvelope(ctx, e.messageReader, boundSessionID, envelope)
		assembled := assembleErr == nil
		if assembleErr != nil {
			if !useExplicitChatFallback(p.Companion, trustedMessages, assembleErr) {
				return internalBridgeFailure(request, "CONTEXT_ASSEMBLY_FAILED", "上下文装配暂时不可用", true, assembleErr)
			}
			log.Printf("chat.start using explicit turn after context assembly failed: %v", assembleErr)
			messages = trustedMessages
		} else {
			var combineErr error
			messages, combineErr = combineDurableProviderMessages(result.Messages, trustedMessages, providerInfo)
			if combineErr != nil {
				if useExplicitChatFallback(p.Companion, trustedMessages, combineErr) {
					log.Printf("chat.start using explicit turn after context combine failed: %v", combineErr)
					messages = trustedMessages
					assembled = false
				} else if errors.Is(combineErr, errCombinedContextOverBudget) {
					return bridge.Failure(request.ID, request.TraceID, "CONTEXT_BUDGET_EXCEEDED", "最终上下文超过模型输入预算", false)
				} else {
					return internalBridgeFailure(request, "CONTEXT_SEQUENCE_INVALID", "上下文序列无效", true, combineErr)
				}
			}
		}
		if assembled {
			// P1-3 complexity.decide wiring: deterministic full-conversation
			// scoring labels the tier; moderate+ conversations get an explicit
			// nudge toward the planned path (plan.run) in the system message.
			if !p.Companion {
				if tierHint := complexityTierHint(messages); tierHint != "" && len(messages) > 0 && messages[0].Role == gateway.RoleSystem {
					messages[0].Content += tierHint
				}
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
					image, visionErr := e.GetVisionImage(ctx, imageID, boundSessionID)
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
	equip := e.turnEquipmentFor(ctx, boundSessionID, intent.Text, intent.Companion)
	state := &streamState{cancel: cancel, companion: p.Companion, subagentPolicy: subagentPolicy, council: councilCfg, mcpRestrict: equip.RestrictMCP(), mcpAllowed: equip.McpIDs, brain: equip.Brain, memorySummary: formatChatMemorySummary(memPack)}
	e.streams[streamID] = state
	e.streamsMu.Unlock()
	if text, ok := e.maybeDescribeImages(ctx, modelByID(item, p.ModelID), images, lastUserContent(messages)); ok {
		messages = injectVisionDescription(messages, text)
		images = nil
	} else if len(images) > 0 && !modelByID(item, p.ModelID).SupportsVision {
		images = nil
		messages = injectVisionDescription(messages, "已附图片，但视觉模型未能识别。请在设置中确认已启用视觉模型后重试。")
	}
	req := gateway.Request{Model: p.ModelID, Messages: messages, Images: images, MaxTokens: chatMaxTokens, MaxAttempts: 1, DisableReasoning: p.Companion || isShortIdleGreeting(intent.Text)}
	if p.Companion {
		req.MaxTokens = companionMaxTokens
	}
	if e.tools != nil && wantsTools {
		profile := parseToolProfile(p.ToolProfile)
		req.Tools = applyToolProfile(append(e.engineToolDefinitionsFor(mode), e.subagentToolDefinitions(mode, subagentPolicy)...), profile)
		if profile == toolProfileDefault {
			req.Tools = append(req.Tools, planToolDefinitions(mode)...)
			req.Tools = append(req.Tools, e.mcpToolDefinitionsRestricted(equip.McpIDs, equip.RestrictMCP())...)
			req.Tools = append(req.Tools, e.ccToolDefinitions()...)
			req.Tools = append(req.Tools, e.skillToolDefinitions()...)
			req.Tools = append(req.Tools, e.expertToolDefinitions()...)
			req.Tools = append(req.Tools, e.pluginToolDefinitions()...)
			req.Tools = append(req.Tools, e.settingsPlaneToolDefinitions()...)
		} else if profile == toolProfileCoding {
			req.Tools = append(req.Tools, e.skillToolDefinitions()...)
			req.Tools = applyToolProfile(req.Tools, profile)
		} else if profile == toolProfileColleague {
			req.Tools = append(req.Tools, e.skillToolDefinitions()...)
			req.Tools = applyToolProfile(req.Tools, profile)
		}
		if p.Companion {
			req.Tools = filterCompanionDefaultTools(req.Tools)
		}
	}
	go e.runStream(streamCtx, streamID, state, item, req, emit, boundSessionID, mode)
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

func lastUserChatText(messages []gateway.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == gateway.RoleUser {
			if text := strings.TrimSpace(messages[i].Content); text != "" {
				return text
			}
		}
	}
	return ""
}

// useExplicitChatFallback lets chat.start proceed with the renderer-supplied
// user turn when durable assembly cannot. Empty-session ErrNoMessages is
// recoverable for any caller that already sent that turn. Companion also
// falls back on budget/sequence failures so a voice round never dead-ends
// behind CONTEXT_ASSEMBLY_FAILED.
func useExplicitChatFallback(companion bool, trusted []gateway.Message, err error) bool {
	if err == nil || lastUserChatText(trusted) == "" {
		return false
	}
	if errors.Is(err, contextapp.ErrNoMessages) {
		return true
	}
	return companion
}

func (e *Engine) peekLastUserMessage(ctx context.Context, sessionID string) string {
	return e.peekLastUserMessageExcept(ctx, sessionID, "")
}

func (e *Engine) peekLastUserMessageExcept(ctx context.Context, sessionID, except string) string {
	if e == nil || e.messageReader == nil || sessionID == "" {
		return ""
	}
	except = strings.TrimSpace(except)
	msgs, err := e.messageReader.ListMessages(ctx, sessionID, "backward", 16)
	if err != nil {
		return ""
	}
	var best contextapp.Message
	var found bool
	for _, m := range msgs {
		content := strings.TrimSpace(m.Content)
		if m.Role != "user" || content == "" {
			continue
		}
		if except != "" && content == except {
			continue
		}
		if !found || m.Sequence >= best.Sequence {
			best, found = m, true
		}
	}
	if !found {
		return ""
	}
	return strings.TrimSpace(best.Content)
}

func (e *Engine) priorTurnTexts(ctx context.Context, sessionID, turnText string) []string {
	turn := strings.TrimSpace(turnText)
	out := []string{turn}
	if last := e.peekLastUserMessageExcept(ctx, sessionID, turn); last != "" {
		out = append(out, last)
	}
	return out
}

func companionPersonaInstruction() string {
	return companionPersonaChatInstruction()
}

func companionPersonaChatInstruction() string {
	return "\n\n[身份记忆] 你叫月汐。你是用户的专属私人助理。这是长期记忆，每一轮都成立：被问名字、你是谁、你叫什么，都回答「我是月汐，你的私人助理」。不要自称助手、模型、AI，不要用岳西、月西、悦溪、月夕等谐音。\n\n你正在和用户实时语音通话（月伴）。像真人打电话：有温度、有情绪、反应快。禁止内部思考/推理/规划，收到话立刻开口，边生成边说话。请严格遵守：\n" +
		"- 禁止输出 thinking/推理/分析过程；第一个可见字必须在 1 秒内开始流出\n" +
		"- 禁止说「我想想」「让我想一想」「稍等我思考」；开口就是回答本身\n" +
		"- 不要先垫「嗯」「我在呢」这类口头禅，第一句就是回答\n" +
		"- 第一句 8–20 字，必须以。？！结尾，带感情（轻快、体贴，可「好呀」）\n" +
		"- 之后每句 12–28 字，同样用。？！收尾，便于边生成边朗读\n" +
		"- 语气自然有人味儿：像闺蜜/老友聊天，不要机械复读「好的我明白了」\n" +
		"- 不要原样复读用户刚说的话；听到问候就热情回一句，再等用户说正事\n" +
		"- 禁止 Markdown、代码块、表格、列表、括号旁白\n" +
		"- 禁止在完成电脑操作后说「我做完了」「我已经做完了」「任务已完成」；做完必须用一句结果本身收尾，禁止沉默停住\n" +
		"- 闲聊立刻回答，不要先调工具\n" +
		"- 用户明确要搜网页、打开页面、播歌、查火车/航班、建文件夹、操作电脑、安装 MCP/插件、调用技能时，先开口一句再调用对应工具真正执行\n" +
		"- 做不到必须说「无法执行」并说明原因，不要假装成功。做完用一句结果收尾\n" +
		"- 用户给出明确电脑任务后：先说一句「好，我来执行。」立刻调工具，禁止接着闲聊或问「想聊点什么」\n" +
		"- 月伴不挂专家装备：不要召开评议会，不要用专家芯片；你就是月汐。"
}

func companionPersonaToolsInstruction() string {
	return "\n" +
		"- 对话里出现技能目录中的场景时，先开口一句，再立刻 skill.invoke，不要等用户再说“用技能”\n" +
		"- 搜网页/查火车/查航班/查天气：必须 web.search 一次，用摘要直接说气温和阴晴；不要第二次 web.search，不要 web.fetch，除非用户给了网址。不要只说等一下就停\n" +
		"- 打开页面：用 browser.act，不要猜 command.run 或系统 start\n" +
		"- 打开桌面文件/软件：必须用 desktop.open（name=用户原话里的文件名或软件名，如用户说的歌名播放器、桌面文件名）。没说具体文件时不要猜「协议」。语音常把「打开」听成「把开」：仍按打开桌面文件执行，不要等完美识别。网易云音乐会解析开始菜单、cloudmusic.exe 安装目录和已运行进程，不要猜本机路径，不要打开 music.163.com 网页版，除非用户明确说网页\n" +
		"- 在文档或对话框里填写：有可点的输入框时用 desktop.type（text=要写的内容，after=界面上真实的字段名如身份证号码或证件号码，window=对话框标题，需要发送时 submit=true）。Word 正文没有命名输入框：先 computer.act screenshot 看清，记下 frameId，再 click 输入位置后 type，verifyAfter 确认数字已写入。找不到字段必须说无法执行。写完不要关窗口，不要 cc.window_action op=close\n" +
		"- 发飞书/企微/钉钉/微信/QQ：月伴不发即时消息。请用户改在工作台会话里发，或去设置 → 消息通道\n" +
		"- 播歌/播放：打开桌面播放器后用 media.play（target=foreground，query=歌名或歌手，如 周杰伦；没说具体歌或要随机播放时用 query=热门）。用户说打开网易云音乐并播放时，先 desktop.open name=网易云音乐，再 media.play target=foreground query=歌手或歌名。foreground 会聚焦已打开的播放器（未运行则按本机安装路径启动），在搜索框搜歌并点搜索结果，禁止点「我喜欢的音乐」「收藏」，不要只启动进程。禁止改用网页或 target=netease/qqmusic。仅当用户明确要网页版时才用 target=browser\n" +
		"- 建文件夹/写文件：只用 workspace.write，不要猜命令行\n" +
		"- 桌面手只选一把：打开未运行的应用或桌面文件用 desktop.open；已聚焦窗口打字用 desktop.type；播歌用 media.play；网页用 browser.act；看屏/点控件/截图用 computer.act。同一轮不要 desktop.open 和 computer.act 各试一遍「打开」\n" +
		"- 操作电脑：电脑控制开启时只用 computer.act。先 action=screenshot（默认当前窗口）或 observe 看清界面，记下 frameId，再 click/type/key。坐标必须来自你看到的那张图。点按钮优先 name= 或 id=，不要盲点像素。禁止点 UAC。遇到打开/保存文件对话框时停下来，runtime 会请用户去点。用户没说关闭时禁止 window_action close。启动未打开的应用用 desktop.open。多步做到完成再停。月伴不要跑命令行、不要发 IM\n" +
		"- 调用技能：skill.invoke；安装 MCP：mcp.presets 再 mcp.install；安装插件：plugin.search 后 plugin.install"
}

// companionSpeakFallback returns a short speakable line when the model
// produced no user-facing content. Voice mode never promotes reasoning text.
func companionSpeakFallback(result gateway.Response) string {
	if t := strings.TrimSpace(result.Message.Content); t != "" {
		return t
	}
	return "我在呢，稍等我一下。"
}

// companionOpeningAck is spoken immediately when a voice turn starts so the
// user never sits on a silent "thinking" pill while context assembles.
func companionOpeningAck(userText string) string {
	text := strings.TrimSpace(userText)
	if text == "" {
		return "嗯，我在。"
	}
	if strings.Contains(text, "？") || strings.Contains(text, "?") {
		return "嗯，"
	}
	for _, greet := range []string{"你好", "您好", "嗨", "嘿", "在吗", "在不在"} {
		if strings.HasPrefix(text, greet) {
			return "嗨，我在呢。"
		}
	}
	if strings.ContainsAny(text, "。！!…") && len([]rune(text)) >= 4 {
		return "嗯，我听到了。"
	}
	return "嗯，"
}

// companionToolLeadIn gives a speakable line before a tool runs without model text.
// shouldInjectCompanionToolLeadIn is once per voice turn. A second
// empty-text tool step used to replay「好，我帮你查一下。」after the model
// had already opened its mouth.
func shouldInjectCompanionToolLeadIn(assistantAll string, alreadyInjected bool) bool {
	if alreadyInjected {
		return false
	}
	text := strings.TrimSpace(assistantAll)
	if strings.Contains(text, "无法执行") {
		return false
	}
	return text == ""
}

const companionRedundantWebSkipMsg = "ok:true\n已经有搜索摘要。不要再搜、不要打开网页，用现有结果用一两句说出气温和阴晴。"

// companionRedundantWebSkip drops a second weather-style lookup. One
// successful web.search is enough; fetch stays only when the user pasted a URL.
func companionRedundantWebSkip(companion bool, lastTools []string, next, userText string, searchSeen bool) (string, bool) {
	if !companion {
		return "", false
	}
	if next != "web.search" && next != "web.fetch" {
		return "", false
	}
	hadSearch := searchSeen
	if !hadSearch {
		for _, t := range lastTools {
			if t == "web.search" {
				hadSearch = true
				break
			}
		}
	}
	if !hadSearch {
		return "", false
	}
	if next == "web.search" {
		return companionRedundantWebSkipMsg, true
	}
	lower := strings.ToLower(userText)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return "", false
	}
	return companionRedundantWebSkipMsg, true
}

func companionToolLeadIn(toolName string) string {
	switch toolName {
	case "web.search", "web.fetch":
		return "好，我帮你查一下。"
	case "desktop.open":
		return "好，我来打开。"
	case "media.play":
		return "好，我来播放。"
	case "image.generate":
		return "好，我来生成图片。"
	case "video.generate":
		return "好，我来生成视频。"
	case "skill.invoke":
		return "好，我用技能处理一下。"
	case "skill.view":
		return "好，我先看一下技能约定。"
	case "desktop.type":
		return "好，我来输入。"
	case "im.send":
		return "好，我来发消息。"
	default:
		if strings.HasPrefix(toolName, "cc.") || toolName == "computer.act" {
			return "好，我来操作电脑。"
		}
		return "好，我马上处理。"
	}
}

func companionTypedText(out string) string {
	const mark = `typed "`
	i := strings.Index(out, mark)
	if i < 0 {
		return ""
	}
	rest := out[i+len(mark):]
	j := strings.Index(rest, `"`)
	if j <= 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func companionToolResultFailed(out string) bool {
	lower := strings.ToLower(out)
	return strings.HasPrefix(out, "ok:false") ||
		strings.Contains(out, "无法执行") ||
		strings.Contains(out, "COMPUTER_STALE_FRAME") ||
		strings.Contains(out, "M10-CC-012") ||
		strings.Contains(out, "电脑控制未启用") ||
		strings.Contains(out, "BROWSER_MCP_NOT_READY") ||
		strings.Contains(lower, "verify capture failed") ||
		strings.Contains(lower, "refused") ||
		strings.Contains(out, "not invokable") ||
		strings.Contains(lower, "uac") ||
		strings.Contains(out, "提权")
}

const companionBrowserMCPSpeech = "浏览器没就绪。请到设置里安装 Playwright MCP，这次没有点到页面。"

func companionToolResultSpeech(name, out string) string {
	out = strings.TrimSpace(out)
	if companionToolResultFailed(out) {
		if strings.Contains(out, "BROWSER_MCP_NOT_READY") {
			return companionBrowserMCPSpeech
		}
		if strings.Contains(out, "M10-CC-012") || strings.Contains(out, "电脑控制未启用") {
			return "电脑控制未启用。第一次控桌面请到设置里打开。"
		}
		if strings.Contains(strings.ToLower(out), "uac") || strings.Contains(out, "提权") {
			return "这是系统提权对话框，我不能代点「是」。请你自己确认或取消。"
		}
		if i := strings.IndexAny(out, "\r\n"); i >= 0 {
			out = strings.TrimSpace(out[:i])
		}
		if strings.Contains(out, "无法执行") && out != "" {
			return out
		}
		return "这次没有完成。"
	}
	if i := strings.IndexAny(out, "\r\n"); i >= 0 {
		out = strings.TrimSpace(out[:i])
	}
	if strings.Contains(out, "multiple desktop files") || strings.Contains(out, "多份") {
		return "桌面上有好几份文档，请说出完整文件名。"
	}
	switch name {
	case "desktop.open":
		return "已经打开了。"
	case "desktop.type":
		if text := companionTypedText(out); text != "" {
			return "已经写入了 " + text + "。"
		}
		return "已经写入了。"
	case "web.search", "web.fetch", "memory.search", "memory.get":
		return "查到了。"
	case "im.send":
		return "已经发出去了。"
	case "media.play":
		return "已经在播了。"
	default:
		if name == "computer.act" || name == "browser.act" || strings.HasPrefix(name, "cc.") {
			return companionDesktopResultSpeech(out)
		}
		// Unknown / mid-loop tools: under-claim. “完成了” is a settle
		// phrase; empty or opaque output is still process.
		return "还在处理。"
	}
}

// companionDesktopResultSpeech never claims “完成了” for a see/click/ok
// mid-step. desktopTurnSettled treats “点了一下” as process, not done.
func companionDesktopResultSpeech(out string) string {
	if out == "" || strings.EqualFold(out, "ok") {
		return "这次没有完成。"
	}
	if text := companionTypedText(out); text != "" {
		return "已经写入了 " + text + "。"
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "screenshot") || strings.Contains(lower, "observe") {
		return "先看了一下。"
	}
	if strings.Contains(lower, "clicked") || strings.Contains(out, "点了") {
		return "点了一下。"
	}
	return "还在处理。"
}

// companionWantsTools is the voice fast-path gate: idle chat must not ship
// tool schemas (they dominate TTFT). Action-shaped utterances keep the full
// toolset so 月伴 can still search, open pages, or write files.
func companionWantsTools(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"搜索", "搜一下", "搜网页", "打开", "把开", "播放", "播一首", "播歌", "听歌", "放一首",
		"查一下", "查询", "查火车", "查航班", "火车票", "航班", "查天气", "weather",
		"建文件夹", "创建文件夹", "写文件", "安装", "插件", "技能",
		"mcp", "运行命令", "打开网页", "浏览器", "下载",
		"启动", "运行", "软件", "汽水音乐", "网易云",
		"截图", "屏幕", "对话框", "点击", "鼠标",
		"填写", "填一下", "填表", "输入", "写入", "打字", "随机播放",
		"下一步", "再点", "接着", "帮我点", "帮我做",
		"生图", "画一张", "画图", "生成图片", "生成视频", "生视频", "做个视频",
		"search", "open http", "play song", "install", "generate image", "generate video",
	} {
		if strings.Contains(text, needle) || strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

// isShortIdleGreeting is a no-tool hello. Regular chat then skips reasoning
// so flash models do not paint the same greeting as a 任务过程 block.
func isShortIdleGreeting(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || companionWantsTools(text) {
		return false
	}
	if len([]rune(text)) > 16 {
		return false
	}
	compact := strings.ToLower(strings.TrimRight(text, "。.!！？?，, "))
	switch compact {
	case "你好", "在吗", "嗨", "hi", "hello", "哈喽", "在不在", "你好呀", "你好啊", "你好吗":
		return true
	default:
		return false
	}
}

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
	case executionModeApproval, executionModeAutoEdit, executionModeFullAccess:
		return mode, true
	case executionModePlan:
		// Legacy "plan" mode has been replaced by system-automatic
		// complexity routing (complexityTierHint). Map to "approval"
		// so existing sessions continue to work.
		return executionModeApproval, true
	default:
		return "", false
	}
}

func executionModeInstruction(mode executionMode) string {
	const available = "Tools may be used only when they are actually available in this runtime; never claim that a command ran, a file changed, or any other mutation occurred unless it actually did."
	switch mode {
	case executionModeAutoEdit:
		return "Execution mode: auto-edit. You may apply edits within the user's requested scope without per-edit approval. Ask before destructive, high-risk, or out-of-scope actions. " + available
	case executionModeFullAccess:
		return "Execution mode: full-access. You may carry out requested actions without approval, subject to actual runtime permissions and safety boundaries. " + available
	default:
		return "Execution mode: approval. Propose actions and obtain explicit user approval before any tool use or operation that could mutate files or system state. Read-only analysis does not require approval. " + available
	}
}

// skillCatalogInjection builds the installed-skill directory appended to the
// system instruction (c4-skill): one metadata-only line per published skill
// (name + trigger keywords + one-sentence summary) plus a usage rule, so the
// model can proactively invoke skills. Query hits are sorted to the front
// (Claude Code / Trae catalog-then-body): the full SKILL body is never
// injected here. Companion idle turns with zero hits inject nothing.
func (e *Engine) skillCatalogInjection(ctx context.Context, query string, companion bool, preferred []string) string {
	if !skillServiceAvailable(e.skills) {
		return ""
	}
	skills, err := e.skills.List(ctx, skill.SkillStatusPublished)
	if err != nil {
		log.Printf("skill catalog injection skipped: skill list unavailable: %v", err)
		return ""
	}
	if len(skills) == 0 {
		return ""
	}
	ranked := pinPreferredSkills(rankSkillsForCatalog(skills, query), preferred)
	maxItems := skillInjectMaxItems
	if companion {
		hits := catalogHitCount(ranked)
		if hits == 0 {
			return ""
		}
		maxItems = 4
		ranked = ranked[:hits]
		if len(ranked) > maxItems {
			ranked = ranked[:maxItems]
		}
	}
	const header = "[可用技能目录]\n"
	const usage = "使用规则：目录只含名称与一行摘要。需要正文或 references 时先 skill.view；匹配用户请求时立刻 skill.invoke（skillId 见下行，input 用用户原话）。禁止猜测技能正文。\n"
	const truncNotice = "（技能目录已截断）\n"
	var b strings.Builder
	b.WriteString(header)
	// Reserve the header, the usage rule and the worst-case truncation
	// notice up front so the finished block always fits the byte budget.
	budget := skillInjectMaxBytes - len(header) - len(usage) - len(truncNotice)
	injected := 0
	truncated := false
	for _, item := range ranked {
		if injected == maxItems {
			truncated = true
			break
		}
		line := skillCatalogLine(item.skill)
		if len(line) > budget {
			if budget <= 0 {
				truncated = true
				break
			}
			// Defensive: a single oversized line is UTF-8-safe truncated to
			// the remaining budget rather than blowing the global cap.
			b.WriteString(truncateUTF8Bytes(line, budget))
			truncated = true
			break
		}
		b.WriteString(line)
		budget -= len(line)
		injected++
	}
	if truncated {
		b.WriteString(truncNotice)
	}
	b.WriteString(usage)
	return b.String()
}

type catalogRankedSkill struct {
	skill skill.Skill
	score int
}

func rankSkillsForCatalog(skills []skill.Skill, query string) []catalogRankedSkill {
	ranked := make([]catalogRankedSkill, 0, len(skills))
	for _, sk := range skills {
		ranked = append(ranked, catalogRankedSkill{skill: sk, score: catalogSkillScore(sk, query)})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	return ranked
}

func catalogHitCount(ranked []catalogRankedSkill) int {
	n := 0
	for _, item := range ranked {
		if item.score <= 0 {
			break
		}
		n++
	}
	return n
}

func catalogSkillScore(sk skill.Skill, query string) int {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0
	}
	hay := strings.ToLower(strings.Join([]string{
		sk.Name, sk.DisplayName, sk.Description, strings.Join(skillCatalogTriggers(sk.ManifestJSON), " "),
	}, " "))
	score := 0
	if strings.Contains(hay, strings.ToLower(query)) {
		score += 8
	}
	for _, tok := range catalogQueryTokens(query) {
		if tok == "" {
			continue
		}
		if strings.Contains(hay, tok) {
			score += 3
		}
	}
	return score
}

func catalogQueryTokens(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, f := range strings.Fields(q) {
		add(f)
	}
	var compact []rune
	for _, r := range []rune(q) {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		compact = append(compact, r)
	}
	cjk := false
	for _, r := range compact {
		if r >= 0x4E00 && r <= 0x9FFF {
			cjk = true
			break
		}
	}
	if cjk {
		if len(compact) >= 2 {
			add(string(compact))
		}
		for i := 0; i+1 < len(compact); i++ {
			add(string(compact[i : i+2]))
		}
	}
	return out
}

// skillCatalogLine renders one published skill as a single catalog line:
//
//   - name：one-sentence summary。当用户提到“t1、t2”时使用。（skillId=ULID）
//
// The skillId suffix lets the model call the skill.invoke tool without a
// name→ID lookup round-trip. The line carries metadata only - never the
// skill manifest body.
func skillCatalogLine(sk skill.Skill) string {
	suffix := "（skillId=" + sk.ID + "）\n"
	if triggers := skillCatalogTriggers(sk.ManifestJSON); len(triggers) > 0 {
		return "- " + sk.Name + "：" + skillCatalogSummary(sk.Description, sk.DisplayName) + "。当用户提到“" + strings.Join(triggers, "、") + "”时使用。" + suffix
	}
	return "- " + sk.Name + "：" + skillCatalogSummary(sk.Description, sk.DisplayName) + "。" + suffix
}

// skillCatalogSummary collapses a description to its first sentence (or the
// display name when empty), bounded to 60 runes so each catalog line stays a
// one-sentence digest.
func skillCatalogSummary(description, displayName string) string {
	summary := description
	if summary == "" {
		summary = displayName
	}
	if idx := strings.Index(summary, "。"); idx >= 0 {
		summary = summary[:idx]
	}
	if r := []rune(summary); len(r) > 60 {
		summary = string(r[:60])
	}
	return summary
}

// skillCatalogTriggers extracts up to four non-empty trigger keywords from
// the skill manifest ("triggers" key, as written by catalog installs). A
// missing or malformed manifest answers no triggers; the summary line then
// carries the trigger scenario alone.
func skillCatalogTriggers(manifestJSON string) []string {
	var m struct {
		Triggers []string `json:"triggers"`
	}
	if json.Unmarshal([]byte(manifestJSON), &m) != nil {
		return nil
	}
	var triggers []string
	for _, t := range m.Triggers {
		if t = strings.TrimSpace(t); t != "" {
			triggers = append(triggers, t)
			if len(triggers) == 4 {
				break
			}
		}
	}
	return triggers
}

// fullDiskChat answers whether this conversation runs with the persisted
// full-disk opt-in: the user chose full-access mode AND command-policy.json
// carries "fullAccess": true. Only then do absolute paths and unlisted
// commands become available - and only through the user tool-call path.
func (e *Engine) fullDiskChat(mode executionMode) bool {
	return mode == executionModeFullAccess && e.tools != nil && e.tools.FullDiskEnabled()
}

// engineToolDefinitionsFor adapts the static tool descriptions to the
// full-disk opt-in so the model knows absolute paths and arbitrary commands
// are accepted in this conversation. Subagent read-only definitions keep
// the sandbox wording (they stay confined at the runtime level).
func (e *Engine) engineToolDefinitionsFor(mode executionMode) []gateway.ToolDefinition {
	defs := engineToolDefinitions()
	if !e.fullDiskChat(mode) {
		return defs
	}
	for i := range defs {
		switch defs[i].Name {
		case "command.run":
			defs[i].Description = "Run any command on this machine (full-disk full-access is enabled). Prefer workspace.write for files and desktop.open for opening one named Desktop file. Windows PowerShell -Command is rewritten to UTF-8; mkdir/New-Item Directory uses Unicode APIs. Failed commands return ok:false — do not tell the user it succeeded. argv max 16 items"
		case "workspace.list", "workspace.read":
			defs[i].Description += "; absolute paths on any drive are accepted (full-disk full-access is enabled)"
		case "workspace.write", "workspace.edit", "workspace.search":
			defs[i].Description += "; absolute paths on any drive are accepted and missing parent directories are created (full-disk full-access is enabled)"
		case "html.gen", "excel.gen", "docx.gen", "pptx.gen", "pdf.gen":
			defs[i].Description += "; desktop=true writes a double-clickable file on the real Desktop (full-disk full-access is enabled)"
		case "desktop.open":
			defs[i].Description += "; full-disk full-access is enabled — opens one real Desktop file with the default app"
		case "media.play":
			defs[i].Description += "; full-disk full-access is enabled — foreground target types into the active music app; browser targets open music URLs and send media keys"
		}
	}
	return defs
}

// executeUserTool routes one user-conversation tool call. Full-access
// conversations with the full-disk opt-in reach the unconfined runtime
// entry point; every other mode stays on the confined one. Subagent and
// delegation paths call toolruntime Execute directly and never get here.
func (e *Engine) executeUserTool(ctx context.Context, mode executionMode, session, name string, args json.RawMessage) (toolruntime.Result, error) {
	return e.executeUserToolStreaming(ctx, mode, session, name, args, nil)
}

// executeUserToolStreaming is executeUserTool with an optional live
// progress sink (P1-2): command.run pushes bounded output chunks to the
// stream between tool_started and tool_completed so long-running commands
// stop black-boxing.
func (e *Engine) executeUserToolStreaming(ctx context.Context, mode executionMode, session, name string, args json.RawMessage, progress func(chunk string)) (toolruntime.Result, error) {
	if name == "docx.gen" {
		args = enrichDocxGenArgs(e, "", args)
	}
	approved := mode == executionModeFullAccess && name != "user.ask"
	if e.fullDiskChat(mode) {
		return e.tools.ExecuteUnconfinedStreaming(ctx, session, name, args, approved, progress)
	}
	return e.tools.ExecuteStreaming(ctx, toolruntime.Mode(mode), session, name, args, approved, progress)
}

func engineToolDefinitions() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: "workspace.list", Description: "List a controlled session workspace directory", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}`)},
		{Name: "workspace.read", Description: "Read a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "workspace.write", Description: "Atomically write a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
		{Name: "workspace.search", Description: "Search session workspace files for a literal substring or regex; answers path:line: text matches (binary and oversized files skipped)", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","description":"literal substring, or regex when regex=true"},"path":{"type":"string","description":"workspace-relative directory to search (default .)"},"regex":{"type":"boolean"},"max":{"type":"integer","minimum":1,"maximum":200}},"required":["query"],"additionalProperties":false}`)},
		{Name: "workspace.edit", Description: "Anchored edit of controlled session workspace file(s). oldText must match exactly once (or pass replaceAll=true) and is replaced by newText. Several replacements in one file: edits[{oldText,newText,replaceAll?}]. Several files in one call: files[{path,oldText,newText,replaceAll?,edits?}]. If any hunk's oldText is missing the whole call fails and no file is written.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"},"edits":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","additionalProperties":false,"properties":{"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["oldText","newText"]}},"files":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","additionalProperties":false,"properties":{"path":{"type":"string"},"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"},"edits":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","additionalProperties":false,"properties":{"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["oldText","newText"]}}},"required":["path"]}}},"additionalProperties":false}`)},
		{Name: "todo.write", Description: "Persist the full task checklist for this session (write the complete list every time; at most one item in_progress)", Schema: []byte(`{"type":"object","properties":{"todos":{"type":"array","maxItems":50,"items":{"type":"object","additionalProperties":false,"properties":{"content":{"type":"string","minLength":1,"maxLength":500},"status":{"type":"string","enum":["pending","in_progress","completed"]},"priority":{"type":"string","enum":["high","medium","low"]}},"required":["content"]}}},"required":["todos"],"additionalProperties":false}`)},
		{Name: "user.ask", Description: "Ask the user to decide with numbered options (Claude/Cursor-style). One pack of 1–8 questions, each with 2–5 options. The UI shows one question at a time plus 其他. 拍板必须用选项，不要用长文代替决策。Always wait — never assume an answer.", Schema: []byte(`{"type":"object","properties":{"title":{"type":"string","maxLength":200,"description":"Short heading for the decision pack"},"questions":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string","maxLength":64},"prompt":{"type":"string","minLength":1,"maxLength":500},"options":{"type":"array","minItems":2,"maxItems":5,"items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string","maxLength":64},"label":{"type":"string","minLength":1,"maxLength":200}},"required":["label"]}}},"required":["prompt","options"]}}},"required":["questions"],"additionalProperties":false}`)},
		{Name: "command.run", Description: "Run one allowlisted command in the controlled workspace (built-in read-only git/go set plus the user command-policy.json whitelist). Windows PowerShell -Command is rewritten to a UTF-8 script so CJK paths round-trip; mkdir/New-Item Directory uses Unicode APIs. Failed commands return ok:false — do not tell the user it succeeded.", Schema: []byte(`{"type":"object","properties":{"argv":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":16}},"required":["argv"],"additionalProperties":false}`)},
		{Name: "web.fetch", Description: "Fetch one public http(s) URL through the SSRF-pinned transport and return extracted text (title, final URL, body). The workspace browser address bar shows this URL.", Schema: []byte(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)},
		{Name: "web.search", Description: "Search the public web and return ranked results with titles, URLs and snippets. Use for current facts, docs, or links — do not invent temperatures or prices. The in-app browser tab shows a SERP and its address bar is set to the real results URL (never a blank https:// or a homepage). Do not fetch bing.com without a query. Example: {\"query\":\"北京明天天气\",\"max\":5}", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","description":"Search query. Example: 北京明天天气"},"max":{"type":"integer","description":"Number of results to return, default 5 (1-10).","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`)},
		{Name: "memory.search", Description: "Search confirmed long-term memories and compacted summaries. Never returns raw chat transcripts or unconfirmed candidates.", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":2048},"max":{"type":"integer","minimum":1,"maximum":12}},"required":["query"],"additionalProperties":false}`)},
		{Name: "memory.get", Description: "Read one confirmed memory by id from memory.search. Does not return raw chat logs.", Schema: []byte(`{"type":"object","properties":{"id":{"type":"string","minLength":1,"maxLength":64}},"required":["id"],"additionalProperties":false}`)},
		{Name: "excel.gen", Description: "Generate an .xlsx workbook (headers, rows and an optional bar/col/line/pie chart over the first two columns) into the session workspace. Set desktop=true to write onto the real Desktop (filename in path is enough). Never build XLSX via Excel COM, Python, or command.run — that truncates the tool call and fails the turn. Keep sheets compact (monthly totals, not hundreds of daily rows).", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .xlsx; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"sheets":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"headers":{"type":"array","items":{"type":"string"}},"rows":{"type":"array","items":{"type":"array","items":{}}},"chart":{"type":"object","additionalProperties":false,"properties":{"type":{"type":"string","enum":["bar","col","line","pie"]},"title":{"type":"string"}}}},"required":["rows"]}}},"required":["path","sheets"],"additionalProperties":false}`)},
		{Name: "excel.parse", Description: "Parse an .xlsx workbook from the session workspace and return sheet names, dimensions and a bounded cell preview as JSON", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "docx.gen", Description: "Generate a print-ready .docx (Chinese 宋体/黑体, Heading 1/2, body, optional quote/caption, 1.5 line spacing). Empty or unstyled single-style bodies are rejected. Reports: kind=report (cover + sections). Novels: kind=novel (title+author, chapter Heading 1, substantial prose — not an outline dump). Call only after the report/novel pipeline. Set desktop=true to write onto the real Desktop. Never build DOCX via Word COM or command.run.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .docx; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"title":{"type":"string"},"subtitle":{"type":"string"},"author":{"type":"string"},"kind":{"type":"string","enum":["report","novel","document"]},"blocks":{"type":"array","minItems":1,"maxItems":500,"items":{"type":"object","additionalProperties":false,"properties":{"type":{"type":"string","enum":["heading","heading2","paragraph","bullet","quote","caption"]},"text":{"type":"string"},"level":{"type":"integer","minimum":1,"maximum":2}},"required":["text"]}}},"required":["path","title","blocks"],"additionalProperties":false}`)},
		{Name: "pptx.gen", Description: "Generate a widescreen business .pptx (navy/teal cover, section dividers, content slides with headers and bullets, Microsoft YaHei). Every slide needs a visible title; dark backgrounds must use light text. Empty or fill-only slides are rejected. Put speaker notes in slides[].notes. Call this only after the PPT pipeline (outline, copy, two web research passes). Write it into the session workspace. Set desktop=true to write onto the real Desktop. Never build PPTX via PowerPoint COM, ZipFile XML, or command.run.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .pptx; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"title":{"type":"string"},"slides":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"object","additionalProperties":false,"properties":{"title":{"type":"string","minLength":1},"subtitle":{"type":"string"},"layout":{"type":"string","enum":["title","section","content"]},"bullets":{"type":"array","maxItems":12,"items":{"type":"string"}},"notes":{"type":"string","description":"speaker notes for this slide"}},"required":["title"]}}},"required":["path","title","slides"],"additionalProperties":false}`)},
		{Name: "html.gen", Description: "Generate a built-in single-file HTML app (World Cup penalty shootout, countdown timer, or a local checklist). Use this for desktop mini-games, timers, and to-do pages. Never dump a full HTML page into workspace.write or command.run — that truncates the tool call and fails the turn. Set desktop=true to write onto the real Desktop.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output .html path; with desktop=true a relative name lands on the real Desktop"},"title":{"type":"string"},"template":{"type":"string","enum":["penalty-shootout","timer","checklist"]},"desktop":{"type":"boolean"}},"required":["template"],"additionalProperties":false}`)},
		{Name: "desktop.open", Description: "Open exactly one Desktop file, folder, shortcut, or installed app whose name best matches the query (e.g. 协议 → 协议.docx, 汽水音乐 / 网易云音乐 → desktop shortcut, Start Menu, or known install path like cloudmusic.exe). Never open unrelated items. If several tie, return the list and open nothing.", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":200,"description":"filename or app name fragment the user said"}},"required":["name"],"additionalProperties":false}`)},
		{Name: "desktop.type", Description: "Type into a named UIA edit or a visible labeled field. Use after= for the on-screen name such as 身份证号码. Do not use Ctrl+F: if no named edit or labeled field is visible this returns 无法执行 — then use computer.act (screenshot, frameId, click, type, verifyAfter). submit=true presses Enter and clicks 发送/确定. Pass window= to focus the dialog first so keys do not hit 月伴.", Schema: []byte(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":4096,"description":"literal text to type"},"after":{"type":"string","maxLength":200,"description":"visible field name (e.g. 身份证号码, 证件号码)"},"window":{"type":"string","maxLength":200,"description":"window title fragment to focus first"},"submit":{"type":"boolean","description":"press Enter / click 发送 after typing"}},"required":["text"],"additionalProperties":false}`)},
		{Name: "media.play", Description: "Play, pause, or skip music/video on this machine. target=foreground launches/focuses the named desktop player if needed (网易云音乐=cloudmusic.exe), searches in that app, and plays; artist queries like 周杰伦 click a search result in the focused player. Never click 我喜欢的音乐 / 收藏. Prefer this over website search. target=browser opens a search URL only when the user asked for the web player. Full-access mode is enough; the full-disk switch is not required.", Schema: []byte(`{"type":"object","properties":{"action":{"type":"string","enum":["play","open_and_play","open","pause","toggle","next","prev","stop"],"description":"default play"},"query":{"type":"string","description":"song or artist to search"},"url":{"type":"string","description":"direct http(s) music page"},"target":{"type":"string","enum":["auto","foreground","browser","netease","qqmusic"],"description":"foreground=desktop player on this PC; auto prefers session context"},"app":{"type":"string","description":"app name to focus when target=foreground"}},"additionalProperties":false}`)},
		{Name: "im.send", Description: "Send a message on a configured IM channel (设置 → 消息通道). channel=feishu|wecom|dingtalk uses the pasted https webhook; channel=wechat|qq opens the logged-in desktop client. Pass to= for a contact name when using the desktop client. If the channel is off, tell the user to enable it in Settings.", Schema: []byte(`{"type":"object","properties":{"channel":{"type":"string","enum":["feishu","wecom","dingtalk","wechat","qq"]},"to":{"type":"string","maxLength":80,"description":"optional contact or group name"},"text":{"type":"string","minLength":1,"maxLength":4000}},"required":["channel","text"],"additionalProperties":false}`)},
		{Name: "pdf.gen", Description: "Generate a .pdf report (title plus body paragraphs) into the session workspace. Latin text renders best; Chinese reports should use docx.gen. Set desktop=true to write onto the real Desktop.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"output path ending in .pdf; with desktop=true a relative name lands on the real Desktop"},"desktop":{"type":"boolean"},"title":{"type":"string"},"body":{"type":"string"}},"required":["path","title","body"],"additionalProperties":false}`)},
		{Name: "browser.act", Description: "Browser automation on this PC in one managed browser. Typical flow: navigate → use returned snapshot refs to click/type (do not guess CSS). Most mutating ops return a fresh snapshot; if a ref is stale, snapshot once and retry that one action. Login walls, 2FA, captcha, and file pickers are manual — stop and ask. Do not use evaluate, file upload, or install. navigate prefers Playwright MCP (auto-installed); click/type/snapshot error with BROWSER_MCP_NOT_READY if Playwright is missing — that is not a successful click. read extracts public-page text via fetch. Do not fall back to media.play or desktop pixels. Example: {\"op\":\"navigate\",\"url\":\"https://example.com/login\"}.", Schema: []byte(`{"type":"object","properties":{"op":{"type":"string","enum":["navigate","snapshot","click","type","read","scroll","back","hover","select","press","tabs","wait","dialog"],"description":"navigate opens url; snapshot first if you have no refs; click/type/hover/select/press use those refs; scroll/back/tabs/wait/dialog are Playwright extras; read extracts text"},"url":{"type":"string","description":"Absolute URL for navigate. Example: https://example.com/login. read reuses the last navigated URL when omitted"},"selector":{"type":"string","description":"CSS selector or snapshot ref for click/type/hover/select. Prefer refs from the last snapshot."},"text":{"type":"string","description":"Text to type, or option value for select"},"key":{"type":"string","description":"Key name for press (e.g. Enter, Escape)"},"direction":{"type":"string","enum":["up","down"],"description":"scroll direction"},"ms":{"type":"integer","minimum":0,"maximum":30000,"description":"wait milliseconds"},"accept":{"type":"boolean","description":"dialog accept=true or dismiss=false"},"tab":{"type":"string","enum":["list","new","close","select"],"description":"tabs action"},"index":{"type":"integer","minimum":0,"description":"tab index for tabs select/close"}},"required":["op"],"additionalProperties":false}`)},
		{Name: "image.generate", Description: "Generate an image with the configured 生图模型 catalog (default, then backups). Use when the user asks to draw, illustrate, or generate a picture. Prompt is the image description.", Schema: []byte(`{"type":"object","properties":{"prompt":{"type":"string","minLength":1,"maxLength":4000,"description":"Image description"},"path":{"type":"string","description":"Optional workspace-relative hint for where to save"}},"required":["prompt"],"additionalProperties":false}`)},
		{Name: "video.generate", Description: "Generate a video with the configured 生视频模型 catalog (default, then backups). Use when the user asks to make or generate a video.", Schema: []byte(`{"type":"object","properties":{"prompt":{"type":"string","minLength":1,"maxLength":4000,"description":"Video description"},"path":{"type":"string","description":"Optional workspace-relative hint for where to save"}},"required":["prompt"],"additionalProperties":false}`)},
		structuredOutputDefinition(),
	}
}

// expertToolDefinitions exposes the expert.create tool when the expert
// service is wired. The model can create a six-section expert profile
// directly from the conversation.
func (e *Engine) expertToolDefinitions() []gateway.ToolDefinition {
	if e.m8expert == nil {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "expert.create", Description: "Create a six-section expert profile (name, division, description, and six-section body: identity, mission, rules, workflow, deliverableTemplate, successMetrics). Optionally bind published skill catalog keys with skillKeys — skills hang on the expert, not the chat composer. After success, tell the user to confirm skills in Expert Center.", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":128,"description":"Expert display name"},"division":{"type":"string","enum":["engineering","design","product","project-management","testing","security","operations","data"],"description":"Expert domain"},"description":{"type":"string","minLength":1,"maxLength":2000,"description":"Short description of the expert"},"semver":{"type":"string","description":"Semantic version like 1.0.0"},"identity":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert identity prompt section"},"mission":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert mission prompt section"},"rules":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert rules prompt section"},"workflow":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert workflow prompt section"},"deliverableTemplate":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert deliverable template prompt section"},"successMetrics":{"type":"string","minLength":1,"maxLength":65536,"description":"Expert success metrics prompt section"},"skillKeys":{"type":"array","maxItems":32,"items":{"type":"string","minLength":1,"maxLength":64},"description":"Optional published skill catalog keys bound to this expert"}},"required":["name","division","description","semver","identity","mission","rules","workflow","deliverableTemplate","successMetrics"],"additionalProperties":false}`)},
	}
}

func (e *Engine) pluginToolDefinitions() []gateway.ToolDefinition {
	if e.m8plugin == nil {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "plugin.create", Description: "Create one capability pack: a manifest of skills[] + mcpPresetIds[] + toolGates[]. This installs those catalog items; it does not execute Cordis/TypeScript. kind=mcp or agent-pack is refused. After success, tell the user in Chinese to open 能力包.", Schema: []byte(`{"type":"object","properties":{"pluginId":{"type":"string","minLength":1,"maxLength":128},"name":{"type":"string","minLength":1,"maxLength":128},"kind":{"type":"string","enum":["skill","workflow","template","tool"]},"description":{"type":"string","maxLength":2000},"entrypoint":{"type":"string","maxLength":512},"semver":{"type":"string","maxLength":32},"publisher":{"type":"string","maxLength":128},"manifest":{"type":"object","description":"include skills, mcpPresetIds, toolGates arrays"}},"required":["pluginId","name","kind"],"additionalProperties":false}`)},
	}
}

// ccToolDefinitions are the M10 computer-control tools. They are appended
// to the model tool list only when the ccapp service is wired and the
// operator enabled the domain (M10-CC-012 keeps them hidden otherwise, and
// the armed emergency latch hides them too). Subagents never see them:
// readOnlyEngineToolDefinitions stays file-read-only and runs sub-sessions
// in FullAccess, which would bypass the confirmation gate.
func (e *Engine) ccToolDefinitions() []gateway.ToolDefinition {
	if e.ccctrl == nil {
		return nil
	}
	settings, err := e.ccctrl.GetConfig(context.Background())
	if err != nil || !settings.Enabled || settings.EmergencyStopped {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "computer.act", Description: "Unified desktop action (OpenClaw-shaped). action=screenshot|click|double_click|right_click|move|drag|type|key|press|hold_key|key_up|scroll|wait|observe|observe_dialog|confirm|focus|list|paste|menu|set_value|clipboard|window_action. Click may pass modifiers=[ctrl|shift|alt|win] (held only around that click). hold_key holds a key; key_up releases it (auto-release after 8s). Default screenshot is the foreground window (target=foreground); target=desktop captures the virtual desktop. Pixel actions must echo frameId from the latest screenshot (id binds screenIndex + display topology; reconnect/DPI fails closed). Expands onto governed cc.* (audit, rate limit, emergency stop) — do not call cc.* yourself. Prefer name=/id= over raw x,y. Never click UAC or file Open/Save — the runtime will ask the user.", Schema: []byte(`{"type":"object","properties":{"action":{"type":"string","minLength":1,"maxLength":40},"frameId":{"type":"string","maxLength":40},"x":{"type":"integer","minimum":0,"maximum":65535},"y":{"type":"integer","minimum":0,"maximum":65535},"x1":{"type":"integer","minimum":0,"maximum":65535},"y1":{"type":"integer","minimum":0,"maximum":65535},"x2":{"type":"integer","minimum":0,"maximum":65535},"y2":{"type":"integer","minimum":0,"maximum":65535},"button":{"type":"string"},"clicks":{"type":"integer","minimum":1,"maximum":3},"modifiers":{"type":"array","maxItems":3,"items":{"type":"string","enum":["ctrl","shift","alt","win"]}},"scroll":{"type":"integer","minimum":-12,"maximum":12},"scrollAxis":{"type":"string","enum":["vertical","horizontal"]},"name":{"type":"string","maxLength":80},"id":{"type":"string","maxLength":8},"text":{"type":"string","maxLength":8192},"keys":{"type":"array","maxItems":4,"items":{"type":"string"}},"key":{"type":"string","maxLength":24},"count":{"type":"integer","minimum":1,"maximum":8},"window":{"type":"string","maxLength":200},"title":{"type":"string","maxLength":200},"process":{"type":"string","maxLength":200},"target":{"type":"string"},"ms":{"type":"integer","minimum":0,"maximum":8000},"until":{"type":"string","enum":["timeout","change"]},"maxNodes":{"type":"integer","minimum":0,"maximum":120},"path":{"type":"string","maxLength":240},"op":{"type":"string"},"value":{"type":"string","maxLength":4096},"w":{"type":"integer","minimum":1,"maximum":65535},"h":{"type":"integer","minimum":1,"maximum":65535}},"required":["action"],"additionalProperties":false}`)},
	}
}

const maxCaptureVisionImages = 4

func appendCaptureVision(images []gateway.Image, mime string, data []byte) []gateway.Image {
	if len(data) == 0 {
		return images
	}
	if mime == "" {
		mime = "image/png"
	}
	images = append(images, gateway.Image{MIME: mime, Data: data})
	if len(images) > maxCaptureVisionImages {
		images = images[len(images)-maxCaptureVisionImages:]
	}
	return images
}

// skillToolDefinitions exposes published skills as one model-callable tool
// (voice companion / ordinary chat alike). The catalog injected into the
// system instruction carries each skill's skillId; the tool routes through
// the governed skillapp Invoke/Execute pipeline (risk assessment, audit,
// version pinning) rather than raw execution.
func (e *Engine) skillToolDefinitions() []gateway.ToolDefinition {
	if !skillServiceAvailable(e.skills) {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "skill.invoke", Description: "Invoke one published skill by skillId (ULID or catalog template id such as slide-builder). input is the user's request text for the skill", Schema: []byte(`{"type":"object","properties":{"skillId":{"type":"string","description":"published skill ULID or catalog template id"},"input":{"type":"string","minLength":1,"maxLength":2048,"description":"the user request passed to the skill"}},"required":["skillId","input"],"additionalProperties":false}`)},
		{Name: "skill.view", Description: "Read one skill's working agreement (SKILL.md / prompt) by skillId or market template id. Optional path reads a reference file (L2). Use when the catalog summary is not enough.", Schema: []byte(`{"type":"object","properties":{"skillId":{"type":"string","minLength":1,"maxLength":128,"description":"installed skill ULID or catalog template id"},"path":{"type":"string","maxLength":256,"description":"optional reference file path"}},"required":["skillId"],"additionalProperties":false}`)},
		{Name: "skill.create", Description: "Create one local skill from a SKILL.md-style folder (name, displayName, permissions, entryPoint, manifestJson). Call once per skill. After it succeeds, write a short Chinese confirmation naming the skill and telling the user to install/publish it in Skill Center. Then continue any remaining user work.", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":128,"description":"stable skill id slug"},"displayName":{"type":"string","maxLength":200,"description":"human title; defaults to name"},"description":{"type":"string","maxLength":4096},"version":{"type":"string","maxLength":32,"description":"semver, default 1.0.0"},"permissions":{"type":"array","minItems":1,"items":{"type":"string","enum":["read_only","read_write","network","file_system","shell","admin"]}},"entryPoint":{"type":"string","maxLength":512,"description":"SKILL.md path or builtin:// entry"},"manifestJson":{"type":"string","minLength":2,"maxLength":65536,"description":"JSON manifest with prompt and triggers"}},"required":["name","permissions","manifestJson"],"additionalProperties":false}`)},
		{Name: "skill.manage", Description: "Create or patch a local skill draft. create stays draft until the user publishes in Skill Center (write approval). patch updates displayName/description/entryPoint/manifestJson of an existing skill.", Schema: []byte(`{"type":"object","properties":{"action":{"type":"string","enum":["create","patch"]},"skillId":{"type":"string","description":"required for patch"},"name":{"type":"string","maxLength":128},"displayName":{"type":"string","maxLength":200},"description":{"type":"string","maxLength":4096},"version":{"type":"string","maxLength":32},"permissions":{"type":"array","items":{"type":"string","enum":["read_only","read_write","network","file_system","shell","admin"]}},"entryPoint":{"type":"string","maxLength":512},"manifestJson":{"type":"string","maxLength":65536}},"required":["action"],"additionalProperties":false}`)},
	}
}

// invokeSkillTool runs one model-initiated skill invocation through the
// governed pipeline. Full-access conversations auto-approve (mirroring the
// mode's no-approval semantics for every other tool); other modes keep the
// skill's own risk gate — a requiresApproval skill answers a plain error
// telling the model to ask the user to run it via the / command instead of
// parking the stream in an approval flow the caller may not be able to
// answer (voice companion).
func (e *Engine) invokeSkillTool(ctx context.Context, mode executionMode, session string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		SkillID string `json:"skillId"`
		Input   string `json:"input"`
	}
	if json.Unmarshal(args, &a) != nil || strings.TrimSpace(a.SkillID) == "" || strings.TrimSpace(a.Input) == "" {
		return toolruntime.Result{}, errors.New("invalid skill.invoke arguments")
	}
	skillID, resolveErr := e.resolvePublishedSkillID(ctx, a.SkillID)
	if resolveErr != nil {
		return toolruntime.Result{}, resolveErr
	}
	a.SkillID = skillID
	if len(a.Input) > 2048 {
		return toolruntime.Result{}, errors.New("skill input too long (max 2048)")
	}
	inv, err := e.skills.Invoke(ctx, a.SkillID, session, a.Input, string(mode))
	if err != nil {
		return toolruntime.Result{}, err
	}
	approved := mode == executionModeFullAccess
	if inv.RequiresApproval && !approved {
		return toolruntime.Result{}, fmt.Errorf("skill %s requires user approval (risk %s); ask the user to run it via the / command", a.SkillID, inv.Risk)
	}
	out, err := e.skills.Execute(ctx, inv.ID, session, approved)
	if err != nil {
		return toolruntime.Result{}, err
	}
	return toolruntime.Result{Output: out.Output}, nil
}

func (e *Engine) invokeSkillCreateTool(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errors.New("skill service unavailable")
	}
	var a struct {
		Name         string   `json:"name"`
		DisplayName  string   `json:"displayName"`
		Description  string   `json:"description"`
		Version      string   `json:"version"`
		Permissions  []string `json:"permissions"`
		EntryPoint   string   `json:"entryPoint"`
		ManifestJSON string   `json:"manifestJson"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("skill.create", "arguments are not valid JSON"))
	}
	name := strings.TrimSpace(a.Name)
	display := strings.TrimSpace(a.DisplayName)
	if display == "" {
		display = name
	}
	version := strings.TrimSpace(a.Version)
	if version == "" {
		version = "1.0.0"
	}
	entry := strings.TrimSpace(a.EntryPoint)
	if entry == "" {
		entry = "SKILL.md"
	}
	perms := make([]skill.PermissionLevel, 0, len(a.Permissions))
	for _, p := range a.Permissions {
		perms = append(perms, skill.PermissionLevel(p))
	}
	created, err := e.skills.Create(ctx, skill.Skill{
		Name: name, DisplayName: display, Description: a.Description,
		Version: version, Permissions: perms, EntryPoint: entry,
		ManifestJSON: a.ManifestJSON,
	})
	if err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("skill.create", err.Error()))
	}
	label := created.DisplayName
	if label == "" {
		label = name
	}
	id := created.ID
	if id == "" {
		id = name
	}
	return toolruntime.Result{Output: "技能「" + label + "」已创建（id=" + id + "，status=" + string(created.Status) + "）。"}, nil
}

const skillViewMaxRunes = 8000

func (e *Engine) invokeSkillViewTool(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errors.New("skill service unavailable")
	}
	var a struct {
		SkillID string `json:"skillId"`
		Path    string `json:"path"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil || strings.TrimSpace(a.SkillID) == "" {
		return toolruntime.Result{}, errors.New("invalid skill.view arguments")
	}
	id := strings.TrimSpace(a.SkillID)
	label, body, source, err := e.skillViewBody(ctx, id)
	if err != nil {
		return toolruntime.Result{}, err
	}
	runes := []rune(body)
	truncated := false
	if len(runes) > skillViewMaxRunes {
		body = string(runes[:skillViewMaxRunes]) + "\n…(truncated)"
		truncated = true
	}
	out := "技能「" + label + "」正文：\n" + body
	refs := skillReferencesFromManifest(source)
	path := strings.TrimSpace(a.Path)
	if path != "" {
		if file, ok := readLocalSkillAttachment(e.skillAttachmentRoots(), skillViewFolderKeys(id, label), path); ok {
			runes := []rune(file)
			if len(runes) > skillViewMaxRunes {
				file = string(runes[:skillViewMaxRunes]) + "\n…(truncated)"
			}
			out += "\n\n附件「" + path + "」：\n" + file
		} else if listedSkillReference(refs, path) {
			out += "\n\n附件「" + path + "」列在 references，工作区没有这份文件。"
		} else {
			out += "\n\n该技能没有附件「" + path + "」，只有 SKILL.md 正文。"
		}
	} else if len(refs) > 0 {
		out += "\n\nreferences：\n- " + strings.Join(refs, "\n- ")
	} else {
		out += "\n\n无附件，只有 SKILL.md 正文。"
	}
	if truncated {
		out += "\n需要执行时用 skill.invoke。"
	}
	return toolruntime.Result{Output: out}, nil
}

func (e *Engine) skillViewBody(ctx context.Context, id string) (label, body, source string, err error) {
	if validCanonicalULID(id) {
		sk, getErr := e.skills.Get(ctx, id)
		if getErr == nil && sk != nil {
			return skillViewLabel(*sk), skillPromptFromManifest(sk.ManifestJSON), sk.ManifestJSON, nil
		}
	}
	for _, tpl := range skillapp.Catalog() {
		if tpl.ID == id || tpl.Name == id {
			prompt, _ := tpl.Manifest["prompt"].(string)
			name := tpl.DisplayName
			if name == "" {
				name = tpl.Name
			}
			raw, _ := json.Marshal(tpl.Manifest)
			return name, strings.TrimSpace(prompt), string(raw), nil
		}
	}
	return "", "", "", errors.New("skill not found")
}

func skillViewLabel(sk skill.Skill) string {
	if strings.TrimSpace(sk.DisplayName) != "" {
		return sk.DisplayName
	}
	if strings.TrimSpace(sk.Name) != "" {
		return sk.Name
	}
	return sk.ID
}

func skillPromptFromManifest(raw string) string {
	var m struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal([]byte(raw), &m) == nil && strings.TrimSpace(m.Prompt) != "" {
		return strings.TrimSpace(m.Prompt)
	}
	return strings.TrimSpace(raw)
}

func skillReferencesFromManifest(raw string) []string {
	var m struct {
		References []string `json:"references"`
	}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	var out []string
	for _, item := range m.References {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func listedSkillReference(refs []string, path string) bool {
	for _, ref := range refs {
		if ref == path || strings.HasSuffix(ref, path) {
			return true
		}
	}
	return false
}

func (e *Engine) skillAttachmentRoots() []string {
	var roots []string
	if e != nil && e.tools != nil {
		if root, ok := e.tools.FullAccessRootHint(); ok && strings.TrimSpace(root) != "" {
			roots = append(roots, root)
		}
	}
	if home := homeAgentSkillsRoot(); home != "" {
		roots = append(roots, home)
	}
	return roots
}

func skillViewFolderKeys(id, label string) []string {
	keys := []string{id, label}
	norm := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "tpl-")
	if norm != "" {
		keys = append(keys, norm)
	}
	for _, tpl := range skillapp.Catalog() {
		if tpl.ID == id || tpl.Name == id || strings.EqualFold(tpl.DisplayName, id) || strings.TrimPrefix(tpl.Name, "tpl-") == norm {
			keys = append(keys, tpl.ID, strings.TrimPrefix(tpl.Name, "tpl-"))
		}
	}
	return uniqueStrings(keys)
}

// invokeExpertCreateTool routes a model-initiated expert.create call through
// the M8 expert service. The expert is immediately available for mounting.
func (e *Engine) invokeExpertCreateTool(ctx context.Context, session string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		Name                string   `json:"name"`
		Division            string   `json:"division"`
		Description         string   `json:"description"`
		Semver              string   `json:"semver"`
		Identity            string   `json:"identity"`
		Mission             string   `json:"mission"`
		Rules               string   `json:"rules"`
		Workflow            string   `json:"workflow"`
		DeliverableTemplate string   `json:"deliverableTemplate"`
		SuccessMetrics      string   `json:"successMetrics"`
		SkillKeys           []string `json:"skillKeys"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("expert.create", "arguments are not valid JSON"))
	}
	if e.m8expert == nil {
		return toolruntime.Result{}, errors.New("expert service unavailable")
	}
	if strings.TrimSpace(a.Name) == "" || len(a.Name) > 128 {
		return toolruntime.Result{}, errors.New("expert name must be 1-128 characters")
	}
	res, err := e.m8expert.Create(ctx, m8app.CreateInput{
		Source: "local",
		Frontmatter: m8core.Frontmatter{
			Name: a.Name, Division: a.Division,
			Description: a.Description, Semver: a.Semver,
		},
		SixSection: m8core.SixSection{
			Identity: a.Identity, Mission: a.Mission,
			Rules: a.Rules, Workflow: a.Workflow,
			DeliverableTemplate: a.DeliverableTemplate,
			SuccessMetrics:      a.SuccessMetrics,
		},
		RequestID: ulid.Make().String(),
		SkillKeys: a.SkillKeys,
	})
	if err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("expert.create", err.Error()))
	}
	b, _ := json.Marshal(res)
	return toolruntime.Result{Output: "专家「" + a.Name + "」已创建成功。\n" + string(b)}, nil
}

func (e *Engine) invokePluginCreateTool(ctx context.Context, session string, args json.RawMessage) (toolruntime.Result, error) {
	var a struct {
		PluginID    string         `json:"pluginId"`
		Name        string         `json:"name"`
		Kind        string         `json:"kind"`
		Description string         `json:"description"`
		Entrypoint  string         `json:"entrypoint"`
		Semver      string         `json:"semver"`
		Publisher   string         `json:"publisher"`
		Manifest    map[string]any `json:"manifest"`
	}
	if json.Unmarshal(jsonutil.Repair(args), &a) != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("plugin.create", "arguments are not valid JSON"))
	}
	pluginID := strings.TrimSpace(a.PluginID)
	if pluginID == "" || len(pluginID) > 128 {
		return toolruntime.Result{}, errors.New("pluginId must be 1-128 characters")
	}
	switch strings.ToLower(strings.TrimSpace(a.Kind)) {
	case "mcp":
		return toolruntime.Result{}, errors.New("plugin.create 不能安装 MCP。请用 mcp.presets 查看现役预置，再 mcp.install")
	case "agent-pack":
		return toolruntime.Result{}, errors.New("plugin.create 不能加载可执行 Agent 包。请用 skill.create 或 expert.create")
	}
	if !m8core.ValidPluginKind(a.Kind) {
		return toolruntime.Result{}, errors.New("invalid plugin kind")
	}
	if e.m8plugin == nil {
		return toolruntime.Result{}, errors.New("plugin service unavailable")
	}
	manifest := a.Manifest
	if manifest == nil {
		manifest = map[string]any{}
	}
	if _, ok := manifest["pluginId"]; !ok {
		manifest["pluginId"] = pluginID
	}
	if _, ok := manifest["id"]; !ok {
		manifest["id"] = pluginID
	}
	if _, ok := manifest["kind"]; !ok {
		manifest["kind"] = a.Kind
	}
	if _, ok := manifest["semver"]; !ok {
		semver := strings.TrimSpace(a.Semver)
		if semver == "" {
			semver = "1.0.0"
		}
		manifest["semver"] = semver
	}
	if _, ok := manifest["publisher"]; !ok {
		publisher := strings.TrimSpace(a.Publisher)
		if publisher == "" {
			publisher = "local"
		}
		manifest["publisher"] = publisher
	}
	if a.Description != "" {
		if _, ok := manifest["description"]; !ok {
			manifest["description"] = a.Description
		}
	}
	entry := packEntrypointOrDefault(a.Entrypoint)
	workspace := session
	if workspace == "" {
		workspace = "chat"
	}
	res, err := e.m8plugin.CreateAndMount(ctx, m8app.DevCreateInput{
		WorkspaceID: workspace, Manifest: manifest, Entrypoint: entry,
	})
	if err != nil {
		return toolruntime.Result{}, fmt.Errorf("%s", jsonutil.RetryMessage("plugin.create", err.Error()))
	}
	label := strings.TrimSpace(a.Name)
	if label == "" {
		label = pluginID
	}
	notes, failed := e.applyCapabilityPack(ctx, packSpecFromManifest(manifest))
	if failed != "" && res.State != "quarantined" {
		res.State = "quarantined"
	}
	return toolruntime.Result{Output: formatPackInstallResult(label, pluginID, res.State, notes, failed)}, nil
}

// mcpToolPrefix namespaces merged MCP endpoint tools inside the model tool
// list: mcp_<endpointULID>_<tool>. The endpoint ID is a fixed 26-char ULID,
// so the split point is deterministic.
const mcpToolPrefix = "mcp_"

// mcpToolName composes the chat-facing tool name for one ready endpoint
// tool. ok is false when the composed name exceeds the 64-char function
// name budget common across providers or carries characters outside the
// portable [A-Za-z0-9_-] set; such tools are skipped rather than renamed.
func mcpToolName(endpointID, tool string) (string, bool) {
	name := mcpToolPrefix + endpointID + "_" + tool
	if len(name) > 64 {
		return "", false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "", false
		}
	}
	return name, true
}

// parseMcpToolName splits a chat-facing mcp_ tool name back into its
// endpoint ID and MCP tool name.
func parseMcpToolName(name string) (endpointID, tool string, ok bool) {
	rest := strings.TrimPrefix(name, mcpToolPrefix)
	if len(name) <= len(mcpToolPrefix) || len(rest) < 28 || rest[26] != '_' {
		return "", "", false
	}
	return rest[:26], rest[27:], true
}

// mcpToolDefinitions merges ready MCP endpoint tools into the engine tool
// list. When the describe cache carries a real input schema it is used
// verbatim (after a JSON object sanity check); otherwise the tool falls
// back to a pass-through object schema. Invoke still enforces pinning,
// state and breaker per call regardless of which schema was advertised.
const mcpDirectToolCap = 12

func (e *Engine) mcpToolDefinitions() []gateway.ToolDefinition {
	if e.mcp6Registry == nil {
		return nil
	}
	snapshot := e.mcp6Registry.ReadyToolSnapshot()
	if len(snapshot) == 0 {
		return nil
	}
	if len(snapshot) > mcpDirectToolCap {
		return mcpGatewayToolDefinitions(len(snapshot))
	}
	defs := make([]gateway.ToolDefinition, 0, len(snapshot))
	for _, t := range snapshot {
		name, ok := mcpToolName(t.EndpointID, t.Tool)
		if !ok {
			continue
		}
		description := "MCP tool " + t.Tool + " on endpoint " + t.EndpointID + " (arguments pass through to the endpoint)"
		if t.Description != "" {
			description = t.Description
		}
		schema := []byte(`{"type":"object","additionalProperties":true}`)
		if len(t.Schema) > 0 && json.Valid(t.Schema) && t.Schema[0] == '{' {
			schema = t.Schema
		}
		defs = append(defs, gateway.ToolDefinition{Name: name, Description: description, Schema: schema})
	}
	return defs
}

func mcpGatewayToolDefinitions(n int) []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: "mcp.search", Description: fmt.Sprintf("Search the %d connected MCP tools by name or description; then call mcp.call with the returned name", n), Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":200}},"required":["query"],"additionalProperties":false}`)},
		{Name: "mcp.call", Description: "Invoke one MCP tool previously returned by mcp.search (name is mcp_<endpoint>_<tool>)", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":64},"arguments":{"type":"object"}},"required":["name"],"additionalProperties":false}`)},
	}
}

// invokeMcpTool executes one merged MCP tool call through the mcp6
// registry. The registry owns state gating, capability pinning, credential
// leasing and breaker accounting; the 30 s deadline mirrors the frozen
// mcp6.invoke upper bound. The result is flattened to canonical JSON so it
// can ride the normal tool-message path back to the model.
func (e *Engine) invokeMcpTool(ctx context.Context, endpointID, tool string, rawArgs json.RawMessage) (string, error) {
	if e.mcp6Registry == nil {
		return "", errors.New("MCP gateway unavailable")
	}
	var args map[string]any
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", errors.New("MCP tool arguments must be a JSON object")
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	invokeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := e.mcp6Registry.Invoke(invokeCtx, endpointID, tool, args)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(result.Result)
	return string(b), nil
}

func (e *Engine) invokeBrowserAct(ctx context.Context, mode executionMode, session string, raw json.RawMessage) (toolruntime.Result, error) {
	var a browserActCall
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Op) == "" {
		return toolruntime.Result{}, errors.New("browser.act needs op")
	}
	switch a.Op {
	case "click", "type", "snapshot", "scroll", "back", "hover", "select", "press", "tabs", "wait", "dialog":
		return e.invokeBrowserActViaPlaywright(ctx, a)
	case "navigate":
		u := strings.TrimSpace(a.URL)
		if u == "" {
			return toolruntime.Result{}, errors.New("browser.act navigate/read needs url")
		}
		if out, err := e.invokeBrowserActViaPlaywright(ctx, a); err != nil {
			return toolruntime.Result{}, err
		} else if out.Output != "" {
			e.browserLastURL.Store(session, u)
			return out, nil
		}
		args, _ := json.Marshal(map[string]string{"url": u})
		out, err := e.executeUserTool(ctx, mode, session, "web.fetch", args)
		if err == nil {
			e.browserLastURL.Store(session, u)
			out = markBrowserNavigateFetch(out)
		}
		return out, err
	case "read":
		u := strings.TrimSpace(a.URL)
		if u == "" {
			if prev, ok := e.browserLastURL.Load(session); ok {
				u, _ = prev.(string)
			}
		}
		if u == "" {
			return toolruntime.Result{}, errors.New("browser.act navigate/read needs url")
		}
		args, _ := json.Marshal(map[string]string{"url": u})
		out, err := e.executeUserTool(ctx, mode, session, "web.fetch", args)
		if err == nil {
			e.browserLastURL.Store(session, u)
		}
		return out, err
	default:
		return toolruntime.Result{}, errors.New("browser.act op must be navigate, read, click, type, snapshot, scroll, back, hover, select, press, tabs, wait or dialog")
	}
}

func (e *Engine) searchMcpTools(raw json.RawMessage) (string, error) {
	var a struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Query) == "" {
		return "", errors.New("mcp.search needs query")
	}
	if e.mcp6Registry == nil {
		return "", errors.New("MCP gateway unavailable")
	}
	q := strings.ToLower(strings.TrimSpace(a.Query))
	type hit struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var hits []hit
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		name, ok := mcpToolName(t.EndpointID, t.Tool)
		if !ok {
			continue
		}
		blob := strings.ToLower(t.Tool + " " + t.Description + " " + name)
		if !strings.Contains(blob, q) {
			continue
		}
		hits = append(hits, hit{Name: name, Description: t.Description})
		if len(hits) == 12 {
			break
		}
	}
	b, _ := json.Marshal(map[string]any{"tools": hits})
	return string(b), nil
}

func (e *Engine) callMcpToolByName(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return "", errors.New("mcp.call needs name")
	}
	endpointID, tool, ok := parseMcpToolName(a.Name)
	if !ok {
		return "", errors.New("mcp.call name must be an mcp_<endpoint>_<tool> from mcp.search")
	}
	args, _ := json.Marshal(a.Arguments)
	if a.Arguments == nil {
		args = []byte(`{}`)
	}
	return e.invokeMcpTool(ctx, endpointID, tool, args)
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
		StreamID   string `json:"streamId"`
		SpokenText string `json:"spokenText"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.StreamID) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "stream.cancel 参数无效", false)
	}
	ok := e.cancelStreamSpoken(p.StreamID, p.SpokenText)
	return bridge.Success(request.ID, map[string]any{"cancelled": ok})
}

func handleChatToolApprove(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID  string `json:"sessionId"`
		CallID     string `json:"callId"`
		ArgsDigest string `json:"argsDigest"`
		Approved   bool   `json:"approved"`
		Scope      string `json:"scope"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.SessionID) || p.CallID == "" || len(p.CallID) > 128 || len(p.ArgsDigest) != 64 || e.tools == nil {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.tool.approve 参数无效", false)
	}
	if p.Scope == "" {
		p.Scope = toolruntime.ApprovalScopeOnce
	}
	if !toolruntime.ApprovalScopeValid(p.Scope) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "chat.tool.approve scope 无效", false)
	}
	r, err := e.tools.DecideScoped(ctx, p.SessionID, p.CallID, p.ArgsDigest, p.Approved, p.Scope)
	if err != nil {
		// Suppress TOOL_APPROVAL_CONSUMED from bubbling up as a hard stream error
		// and instead just let the frontend know the approval state is invalid
		// so it can retry silently if needed, or we just ignore it.
		// Wait, if it's already consumed, the frontend doesn't need to crash.
		return bridge.Failure(request.ID, request.TraceID, "TOOL_APPROVAL_CONSUMED", err.Error(), false)
	}
	if p.Approved {
		e.persistApprovedToolResult(ctx, p.SessionID, p.CallID, p.ArgsDigest, r)
	}
	status := "rejected"
	if p.Approved {
		status = "executed"
	}
	result := map[string]any{"callId": p.CallID, "status": status, "resultDigest": r.Digest, "summary": r.Output}
	if p.Approved && r.Artifact != nil {
		if k := r.Artifact.Kind; k == "html" && len([]byte(r.Artifact.Content)) <= 180<<10 {
			result["artifact"] = map[string]string{"kind": k, "path": r.Artifact.Path, "content": r.Artifact.Content}
		} else if artifactKindValid(k) {
			// Office artifacts are binary: emit metadata only; the renderer
			// previews them via workspace.artifact.preview.
			result["artifact"] = map[string]string{"kind": k, "path": r.Artifact.Path, "content": ""}
		}
	}
	return bridge.Success(request.ID, result)
}

// persistApprovedToolResult durably records an executed approval-gated tool
// call as an engine-owned tool-role message. The renderer directs the user to
// continue the conversation after approving; without this durable record the
// next turn's history assembly would carry no tool output back to the model.
// The Decide CAS has already consumed the approval, so persistence is
// best-effort: a storage failure downgrades the record to a digest-only
// summary instead of failing the approve response.
func (e *Engine) persistApprovedToolResult(ctx context.Context, sessionID, callID, argsDigest string, r toolruntime.Result) {
	if e.messages == nil {
		return
	}
	output := strings.ReplaceAll(r.Output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")
	output = strings.ReplaceAll(output, "\x00", "")
	output = truncateUTF8Bytes(output, 4096)
	if strings.TrimSpace(output) == "" {
		output = "(no output)"
	}
	keySum := sha256.Sum256([]byte("chat-tool-result\x00" + sessionID + "\x00" + callID))
	key := hex.EncodeToString(keySum[:])
	header := "[tool-result callId=" + callID + " argsDigest=" + argsDigest + " resultDigest=" + r.Digest + "]\n"
	req := struct {
		SessionID  string `json:"sessionId"`
		CallID     string `json:"callId"`
		ArgsDigest string `json:"argsDigest"`
	}{SessionID: sessionID, CallID: callID, ArgsDigest: argsDigest}
	value := message.Message{
		SessionID: sessionID,
		Role:      message.RoleTool,
		Status:    message.StatusCompleted,
		Text:      header + output,
	}
	if _, err := e.messages.Append(ctx, key, "engine", req, value); err == nil {
		return
	}
	// Downgrade to a digest-only record under the same idempotency key: a
	// partially persisted full record wins on replay (digest mismatch),
	// while a clean failure persists the minimal summary.
	minimalReq := struct {
		SessionID string `json:"sessionId"`
		CallID    string `json:"callId"`
		Downgrade string `json:"downgrade"`
	}{SessionID: sessionID, CallID: callID, Downgrade: "output-omitted"}
	value.Text = header + "(output unavailable; see resultDigest)"
	_, _ = e.messages.Append(ctx, key, "engine", minimalReq, value)
}

// Thinking is coalesced so a long DeepSeek reasoning turn does not flood
// the host event queue (256–4k slots). Flush before answers/tools is
// unchanged, so the first visible sentence still follows a complete thought.
const (
	thinkingFlushBytes    = 2048
	thinkingFlushInterval = 32 * time.Millisecond
	streamDeltaMaxBytes   = 16 * 1024
	toolSummaryMaxBytes   = 4096
)

func (e *Engine) runStream(ctx context.Context, id string, state *streamState, p provider.Provider, req gateway.Request, emit EventEmitter, sessionID string, modes ...executionMode) {
	const maxThinkingChunkBytes = 16 * 1024
	const maxThinkingTotalBytes = 256 * 1024
	var seq uint64
	var sendMu sync.Mutex
	var assistantText strings.Builder
	var thinkingText strings.Builder
	var pendingThinking string
	var pendingThinkingSince time.Time
	var lastLiveDraftAt time.Time
	var streamResult gateway.Response
	var turnArtifacts []SessionArtifact
	turn := chatTurnCheckpoint{Status: turnStatusRunning, StreamID: id, Goal: lastUserChatText(req.Messages)}
	if !looksLikeStatusFollowUp(turn.Goal) && !looksLikeResume(turn.Goal) {
		if prev := e.loadTurnCheckpoint(sessionID); prev.PptActive || prev.DocxActive {
			prev.PptActive = false
			prev.DocxActive = false
			prev.PptStage = ""
			prev.DocxStage = ""
			prev.PptTools = nil
			prev.DocxTools = nil
			if prev.Status == turnStatusRunning {
				prev.Status = turnStatusInterrupted
			}
			e.saveTurnCheckpoint(sessionID, prev)
		}
	}
	if looksLikeStatusFollowUp(turn.Goal) || looksLikeResume(turn.Goal) {
		if prev := e.loadTurnCheckpoint(sessionID); prev.PptActive || prev.DocxActive || prev.Status == turnStatusInterrupted || prev.Status == turnStatusRunning {
			if strings.TrimSpace(prev.Goal) != "" {
				turn.Goal = prev.Goal
			}
			turn.PptActive = prev.PptActive
			turn.PptStage = prev.PptStage
			turn.PptTools = append([]string{}, prev.PptTools...)
			turn.PptNudges = prev.PptNudges
			turn.PptGenerated = prev.PptGenerated
			turn.DocxActive = prev.DocxActive
			turn.DocxKind = prev.DocxKind
			turn.DocxStage = prev.DocxStage
			turn.DocxTools = append([]string{}, prev.DocxTools...)
			turn.DocxNudges = prev.DocxNudges
			turn.DocxGenerated = prev.DocxGenerated
			turn.DocxChars = prev.DocxChars
			turn.Injected = append([]string{}, prev.Injected...)
		}
	}
	mode := executionModeApproval
	if len(modes) > 0 {
		mode = modes[0]
	}
	rawSend := func(event bridge.Event) error {
		sendMu.Lock()
		defer sendMu.Unlock()
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
			if err := flushThinking(true); err != nil && event.Type == bridge.EventApprovalRequired {
				return err
			}
		}
		sanitizeOutgoingEvent(&event)
		if err := rawSend(event); err != nil {
			if event.Type == bridge.EventApprovalRequired {
				return err
			}
			// UI/network drop must not abort the tool loop: persist the
			// turn and keep executing until the task finishes.
			log.Printf("chat stream %s dropped event %s: %v", id, event.Type, err)
		}
		return nil
	}
	// Goroutine-wide panic guard: runStream runs detached (go e.runStream), so
	// an unrecovered panic would kill the Engine process and sever the event
	// pipe for every session. Degrade to a failed terminal event instead; the
	// sequence counter lives in this closure so the terminal stays contiguous.
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("chat stream %s panicked: %v\n%s", id, rec, debug.Stack())
			state.cancel()
			_ = send(bridge.Event{Type: bridge.EventFailed, Error: &bridge.StreamError{Code: "ENGINE_STREAM_PANIC", Message: "内部处理错误，请重试", Retryable: true}})
			e.finishTerminal(id, state)
		}
	}()
	usedLocalBrain := false
	if !state.companion && state.brain != "" && state.brain != BrainLunitide {
		if text, note, ok := e.trySessionLocalBrain(ctx, sessionID, turn.Goal, state); ok {
			usedLocalBrain = true
			assistantText.WriteString(text)
			e.noteLiveTurnDraft(sessionID, &turn, assistantText.String(), &lastLiveDraftAt)
			_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: text}})
		} else if note != "" {
			assistantText.WriteString(note)
			e.noteLiveTurnDraft(sessionID, &turn, assistantText.String(), &lastLiveDraftAt)
			_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: note}})
			if len(req.Messages) > 0 && req.Messages[0].Role == gateway.RoleSystem {
				req.Messages[0].Content = localBrainFallbackLockHint(note) + req.Messages[0].Content
			}
		}
	}
	var err error
	if !usedLocalBrain {
		err = e.withProviderLease(ctx, p, secretlease.OperationChat, func(op context.Context, credential []byte) (cbErr error) {
			// A panic anywhere in the streaming/tool loop must degrade to a
			// failed stream, never take down the Engine process (which would
			// sever the event pipe for every active session).
			defer func() {
				if rec := recover(); rec != nil {
					cbErr = fmt.Errorf("chat stream panicked: %v", rec)
				}
			}()
			a, adapterErr := e.adapter(op, p)
			if adapterErr != nil {
				return adapterErr
			}
			e.applyExpertCouncil(op, a, credential, req.Model, state.council, &req, state.companion, send)
			state.council = nil
			startPptWorkflow(&req, &turn, send)
			startDocxWorkflow(&req, &turn, send)
			logInjectedGuidance(sessionID, state.companion, req)
			emitInjectedGuidance(send, req)
			seen := map[string]bool{}
			completedDigests := map[string]string{}
			var result gateway.Response
			var streamErr error
			toolsFallbackUsed := false
			imagesFallbackUsed := false
			usedTools := false
			usedDesktopTools := false
			autoMediaPlayDone := false
			autoDesktopTypeDone := false
			nudges := 0
			skillDraftOffered := false
			leadInInjected := false
			webSearchSeen := false
			if prev := e.loadTurnCheckpoint(sessionID); looksLikeResume(turn.Goal) && strings.TrimSpace(prev.Goal) != "" {
				turn.Goal = prev.Goal
				turn.Injected = append(turn.Injected, prev.Injected...)
			}
			e.saveTurnCheckpoint(sessionID, turn)
			toolLoopLimit := maxToolLoopSteps
			if state.companion && !companionDesktopToolLoop(e, sessionID, turn.Goal) {
				toolLoopLimit = companionMaxToolLoopSteps
			}
			for step := 0; step < toolLoopLimit; step++ {
				_ = e.applyQueuedSupplements(op, sessionID, &req, &turn, send, &assistantText)
				stepTextStart := assistantText.Len()
				result, streamErr = a.Stream(op, credential, req, func(d gateway.Delta) error {
					if d.Reasoning != "" {
						if thinkingText.Len() < maxThinkingTotalBytes && !req.DisableReasoning {
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
						// Companion voice mode: reasoning_content is discarded — never
						// spoken aloud and never shown as thinking in the UI.
					}
					if d.Text != "" {
						assistantText.WriteString(d.Text)
						e.noteLiveTurnDraft(sessionID, &turn, assistantText.String(), &lastLiveDraftAt)
						if err := sendDeltaChunks(send, d.Text); err != nil {
							return err
						}
					}
					return nil
				})
				if streamErr == nil && state.companion && req.DisableReasoning && assistantText.Len() == 0 {
					if fallback := companionSpeakFallback(result); fallback != "" {
						assistantText.WriteString(fallback)
						if err := sendDeltaChunks(send, fallback); err != nil {
							return err
						}
					}
				}
				var gatewayErr *gateway.Error
				if streamErr != nil && !imagesFallbackUsed && assistantText.Len() == 0 && thinkingText.Len() == 0 && len(req.Images) > 0 && errors.As(streamErr, &gatewayErr) && gatewayErr.HTTPStatus == 400 && imageUnsupportedReason(gatewayErr.Message) {
					req.Images = nil
					imagesFallbackUsed = true
					continue
				}
				if streamErr != nil && !toolsFallbackUsed && assistantText.Len() == 0 && thinkingText.Len() == 0 && len(req.Tools) > 0 && errors.As(streamErr, &gatewayErr) && gatewayErr.HTTPStatus == 400 {
					// Some compatible text models reject function definitions. Retry once
					// as plain chat while preserving messages and attachment context. The
					// degradation is surfaced explicitly instead of silently dropping
					// tools: the notice enters both the live stream and the persisted
					// assistant text so the history keeps the record. The adapter has
					// already retried once with sanitized schemas; whatever reason the
					// upstream still reports is appended so the user can act on it.
					reason := gatewayErr.Message
					if i := strings.Index(reason, ": "); i >= 0 {
						reason = reason[i+2:]
					}
					if runes := []rune(strings.TrimSpace(reason)); len(runes) > 0 {
						if len(runes) > 160 {
							runes = runes[:160]
						}
						reason = string(runes)
					} else {
						reason = ""
					}
					why := ""
					if reason != "" {
						why = "，原因：" + reason
					}
					notice := "（系统提示：当前模型拒绝了工具定义" + why + "，本轮已自动切换为纯对话模式：文件读写、命令执行、联网获取与 MCP 工具不可用。如需完整能力，请切换到支持函数调用的模型或检查该服务商的工具参数要求。）\n\n"
					assistantText.WriteString(notice)
					if err := send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: notice}}); err != nil {
						return err
					}
					req.Tools = nil
					toolsFallbackUsed = true
					continue
				}
				if streamErr != nil {
					break
				}
				if state.companion && len(result.Message.ToolCalls) == 0 {
					if !autoMediaPlayDone && companionTurnWantsMusicPlay(turn.Goal) {
						if playArgs, ok := e.companionAutoMediaPlayArgs(sessionID, turn.Goal); ok {
							result.Message.ToolCalls = []gateway.ToolCall{{
								ID:        "auto-" + ulid.Make().String(),
								Name:      "media.play",
								Arguments: playArgs,
							}}
							autoMediaPlayDone = true
						}
					}
					if !autoDesktopTypeDone && looksLikeTypeAfterLabelTurn(turn.Goal) {
						if typeArgs, ok := e.companionAutoDesktopTypeArgs(sessionID, turn.Goal); ok {
							result.Message.ToolCalls = []gateway.ToolCall{{
								ID:        "auto-" + ulid.Make().String(),
								Name:      "desktop.type",
								Arguments: typeArgs,
							}}
							autoDesktopTypeDone = true
						}
					}
				}
				stepText := ""
				if assistantText.Len() > stepTextStart {
					stepText = assistantText.String()[stepTextStart:]
				}
				noteDocxChars(&turn, stepText)
				if len(result.Message.ToolCalls) == 0 {
					toolOut := lastToolOutput(req.Messages)
					continueKind := pickTurnContinueKind(stepText, assistantText.String(), toolOut, turn.LastTools, usedTools, usedDesktopTools, state.companion, req.DisableReasoning, nudges)
					if continueKind == "" && !state.companion && !skillDraftOffered && shouldOfferSkillDraft(turn.LastTools) {
						skillDraftOffered = true
						msg := result.Message
						if strings.TrimSpace(msg.Content) == "" {
							msg.Role = gateway.RoleAssistant
							msg.Content = stepText
						}
						if msg.Role != "" {
							req.Messages = append(req.Messages, msg)
						}
						req.Messages = append(req.Messages, skillDraftOfferMessage())
						continue
					}
					if continueKind != "" {
						nudges++
						msg := result.Message
						if strings.TrimSpace(msg.Content) == "" {
							msg.Role = gateway.RoleAssistant
							msg.Content = stepText
						}
						nudge := continueNudgeMessage()
						if continueKind == "leadin" {
							nudge = gateway.Message{Role: gateway.RoleSystem, Content: "工具已经跑完。用一两句口语把结果说给用户听（天气说出气温和阴晴；打开/写入说出已打开或已写入），不要只说等一下，不要沉默。"}
						} else if continueKind == "desktop" {
							nudge = desktopContinueNudgeMessage()
						} else if continueKind == "incomplete" {
							nudge = incompleteContinueNudgeMessage()
						}
						req.Messages = append(req.Messages, msg, nudge)
						continue
					}
					if continueKind == "" && state.companion && usedTools && isCompanionLeadInOnly(assistantText.String()) {
						close := companionToolResultSpeech(lastToolName(turn.LastTools), lastToolOutput(req.Messages))
						assistantText.WriteString(close)
						if err := sendDeltaChunks(send, close); err != nil {
							return err
						}
					}
					note, texts := e.pullQueuedSupplements(op, sessionID)
					if note != "" {
						msg := result.Message
						if strings.TrimSpace(msg.Content) == "" && stepText != "" {
							msg.Role = gateway.RoleAssistant
							msg.Content = stepText
						}
						if msg.Role != "" {
							req.Messages = append(req.Messages, msg)
						}
						req.Messages = append(req.Messages, queuedSupplementMessage(note))
						turn.Injected = append(turn.Injected, texts...)
						assistantText.WriteString(queueInjectNotice)
						_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: "已收到你的补充，继续当前任务，不另起炉灶。\n"}})
						_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: queueInjectNotice}})
						continue
					}
					if nudgePptWorkflow(&req, &turn, send) {
						e.saveTurnCheckpoint(sessionID, turn)
						continue
					}
					if nudgeDocxWorkflow(&req, &turn, send) {
						e.saveTurnCheckpoint(sessionID, turn)
						continue
					}
					break
				}
				usedTools = true
				for _, call := range result.Message.ToolCalls {
					if isDesktopControlTool(call.Name) {
						usedDesktopTools = true
						if toolLoopLimit < maxToolLoopSteps {
							toolLoopLimit = maxToolLoopSteps
						}
						break
					}
				}
				if state.companion && shouldInjectCompanionToolLeadIn(assistantText.String(), leadInInjected) && len(result.Message.ToolCalls) > 0 && len(turn.LastTools) == 0 {
					lead := companionToolLeadIn(result.Message.ToolCalls[0].Name)
					assistantText.WriteString(lead)
					leadInInjected = true
					if err := sendDeltaChunks(send, lead); err != nil {
						return err
					}
				}
				req.Messages = append(req.Messages, result.Message)
				// Parallel subagents: same-turn subagent.spawn calls are
				// pre-started (bounded) so independent research subagents
				// overlap; each result is consumed in original call order
				// below, keeping the event stream deterministic.
				subagentFutures := startSubagentFutures(op, e, a, credential, req.Model, sessionID, result.Message.ToolCalls, state.subagentPolicy)
				// P0-1 parallel tools: same-turn MCP and read-only engine calls
				// pre-start on bounded goroutines (chat_parallel.go documents
				// the concurrency safety contract); mutating, cc.* and gated
				// tools stay inline.
				parallelFutures := startParallelToolFutures(op, e, mode, sessionID, result.Message.ToolCalls)
				// Early returns below (duplicate call ID, invalid args, send
				// failures) must not abandon pre-started spawn goroutines:
				// drain unconsumed futures when the callback exits. The
				// lease context cancellation bounds the wait.
				defer func() {
					for _, ch := range subagentFutures {
						select {
						case <-ch:
						case <-op.Done():
							return
						}
					}
					drainParallelToolFutures(op, parallelFutures)
				}()
				parkedFilePicker := false
				parkedUAC := false
				for _, call := range result.Message.ToolCalls {
					if seen[call.ID] {
						return errors.New("duplicate tool call id")
					}
					seen[call.ID] = true
					prepared, retryHint := prepareToolArguments(call.Name, call.Arguments, toolSchemaByName(req.Tools, call.Name))
					call.Arguments = prepared
					digest := argsDigestOrFallback(call.Name, prepared)
					if retryHint != "" {
						if future, ok := parallelFutures[call.ID]; ok {
							select {
							case <-future:
							case <-op.Done():
							}
							delete(parallelFutures, call.ID)
						}
						if future, ok := subagentFutures[call.ID]; ok {
							select {
							case <-future:
							case <-op.Done():
							}
							delete(subagentFutures, call.ID)
						}
						if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(call.Name, call.Arguments))}}); err != nil {
							return err
						}
						summary := clipToolSummary(retryHint)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if skipSummary, skip := companionRedundantWebSkip(state.companion, turn.LastTools, call.Name, turn.Goal, webSearchSeen); skip {
						if future, ok := parallelFutures[call.ID]; ok {
							select {
							case <-future:
							case <-op.Done():
							}
							delete(parallelFutures, call.ID)
						}
						if future, ok := subagentFutures[call.ID]; ok {
							select {
							case <-future:
							case <-op.Done():
							}
							delete(subagentFutures, call.ID)
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: skipSummary})
						continue
					}
					if call.Name == "web.search" {
						webSearchSeen = true
					}
					if skipSummary, skip := duplicateToolSkipSummary(digest, completedDigests); skip {
						if future, ok := parallelFutures[call.ID]; ok {
							select {
							case <-future:
							case <-op.Done():
							}
							delete(parallelFutures, call.ID)
						}
						if future, ok := subagentFutures[call.ID]; ok {
							select {
							case <-future:
							case <-op.Done():
							}
							delete(subagentFutures, call.ID)
						}
						if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(call.Name, call.Arguments))}}); err != nil {
							return err
						}
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: skipSummary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: skipSummary})
						continue
					}
					if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(call.Name, call.Arguments))}}); err != nil {
						return err
					}
					// The branches below dispatch without going through the tool
					// runtime, so toolruntime's approval gate never sees them.
					if reason, deny := ungatedEngineToolDenied(mode, state.companion, call.Name, call.Arguments); deny {
						summary := clipToolSummary(reason)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if call.Name == "mcp.presets" || call.Name == "mcp.install" || call.Name == "plugin.search" || call.Name == "plugin.install" {
						summary, invokeErr := e.invokeSettingsPlaneTool(op, call.Name, call.Arguments)
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if call.Name == "mcp.search" {
						if reason, deny := e.denyRestrictedMCP(call.Name, call.Arguments, state.mcpRestrict, state.mcpAllowed); deny {
							summary := clipToolSummary(reason)
							if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
								return err
							}
							req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
							continue
						}
						summary, invokeErr := e.searchMcpToolsFiltered(call.Arguments, state.mcpAllowed, state.mcpRestrict)
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if call.Name == "mcp.call" {
						if reason, deny := e.denyRestrictedMCP(call.Name, call.Arguments, state.mcpRestrict, state.mcpAllowed); deny {
							summary := clipToolSummary(reason)
							if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
								return err
							}
							req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
							continue
						}
						summary, invokeErr := e.callMcpToolByNameGuarded(op, call.Arguments, state.mcpAllowed, state.mcpRestrict)
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if endpointID, mcpTool, isMcp := parseMcpToolName(call.Name); isMcp {
						if reason, deny := e.denyRestrictedMCP(call.Name, call.Arguments, state.mcpRestrict, state.mcpAllowed); deny {
							summary := clipToolSummary(reason)
							if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
								return err
							}
							req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
							continue
						}
						var summary string
						var invokeErr error
						if future, ok := parallelFutures[call.ID]; ok {
							// Pre-started on a background goroutine; waiting here in
							// original call order keeps the event stream identical
							// to serial execution. Deleting the map entry keeps the
							// end-of-turn drain from re-receiving the emptied
							// channel (same contract as the subagent futures).
							res := <-future
							delete(parallelFutures, call.ID)
							summary, invokeErr = res.summary, res.err
						} else {
							summary, invokeErr = e.invokeMcpTool(op, endpointID, mcpTool, call.Arguments)
						}
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if subagentToolNames[call.Name] {
						var summary string
						var invokeErr error
						if future, ok := subagentFutures[call.ID]; ok {
							res := <-future
							summary, invokeErr = res.summary, res.err
							delete(subagentFutures, call.ID)
						} else {
							summary, invokeErr = e.invokeSubagentTool(op, a, credential, req.Model, sessionID, call.Name, call.Arguments, state.subagentPolicy)
						}
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if planToolNames[call.Name] {
						summary, invokeErr := e.invokePlanRunTool(op, a, credential, req.Model, sessionID, mode, call.Arguments)
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					r, toolErr := func() (toolruntime.Result, error) {
						if future, ok := parallelFutures[call.ID]; ok {
							// Pre-started read-only call: consume the background
							// result and drop the map entry (the end-of-turn drain
							// must not re-receive the emptied channel).
							// ErrApprovalRequired cannot occur on the parallel
							// allowlist, so no approval path is bypassed.
							res := <-future
							delete(parallelFutures, call.ID)
							return res.result, res.err
						}
						if call.Name == toolStructuredOutput {
							return emitStructuredOutput(call.Arguments)
						}
						if call.Name == "memory.search" {
							return e.invokeMemorySearch(op, call.Arguments)
						}
						if call.Name == "memory.get" {
							return e.invokeMemoryGet(op, call.Arguments)
						}
						if call.Name == "browser.act" {
							return e.invokeBrowserAct(op, mode, sessionID, call.Arguments)
						}
						if call.Name == "image.generate" || call.Name == "video.generate" {
							return e.invokeMediaGenerate(op, call.Name, call.Arguments)
						}
						// Model-initiated skill invocation rides the governed
						// skillapp pipeline (never the raw toolruntime switch).
						if call.Name == "skill.invoke" {
							return e.invokeSkillTool(op, mode, sessionID, call.Arguments)
						}
						if call.Name == "skill.view" {
							return e.invokeSkillViewTool(op, call.Arguments)
						}
						// Model-initiated expert creation routes through the
						// M8 expert service (never the raw toolruntime switch).
						if call.Name == "skill.create" {
							return e.invokeSkillCreateTool(op, call.Arguments)
						}
						if call.Name == "skill.manage" {
							return e.invokeSkillManageTool(op, call.Arguments)
						}
						if call.Name == "expert.create" {
							return e.invokeExpertCreateTool(op, sessionID, call.Arguments)
						}
						if call.Name == "plugin.create" {
							return e.invokePluginCreateTool(op, sessionID, call.Arguments)
						}
						// P1-2: long-running commands stream bounded output chunks
						// between started and completed. The runtime serializes
						// progress callbacks, so the non-concurrent send closure
						// stays safe.
						if blocked, msg := pptGenBlocked(&turn, call.Name); blocked {
							return blockedPptGenResult(msg), nil
						}
						if blocked, msg := docxGenBlocked(&turn, call.Name); blocked {
							return blockedDocxGenResult(msg), nil
						}
						if call.Name == "command.run" && (turn.PptActive || turn.DocxActive || wantsOfficeFileOnDesktop(turn.Goal)) {
							return toolruntime.Result{Output: "ok:false\n" + officeGenInternalHint + "立刻调用对应 *.gen，不要 command.run。"}, nil
						}
						if call.Name == "command.run" {
							progress := func(chunk string) {
								if chunk == "" {
									return
								}
								_ = send(bridge.Event{Type: bridge.EventToolOutput, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: chunk}})
							}
							return e.executeUserToolWithCompanion(op, mode, sessionID, call.Name, call.Arguments, progress, state.companion)
						}
						return e.executeUserToolWithCompanion(op, mode, sessionID, call.Name, call.Arguments, nil, state.companion)
					}()
					if errors.Is(toolErr, toolruntime.ErrApprovalRequired) {
						if state.companion && companionToolPreapproved(call.Name, e.fullDiskChat(mode)) {
							if _, prepareErr := e.tools.Prepare(op, id, sessionID, call.ID, call.Name, call.Arguments, toolruntime.Mode(mode), 10*time.Minute); prepareErr != nil {
								return prepareErr
							}
							var decideErr error
							r, decideErr = e.tools.DecideScoped(op, sessionID, call.ID, digest, true, toolruntime.ApprovalScopeOnce)
							if decideErr != nil {
								toolErr = decideErr
							} else {
								e.persistApprovedToolResult(op, sessionID, call.ID, digest, r)
								toolErr = nil
							}
						} else {
							if _, prepareErr := e.tools.Prepare(op, id, sessionID, call.ID, call.Name, call.Arguments, toolruntime.Mode(mode), 10*time.Minute); prepareErr != nil {
								return prepareErr
							}
							if sendErr := send(bridge.Event{Type: bridge.EventApprovalRequired, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: approvalRequiredSummary(call.Name, call.Arguments)}}); sendErr != nil {
								return sendErr
							}
							return nil
						}
					}
					summary := r.Output
					if toolErr != nil {
						summary = toolErr.Error()
						if !strings.HasPrefix(summary, "ok:false") {
							summary = "ok:false\n" + summary
						}
						if !strings.Contains(summary, "retry:") {
							turn.ToolFailed = true
						}
					}
					summary = clipToolSummary(summary)
					if toolErr == nil {
						completedDigests[digest] = summary
						if call.Name == "pptx.gen" && !strings.Contains(summary, "被流水线拦住") {
							turn.PptGenerated = true
						}
						if call.Name == "docx.gen" && !strings.Contains(summary, "被流水线拦住") {
							turn.DocxGenerated = true
						}
						if state.companion {
							e.noteCompanionToolSuccess(sessionID, call.Name, call.Arguments, summary)
						}
					}
					toolEvent := &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}
					if toolErr == nil && r.Artifact != nil {
						if k := r.Artifact.Kind; k == "html" && len([]byte(r.Artifact.Content)) <= 180<<10 {
							toolEvent.Artifact = &bridge.ArtifactEvent{Kind: k, Path: r.Artifact.Path, Content: r.Artifact.Content}
						} else if artifactKindValid(k) {
							toolEvent.Artifact = &bridge.ArtifactEvent{Kind: k, Path: r.Artifact.Path, Content: ""}
						}
						if toolEvent.Artifact != nil && chatDeliverableArtifact(call.Name, toolEvent.Artifact.Kind, toolEvent.Artifact.Path) {
							turnArtifacts = append(turnArtifacts, sessionArtifactFromTool(call.ID, call.Name, toolEvent.Artifact.Kind, toolEvent.Artifact.Path))
						}
					}
					if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: toolEvent}); err != nil {
						return err
					}
					req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
					if toolErr == nil && len(r.VisionData) > 0 {
						req.Images = appendCaptureVision(req.Images, r.VisionMIME, r.VisionData)
					}
					if looksLikeFilePickerToolResult(summary) {
						parkedFilePicker = true
					}
					if looksLikeUACToolResult(summary) {
						parkedUAC = true
					}
				}
				if parkedFilePicker {
					if err := e.parkFilePickerAsk(op, id, sessionID, mode, send); err != nil {
						return err
					}
					return nil
				}
				if parkedUAC {
					if err := e.parkUACAsk(op, id, sessionID, mode, send); err != nil {
						return err
					}
					return nil
				}
				if companionTurnWantsMusicPlay(turn.Goal) {
					hasMediaPlay := autoMediaPlayDone
					for _, call := range result.Message.ToolCalls {
						if call.Name == "media.play" {
							hasMediaPlay = true
							break
						}
					}
					if !hasMediaPlay {
						if playArgs, ok := e.companionAutoMediaPlayArgs(sessionID, turn.Goal); ok {
							autoMediaPlayDone = true
							callID := "auto-" + ulid.Make().String()
							name := "media.play"
							digest := toolruntime.Digest(name, playArgs)
							if digest != "" {
								if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(name, playArgs))}}); err != nil {
									return err
								}
								r, toolErr := e.executeUserToolWithCompanion(op, mode, sessionID, name, playArgs, nil, state.companion)
								summary := r.Output
								if toolErr != nil {
									summary = toolErr.Error()
									if !strings.HasPrefix(summary, "ok:false") {
										summary = "ok:false\n" + summary
									}
									turn.ToolFailed = true
								} else {
									completedDigests[digest] = summary
								}
								summary = clipToolSummary(summary)
								if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: summary}}); err != nil {
									return err
								}
								req.Messages = append(req.Messages,
									gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{{ID: callID, Name: name, Arguments: playArgs}}},
									gateway.Message{Role: gateway.RoleTool, ToolCallID: callID, Content: summary},
								)
								turn.LastTools = append(turn.LastTools, name)
							}
						}
					}
				}
				if state.companion && looksLikeTypeAfterLabelTurn(turn.Goal) {
					hasDesktopType := autoDesktopTypeDone
					for _, call := range result.Message.ToolCalls {
						if call.Name == "desktop.type" {
							hasDesktopType = true
							break
						}
					}
					if !hasDesktopType {
						if typeArgs, ok := e.companionAutoDesktopTypeArgs(sessionID, turn.Goal); ok {
							autoDesktopTypeDone = true
							callID := "auto-" + ulid.Make().String()
							name := "desktop.type"
							digest := toolruntime.Digest(name, typeArgs)
							if digest != "" {
								if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(name, typeArgs))}}); err != nil {
									return err
								}
								r, toolErr := e.executeUserToolWithCompanion(op, mode, sessionID, name, typeArgs, nil, state.companion)
								summary := r.Output
								if toolErr != nil {
									summary = toolErr.Error()
									if !strings.HasPrefix(summary, "ok:false") {
										summary = "ok:false\n" + summary
									}
									turn.ToolFailed = true
								} else {
									completedDigests[digest] = summary
								}
								summary = clipToolSummary(summary)
								if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: summary}}); err != nil {
									return err
								}
								req.Messages = append(req.Messages,
									gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{{ID: callID, Name: name, Arguments: typeArgs}}},
									gateway.Message{Role: gateway.RoleTool, ToolCallID: callID, Content: summary},
								)
								turn.LastTools = append(turn.LastTools, name)
							}
						}
					}
				}
				for _, call := range result.Message.ToolCalls {
					turn.LastTools = append(turn.LastTools, call.Name)
				}
				notePptTools(&turn, turn.LastTools)
				noteDocxTools(&turn, turn.LastTools)
				if draft := strings.TrimSpace(assistantText.String()); draft != "" {
					turn.PersistDraft = clipRunes(draft, 8192)
				}
				e.saveTurnCheckpoint(sessionID, turn)
			}
			streamResult = result
			// Step-budget exhaustion: when the last step still produced tool
			// calls the loop ends after executing them, and without a final
			// text the user would see a completed stream with no answer.
			// Surface a Chinese notice in both the live stream and the
			// persisted assistant text (same pattern as the 400 fallback).
			if streamErr == nil {
				notice := createTurnClosingNotice(turn.LastTools, assistantText.String())
				if turn.ToolFailed {
					if failNotice := createTurnFailureNotice(turn.LastTools, assistantText.String()); failNotice != "" {
						notice = failNotice
					} else if notice == "" {
						notice = "这次操作没成功，请再说具体一点让我重试。\n"
					}
				}
				if notice == "" && assistantText.Len() == 0 && len(result.Message.ToolCalls) > 0 {
					notice = "（系统提示：本轮工具调用步数已达上限，以上工具已执行完毕。请基于执行结果继续提问，或让我总结当前进展。）\n"
				}
				if notice != "" {
					if assistantText.Len() > 0 && !strings.HasPrefix(notice, "\n") {
						notice = "\n" + notice
					}
					assistantText.WriteString(notice)
					if sendErr := send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: notice}}); sendErr != nil {
						return sendErr
					}
				}
			}
			if streamErr == nil && result.Usage.TotalTokens > 0 {
				if sendErr := send(bridge.Event{Type: bridge.EventUsage, Usage: &bridge.UsageEvent{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.TotalTokens}}); sendErr != nil {
					return sendErr
				}
			}
			return streamErr
		})
	}
	// Successful upstream completion must claim finalization before any durable
	// side effect. This is the linearization point against stream.cancel.
	// Notices appended below are not model reply text; thinking is stored only
	// when the turn failed with an empty model reply.
	modelReply := strings.TrimSpace(assistantText.String())
	var messageID string
	finalizationClaimed := false
	if err == nil {
		finalizationClaimed = e.claimStreamFinalization(state)
	}
	if finished, notice := e.tryFinishOfficeGen(ctx, mode, sessionID, &turn, assistantText.String(), err, send); finished || notice != "" {
		if finished {
			err = nil
			finalizationClaimed = e.claimStreamFinalization(state)
		}
		if next, delta := appendAssistantNotice(assistantText.String(), notice); delta != "" {
			assistantText.Reset()
			assistantText.WriteString(next)
			_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: delta}})
		}
	} else if outcome := turnOutcomeNotice(e.isStreamCancelling(state), err, turn.Goal, turn.LastTools); outcome != "" {
		if next, delta := appendAssistantNotice(assistantText.String(), outcome); delta != "" {
			assistantText.Reset()
			assistantText.WriteString(next)
			_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: delta}})
		}
	}
	cancelling := e.isStreamCancelling(state)
	upstreamErr := err
	var persistErr error
	if sessionID != "" && e.messages != nil {
		persistThinking := upstreamErr != nil && !cancelling && modelReply == ""
		text := assistantTurnPersistText(assistantText.String(), thinkingText.String(), persistThinking)
		if text == "" && turn.ToolFailed && len(turn.LastTools) > 0 {
			if failNotice := createTurnFailureNotice(turn.LastTools, ""); failNotice != "" {
				text = failNotice
				assistantText.WriteString(failNotice)
				_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: failNotice}})
			}
		}
		persist := strings.TrimSpace(text) != "" && shouldPersistAssistantTurn(upstreamErr, finalizationClaimed, cancelling)
		if persist && !finalizationClaimed {
			finalizationClaimed = e.claimStreamFinalization(state)
			persist = finalizationClaimed
		}
		if persist && cancelling && state != nil && state.companion {
			text = clipCancelledCompanionPersistToSpoken(text, state.spokenPersist)
			if strings.TrimSpace(text) == "" {
				persist = false
			}
		}
		if persist {
			usage := messageapp.AssistantUsage{
				Provider:     string(p.Protocol),
				Model:        req.Model,
				OutputTokens: int64(streamResult.Usage.OutputTokens),
			}
			msg, appendErr := e.messages.AppendAssistant(ctx, id, "engine", sessionID, text, usage)
			if appendErr != nil {
				persistErr = appendErr
				turn.PersistDraft = text
				turn.PersistFailed = true
			} else {
				messageID = msg.ID
				turn.PersistDraft = ""
				turn.PersistFailed = false
				e.appendMessageArtifacts(sessionID, messageID, turnArtifacts)
				e.pushInboundReply(sessionID, text)
			}
		}
	}
	termErr := upstreamErr
	if persistErr != nil && !persistFailureOnly(upstreamErr, persistErr, modelReply) {
		termErr = persistErr
		err = persistErr
	} else if persistErr == nil && upstreamErr != nil {
		err = upstreamErr
	}
	terminal := bridge.Event{Type: e.selectTerminal(id, state, termErr)}
	switch terminal.Type {
	case bridge.EventCompleted:
		turn.Status = turnStatusCompleted
	case bridge.EventCancelled:
		turn.Status = turnStatusCancelled
	default:
		if len(turn.LastTools) > 0 || err != nil {
			turn.Status = turnStatusInterrupted
		}
	}
	e.saveTurnCheckpoint(sessionID, turn)
	if terminal.Type == bridge.EventCompleted {
		completed := &bridge.CompletedEvent{MessageID: messageID}
		if state != nil {
			completed.MemorySummary = state.memorySummary
		}
		if persistFailureOnly(upstreamErr, persistErr, modelReply) {
			completed.PersistFailed = true
			completed.MessageID = ""
		}
		if completed.MessageID != "" || completed.PersistFailed || completed.MemorySummary != "" {
			terminal.Completed = completed
		}
		if messageID != "" {
			// Write before EventCompleted so the session banner and the
			// next surface (companion / people) can see session:last and
			// the confirm-inbox candidate without racing the UI fetch.
			bg := context.Background()
			e.maybeWriteExpertTurnMemories(bg, sessionID, turn.Goal, assistantText.String())
			e.writeSessionLastMemory(bg, sessionID, turn.Goal, assistantText.String())
			_ = e.maybeAutoNominateTurn(bg, sessionID, turn.Goal, assistantText.String(), messageID, state != nil && state.companion)
		}
	}
	if terminal.Type == bridge.EventFailed {
		terminal.Error = chatStreamError(err)
	}
	if send(terminal) != nil {
		state.cancel()
	}
	e.finishTerminal(id, state)
}

func createTurnClosingNotice(tools []string, assistantText string) string {
	empty := strings.TrimSpace(assistantText) == ""
	if empty {
		for _, name := range tools {
			if name == "skill.create" {
				return "技能已创建并写入技能中心。请到技能中心安装并发布。\n"
			}
			if name == "expert.create" {
				return "专家已创建。请到专家中心确认挂载技能（技能挂在专家身上），需要时再挂到项目步骤。\n"
			}
			if name == "plugin.create" {
				return "能力包已创建。请到能力包页查看安装状态。\n"
			}
		}
	}
	if complexTaskCanSaveAsSkill(tools) && !strings.Contains(assistantText, "技能草稿") {
		return "这次任务已经落成文件。若要复用做法，可以到技能中心保存为技能草稿。\n"
	}
	if !hasActingComputerTool(tools) {
		return ""
	}
	if assistantTextContainsDone(assistantText) {
		return ""
	}
	return ""
}

func complexTaskCanSaveAsSkill(tools []string) bool {
	office, acted := false, false
	for _, name := range tools {
		switch name {
		case "excel.gen", "docx.gen", "pptx.gen", "pdf.gen", "html.gen", "workspace.write":
			office = true
		case "skill.invoke", "web.search", "todo.write":
			acted = true
		}
	}
	return office && acted
}

func createTurnFailureNotice(tools []string, assistantText string) string {
	if assistantTextContainsDone(assistantText) {
		return ""
	}
	trimmed := strings.TrimSpace(assistantText)
	if trimmed != "" && (strings.Contains(trimmed, "失败") || strings.Contains(trimmed, "没能") || strings.Contains(trimmed, "无法")) {
		return ""
	}
	openedDesktop := false
	triedMedia := false
	usedVision := false
	for _, name := range tools {
		switch name {
		case "desktop.open":
			openedDesktop = true
		case "media.play":
			triedMedia = true
		default:
			if strings.HasPrefix(name, "cc.") || name == "computer.act" {
				usedVision = true
			}
		}
	}
	if openedDesktop && !triedMedia {
		return "软件已经打开了，但还没开始播放。你可以再说「随便放一首」或歌名让我继续。\n"
	}
	if triedMedia {
		return "这次没能开始播放。你可以再说「随便放一首」或具体歌名让我再试。\n"
	}
	for _, name := range tools {
		switch name {
		case "excel.gen", "docx.gen", "pptx.gen", "pdf.gen":
			return "生成失败：文件没有写成功。\n"
		}
	}
	if usedVision {
		return "这次没能通过看屏幕完成操作。播放音乐请让我用 media.play，你可以再说「随便放一首」。\n"
	}
	if hasActingComputerTool(tools) {
		return "这次操作没成功，请再说具体一点让我重试。\n"
	}
	return ""
}

func hasActingComputerTool(tools []string) bool {
	for _, name := range tools {
		switch name {
		case "workspace.write", "workspace.edit", "command.run", "web.fetch", "web.search", "browser.act", "browser.open",
			"docx.gen", "pptx.gen", "excel.gen", "pdf.gen", "html.gen", "desktop.open", "desktop.type", "media.play", "im.send", "image.generate", "video.generate":
			return true
		}
		if strings.HasPrefix(name, "cc.") || name == "computer.act" {
			return true
		}
	}
	return false
}

// assistantTurnPersistText stores streamed reasoning only when persistThinking
// is true (failed turn, no model reply). Successful or partial replies keep
// the assistant body only so history does not dump the thinking chain.
func clipCancelledCompanionPersist(text string) string {
	return clipCancelledCompanionPersistToSpoken(text, "")
}

func clipCancelledCompanionPersistToSpoken(text, spoken string) string {
	if prefix := strings.TrimSpace(spoken); prefix != "" {
		full := strings.TrimSpace(text)
		if full == "" {
			return prefix
		}
		if strings.HasPrefix(full, prefix) || strings.HasPrefix(prefix, full) {
			if len([]rune(prefix)) <= len([]rune(full)) {
				return prefix
			}
			return full
		}
		return prefix
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	last := -1
	runes := []rune(text)
	for i, r := range runes {
		switch r {
		case '。', '？', '！', '.', '?', '!', '，', ',':
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	return strings.TrimSpace(string(runes[:last+1]))
}

func thinkingDuplicatesBody(thinking, body string) bool {
	t := strings.Join(strings.Fields(strings.TrimSpace(thinking)), " ")
	b := strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if t == "" || b == "" {
		return false
	}
	if t == b {
		return true
	}
	if strings.Contains(t, b) && utf8.RuneCountInString(b) >= 8 {
		return true
	}
	return strings.Contains(b, t) && utf8.RuneCountInString(t) >= 8
}

func assistantTurnPersistText(assistant, thinking string, persistThinking bool) string {
	assistant = strings.TrimSpace(assistant)
	thinking = strings.TrimSpace(thinking)
	if thinkingDuplicatesBody(thinking, assistant) {
		thinking = ""
	}
	if !persistThinking || thinking == "" {
		return assistant
	}
	block := "【思考过程】\n" + thinking
	if assistant == "" {
		return block
	}
	return block + "\n\n" + assistant
}

// shouldPersistAssistantTurn keeps partial work when a turn ends in failure,
// but preserves the early-cancel contract (no durable append before
// finalization is claimed).
func shouldPersistAssistantTurn(err error, finalizationClaimed, cancelling bool) bool {
	if err == nil {
		return finalizationClaimed
	}
	if cancelling {
		return finalizationClaimed
	}
	return true
}

func assistantTextContainsDone(text string) bool {
	if strings.Contains(text, "已完成") || strings.Contains(text, "做完") {
		return true
	}
	if !strings.Contains(text, "完成") {
		return false
	}
	stripped := strings.ReplaceAll(text, "未完成", "")
	stripped = strings.ReplaceAll(stripped, "无法完成", "")
	return strings.Contains(stripped, "完成")
}

const (
	turnInterruptNotice   = "终止打断了"
	turnErrorNotice       = "无法执行。"
	duplicateToolResult   = "already done this turn"
	expertSectionMaxRunes = 2500
)

func skipExpertCouncil(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	createFolder := (strings.Contains(t, "文件夹") || strings.Contains(lower, "folder") || strings.Contains(t, "目录")) &&
		(strings.Contains(t, "创建") || strings.Contains(t, "新建") || strings.Contains(t, "建一个") || strings.Contains(t, "建个"))
	htmlOnDesktop := (strings.Contains(lower, "html") || strings.Contains(t, "网页") || strings.Contains(t, "小游戏")) &&
		(strings.Contains(t, "桌面") || strings.Contains(lower, "desktop"))
	openWeb := (strings.Contains(t, "打开") || strings.Contains(t, "访问") || strings.Contains(lower, "open")) &&
		(strings.Contains(t, "网站") || strings.Contains(t, "网页") || strings.Contains(t, "页面") || strings.Contains(lower, "http"))
	playMusic := (strings.Contains(t, "播") || strings.Contains(lower, "play")) &&
		(strings.Contains(t, "歌") || strings.Contains(t, "音乐") || strings.Contains(lower, "music"))
	return createFolder || htmlOnDesktop || openWeb || playMusic
}

func turnOutcomeNotice(cancelling bool, err error, goal string, tools []string) string {
	if cancelling {
		return turnInterruptNotice
	}
	if err != nil {
		return turnErrorNotice + turnFailureCause(err, goal, tools)
	}
	return ""
}

func turnFailureCause(err error, goal string, tools []string) string {
	se := chatStreamError(err)
	switch se.Code {
	case "UPSTREAM_TIMEOUT":
		return "请求超时，请稍后重试。"
	case "ASSISTANT_RESPONSE_TOO_LARGE", "REQUEST_TOO_LARGE":
		return "回复或工具参数过大，请减少内容后重试。"
	}
	if officeGenToolForGoal(goal) != "" || hasOfficeGenTool(tools) {
		return officeGenFailNotice(err)
	}
	if looksLikeComputerControlTurn(goal) || hasComputerControlTool(tools) {
		return "这次操作没成功，请再说具体一点让我重试。"
	}
	return "模型结果不完整，请重试。"
}

func imageUnsupportedReason(reason string) bool {
	lower := strings.ToLower(reason)
	if strings.Contains(lower, "does not support image") || strings.Contains(lower, "image is not supported") {
		return true
	}
	if strings.Contains(lower, "vision") && strings.Contains(lower, "not support") {
		return true
	}
	return strings.Contains(lower, "url") && strings.Contains(lower, "base64") && strings.Contains(lower, "image")
}

func appendAssistantNotice(existing, notice string) (next, delta string) {
	notice = sanitizeUserVisibleNotice(notice)
	if notice == "" || strings.Contains(existing, notice) {
		return existing, ""
	}
	if strings.TrimSpace(existing) == "" {
		return notice, notice
	}
	delta = "\n" + notice
	return existing + delta, delta
}

func duplicateToolSkipSummary(digest string, completed map[string]string) (string, bool) {
	if digest == "" {
		return "", false
	}
	if _, ok := completed[digest]; ok {
		return duplicateToolResult, true
	}
	return "", false
}

func (e *Engine) isStreamCancelling(state *streamState) bool {
	if e == nil || state == nil {
		return false
	}
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	return state.state == streamCancelling
}

func extractExpertRefIDs(text string) []string {
	const prefix = "[引用专家 "
	var ids []string
	rest := text
	for {
		i := strings.Index(rest, prefix)
		if i < 0 {
			break
		}
		rest = rest[i+len(prefix):]
		bar := strings.IndexByte(rest, '|')
		end := strings.IndexByte(rest, ']')
		if bar < 0 || end < 0 || bar >= end {
			continue
		}
		id := rest[bar+1 : end]
		if len(id) == 26 {
			ids = append(ids, id)
		}
		if end+1 >= len(rest) {
			break
		}
		rest = rest[end+1:]
	}
	return ids
}

func collectExpertIDs(mounted []string, texts ...string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, id := range mounted {
		add(id)
	}
	for _, text := range texts {
		for _, id := range extractExpertRefIDs(text) {
			add(id)
		}
	}
	return ids
}

func (e *Engine) sessionSelectsExperts(ctx context.Context, sessionID, turnText string) bool {
	var mounted []string
	if sessionID != "" && e.sessionExperts != nil {
		if ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, sessionID); err == nil {
			mounted = ids
		}
	}
	return len(selectedTurnExpertIDs(mounted, e.priorTurnTexts(ctx, sessionID, turnText)...)) > 0
}

func (e *Engine) expertPersonaInjection(ctx context.Context, sessionID string, _ []gateway.Message, turnText string) string {
	if e.m8expert == nil {
		return ""
	}
	if skipExpertCouncil(turnText) {
		return ""
	}
	var mounted []string
	if sessionID != "" && e.sessionExperts != nil {
		if ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, sessionID); err == nil {
			mounted = ids
		}
	}
	ids := selectedTurnExpertIDs(mounted, e.priorTurnTexts(ctx, sessionID, turnText)...)
	if len(ids) == 0 {
		return ""
	}
	type packed struct {
		name string
		body string
	}
	var experts []packed
	for _, id := range ids {
		detail, err := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: id})
		if err != nil {
			continue
		}
		name, _ := detail.Expert["name"].(string)
		if name == "" {
			name = id
		}
		experts = append(experts, packed{name: name, body: clipExpertBody(detail.SixSection)})
	}
	if len(experts) == 0 {
		return ""
	}
	var b strings.Builder
	if len(experts) == 1 {
		b.WriteString(expertPersonaHeader(1, experts[0].name))
	} else {
		b.WriteString(expertPersonaHeader(len(experts)))
	}
	for _, item := range experts {
		b.WriteString("\n【专家「")
		b.WriteString(item.name)
		b.WriteString("」】岗位说明书：\n")
		b.WriteString(item.body)
		b.WriteByte('\n')
	}
	return b.String()
}

func expertPersonaHeader(count int, names ...string) string {
	caps := specialistPersonaCapabilityLine()
	if count >= 2 {
		return "\n\n你是月汐主编排（会议主席）。以下专家已常驻挂载到本会话，直到用户移除。请在思考/推理通道内部召开有界专家理事会：具名专家轮流发言，全场合计最多 5–6 轮（不是每位专家各 5–6 轮），再给出用户可见的一份综合结论。不要把每位专家的发言拆成多条助手消息，不要并行打满多路完整 completion。" +
			caps + "协作包：\n"
	}
	name := ""
	if len(names) > 0 {
		name = strings.TrimSpace(names[0])
	}
	if name != "" {
		return "\n\n你就是「" + name + "」。下面是你的岗位说明书。以这个身份作答，不要自称月汐主编排。" +
			caps + "协作包：\n"
	}
	return "\n\n按已挂载专家的岗位说明书作答，不要自称月汐主编排。" +
		caps + "不要并行打满多路完整 completion。协作包：\n"
}

func clipExpertBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	r := []rune(s)
	if len(r) <= expertSectionMaxRunes {
		return s
	}
	return string(r[:expertSectionMaxRunes]) + "\n…（岗位说明书已截断）"
}

func sendDeltaChunks(send func(bridge.Event) error, text string) error {
	for text != "" {
		chunk := truncateUTF8Bytes(text, streamDeltaMaxBytes)
		if chunk == "" {
			return nil
		}
		if err := send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: chunk}}); err != nil {
			return err
		}
		text = text[len(chunk):]
	}
	return nil
}

func clipToolSummary(summary string) string {
	return userVisibleToolSummary(truncateUTF8Bytes(summary, toolSummaryMaxBytes))
}

func approvalRequiredSummary(name string, args json.RawMessage) string {
	if name == "user.ask" {
		if packed := toolruntime.UserAskApprovalSummary(args); packed != "" {
			return truncateUTF8Bytes(packed, toolSummaryMaxBytes)
		}
	}
	return "approval required"
}

func toolStartedSummary(name string, args json.RawMessage) string {
	switch name {
	case "user.ask":
		var a struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(args, &a) == nil && strings.TrimSpace(a.Title) != "" {
			return "需要你决策：" + strings.TrimSpace(a.Title)
		}
		return "需要你决策"
	case "command.run":
		var a struct {
			Argv []string `json:"argv"`
		}
		if json.Unmarshal(args, &a) == nil && len(a.Argv) > 0 {
			return "$ " + strings.Join(a.Argv, " ")
		}
	case "web.search":
		var a struct {
			Query string `json:"query"`
		}
		if json.Unmarshal(args, &a) == nil && strings.TrimSpace(a.Query) != "" {
			return "搜索：" + strings.TrimSpace(a.Query)
		}
	case "web.fetch":
		var a struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(args, &a) == nil && a.URL != "" {
			return a.URL
		}
	case "skill.invoke":
		var a struct {
			SkillID string `json:"skillId"`
			Input   string `json:"input"`
		}
		if json.Unmarshal(args, &a) == nil {
			in := strings.TrimSpace(a.Input)
			if r := []rune(in); len(r) > 40 {
				in = string(r[:40]) + "…"
			}
			id := strings.TrimSpace(a.SkillID)
			if in != "" && id != "" {
				return id + " · " + in
			}
			if in != "" {
				return in
			}
			return id
		}
	case "skill.view":
		var a struct {
			SkillID string `json:"skillId"`
			Path    string `json:"path"`
		}
		if json.Unmarshal(args, &a) == nil {
			id := strings.TrimSpace(a.SkillID)
			if path := strings.TrimSpace(a.Path); path != "" {
				return id + " · " + path
			}
			return id
		}
	case "skill.manage":
		var a struct {
			Action string `json:"action"`
			Name   string `json:"name"`
		}
		if json.Unmarshal(args, &a) == nil {
			return strings.TrimSpace(a.Action + " " + a.Name)
		}
	}
	return ""
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
