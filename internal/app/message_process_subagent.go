package app

import (
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
)

func subagentProcessTool(name string) bool {
	return name == "subagent.spawn" || name == "subagent.join"
}

// Only structured lifecycle updates may enter process history through
// tool_output. Ordinary streaming tool output keeps its existing behavior.
func parseProcessSubagent(raw string, progressOnly bool) (subagentProgress, bool) {
	var wire struct {
		subagentProgress
		SubagentID string `json:"subagentId"`
		Summary    string `json:"summary"`
	}
	if len(raw) > 64*1024 || json.Unmarshal([]byte(raw), &wire) != nil {
		return subagentProgress{}, false
	}
	if progressOnly && wire.Marker != "subagent_progress" {
		return subagentProgress{}, false
	}
	if wire.ID != "" && wire.SubagentID != "" && wire.ID != wire.SubagentID {
		return subagentProgress{}, false
	}
	if wire.ID == "" {
		wire.ID = wire.SubagentID
	}
	if !message.CanonicalULID(wire.ID) {
		return subagentProgress{}, false
	}
	switch wire.Status {
	case "running", "completed", "failed", "cancelled":
	default:
		return subagentProgress{}, false
	}
	switch wire.Stage {
	case "", "starting", "thinking", "searching", "tool", "completed", "failed", "cancelled":
	default:
		wire.Stage = ""
	}
	if wire.Step < 0 {
		wire.Step = 0
	}
	if strings.TrimSpace(wire.Summary) != "" {
		wire.Detail = wire.Summary
	}
	return wire.subagentProgress, true
}

// Identity/status are never clipped. The existing progress encoder trims only
// optional display text and serializes whole JSON within 512 UTF-8 bytes.
func subagentProcessSummary(event bridge.Event, prior messageProcessTool) (string, bool) {
	old, hadOld := parseProcessSubagent(prior.Summary, false)
	update, ok := parseProcessSubagent(event.Tool.Summary, event.Type == bridge.EventToolOutput)
	if event.Type == bridge.EventToolOutput && !ok {
		return "", false
	}
	if hadOld {
		if event.Type == bridge.EventToolStarted || event.Type == bridge.EventApprovalRequired {
			return prior.Summary, true
		}
		if !ok || old.ID != update.ID {
			return prior.Summary, true
		}
		if old.Status != "running" && update.Status == "running" {
			return prior.Summary, true
		}
		if update.Profile == "" || old.Profile != "" {
			update.Profile = old.Profile
		}
		if update.Purpose == "" {
			update.Purpose = old.Purpose
		}
	}
	if !ok {
		return "", false
	}
	if update.Status != "running" {
		update.Stage = update.Status
		update.Tool = ""
	}
	return update.JSONSummary(), true
}

// Build the renderer summary from the complete result before the normal model
// context budget clips it. join may have no preceding progress event to restore
// an identity from, so its final JSON must also remain complete on the wire.
func subagentDisplaySummary(name, result string) string {
	if subagentProcessTool(name) {
		progress, ok := parseProcessSubagent(result, false)
		if ok {
			if progress.Status != "running" {
				progress.Stage = progress.Status
				progress.Tool = ""
			}
			return progress.JSONSummary()
		}
	}
	return clipToolSummary(result)
}
