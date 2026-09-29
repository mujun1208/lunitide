package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
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
			if strings.TrimSpace(prev.PersistDraft) != "" {
				turn.PersistDraft = prev.PersistDraft
			}
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
