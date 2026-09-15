package modelfit

import "encoding/json"

type ProtocolToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ProtocolMessage struct {
	Role             string             `json:"role"`
	Content          string             `json:"content,omitempty"`
	ReasoningContent string             `json:"reasoningContent,omitempty"`
	ToolCallID       string             `json:"toolCallId,omitempty"`
	ToolCalls        []ProtocolToolCall `json:"toolCalls,omitempty"`
}

type MessageGroup struct {
	ID        string            `json:"id,omitempty"`
	Sequence  int               `json:"sequence"`
	Assistant ProtocolMessage   `json:"assistant"`
	Tools     []ProtocolMessage `json:"tools,omitempty"`
	Complete  bool              `json:"complete"`
}

func CanExecuteToolCalls(calls []ProtocolToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, c := range calls {
		if c.ID == "" || c.Name == "" || !json.Valid(c.Arguments) {
			return false
		}
	}
	return true
}

func MessageGroupComplete(g MessageGroup) bool {
	if g.Assistant.Role != "assistant" || !CanExecuteToolCalls(g.Assistant.ToolCalls) {
		return false
	}
	seen := map[string]bool{}
	for _, tool := range g.Tools {
		if tool.Role != "tool" || tool.ToolCallID == "" {
			return false
		}
		seen[tool.ToolCallID] = true
	}
	for _, c := range g.Assistant.ToolCalls {
		if !seen[c.ID] {
			return false
		}
	}
	return true
}

type ForcedSummaryPlan struct {
	AppendSummary         bool
	ReappendToolAssistant bool
	ExecuteTools          bool
	ToolCallIDs           []string
}

func PlanForcedSummary(groups []MessageGroup, event string) ForcedSummaryPlan {
	var ids []string
	complete := false
	for _, g := range groups {
		if !MessageGroupComplete(g) {
			continue
		}
		complete = true
		for _, c := range g.Assistant.ToolCalls {
			ids = append(ids, c.ID)
		}
	}
	return ForcedSummaryPlan{
		AppendSummary:         event == "after_tools" && complete,
		ReappendToolAssistant: false,
		ExecuteTools:          false,
		ToolCallIDs:           ids,
	}
}

func ExtractMessageGroups(messages []ProtocolMessage) []MessageGroup {
	var out []MessageGroup
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		g := MessageGroup{Sequence: len(out), Assistant: msg}
		j := i + 1
		for j < len(messages) && messages[j].Role == "tool" {
			g.Tools = append(g.Tools, messages[j])
			j++
		}
		g.Complete = MessageGroupComplete(g)
		out = append(out, g)
		i = j - 1
	}
	return out
}
