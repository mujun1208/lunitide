package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

func validateToolCallIDs(calls []llmadapter.ToolCall) error {
	seen := map[string]bool{}
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			return errors.New("empty tool call id")
		}
		if seen[call.ID] {
			return errors.New("duplicate tool call id")
		}
		seen[call.ID] = true
	}
	return nil
}

func writeGUIFallbackResult(send func(bridge.Event) error, req *llmadapter.Request, completedDigests map[string]string, turn *chatTurnCheckpoint, usedTools, usedDesktopTools *bool, fb toolruntime.Result, fbArgs json.RawMessage) error {
	summary := strings.TrimSpace(fb.Output)
	if summary == "" {
		summary = guiFallbackFailResult("屏幕读号失败").Output
	}
	summary = clipToolSummary(summary)
	if len(fbArgs) == 0 {
		fbArgs = json.RawMessage(`{"action":"click"}`)
	}
	callID := "gui-" + ulid.Make().String()
	name := "computer.act"
	digest := argsDigestOrFallback(name, fbArgs)
	if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(name, fbArgs))}}); err != nil {
		return err
	}
	completedDigests[digest] = summary
	if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: summary}}); err != nil {
		return err
	}
	req.Messages = append(req.Messages,
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: callID, Name: name, Arguments: fbArgs}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: callID, Content: summary},
	)
	if len(fb.VisionData) > 0 {
		req.Images = appendCaptureVision(req.Images, fb.VisionMIME, fb.VisionData)
	}
	if strings.Contains(summary, "ok:false") {
		turn.ToolFailed = true
	} else {
		*usedTools = true
		*usedDesktopTools = true
	}
	turn.LastTools = append(turn.LastTools, name)
	return nil
}

// reconcileTurnCheckpointOnStart resolves prior-turn workflow state at the
// start of a new stream. For a fresh (non status/resume) goal it clears any
// dangling PPT/DOCX workflow flags from an interrupted prior turn; for a
// status follow-up or resume it hydrates the new turn from the persisted
// checkpoint so an in-flight PPT/DOCX workflow continues. Extracted verbatim
// from runStream (Q-01') to keep the stream body's complexity bounded.
func (e *Engine) reconcileTurnCheckpointOnStart(sessionID string, turn *chatTurnCheckpoint) error {
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
			if err := e.saveTurnCheckpoint(sessionID, prev); err != nil {
				return err
			}
		}
	}
	if looksLikeStatusFollowUp(turn.Goal) || looksLikeResume(turn.Goal) {
		if prev := e.loadTurnCheckpoint(sessionID); prev.PptActive || prev.DocxActive || prev.Status == turnStatusInterrupted || prev.Status == turnStatusRunning || prev.Continuation != nil {
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
			turn.SkipOfficeResearch = prev.SkipOfficeResearch
			turn.Injected = append([]string{}, prev.Injected...)
			if prev.Continuation != nil {
				cloned := *prev.Continuation
				if len(cloned.OperationRefs) > 0 {
					cloned.OperationRefs = append([]string{}, cloned.OperationRefs...)
				}
				turn.Continuation = &cloned
			}
		}
	}
	return nil
}

