package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"
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
						switch continueKind {
						case "leadin":
							nudge = gateway.Message{Role: gateway.RoleSystem, Content: "工具已经跑完。用一两句口语把结果说给用户听（天气说出气温和阴晴；打开/写入说出已打开或已写入），不要只说等一下，不要沉默。"}
						case "desktop":
							nudge = desktopContinueNudgeMessage()
						case "incomplete":
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
						switch decideApprovalOutcome(state.companion && companionToolPreapproved(call.Name, e.fullDiskChat(mode)), unattended(op)) {
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
