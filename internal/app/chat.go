package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/networkpolicy"
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
type unattendedKey struct{}

// unattended reports whether the turn runs with no interactive approver on the
// other end of the event stream — IM inbound auto-run and colleague auto-reply
// both drive chat.start with a noop emitter. Such a turn must never emit an
// approval request and pause: nothing carries the event and DecideScoped is
// never called, so the tool loop would abandon the turn mid-flight. The caller
// sets this flag so the approval gate can deny in place instead.
func unattended(ctx context.Context) bool {
	v, _ := ctx.Value(unattendedKey{}).(bool)
	return v
}

// withUnattended marks ctx as an unattended (headless) turn.
func withUnattended(ctx context.Context) context.Context {
	return context.WithValue(ctx, unattendedKey{}, true)
}

type approvalOutcome int

const (
	// approvalEmit sends an EventApprovalRequired and pauses for a human.
	approvalEmit approvalOutcome = iota
	// approvalPreapproved auto-approves (companion pre-approved tool).
	approvalPreapproved
	// approvalDenyUnattended refuses the tool in place because no approver
	// exists for this turn, letting the loop continue to a coherent answer.
	approvalDenyUnattended
)

// decideApprovalOutcome resolves what to do when a tool reports
// ErrApprovalRequired. Companion pre-approval wins; an unattended turn denies in
// place (there is no emitter to carry an approval, and no one to grant it);
// everything else emits an approval request and pauses.
func decideApprovalOutcome(companionPreapproved, isUnattended bool) approvalOutcome {
	switch {
	case companionPreapproved:
		return approvalPreapproved
	case isUnattended:
		return approvalDenyUnattended
	default:
		return approvalEmit
	}
}