func (e *Engine) runStream(ctx context.Context, id string, state *streamState, p provider.Provider, req llmadapter.Request, emit EventEmitter, sessionID string, modes ...executionMode) {
	const maxThinkingChunkBytes = 16 * 1024
	const maxThinkingTotalBytes = 256 * 1024
	var seq uint64
	var sendMu sync.Mutex
	var processTrace messageProcess
	completedToolEvents := make(map[string]bool)
	streamEnded := false
	waitingForApproval := false
	waitingForSpokenInput := false
	var assistantText strings.Builder
	var thinkingText strings.Builder
	var pendingThinking string
	var pendingThinkingSince time.Time
	var lastLiveDraftAt time.Time
	var streamResult llmadapter.Response
	var generationBudget turnGenerationBudget
	var turnArtifacts []SessionArtifact
	turn := chatTurnCheckpoint{Status: turnStatusRunning, StreamID: id, Goal: lastUserChatText(req.Messages)}
	computerTurn := computerExecutionTurn(turn.Goal)
	checkpointErr := e.reconcileTurnCheckpointOnStart(sessionID, &turn)
	seedContinuationFromScope(ctx, &turn)
	pinContinuationIdentity(&turn, continuationIdentity{
		ProviderDeploymentRef: strings.TrimSpace(p.ID),
		ModelRequested:        strings.TrimSpace(req.Model),
		OwnerScope:            ownerScope(sessionID),
		TaskRef:               sessionID,
		CodecVersion:          modelfit.CodecForModel(req.Model, string(p.Protocol)),
	})
	turn.CapabilityWork = capabilityWorkRequest(req) || capabilityWorkTask(turn.Goal) || skillTrialsActive(ctx, sessionID)
	mode := executionModeApproval
	if len(modes) > 0 {
		mode = modes[0]
	}
	rawSend := func(event bridge.Event) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		if event.Type == bridge.EventToolOutput && (streamEnded || (event.Tool != nil && completedToolEvents[event.Tool.CallID])) {
			return nil
		}
		if event.Type == bridge.EventToolCompleted && event.Tool != nil {
			completedToolEvents[event.Tool.CallID] = true
		}
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed || event.Type == bridge.EventCancelled {
			streamEnded = true
		}
		processTrace.capture(event)
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
			// rawSend assigns the sequence and records the process even if
			// the renderer transport fails. Consume once; a later flush must
			// not record/re-emit the same reasoning again.
			pendingThinking = pendingThinking[len(chunk):]
			pendingThinkingSince = time.Now()
			if pendingThinking == "" {
				pendingThinkingSince = time.Time{}
			}
			if err := rawSend(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: chunk}}); err != nil {
				return err
			}
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
		if event.Type == bridge.EventApprovalRequired {
			waitingForApproval = true
		}
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
	if checkpointErr != nil {
		state.cancel()
		_ = send(bridge.Event{Type: bridge.EventFailed, Error: &bridge.StreamError{Code: "STORAGE_UNAVAILABLE", Message: "会话恢复记录暂时不可用，请重试", Retryable: true}})
		e.finishTerminal(id, state)
		return
	}
	scoped, releaseCapability, capabilityErr := e.AcquireCapability(ctx, "llm", "session")
	if capabilityErr != nil {
		state.cancel()
		_ = send(bridge.Event{Type: bridge.EventFailed, Error: &bridge.StreamError{Code: "FORBIDDEN", Message: "对话所需能力已禁用", Retryable: false}})
		e.finishTerminal(id, state)
		return
	}
	defer releaseCapability()
	ctx = scoped
	if state.equipEvent != nil {
		_ = send(bridge.Event{Type: bridge.EventEquip, Equip: state.equipEvent})
	}
	if lead := strings.TrimSpace(state.inviteLead); lead != "" && !strings.HasPrefix(strings.TrimLeft(assistantText.String(), " \n"), lead) {
		notice := lead + "。\n"
		assistantText.WriteString(notice)
		_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: notice}})
	}
	var err error
	usedLocalBrain := false
	if !state.companion && state.brain != "" && state.brain != BrainLunitide {
		if text, note, ok := e.trySessionLocalBrain(ctx, sessionID, turn.Goal, state); ok && ctx.Err() == nil {
			usedLocalBrain = true
			if len(text) > turnGenerationMaxBytes {
				text = truncateUTF8Bytes(text, turnGenerationMaxBytes)
				err = errTurnGenerationBudget
			}
			assistantText.WriteString(text)
			err = errors.Join(err, e.noteLiveTurnDraft(sessionID, &turn, assistantText.String(), &lastLiveDraftAt))
			_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: text}})
		} else if note != "" {
			assistantText.WriteString(note)
			err = e.noteLiveTurnDraft(sessionID, &turn, assistantText.String(), &lastLiveDraftAt)
			_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: note}})
			if len(req.Messages) > 0 && req.Messages[0].Role == llmadapter.RoleSystem {
				req.Messages[0].Content = localBrainFallbackLockHint(note) + req.Messages[0].Content
			}
		}
	}
	if !usedLocalBrain && err == nil {
		rot := &leaseRotateState{}
		deltaSent := false
		origSend := send
		send = func(ev bridge.Event) error {
			if ev.Type == bridge.EventDelta {
				deltaSent = true
			}
			return origSend(ev)
		}
		emitted := func() bool {
			return deltaSent || assistantText.Len() > 0 || thinkingText.Len() > 0
		}
		err = e.withRotatingProviderLease(ctx, p, secretlease.OperationChat, rot, emitted, func(op context.Context, credential []byte) (cbErr error) {
			op = withLeaseRotate(op, p, rot, emitted)
			purpose := continuityScopeFrom(op).Purpose
			if purpose == "" {
				purpose = "chat"
			}
			op = withContinuityScope(op, continuityScope{Owner: ownerScope(sessionID), Task: sessionID, Turn: id, Purpose: purpose})
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
			e.applyExpertCouncil(op, turnBudgetAdapter{Adapter: a, budget: &generationBudget}, credential, req.Model, state.council, &req, state.companion, send)
			state.council = nil
			startOfficeWorkflowsIfNeeded(&req, &turn, send, state.lane, officeTaskContextID(op), turn.CapabilityWork)
			logInjectedGuidance(sessionID, state.companion, req)
			emitInjectedGuidance(send, req, state.lane.Lane)
			seen := map[string]bool{}
			completedDigests := map[string]string{}
			failedDesktopAttempts := map[string]int{}
			var result llmadapter.Response
			var streamErr error
			toolsFallbackUsed := false
			guiFallbackUsed := false
			observedThisTurn := false
			desktopTypeL0Passed := false
			imagesFallbackUsed := false
			usedTools := false
			usedDesktopTools := false
			autoMediaPlayDone := false
			autoDesktopTypeDone := false
			autoLookupDone := false
			autoMediaGenerationDone := false
			autoDesktopOpenDone := false
			autoDesktopObserveDone := false
			nudges := 0
			skillDraftOffered := false
			leadInInjected := false
			spokenGoal := turn.Goal
			if prev := e.loadTurnCheckpoint(sessionID); looksLikeResume(turn.Goal) && strings.TrimSpace(prev.Goal) != "" {
				turn.Goal = prev.Goal
				turn.Injected = append(turn.Injected, prev.Injected...)
			}
			turn.liveProtocol = req.Messages
			if err := e.saveTurnCheckpoint(sessionID, turn); err != nil {
				return err
			}
			toolLoopLimit := maxToolLoopSteps
			if state.companion && !companionDesktopToolLoop(e, sessionID, turn.Goal) {
				toolLoopLimit = companionMaxToolLoopSteps
			}
			if inventoryLookupBlocksPublicWeb(turn.Goal) {
				toolLoopLimit = 2
			}
			toolLoopLimit = capToolLoopLimit(toolLoopLimit, state.lane)
			for step := 0; step < toolLoopLimit; step++ {
				turn.liveProtocol = req.Messages
				if err := e.CheckCapability(op, "llm", "session"); err != nil {
					return err
				}
				if _, err := e.applyQueuedSupplements(op, sessionID, &req, &turn, send, &assistantText); err != nil {
					return err
				}
				stepTextStart := assistantText.Len()
				stepThinkingStart := thinkingText.Len()
				mediaTurn := playbackOnlyGoal(turn.Goal)
				bufferReply := computerTurn || mediaTurn || (state.companion && len(req.Tools) > 0 && !companionLookupCanStream(turn.Goal, req.Messages))
				var stepReply strings.Builder
				result, streamErr = generationBudget.stream(op, a, credential, req, func(d llmadapter.Delta) error {
					if err := e.CheckCapability(op, "llm", "session"); err != nil {
						return err
					}
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
						// Playback acknowledgements must follow tool evidence, never
						// a model's speculative success text streamed before the action.
						if bufferReply {
							stepReply.WriteString(d.Text)
							return nil
						}
						assistantText.WriteString(d.Text)
						if err := e.noteLiveTurnDraft(sessionID, &turn, assistantText.String(), &lastLiveDraftAt); err != nil {
							return err
						}
						if err := sendDeltaChunks(send, d.Text); err != nil {
							return err
						}
					}
					return nil
				})
				if bufferReply && stepReply.Len() > 0 {
					result.Message.Content = stepReply.String()
				}
				if len(result.Message.ToolCalls) > 0 {
					if err := e.CheckCapability(op, "agent-loop"); err != nil {
						return err
					}
				}
				if !bufferReply && streamErr == nil && state.companion && req.DisableReasoning && assistantText.Len() == 0 && len(result.Message.ToolCalls) == 0 {
					if fallback := companionSpeakFallback(result); fallback != "" {
						assistantText.WriteString(fallback)
						if err := sendDeltaChunks(send, fallback); err != nil {
							return err
						}
					}
				}
				var gatewayErr *llmadapter.Error
				if streamErr != nil && !imagesFallbackUsed && assistantText.Len() == stepTextStart && thinkingText.Len() == stepThinkingStart && len(req.Images) > 0 && errors.As(streamErr, &gatewayErr) && gatewayErr.HTTPStatus == 400 && imageUnsupportedReason(gatewayErr.Message) {
					if text, ok := e.maybeDescribeImages(op, provider.Model{ModelID: req.Model}, req.Images, chatRoutingText(lastUserContent(req.Messages))); ok {
						req.Messages = injectVisionDescription(req.Messages, text)
					} else {
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleSystem, Content: "当前模型拒绝图片，配置的 OCR/视觉模型也未能完成识别。画面未被读取，不得猜测图片或视频帧内容。请简洁说明识别失败。"})
					}
					req.Images = nil
					imagesFallbackUsed = true
					continue
				}
				if streamErr != nil && !toolsFallbackUsed && !unattended(op) && !skillTrialsActive(op, sessionID) && assistantText.Len() == 0 && thinkingText.Len() == 0 && len(req.Tools) > 0 && errors.As(streamErr, &gatewayErr) && gatewayErr.HTTPStatus == 400 && gatewayErr.Stage != llmadapter.StageStream {
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
					log.Printf("chat stream %s model_call=%d failed: %s received_text_bytes=%d received_thinking_bytes=%d", id, step+1, chatModelFailureDiagnostic(streamErr), assistantText.Len()-stepTextStart+stepReply.Len(), thinkingText.Len()-stepThinkingStart)
					if bufferReply && stepReply.Len() > 0 {
						assistantText.WriteString(stepReply.String())
						_ = sendDeltaChunks(send, stepReply.String())
					}
					break
				}
				if state.companion {
					for _, call := range result.Message.ToolCalls {
						if call.Name != "user.ask" {
							continue
						}
						// A spoken question ends this turn normally. Do not open an
						// invisible approval or execute sibling tools before the answer.
						waitingForSpokenInput = true
						question := companionSpokenQuestion(call.Arguments)
						if next, delta := appendAssistantNotice(assistantText.String(), question); delta != "" {
							assistantText.Reset()
							assistantText.WriteString(next)
							return sendDeltaChunks(send, delta)
						}
						return nil
					}
				}
				if len(result.Message.ToolCalls) == 0 {
					if state.companion && companionNeedsSpokenInput(result.Message.Content) {
						waitingForSpokenInput = true
						if bufferReply {
							assistantText.WriteString(result.Message.Content)
							return sendDeltaChunks(send, result.Message.Content)
						}
						return nil
					}
					if !autoLookupDone && !turnAttemptedAction(req.Messages, "lookup") && looksLikeCurrentLookupTurn(turn.Goal) {
						if inventoryLookupBlocksPublicWeb(turn.Goal) && toolDefinitionsHave(req.Tools, "mcp.search") {
							if searchArgs := fallbackMcpSearchArgs(turn.Goal); len(searchArgs) > 0 {
								result.Message.ToolCalls = []llmadapter.ToolCall{{
									ID:        "auto-" + ulid.Make().String(),
									Name:      "mcp.search",
									Arguments: searchArgs,
								}}
								autoLookupDone = true
							}
						} else if laneAllowsWebSearch(state.lane) && toolDefinitionsHave(req.Tools, "web.search") {
							if searchArgs := fallbackWebSearchArgs(turn.Goal); len(searchArgs) > 0 {
								result.Message.ToolCalls = []llmadapter.ToolCall{{
									ID:        "auto-" + ulid.Make().String(),
									Name:      "web.search",
									Arguments: searchArgs,
								}}
								autoLookupDone = true
							}
						}
					}
					if len(result.Message.ToolCalls) == 0 && !autoMediaGenerationDone {
						if name := mediaGenerationKind(turn.Goal); name != "" {
							if toolDefinitionsHave(req.Tools, name) && !usedAnyTool(turn.LastTools, name) {
								mediaArgs := fallbackMediaGenerationArgs(turn.Goal)
								if len(mediaArgs) > 0 {
									result.Message.ToolCalls = []llmadapter.ToolCall{{
										ID:        "auto-" + ulid.Make().String(),
										Name:      name,
										Arguments: mediaArgs,
									}}
									autoMediaGenerationDone = true
								}
							}
						}
					}
					if len(result.Message.ToolCalls) == 0 && !autoMediaPlayDone && toolDefinitionsHave(req.Tools, "media.play") && !usedAnyTool(turn.LastTools, "media.play") && (companionShouldAutoMediaPlay(turn.Goal) || companionRetryActionTurn(spokenGoal)) {
						if playArgs, ok := e.companionAutoMediaPlayArgsForTurn(sessionID, turn.Goal, spokenGoal); ok {
							result.Message.ToolCalls = []llmadapter.ToolCall{{
								ID:        "auto-" + ulid.Make().String(),
								Name:      "media.play",
								Arguments: playArgs,
							}}
							autoMediaPlayDone = true
						}
					}
					if len(result.Message.ToolCalls) == 0 && !autoDesktopOpenDone && toolDefinitionsHave(req.Tools, "desktop.open") && !turnAttemptedAction(req.Messages, "open") {
						if openArgs := fallbackDesktopOpenArgs(turn.Goal); len(openArgs) > 0 {
							result.Message.ToolCalls = []llmadapter.ToolCall{{
								ID:        "auto-" + ulid.Make().String(),
								Name:      "desktop.open",
								Arguments: openArgs,
							}}
							autoDesktopOpenDone = true
						}
					}
					if len(result.Message.ToolCalls) == 0 && !autoDesktopTypeDone && toolDefinitionsHave(req.Tools, "desktop.type") && !turnAttemptedAction(req.Messages, "type") && looksLikeTypeAfterLabelTurn(turn.Goal) {
						if typeArgs, ok := e.companionAutoDesktopTypeArgs(sessionID, turn.Goal); ok {
							result.Message.ToolCalls = []llmadapter.ToolCall{{
								ID:        "auto-" + ulid.Make().String(),
								Name:      "desktop.type",
								Arguments: typeArgs,
							}}
							autoDesktopTypeDone = true
						}
					}
					if len(result.Message.ToolCalls) == 0 && !autoDesktopObserveDone && toolDefinitionsHave(req.Tools, "computer.act") && !turnAttemptedAction(req.Messages, "observe") && looksLikeDesktopObserveTurn(turn.Goal) {
						result.Message.ToolCalls = []llmadapter.ToolCall{{
							ID:        "auto-" + ulid.Make().String(),
							Name:      "computer.act",
							Arguments: autoDesktopObserveArgs(),
						}}
						autoDesktopObserveDone = true
					}
				}
				if mediaTurn && len(result.Message.ToolCalls) == 0 {
					text := mediaTurnResultSpeech(req.Messages)
					result.Message.Content = text
				}
				stepText := ""
				if assistantText.Len() > stepTextStart {
					stepText = assistantText.String()[stepTextStart:]
				}
				if bufferReply {
					stepText = result.Message.Content
				}
				noteDocxChars(&turn, stepText)
				if len(result.Message.ToolCalls) == 0 {
					toolOut := lastToolOutput(req.Messages)
					continueKind := pickTurnContinueKind(stepText, assistantText.String(), toolOut, turn.LastTools, usedTools, usedDesktopTools, state.companion, req.DisableReasoning, nudges, turn.Goal, len(req.Tools) > 0)
					if !laneAllowsContinueNudges(state.lane) && !(continueKind == "desktop" && laneAllowsDesktopContinue(state.lane)) {
						continueKind = ""
					}
					if state.lane.Lane == LaneL2 && continueKind != "" && continueKind != "incomplete" {
						continueKind = ""
					}
					if continueKind == "desktop" && companionBrowserLookupSettled(turn.Goal, stepText, req.Messages) {
						continueKind = ""
					}
					if (state.companion || computerTurn) && continueKind == "desktop" && computerReceiptCloseout(req.Messages, turn.Goal) != "" {
						continueKind = ""
					}
					if continueKind == "" && !state.companion && !skillDraftOffered && shouldOfferSkillDraft(turn.LastTools) {
						skillDraftOffered = true
						msg := result.Message
						if strings.TrimSpace(msg.Content) == "" {
							msg.Role = llmadapter.RoleAssistant
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
							msg.Role = llmadapter.RoleAssistant
							msg.Content = stepText
						}
						nudge := continueNudgeMessage()
						switch continueKind {
						case "leadin":
							nudge = llmadapter.Message{Role: llmadapter.RoleSystem, Content: "根据本轮工具证据，用一两句报告结果和未完成部分。命令发送、磁盘写入、窗口更新是不同证据，不能混为一谈。不要只说稍等，也不要重复过程。"}
						case "desktop":
							nudge = desktopContinueNudgeMessage()
						case "incomplete":
							nudge = incompleteContinueNudgeMessage()
						case "wait":
							nudge = llmadapter.Message{Role: llmadapter.RoleSystem, Content: "立刻调用本轮已装备的工具执行。不要再承诺稍等。下一句必须是结果或无法执行。"}
						}
						if bufferReply {
							nudge.Content += "\n前面的回答草稿尚未发送给用户。最终回答必须直接给出实质结果，不能只说上面已经给出或不用重复。"
						}
						req.Messages = append(req.Messages, msg, nudge)
						continue
					}
					if bufferReply {
						stepText = companionFinalResult(req.Messages, stepText, turn.Goal)
						if looksLikeCompanionWaitPromise(stepText) || isCompanionLeadInOnly(stepText) {
							stepText = "这轮任务未完成，没有取得可验证的结果。"
						}
						result.Message.Content = stepText
						assistantText.WriteString(stepText)
						if err := sendDeltaChunks(send, stepText); err != nil {
							return err
						}
					}
					if continueKind == "" && state.companion && !usedTools &&
						(looksLikeCompanionWaitPromise(assistantText.String()) || isCompanionLeadInOnly(assistantText.String())) &&
						(len(req.Tools) > 0 || companionShouldAutoMediaPlay(turn.Goal) || companionRetryActionTurn(spokenGoal) || companionWantsDesktopControl(spokenGoal)) {
						close := companionStuckLeadInSpeech(turn.Goal, spokenGoal)
						assistantText.WriteString(close)
						if err := sendDeltaChunks(send, close); err != nil {
							return err
						}
						break
					}
					if continueKind == "" && state.companion && usedTools && isCompanionLeadInOnly(assistantText.String()) {
						close := companionToolResultSpeech(lastToolName(turn.LastTools), lastToolOutput(req.Messages))
						assistantText.WriteString(close)
						if err := sendDeltaChunks(send, close); err != nil {
							return err
						}
					}
					note, _, queueErr := e.pullQueuedSupplements(op, sessionID, &turn)
					if queueErr != nil {
						return queueErr
					}
					if note != "" {
						msg := result.Message
						if strings.TrimSpace(msg.Content) == "" && stepText != "" {
							msg.Role = llmadapter.RoleAssistant
							msg.Content = stepText
						}
						if msg.Role != "" {
							req.Messages = append(req.Messages, msg)
						}
						req.Messages = append(req.Messages, queuedSupplementMessage(note))
						assistantText.WriteString(queueInjectNotice)
						_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: "已收到你的补充，继续当前任务，不另起炉灶。\n"}})
						_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: queueInjectNotice}})
						continue
					}
					if shouldStartOfficeResearch(state.lane, officeTaskContextID(op), turn.CapabilityWork) {
						if nudgePptWorkflow(&req, &turn, send) {
							if err := e.saveTurnCheckpointAfterModel(sessionID, &turn, result, thinkingText.String(), req.DisableReasoning, req.Messages); err != nil {
								return err
							}
							continue
						}
						if nudgeDocxWorkflow(&req, &turn, send) {
							if err := e.saveTurnCheckpointAfterModel(sessionID, &turn, result, thinkingText.String(), req.DisableReasoning, req.Messages); err != nil {
								return err
							}
							continue
						}
					}
					break
				}
				usedTools = true
				// E4 adaptive ceiling: a non-companion turn that keeps making
				// real tool calls near its limit gets more room instead of a
				// silent mid-batch truncation. Companion (voice) turns keep
				// their fixed budget so a spoken reply never runs long.
				if !state.companion && !inventoryLookupBlocksPublicWeb(turn.Goal) && laneMayExtendToolLoop(state.lane) {
					toolLoopLimit = extendToolLoopLimit(toolLoopLimit, step)
				}
				for _, call := range result.Message.ToolCalls {
					if isDesktopControlTool(call.Name) {
						usedDesktopTools = true
						if !inventoryLookupBlocksPublicWeb(turn.Goal) && toolLoopLimit < maxToolLoopSteps && laneAllowsDesktopContinue(state.lane) {
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
				if err := validateToolCallIDs(result.Message.ToolCalls); err != nil {
					return err
				}
				// Parallel subagents: same-turn subagent.spawn calls are
				// pre-started (bounded) so independent research subagents
				// overlap; each result is consumed in original call order
				// below, keeping the event stream deterministic.
				// Progress may arrive from several agents while the first result is
				// still pending. Only rawSend is goroutine-safe; send also owns
				// the main model's pending thinking buffer.
				subagentDigests := make(map[string]string)
				for _, call := range result.Message.ToolCalls {
					if call.Name == "subagent.spawn" {
						subagentDigests[call.ID] = argsDigestOrFallback(call.Name, call.Arguments)
					}
				}
				subagentFutures := startSubagentFutures(op, e, a, credential, req.Model, sessionID, result.Message.ToolCalls, state.subagentPolicy, func(callID string, progress subagentProgress) {
					_ = rawSend(bridge.Event{Type: bridge.EventToolOutput, Tool: &bridge.ToolEvent{CallID: callID, Name: "subagent.spawn", ArgsDigest: subagentDigests[callID], Summary: progress.JSONSummary()}})
				})
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
				parkedBrowserWall := ""
				lastGUIFail := false
				for _, call := range result.Message.ToolCalls {
					if seen[call.ID] {
						return errors.New("duplicate tool call id")
					}
					seen[call.ID] = true
					call.Arguments = officeBoundToolArgs(op, call.Name, call.Arguments)
					if call.Name == "media.play" {
						call.Arguments = mediaArgsForGoal(turn.Goal, call.Arguments)
					}
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
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					skipSummary, skip := duplicateToolSkipSummary(digest, completedDigests)
					if skip && (isSkillInvocationTool(call.Name) || isDesktopControlTool(call.Name)) {
						skipSummary = completedDigests[digest]
					}
					if !skip && call.Name == "desktop.browse" {
						skipSummary = reuseBrowserSearchEntry(turn.Goal, call.Arguments, req.Messages)
						skip = skipSummary != ""
					}
					if skip && !liveDesktopObservation(call.Name, call.Arguments) {
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
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: skipSummary})
						continue
					}
					if err := send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: clipToolSummary(toolStartedSummary(call.Name, call.Arguments))}}); err != nil {
						return err
					}
					log.Printf("chat stream %s tool start name=%s", id, call.Name)
					// The branches below dispatch without going through the tool
					// runtime, so toolruntime's approval gate never sees them.
					if reason, deny := ungatedEngineToolDenied(mode, state.companion, call.Name, call.Arguments); deny {
						summary := clipToolSummary(reason)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
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
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if call.Name == "mcp.search" {
						if reason, deny := e.denyRestrictedMCP(call.Name, call.Arguments, state.mcpRestrict, state.mcpAllowed); deny {
							summary := clipToolSummary(reason)
							if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
								return err
							}
							req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
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
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if call.Name == "mcp.call" {
						if reason, deny := e.denyRestrictedMCP(call.Name, call.Arguments, state.mcpRestrict, state.mcpAllowed); deny {
							summary := clipToolSummary(reason)
							if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
								return err
							}
							req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
							continue
						}
						summary, invokeErr := e.callMcpToolByNameGuarded(op, sessionID, call.Arguments, state.mcpAllowed, state.mcpRestrict)
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if endpointID, mcpTool, isMcp := parseMcpToolName(call.Name); isMcp {
						if reason, deny := e.denyRestrictedMCP(call.Name, call.Arguments, state.mcpRestrict, state.mcpAllowed); deny {
							summary := clipToolSummary(reason)
							if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
								return err
							}
							req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
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
							summary, invokeErr = e.invokeMcpTool(op, sessionID, endpointID, mcpTool, call.Arguments)
						}
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
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
						displaySummary := subagentDisplaySummary(call.Name, summary)
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: displaySummary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					if planToolNames[call.Name] {
						summary, invokeErr := e.invokePlanRunToolRouted(op, a, credential, req.Model, sessionID, mode, call.Arguments, state.taskRoute)
						if invokeErr != nil {
							summary = invokeErr.Error()
						}
						summary = clipToolSummary(summary)
						if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: summary}}); err != nil {
							return err
						}
						req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary})
						continue
					}
					r, toolErr := func() (toolruntime.Result, error) {
						op := toolruntime.WithExecutionKey(op, sessionID, call.ID)
						if desktopMutationRetryBlocked(failedDesktopAttempts, call.Name, call.Arguments) {
							return toolruntime.Result{}, errors.New("无法执行：相同目标和参数已失败，未重复操作。请重新核对目标或改用已验证的路径。")
						}
						if err := guardCurrentTurnToolHistory(turn.Goal, call.Name, req.Messages); err != nil {
							return toolruntime.Result{}, err
						}
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
							return e.invokeMediaGenerate(op, sessionID, call.Name, call.Arguments)
						}
						// Model-initiated skill invocation rides the governed
						// skillapp pipeline (never the raw toolruntime switch).
						if call.Name == "skill.invoke" {
							return e.invokeSkillTool(op, mode, sessionID, call.Arguments)
						}
						if call.Name == "skill.try" {
							return e.invokeSkillTrialTool(op, mode, sessionID, call.Arguments)
						}
						if call.Name == "skill.view" {
							return e.invokeSkillViewTool(op, call.Arguments)
						}
						// Model-initiated expert creation routes through the
						// M8 expert service (never the raw toolruntime switch).
						if call.Name == "skill.create" {
							return e.invokeSkillCreateTool(withSkillCreationSession(op, sessionID), sessionID, call.Arguments)
						}
						if call.Name == "skill.manage" {
							return e.invokeSkillManageTool(withSkillCreationSession(op, sessionID), sessionID, call.Arguments)
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
						if officeManagedBypass(call.Name, officeTaskContextID(op), &turn, call.Arguments) {
							return toolruntime.Result{Output: "ok:false\n" + officeGenInternalHint + "立刻调用 office.generate 或对应 *.gen，不要 command.run 或 workspace.write。"}, nil
						}
						if call.Name == "command.run" || call.Name == "run_terminal_cmd" {
							progress := func(chunk string) {
								if chunk == "" {
									return
								}
								// Progress may arrive asynchronously while the tool drains
								// output. Only the stream loop owns pending thinking state.
								event := bridge.Event{Type: bridge.EventToolOutput, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: chunk}}
								sanitizeOutgoingEvent(&event)
								_ = rawSend(event)
							}
							return e.executeUserToolWithCompanion(op, mode, sessionID, call.Name, call.Arguments, progress, state.companion)
						}
						return e.executeUserToolWithCompanion(op, mode, sessionID, call.Name, call.Arguments, nil, state.companion)
					}()
					if errors.Is(toolErr, toolruntime.ErrApprovalRequired) {
						switch decideApprovalOutcome(state.companion && companionToolPreapproved(call.Name, e.fullDiskChat(mode), e.companionCcEnabled(op)), unattended(op)) {
						case approvalPreapproved:
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
						case approvalDenyUnattended:
							// Headless turn: refuse in place and let the loop
							// continue, instead of emitting an approval that the
							// noop emitter drops and no one can ever grant.
							r = toolruntime.Result{}
							toolErr = errors.New(unattendedApprovalDenial(call.Name))
						default:
							if _, prepareErr := e.tools.Prepare(op, id, sessionID, call.ID, call.Name, call.Arguments, toolruntime.Mode(mode), 10*time.Minute); prepareErr != nil {
								return prepareErr
							}
							if sendErr := send(bridge.Event{Type: bridge.EventApprovalRequired, Tool: &bridge.ToolEvent{CallID: call.ID, Name: call.Name, ArgsDigest: digest, Summary: approvalRequiredSummary(call.Name, call.Arguments)}}); sendErr != nil {
								return sendErr
							}
							return nil
						}
					}
					fullSkillOutput := ""
					if toolErr == nil && isDesktopControlTool(call.Name) && companionToolResultFailed(r.Output) {
						toolErr = errors.New(r.Output)
					}
					if desktopMutation(call.Name, call.Arguments) {
						key := desktopAttemptKey(call.Name, call.Arguments)
						proof, hasProof := extractL0(r.Output)
						if toolErr != nil || companionToolResultFailed(r.Output) || (hasProof && (!proof.Passed || proof.Uncertain)) {
							summary := r.Output
							if toolErr != nil {
								summary = toolErr.Error()
							}
							recordDesktopMutationFailure(failedDesktopAttempts, call.Name, call.Arguments, summary)
						} else {
							delete(failedDesktopAttempts, key)
						}
					}
					if toolErr == nil && isSkillInvocationTool(call.Name) {
						if fitErr := skillInvocationFitsContext(p, req, r.Output); fitErr != nil {
							toolErr = fitErr
						} else {
							fullSkillOutput = r.Output
						}
					}
					if toolErr == nil && call.Name == "skill.view" {
						assembled := e.assembleSkillViewForModel(op, call.Arguments, r.Output)
						if fitErr := skillInvocationFitsContext(p, req, assembled); fitErr != nil {
							toolErr = fitErr
						} else {
							fullSkillOutput = assembled
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
					summary = clipExecutionToolSummary(call.Name, summary)
					modelSummary := summary
					if fullSkillOutput != "" {
						modelSummary = fullSkillOutput
						summary = skillInvocationDisplay(fullSkillOutput)
					}
					if toolErr == nil {
						completedDigests[digest] = summary
						if fullSkillOutput != "" {
							completedDigests[digest] = skillInvocationReplay(fullSkillOutput, call.ID, call.Name)
						}
						if call.Name == "pptx.gen" && !strings.Contains(summary, "被流水线拦住") {
							turn.PptGenerated = true
						}
						if call.Name == "docx.gen" && !strings.Contains(summary, "被流水线拦住") {
							turn.DocxGenerated = true
						}
						if !companionToolResultFailed(summary) {
							// Count successful executions, not unique tool names or
							// the accumulated attempt list replayed at every step.
							notePptTools(&turn, []string{call.Name})
							noteDocxTools(&turn, []string{call.Name})
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
							turnArtifacts = append(turnArtifacts, sessionArtifactFromTool(call.ID, call.Name, toolEvent.Artifact.Kind, toolEvent.Artifact.Path, officeTaskContextID(ctx)))
						}
					}
					if err := send(bridge.Event{Type: bridge.EventToolCompleted, Tool: toolEvent}); err != nil {
						return err
					}
					req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: modelSummary})
					if errors.Is(toolErr, errSkillContextBudget) {
						return toolErr
					}
					if len(r.VisionData) > 0 {
						req.Images = appendCaptureVision(req.Images, r.VisionMIME, r.VisionData)
					}
					if toolErr == nil && computerActIsObserve(call.Name, call.Arguments) {
						observedThisTurn = true
					}
					if looksLikeFilePickerToolResult(summary) {
						parkedFilePicker = true
					}
					if looksLikeUACToolResult(summary) {
						parkedUAC = true
					}
					if reason := browserWallReason(summary); reason != "" {
						parkedBrowserWall = reason
					}
					if desktopTypePassedL0(call.Name, summary) {
						desktopTypeL0Passed = true
					}
					lastGUIFail = noteDesktopGUIFail(call.Name, summary, toolErr, lastGUIFail)
				}
				if lastGUIFail && !guiFallbackUsed && !parkedFilePicker && !parkedUAC && parkedBrowserWall == "" {
					if fb, fbArgs, used := e.tryGUIFallback(op, mode, sessionID, turn.Goal, req.Model, state, req.Images, guiFallbackUsed, desktopTypeL0Passed, observedThisTurn); used {
						guiFallbackUsed = true
						if err := writeGUIFallbackResult(send, &req, completedDigests, &turn, &usedTools, &usedDesktopTools, fb, fbArgs); err != nil {
							return err
						}
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
				if parkedBrowserWall != "" {
					if err := e.parkBrowserWallAsk(op, id, sessionID, mode, parkedBrowserWall, send); err != nil {
						return err
					}
					return nil
				}
				if companionShouldAutoMediaPlay(turn.Goal) || companionRetryActionTurn(spokenGoal) {
					hasMediaPlay := autoMediaPlayDone
					for _, call := range result.Message.ToolCalls {
						if call.Name == "media.play" {
							hasMediaPlay = true
							break
						}
					}
					if !hasMediaPlay {
						if playArgs, ok := e.companionAutoMediaPlayArgsForTurn(sessionID, turn.Goal, spokenGoal); ok {
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
									llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: callID, Name: name, Arguments: playArgs}}},
									llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: callID, Content: summary},
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
									llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: callID, Name: name, Arguments: typeArgs}}},
									llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: callID, Content: summary},
								)
								turn.LastTools = append(turn.LastTools, name)
							}
						}
					}
				}
				for _, call := range result.Message.ToolCalls {
					turn.LastTools = append(turn.LastTools, call.Name)
				}
				if draft := strings.TrimSpace(assistantText.String()); draft != "" {
					turn.PersistDraft = draft
				}
				if err := e.saveTurnCheckpointAfterModel(sessionID, &turn, result, thinkingText.String(), req.DisableReasoning, req.Messages); err != nil {
					return err
				}
			}
			streamResult = result
			// Step-budget exhaustion: when the last step still produced tool
			// calls the loop ends after executing them, and without a final
			// text the user would see a completed stream with no answer.
			// Surface a Chinese notice in both the live stream and the
			// persisted assistant text (same pattern as the 400 fallback).
			if streamErr == nil {
				// UX-05 #3: forced end-of-turn summary. A multi-tool / multi-round
				// loop that exhausts its step budget with tool calls still pending
				// and no final text used to finish silently (or with a canned
				// notice). Run one more pass WITHOUT tools so the model must wrap
				// up in natural language; fall through to the static notice only
				// if that pass also yields nothing.
				if assistantText.Len() == 0 && len(result.Message.ToolCalls) > 0 && (companionShouldAutoMediaPlay(turn.Goal) || companionRetryActionTurn(spokenGoal)) {
					text := mediaTurnResultSpeech(req.Messages)
					assistantText.WriteString(text)
					if err := sendDeltaChunks(send, text); err != nil {
						return err
					}
				}
				if assistantText.Len() == 0 && len(result.Message.ToolCalls) > 0 {
					sumReq := req
					sumReq.Tools = nil
					sumReq.Messages = append(append([]llmadapter.Message{}, req.Messages...), result.Message, forceSummaryNudgeMessage())
					sumRes, sumErr := generationBudget.stream(op, a, credential, sumReq, func(d llmadapter.Delta) error {
						if d.Text != "" {
							assistantText.WriteString(d.Text)
							if err := e.noteLiveTurnDraft(sessionID, &turn, assistantText.String(), &lastLiveDraftAt); err != nil {
								return err
							}
							if err := sendDeltaChunks(send, d.Text); err != nil {
								return err
							}
						}
						return nil
					})
					if errors.Is(sumErr, errTurnGenerationBudget) || chatModelCompletionFailed(sumErr) {
						streamErr = sumErr
					}
					if sumErr == nil {
						if assistantText.Len() == 0 && state.companion && req.DisableReasoning {
							if fallback := companionSpeakFallback(sumRes); fallback != "" {
								assistantText.WriteString(fallback)
								if err := sendDeltaChunks(send, fallback); err != nil {
									return err
								}
							}
						}
					}
				}
				notice := createTurnClosingNotice(turn.LastTools, assistantText.String())
				if turn.ToolFailed && (assistantText.Len() == 0 || isCompanionLeadInOnly(assistantText.String())) {
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
			return streamErr
		})
	}
	// Publish one aggregate for the complete turn, including council passes,
	// tool continuations and forced summaries. The budget counts each model
	// call once regardless of repeated provider usage frames.
	if u := generationBudget.usageSnapshot(); u.TotalTokens > 0 || u.CacheUsageReported {
		usage := &bridge.UsageEvent{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens, CachedInputTokens: u.CachedInputTokens, CacheWriteInputTokens: u.CacheWriteInputTokens, CacheUsageReported: u.CacheUsageReported}
		if sendErr := send(bridge.Event{Type: bridge.EventUsage, Usage: usage}); sendErr != nil {
			err = errors.Join(err, sendErr)
		}
	}
	// Successful upstream completion must claim finalization before any durable
	// side effect. This is the linearization point against stream.cancel.
	// Notices appended below are not model reply text; thinking is stored only
	// when the turn failed with an empty model reply.
	if errors.Is(err, errTurnGenerationBudget) {
		assistantText.WriteString(turnGenerationBudgetNotice)
		_ = sendDeltaChunks(send, turnGenerationBudgetNotice)
	}
	modelReply := strings.TrimSpace(assistantText.String())
	var messageID string
	finalizationClaimed := false
	if err == nil {
		finalizationClaimed = e.claimStreamFinalization(state)
	}
	if !waitingForApproval && !waitingForSpokenInput && !e.isStreamCancelling(state) {
		finished, notice := false, ""
		if !chatModelCompletionFailed(err) {
			finished, notice = e.tryFinishOfficeGen(ctx, mode, sessionID, &turn, assistantText.String(), err, func(event bridge.Event) error {
				if event.Tool != nil && event.Tool.Artifact != nil && event.Type == bridge.EventToolCompleted {
					a := event.Tool.Artifact
					turnArtifacts = append(turnArtifacts, sessionArtifactFromTool(event.Tool.CallID, event.Tool.Name, a.Kind, a.Path, officeTaskContextID(ctx)))
				}
				return send(event)
			}, state.companion)
		}
		if finished || notice != "" {
			if !finished && err != nil && notice == officeGenFailNotice(errOfficeGenEmpty) {
				// Missing model output after a stream failure is not missing user input.
				notice = chatModelOutcomeNotice(false, err)
			}
			if finished {
				err = nil
				// A normal model completion already owns finalization. Claim only
				// when the fallback recovered an earlier upstream failure.
				if !finalizationClaimed {
					finalizationClaimed = e.claimStreamFinalization(state)
				}
			}
			if next, delta := appendAssistantNotice(assistantText.String(), notice); delta != "" {
				assistantText.Reset()
				assistantText.WriteString(next)
				_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: delta}})
			}
		} else if outcome := chatModelOutcomeNotice(e.isStreamCancelling(state), err); outcome != "" {
			if next, delta := appendAssistantNotice(assistantText.String(), outcome); delta != "" {
				assistantText.Reset()
				assistantText.WriteString(next)
				_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: delta}})
			}
		}
	}

	cancelling := e.isStreamCancelling(state)
	upstreamErr := err
	attachObtainedProtocol(&turn, obtainedProtocolReasoning(streamResult, thinkingText.String(), req.DisableReasoning))
	var persistErr error
	if sessionID != "" && e.messages != nil {
		// Reasoning is available through message.process, never model history.
		text := assistantTurnPersistText(assistantText.String(), "", false)
		if state != nil {
			text = pinCouncilInviteLead(text, state.inviteLead)
		}
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
		if cancelling && !persist {
			// Cancellation deliberately won before finalization (or no spoken
			// companion text remains). Do not let the live recovery draft undo
			// that decision on the next start. Failed writes still retain drafts.
			turn.PersistDraft = ""
			turn.PersistUsage = messageapp.AssistantUsage{}
			turn.PersistFailed = false
		}
		if persist {
			if next, delta := applyMROAnswerGate(text, state); next != text {
				text = next
				if delta != "" {
					assistantText.WriteString(delta)
					_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: delta}})
				}
			}
			usageSrc := generationBudget.usageSnapshot()
			if usageSrc.TotalTokens == 0 && !usageSrc.CacheUsageReported {
				usageSrc = streamResult.Usage
			}
			usage := persistUsageFromStream(string(p.Protocol), req.Model, usageSrc)
			turn.PersistDraft, turn.PersistUsage = text, usage
			turn.liveProtocol = req.Messages
			journalErr := e.saveTurnCheckpoint(sessionID, turn)
			var msg message.Message
			appendErr := journalErr
			if appendErr == nil {
				msg, appendErr = e.appendAssistantTurn(ctx, id, "engine", sessionID, text, usage)
			}
			if appendErr != nil {
				persistErr = appendErr
				turn.PersistDraft = text
				turn.PersistFailed = true
			} else {
				messageID = msg.ID
				turn.PersistDraft = ""
				turn.PersistFailed = false
				for i := range turnArtifacts {
					turnArtifacts[i].OfficeTaskID = officeTaskContextID(ctx)
				}
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
	turn.liveProtocol = req.Messages
	if journalErr := e.saveTurnCheckpoint(sessionID, turn); journalErr != nil {
		persistErr = errors.Join(persistErr, journalErr)
	}
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
	}
	if terminal.Type == bridge.EventFailed {
		terminal.Error = chatModelStreamError(err)
	}
	if e.queue != nil && e.queue.Deliveries() != nil && (len(turn.QueueDeliveries) > 0 || ctx.Value(queueDeliveryStartKey{}) != nil) {
		receiptCtx, receiptCancel := turnJournalContext()
		receiptErr := e.queue.Deliveries().FinishQueueDeliveries(receiptCtx, sessionID, id, terminal.Type == bridge.EventCompleted && persistErr == nil)
		receiptCancel()
		if receiptErr != nil {
			terminal = bridge.Event{Type: bridge.EventFailed, Error: chatStreamError(receiptErr)}
		}
	}
	if messageID != "" {
		// Flush any final thinking and freeze a detached metadata snapshot before
		// the terminal receipt; this is never appended to the model's history.
		_ = flushThinking(true)
		sendMu.Lock()
		snapshot := processTrace
		snapshot.Tools = append([]messageProcessTool(nil), processTrace.Tools...)
		sendMu.Unlock()
		e.saveMessageProcess(sessionID, messageID, snapshot)
	}
	if send(terminal) != nil {
		state.cancel()
	}
	e.finishTerminal(id, state)
	if len(turnArtifacts) > 0 && messageID != "" && persistErr == nil {
		e.archiveOfficeTurn(ctx, sessionID)
	}
	if terminal.Type == bridge.EventCompleted && messageID != "" && persistErr == nil {
		e.enqueueChatMemory(sessionID, turn.Goal, assistantText.String(), messageID, state != nil && state.companion)
	}
}
