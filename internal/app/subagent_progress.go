package app

import (
	"context"
	"encoding/json"
	"strings"
)

type subagentProgress struct {
	Marker  string `json:"marker"`
	ID      string `json:"id"`
	Profile string `json:"profile,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Status  string `json:"status"`
	Stage   string `json:"stage,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Step    int    `json:"step,omitempty"`
}

// Keep the JSON whole and below the event summary byte limit, including JSON
// escaping. Never clip an encoded JSON string into an unreadable fragment.
func (p subagentProgress) JSONSummary() string {
	p.Marker = "subagent_progress"
	p.ID = truncateUTF8Bytes(p.ID, 26)
	p.Status = truncateUTF8Bytes(p.Status, 16)
	p.Stage = truncateUTF8Bytes(p.Stage, 16)
	p.Profile = truncateUTF8Bytes(p.Profile, 32)
	p.Purpose = truncateUTF8Bytes(p.Purpose, 120)
	p.Tool = truncateUTF8Bytes(p.Tool, 64)
	p.Detail = truncateUTF8Bytes(p.Detail, 96)
	for {
		data, _ := json.Marshal(p)
		if len(data) <= 512 {
			return string(data)
		}
		if len(p.Purpose) > 0 {
			p.Purpose = truncateUTF8Bytes(p.Purpose, len(p.Purpose)/2)
			continue
		}
		if len(p.Detail) > 0 {
			p.Detail = truncateUTF8Bytes(p.Detail, len(p.Detail)/2)
			continue
		}
		p.Profile = ""
		p.Tool = ""
	}
}

type subagentProgressContext struct {
	Base subagentProgress
	Emit func(subagentProgress)
}
type subagentProgressKey struct{}

func withSubagentObserver(ctx context.Context, emit func(subagentProgress)) context.Context {
	return context.WithValue(ctx, subagentProgressKey{}, subagentProgressContext{Emit: emit})
}
func withSubagentProgress(ctx context.Context, base subagentProgress) context.Context {
	previous, _ := ctx.Value(subagentProgressKey{}).(subagentProgressContext)
	return context.WithValue(ctx, subagentProgressKey{}, subagentProgressContext{Base: base, Emit: previous.Emit})
}
func emitSubagentProgress(ctx context.Context, stage, tool, detail string) {
	progress, ok := ctx.Value(subagentProgressKey{}).(subagentProgressContext)
	if !ok || progress.Emit == nil || progress.Base.ID == "" {
		return
	}
	update := progress.Base
	update.Stage, update.Tool, update.Detail = stage, tool, strings.TrimSpace(detail)
	if stage == "completed" || stage == "failed" || stage == "cancelled" {
		update.Status = stage
	} else {
		update.Status = "running"
	}
	progress.Emit(update)
}