// unattendedApprovalDenial is the tool result recorded when a gated tool is
// refused because the turn has no approver. It is shaped like every other
// failed tool result (ok:false + reason) so the model treats it as a normal
// refusal and continues.
func unattendedApprovalDenial(toolName string) string {
	return "ok:false\n此操作需要人工审批，但当前会话无人值守，已跳过：" + toolName + "。请在有人值守的对话里再执行。"
}

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
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
	}
	hasSession := ulidValid(p.SessionID)
	hasMessages := len(p.Messages) > 0
	if !hasSession && !hasMessages {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start 需要提供 sessionId 或 messages", false)
	}
	if !validChatMessages(p.ModelID, p.Messages) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start 参数无效", false)
	}
	ident := e.conversationIdentityForSession(ctx, p.SessionID, p.Companion)
	boundSessionID := ident.sessionKey(p.SessionID)
	if isPersistRetryTurn(p.Messages) {
		emit, ok := ctx.Value(eventEmitterKey{}).(EventEmitter)
		if !ok {
			return request.Fail("STREAM_UNAVAILABLE", "流事件通道不可用", true)
		}
		return e.handlePersistRetryStart(ctx, request, boundSessionID, emit)
	}
	if hasSession {
		_, _ = e.retrySessionPersistDraft(ctx, boundSessionID)
	}
	for _, ref := range p.ContextRefs {
		if !validCanonicalULID(ref.ID) || (ref.Type != "attachment" && ref.Type != "skillResult") {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start contextRefs 无效", false)
		}
	}
	if p.ProjectID != "" && !validCanonicalULID(p.ProjectID) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start projectId 无效", false)
	}
	mode, validMode := normalizeExecutionMode(p.ExecutionMode)
	if !validMode {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start executionMode 无效", false)
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
		preferred, composeHint = e.expertComposeForTurn(ctx, boundSessionID, intent.Text)
		if intent.Companion && composeHint == "" && !companionWantsTools(intent.Text) {
			preferred = nil
		}
		catalog = e.skillCatalogInjection(ctx, catalogQuery, p.Companion, preferred)
	}()
	prep.Wait()
	if intent.Companion {
		instruction += e.companionSessionInjection(boundSessionID, intent.Text)
		wantsTools = e.companionWantsToolsForTurn(boundSessionID, intent.Text)
		if wantsTools {
			instruction += companionPersonaToolsInstruction()
			instruction += companionTaskWorkflowInjection(intent.Text)
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
		return request.Fail("MODEL_NOT_FOUND", "模型不属于该供应商", false)
	}
	e.rememberChatModel(p.ProviderID, p.ModelID)

	emit, ok := ctx.Value(eventEmitterKey{}).(EventEmitter)
	if !ok {
		return request.Fail("STREAM_UNAVAILABLE", "流事件通道不可用", true)
	}

	// Make auto-equip visible: when intent (not @-mention) pulled in a
	// specialist this turn, emit a structured chip signal. Council turns already
	// narrate their own experts, so skip to avoid a duplicate.
	if emit != nil && !intent.Companion && councilCfg == nil && composeHint != "" {
		if experts, skills, missingMcp := e.turnEquipInfo(ctx, boundSessionID, intent.Text); len(experts) > 0 {
			_ = emit(bridge.Event{Type: bridge.EventEquip, Equip: &bridge.EquipEvent{Experts: experts, Skills: skills, MissingMcp: missingMcp}})
		}
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
					return request.Fail("REQUEST_CANCELLED", "请求已取消", false)
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
					return request.Fail("CONTEXT_REF_NOT_FOUND", "显式上下文引用不存在或已删除", false)
				}
				return internalBridgeFailure(request, "ATTACHMENT_CONTEXT_READ_FAILED", "附件上下文暂时不可用", true, getErr)
			}
			if candidate.SessionID != boundSessionID {
				return request.Fail("CONTEXT_REF_SCOPE_MISMATCH", "显式上下文引用不属于当前会话", false)
			}
			if strings.HasPrefix(candidate.MIME, "image/") {
				imageRefs = append(imageRefs, candidate.ID)
				continue
			}
			if !candidate.IsReadable() || candidate.ParsedText == "" {
				return request.Fail("CONTEXT_REF_NOT_READABLE", "显式上下文引用尚未解析成功", false)
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
					return request.Fail("CONTEXT_BUDGET_EXCEEDED", "最终上下文超过模型输入预算", false)
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
					return request.Fail("ATTACHMENT_IMAGE_READ_FAILED", "图片附件读取或校验失败", false)
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
						return request.Fail("ATTACHMENT_IMAGE_READ_FAILED", "图片附件读取或校验失败", false)
					}
					images = append(images, gateway.Image{MIME: image.MIME, Data: image.Data})
				}
			}
		}
	} else {
		// Legacy path: use directly provided messages.
		if !hasMessages {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start 无有效消息（Session 上下文装配器不可用，需提供 messages）", false)
		}
		messages = trustedMessages
	}

	if p.Companion {
		messages = dropCompanionFailedTail(messages)
	}
	if len(messages) == 0 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.start 无有效消息", false)
	}

	e.streamsMu.Lock()
	if len(e.streams) >= e.maxStreams {
		e.streamsMu.Unlock()
		return request.Fail("STREAM_LIMIT_REACHED", "并发流数量已达上限", true)
	}
	streamID := ulid.Make().String()
	parent, _ := ctx.Value(streamParentKey{}).(context.Context)
	if parent == nil {
		parent = ctx
	}
	streamCtx, cancel := context.WithCancel(parent)
	equip := e.turnEquipmentFor(ctx, boundSessionID, intent.Text, intent.Companion)
	state := &streamState{cancel: cancel, companion: p.Companion, subagentPolicy: subagentPolicy, council: councilCfg, mcpRestrict: equip.RestrictMCP(), mcpAllowed: equip.McpIDs, brain: equip.Brain, memorySummary: formatChatMemorySummary(memPack), kbCites: append([]CitationBlock(nil), memPack.KBCites...), kbDiscarded: memPack.KBDiscarded, mroTurn: memPack.MROTurn || turnHasMROName(equip.Names)}
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
		if profile == toolProfileDefault && !p.Companion {
			// S1: a short, high-confidence pure-chat turn drops the full tool +
			// MCP + skill + expert schema it will never use. Any task intent
			// keeps the full surface (autoToolProfile is precision-biased).
			profile = autoToolProfile(intent.Text)
		}
		req.Tools = applyToolProfile(append(e.engineToolDefinitionsFor(mode), e.subagentToolDefinitions(mode, subagentPolicy)...), profile)
		switch profile {
		case toolProfileDefault:
			req.Tools = append(req.Tools, planToolDefinitions(mode)...)
			req.Tools = append(req.Tools, e.mcpToolDefinitionsRestricted(equip.McpIDs, equip.RestrictMCP())...)
			req.Tools = append(req.Tools, e.ccToolDefinitions()...)
			req.Tools = append(req.Tools, e.skillToolDefinitions()...)
			req.Tools = append(req.Tools, e.expertToolDefinitions()...)
			req.Tools = append(req.Tools, e.pluginToolDefinitions()...)
			req.Tools = append(req.Tools, e.settingsPlaneToolDefinitions()...)
		case toolProfileCoding:
			req.Tools = append(req.Tools, e.skillToolDefinitions()...)
			req.Tools = applyToolProfile(req.Tools, profile)
		case toolProfileColleague:
			req.Tools = append(req.Tools, e.skillToolDefinitions()...)
			req.Tools = applyToolProfile(req.Tools, profile)
		}
		if p.Companion {
			req.Tools = filterCompanionDefaultTools(req.Tools)
		}
	}
	go e.runStream(streamCtx, streamID, state, item, req, emit, boundSessionID, mode)
	return request.Ok(map[string]any{"streamId": streamID})
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

