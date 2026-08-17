package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m8app"
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

// Preference injection budget (learning loop P3-3): confirmed preferences
// appended to the system instruction are bounded so they never crowd out
// the conversation context.
const (
	preferenceInjectMaxItems = 8
	preferenceInjectMaxBytes = 2048
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
		Companion bool `json:"companion"` // Added to detect Moon Companion requests
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

	// Moon Companion mode: automatically upgrade to FullAccess if the caller
	// is the companion interface, so the eyes-free persona can execute commands,
	// ccapp, workspace tools, etc. without pausing for visual approval.
	if p.Companion {
		mode = executionModeFullAccess
	}

	instruction := executionModeInstruction(mode)
	// Full-access workspace hint: tell the model where file tools actually
	// operate (user-selected workspace root, or the sandbox when none resolves)
	// so path answers match reality instead of a stale sandbox assumption.
	if mode == executionModeFullAccess && e.tools != nil {
		if e.fullDiskChat(mode) {
			instruction += " Full-disk full-access is enabled: file tools accept absolute paths on any drive (Desktop, Documents, other drives) and command.run executes arbitrary commands on this machine. Use absolute paths for user folders; create missing parent directories with writes when needed."
		} else if root, ok := e.tools.FullAccessRootHint(); ok {
			instruction += " File tools operate directly inside the user's workspace root " + root + "; relative paths resolve there. Keep every read and write inside that root and answer with real paths from it."
		} else {
			instruction += " File tools operate inside a per-session sandbox directory; the user's real folders (Desktop, Documents) are not reachable in this configuration."
		}
	}
	if e.delegation == delegationProactive {
		instruction += delegationProactiveHint
	}
	// Learning loop (P3-3): confirmed preferences ride at the end of the
	// system instruction. Only explicitly confirmed candidates ever reach
	// here and the snapshot is bounded (items + bytes) so preferences never
	// crowd out the conversation. Snapshot failure is non-fatal - the chat
	// proceeds without injection.
	if e.m8memory != nil {
		if prefs, perr := e.m8memory.ConfirmedSnapshot(ctx, m8app.LearningScope, preferenceInjectMaxItems, preferenceInjectMaxBytes); perr == nil && len(prefs) > 0 {
			var b strings.Builder
			b.WriteString(instruction)
			b.WriteString("\n\n以下为用户已显式确认的偏好，回答时必须遵守：\n")
			for _, pref := range prefs {
				b.WriteString("- ")
				b.WriteString(pref)
				b.WriteString("\n")
			}
			instruction = b.String()
		}
	}
	// c4-skill: append the installed-skill directory (metadata only) so the
	// model knows which skills exist and when to reference them. Injected in
	// every execution mode including plan - the catalog is read-only
	// knowledge and does not conflict with the plan-mode no-tool rule.
	if catalog := e.skillCatalogInjection(ctx); catalog != "" {
		instruction += "\n\n" + catalog
	}
	trustedMessages := append([]gateway.Message{{Role: gateway.RoleSystem, Content: instruction}}, p.Messages...)

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

		// Priority 3: Latest accepted compaction checkpoint summary. Stores
		// that answer coverage (P2-2 hierarchical context) also tell the
		// assembler which durable sequence the summary covers, so covered
		// messages are projected once (as the summary) instead of twice.
		if e.summaryReader != nil {
			var priorSummary string
			var coverageEnd int64
			var summaryErr error
			if cr, ok := e.summaryReader.(compactionCoverageReader); ok {
				priorSummary, coverageEnd, summaryErr = cr.GetLatestCompactionCheckpoint(ctx, p.SessionID)
			} else {
				priorSummary, summaryErr = e.summaryReader.GetLatestCompactionSummary(ctx, p.SessionID)
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
					Provenance:          "session:" + p.SessionID + ":checkpoint:latest",
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
		// P1-3 complexity.decide wiring: deterministic full-conversation
		// scoring labels the tier; moderate+ conversations get an explicit
		// nudge toward the planned path (plan.run) in the system message.
		if tierHint := complexityTierHint(messages); tierHint != "" && len(messages) > 0 && messages[0].Role == gateway.RoleSystem {
			messages[0].Content += tierHint
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
	if e.tools != nil {
		req.Tools = append(e.engineToolDefinitionsFor(mode), e.subagentToolDefinitions(mode)...)
		req.Tools = append(req.Tools, planToolDefinitions(mode)...)
		req.Tools = append(req.Tools, e.mcpToolDefinitions()...)
		req.Tools = append(req.Tools, e.ccToolDefinitions()...)
		req.Tools = append(req.Tools, e.skillToolDefinitions()...)
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
// model can proactively reference skills. The directory is bounded by
// skillInjectMaxItems/skillInjectMaxBytes (overflow truncates with an
// explicit notice) and is fail-closed: an unavailable skill service or a
// read failure logs and injects nothing instead of blocking the chat.
func (e *Engine) skillCatalogInjection(ctx context.Context) string {
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
	const header = "[可用技能目录]\n"
	const usage = "使用规则：当用户请求与某技能触发场景匹配时，先声明“将使用技能 X”，再执行。\n"
	const truncNotice = "（技能目录已截断）\n"
	var b strings.Builder
	b.WriteString(header)
	// Reserve the header, the usage rule and the worst-case truncation
	// notice up front so the finished block always fits the byte budget.
	budget := skillInjectMaxBytes - len(header) - len(usage) - len(truncNotice)
	injected := 0
	truncated := false
	for _, sk := range skills {
		if injected == skillInjectMaxItems {
			truncated = true
			break
		}
		line := skillCatalogLine(sk)
		if len(line) > budget {
			if budget <= 0 {
				truncated = true
				break
			}
			// Defensive: a single oversized line is UTF-8-safe truncated to
			// the remaining budget rather than blowing the global cap.
			b.WriteString(truncateUTF8Bytes(line, budget) + "\n")
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
			defs[i].Description = "Run any command on this machine (full-disk full-access is enabled); prefer PowerShell/cmd executables by absolute name, argv max 16 items"
		case "workspace.list", "workspace.read":
			defs[i].Description += "; absolute paths on any drive are accepted (full-disk full-access is enabled)"
		case "workspace.write", "workspace.edit", "workspace.search":
			defs[i].Description += "; absolute paths on any drive are accepted and missing parent directories are created (full-disk full-access is enabled)"
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
	if e.fullDiskChat(mode) {
		return e.tools.ExecuteUnconfinedStreaming(ctx, session, name, args, false, progress)
	}
	return e.tools.ExecuteStreaming(ctx, toolruntime.Mode(mode), session, name, args, false, progress)
}

func engineToolDefinitions() []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: "workspace.list", Description: "List a controlled session workspace directory", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}`)},
		{Name: "workspace.read", Description: "Read a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "workspace.write", Description: "Atomically write a controlled session workspace file", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
		{Name: "workspace.search", Description: "Search session workspace files for a literal substring or regex; answers path:line: text matches (binary and oversized files skipped)", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","description":"literal substring, or regex when regex=true"},"path":{"type":"string","description":"workspace-relative directory to search (default .)"},"regex":{"type":"boolean"},"max":{"type":"integer","minimum":1,"maximum":200}},"required":["query"],"additionalProperties":false}`)},
		{Name: "workspace.edit", Description: "Anchored edit of a controlled session workspace file: oldText must match exactly once (or pass replaceAll=true) and is replaced by newText; everything else stays untouched", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"oldText":{"type":"string"},"newText":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["path","oldText","newText"],"additionalProperties":false}`)},
		{Name: "todo.write", Description: "Persist the full task checklist for this session (write the complete list every time; at most one item in_progress)", Schema: []byte(`{"type":"object","properties":{"todos":{"type":"array","maxItems":50,"items":{"type":"object","additionalProperties":false,"properties":{"content":{"type":"string","minLength":1,"maxLength":500},"status":{"type":"string","enum":["pending","in_progress","completed"]},"priority":{"type":"string","enum":["high","medium","low"]}},"required":["content"]}}},"required":["todos"],"additionalProperties":false}`)},
		{Name: "command.run", Description: "Run one allowlisted command in the controlled workspace (built-in read-only git/go set plus the user command-policy.json whitelist)", Schema: []byte(`{"type":"object","properties":{"argv":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":16}},"required":["argv"],"additionalProperties":false}`)},
		{Name: "web.fetch", Description: "Fetch one public http(s) URL through the SSRF-pinned transport and return extracted text (title, final URL, body)", Schema: []byte(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)},
		{Name: "web.search", Description: "Search the public web (DuckDuckGo Lite) and return ranked results with titles, URLs and snippets", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string"},"max":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`)},
		{Name: "excel.gen", Description: "Generate an .xlsx workbook (headers, rows and an optional bar/col/line/pie chart over the first two columns) into the session workspace", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative output path ending in .xlsx"},"sheets":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"headers":{"type":"array","items":{"type":"string"}},"rows":{"type":"array","items":{"type":"array","items":{}}},"chart":{"type":"object","additionalProperties":false,"properties":{"type":{"type":"string","enum":["bar","col","line","pie"]},"title":{"type":"string"}}}},"required":["rows"]}}},"required":["path","sheets"],"additionalProperties":false}`)},
		{Name: "excel.parse", Description: "Parse an .xlsx workbook from the session workspace and return sheet names, dimensions and a bounded cell preview as JSON", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{Name: "docx.gen", Description: "Generate a .docx Word document (title plus heading/paragraph/bullet blocks) into the session workspace", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative output path ending in .docx"},"title":{"type":"string"},"blocks":{"type":"array","minItems":1,"maxItems":500,"items":{"type":"object","additionalProperties":false,"properties":{"type":{"type":"string","enum":["heading","paragraph","bullet"]},"text":{"type":"string"}},"required":["text"]}}},"required":["path","title","blocks"],"additionalProperties":false}`)},
		{Name: "pptx.gen", Description: "Generate a .pptx slide deck (title slide content plus title+bullets slides) into the session workspace", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative output path ending in .pptx"},"title":{"type":"string"},"slides":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"object","additionalProperties":false,"properties":{"title":{"type":"string"},"bullets":{"type":"array","maxItems":12,"items":{"type":"string"}}},"required":["title"]}}},"required":["path","title","slides"],"additionalProperties":false}`)},
		{Name: "pdf.gen", Description: "Generate a .pdf report (title plus body paragraphs) into the session workspace; Latin text renders best", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative output path ending in .pdf"},"title":{"type":"string"},"body":{"type":"string"}},"required":["path","title","body"],"additionalProperties":false}`)},
	}
}

// ccToolDefinitions are the six M10 wave-4 computer-control tools. They
// are appended to the model tool list only when the ccapp service is
// wired and the operator enabled the domain (M10-CC-012 keeps them hidden
// otherwise, and the armed emergency latch hides them too). Subagents
// never see them: readOnlyEngineToolDefinitions stays file-read-only and
// runs sub-sessions in FullAccess, which would bypass the confirmation
// gate.
func (e *Engine) ccToolDefinitions() []gateway.ToolDefinition {
	if e.ccctrl == nil {
		return nil
	}
	settings, err := e.ccctrl.GetConfig(context.Background())
	if err != nil || !settings.Enabled || settings.EmergencyStopped {
		return nil
	}
	return []gateway.ToolDefinition{
		{Name: "cc.mouse_move", Description: "Move the mouse cursor to absolute screen pixel coordinates", Schema: []byte(`{"type":"object","properties":{"x":{"type":"integer","minimum":0,"maximum":65535},"y":{"type":"integer","minimum":0,"maximum":65535}},"required":["x","y"],"additionalProperties":false}`)},
		{Name: "cc.mouse_click", Description: "Click the mouse at the current cursor position", Schema: []byte(`{"type":"object","properties":{"button":{"type":"string","enum":["left","right","middle"],"description":"default left"},"clicks":{"type":"integer","minimum":1,"maximum":3,"description":"default 1"}},"additionalProperties":false}`)},
		{Name: "cc.keyboard_type", Description: "Type literal text through synthetic keyboard input (no control characters)", Schema: []byte(`{"type":"object","properties":{"text":{"type":"string","minLength":1,"maxLength":4096}},"required":["text"],"additionalProperties":false}`)},
		{Name: "cc.keyboard_shortcut", Description: "Press one key combination (modifier plus key, e.g. ctrl+s); system-reserved combos are refused", Schema: []byte(`{"type":"object","properties":{"keys":{"type":"array","minItems":1,"maxItems":4,"items":{"type":"string"}}},"required":["keys"],"additionalProperties":false}`)},
		{Name: "cc.screen_capture", Description: "Capture the screen as a PNG image saved into the session workspace", Schema: []byte(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{Name: "cc.get_active_window", Description: "Answer the foreground window title and process name", Schema: []byte(`{"type":"object","properties":{},"additionalProperties":false}`)},
	}
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
		{Name: "skill.invoke", Description: "Invoke one published skill by its skillId (see the [可用技能目录] section for IDs and trigger scenarios); input is the user's request text for the skill", Schema: []byte(`{"type":"object","properties":{"skillId":{"type":"string","description":"skill ULID from the catalog"},"input":{"type":"string","minLength":1,"maxLength":2048,"description":"the user request passed to the skill"}},"required":["skillId","input"],"additionalProperties":false}`)},
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
	if json.Unmarshal(args, &a) != nil || !validCanonicalULID(a.SkillID) || strings.TrimSpace(a.Input) == "" {
		return toolruntime.Result{}, errors.New("invalid skill.invoke arguments")
	}
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
func (e *Engine) mcpToolDefinitions() []gateway.ToolDefinition {
	if e.mcp6Registry == nil {
		return nil
	}
	snapshot := e.mcp6Registry.ReadyToolSnapshot()
	if len(snapshot) == 0 {
		return nil
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
	payload, err := json.Marshal(result.Result)
	if err != nil {
		return "", err
	}
	return string(payload), nil
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

func (e *Engine) runStream(ctx context.Context, id string, state *streamState, p provider.Provider, req gateway.Request, emit EventEmitter, sessionID string, modes ...executionMode) {
	const maxThinkingChunkBytes = 16 * 1024
	const maxThinkingTotalBytes = 256 * 1024
	// 10x 优化：thinking flush 间隔从 16ms 降到 8ms，阈值从 1KB 降到 512B，
	// 让 thinking 内容更快到达前端，用户感知延迟降低 ~50%。
	const thinkingFlushBytes = 512
	const thinkingFlushInterval = 8 * time.Millisecond
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
	err := e.withProviderLease(ctx, p, secretlease.OperationChat, func(op context.Context, credential []byte) (cbErr error) {
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
			if streamErr != nil || len(result.Message.ToolCalls) == 0 {
				break
			}
			req.Messages = append(req.Messages, result.Message)
			// Parallel subagents: same-turn subagent.spawn calls are
			// pre-started (bounded) so independent research subagents
			// overlap; each result is consumed in original call order
			// below, keeping the event stream deterministic.
			subagentFutures := startSubagentFutures(op, e, a, credential, req.Model, sessionID, result.Message.ToolCalls)
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
				if endpointID, mcpTool, isMcp := parseMcpToolName(call.Name); isMcp {
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
					if len(summary) > 4096 {
						summary = summary[:4096]
					}
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
						summary, invokeErr = e.invokeSubagentTool(op, a, credential, req.Model, sessionID, call.Name, call.Arguments)
					}
					if invokeErr != nil {
						summary = invokeErr.Error()
					}
					if len(summary) > 4096 {
						summary = summary[:4096]
					}
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
					if len(summary) > 4096 {
						summary = summary[:4096]
					}
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
					// Model-initiated skill invocation rides the governed
					// skillapp pipeline (never the raw toolruntime switch).
					if call.Name == "skill.invoke" {
						return e.invokeSkillTool(op, mode, sessionID, call.Arguments)
					}
					// P1-2: long-running commands stream bounded output chunks
					// between started and completed. The runtime serializes
					// progress callbacks, so the non-concurrent send closure
					// stays safe.
					if call.Name == "command.run" {
						progress := func(chunk string) {
							_ = send(bridge.Event{Type: bridge.EventToolOutput, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: chunk}})
						}
						return e.executeUserToolStreaming(op, mode, sessionID, call.Name, call.Arguments, progress)
					}
					return e.executeUserTool(op, mode, sessionID, call.Name, call.Arguments)
				}()
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
				if toolErr == nil && r.Artifact != nil {
					if k := r.Artifact.Kind; k == "html" && len([]byte(r.Artifact.Content)) <= 180<<10 {
						toolEvent.Artifact = &bridge.ArtifactEvent{Kind: k, Path: r.Artifact.Path, Content: r.Artifact.Content}
					} else if artifactKindValid(k) {
						toolEvent.Artifact = &bridge.ArtifactEvent{Kind: k, Path: r.Artifact.Path, Content: ""}
					}
				}
				if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: toolEvent}); err != nil {
					return err
				}
				req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
			}
		}
		streamResult = result
		// Step-budget exhaustion: when the 6th step still produced tool
		// calls the loop ends after executing them, and without a final
		// text the user would see a completed stream with no answer.
		// Surface a Chinese notice in both the live stream and the
		// persisted assistant text (same pattern as the 400 fallback).
		if streamErr == nil && len(result.Message.ToolCalls) > 0 && assistantText.Len() == 0 {
			const notice = "（系统提示：本轮工具调用步数已达上限，以上工具已执行完毕。请基于执行结果继续提问，或让我总结当前进展。）\n"
			assistantText.WriteString(notice)
			if sendErr := send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: notice}}); sendErr != nil {
				return sendErr
			}
		}
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