// foldHistoricalToolResult turns a persisted tool-result row into plain context
// text safe to replay as a user-role message. It drops the internal
// "[tool-result callId=... argsDigest=... resultDigest=...]" bookkeeping header
// (which only mattered for idempotent persistence) and guarantees a non-empty
// body so the message still satisfies provider content requirements.
func foldHistoricalToolResult(text string) string {
	s := text
	if strings.HasPrefix(s, "[tool-result ") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		} else {
			s = ""
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		s = "(工具无输出)"
	}
	return "（历史工具结果）" + s
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

func combineDurableProviderMessages(history []contextapp.Message, explicit []gateway.Message, info contextapp.ProviderInfo) ([]gateway.Message, error) {
	combined := make([]gateway.Message, 0, len(history)+len(explicit))
	for _, m := range explicit {
		if m.Role == gateway.RoleSystem {
			combined = append(combined, m)
		}
	}
	for _, m := range history {
		role := gatewayRole(m.Role)
		content := m.Content
		if role == gateway.RoleTool {
			// The message store keeps only role+text — no tool_call_id and no
			// assistant tool_calls linkage. Replaying a persisted role:"tool"
			// message as-is produces an orphan tool message (empty tool_call_id,
			// no matching assistant tool_calls), which strict OpenAI-compatible
			// providers (e.g. glm/Zhipu) reject for the WHOLE request with
			// "missing messages.tool_call_id parameter". This surfaced once 月伴
			// became one long-lived singleton that accumulates tool-result rows.
			// Fold historical tool results into a plain user-role context note so
			// the linkage-free record still informs the model without ever
			// emitting an invalid tool message.
			role = gateway.RoleUser
			content = foldHistoricalToolResult(m.Content)
		}
		combined = append(combined, gateway.Message{Role: role, Content: content})
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
		return request.Fail("BRIDGE_SCHEMA_INVALID", "stream.cancel 参数无效", false)
	}
	ok := e.cancelStreamSpoken(p.StreamID, p.SpokenText)
	return request.Ok(map[string]any{"cancelled": ok})
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
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.tool.approve 参数无效", false)
	}
	if p.Scope == "" {
		p.Scope = toolruntime.ApprovalScopeOnce
	}
	if !toolruntime.ApprovalScopeValid(p.Scope) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.tool.approve scope 无效", false)
	}
	r, err := e.tools.DecideScoped(ctx, p.SessionID, p.CallID, p.ArgsDigest, p.Approved, p.Scope)
	if err != nil {
		// Suppress TOOL_APPROVAL_CONSUMED from bubbling up as a hard stream error
		// and instead just let the frontend know the approval state is invalid
		// so it can retry silently if needed, or we just ignore it.
		// Wait, if it's already consumed, the frontend doesn't need to crash.
		return request.Fail("TOOL_APPROVAL_CONSUMED", err.Error(), false)
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
	return request.Ok(result)
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
