package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/atomicfile"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"io"
	"log"
	"os"
	"path/filepath"
)

const messageProcessMaxBytes = 127 * 1024 // leaves room for the bridge envelope
const messageProcessThinkingBytes = 64 * 1024

type messageProcessTool struct {
	CallID     string `json:"callId"`
	Name       string `json:"name"`
	ArgsDigest string `json:"argsDigest,omitempty"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
}
type messageProcess struct {
	MessageID string               `json:"messageId"`
	Thinking  string               `json:"thinking"`
	Equipment *bridge.EquipEvent   `json:"equipment,omitempty"`
	Tools     []messageProcessTool `json:"tools"`
	Truncated bool                 `json:"truncated"`
}

// Called under runStream's sendMu so asynchronous tool output cannot race
// snapshots; only the actual displayed thinking/equipment/status is recorded.
func (p *messageProcess) capture(event bridge.Event) {
	switch event.Type {
	case bridge.EventThinking:
		if event.Thinking == nil {
			return
		}
		text := event.Thinking.Text
		room := messageProcessThinkingBytes - len(p.Thinking)
		if len(text) > room {
			text = truncateUTF8Bytes(text, room)
			p.Truncated = true
		}
		p.Thinking += text
	case bridge.EventEquip:
		if event.Equip == nil {
			return
		}
		clean := func(items []string) []string {
			out := make([]string, 0, len(items))
			for _, item := range items {
				if len(out) >= 16 {
					p.Truncated = true
					break
				}
				out = append(out, clipRunes(item, 128))
			}
			return out
		}
		p.Equipment = &bridge.EquipEvent{Experts: clean(event.Equip.Experts), Skills: clean(event.Equip.Skills), MissingMcp: clean(event.Equip.MissingMcp)}
	case bridge.EventToolStarted, bridge.EventToolCompleted, bridge.EventApprovalRequired, bridge.EventToolOutput:
		if event.Tool == nil || event.Tool.CallID == "" || event.Tool.Name == "" {
			return
		}
		childTool := subagentProcessTool(event.Tool.Name)
		if event.Type == bridge.EventToolOutput && !childTool {
			return
		}
		var previous messageProcessTool
		for _, prior := range p.Tools {
			if prior.CallID == event.Tool.CallID {
				previous = prior
				break
			}
		}
		if childTool && event.Type == bridge.EventToolOutput && previous.Status == string(bridge.EventToolCompleted) {
			return
		}
		tool := messageProcessTool{CallID: clipRunes(event.Tool.CallID, 128), Name: clipRunes(event.Tool.Name, 128), ArgsDigest: clipRunes(event.Tool.ArgsDigest, 64), Status: string(event.Type), Summary: clipRunes(event.Tool.Summary, 512)}
		if childTool {
			summary, valid := subagentProcessSummary(event, previous)
			if event.Type == bridge.EventToolOutput {
				if !valid {
					return
				}
				tool.Status = string(bridge.EventToolStarted)
			}
			if valid {
				tool.Summary = summary
				if event.Type == bridge.EventToolStarted && previous.CallID != "" {
					tool.Status = previous.Status
				}
			}
		}
		for i, prior := range p.Tools {
			if prior.CallID == tool.CallID {
				p.Tools[i] = tool
				return
			}
		}
		if len(p.Tools) >= 64 {
			p.Truncated = true
			return
		}
		p.Tools = append(p.Tools, tool)
	}
}
func (p messageProcess) hasContent() bool {
	return p.Thinking != "" || p.Equipment != nil || len(p.Tools) > 0
}
func (p messageProcess) boundedJSON() ([]byte, error) {
	if p.Tools == nil {
		p.Tools = []messageProcessTool{}
	}
	for {
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		if len(raw) <= messageProcessMaxBytes {
			return raw, nil
		}
		p.Truncated = true
		if len(p.Thinking) > 0 {
			p.Thinking = truncateUTF8Bytes(p.Thinking, len(p.Thinking)/2)
			continue
		}
		if len(p.Tools) > 0 {
			p.Tools = p.Tools[:len(p.Tools)-1]
			continue
		}
		p.Equipment = nil
	}
}
func (e *Engine) messageProcessPath(sessionID, messageID string) string {
	if e == nil || e.tools == nil || !message.CanonicalULID(sessionID) || !message.CanonicalULID(messageID) {
		return ""
	}
	// Metadata is beside .turns, outside every SessionFolder workspace digest.
	return filepath.Join(e.tools.WorkspaceRoot(), ".message-process", sessionID, messageID+".json")
}
func (e *Engine) saveMessageProcess(sessionID, messageID string, p messageProcess) {
	if !p.hasContent() {
		return
	}
	path := e.messageProcessPath(sessionID, messageID)
	if path == "" {
		return
	}
	p.MessageID = messageID
	raw, err := p.boundedJSON()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0700)
	}
	if err == nil {
		err = atomicfile.Write(path, raw, 0600)
	}
	if err != nil {
		log.Printf("chat process metadata save failed: %v", err)
	}
}
func (e *Engine) enrichMessageProcessPage(sessionID string, payload map[string]any) {
	items, _ := payload["items"].([]any)
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok || row["role"] != "assistant" {
			continue
		}
		id, _ := row["id"].(string)
		path := e.messageProcessPath(sessionID, id)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Size() <= messageProcessMaxBytes {
			row["hasProcess"] = true
		}
	}
}
func handleMessageProcess(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		MessageID string `json:"messageId"`
	}
	if decodePayload(r.Payload, &p) != nil || !message.CanonicalULID(p.SessionID) || !message.CanonicalULID(p.MessageID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "对话过程参数无效", false)
	}
	reader, ok := e.messages.(interface {
		GetInSession(context.Context, string, string) (message.Message, error)
	})
	if !ok {
		return r.Fail("STORAGE_UNAVAILABLE", "对话过程暂时不可用", true)
	}
	msg, err := reader.GetInSession(ctx, p.SessionID, p.MessageID)
	if err != nil {
		return messageFailure(r, err)
	}
	if msg.Role != message.RoleAssistant {
		return r.Fail("MESSAGE_NOT_FOUND", "该消息没有助手处理过程", false)
	}
	empty := messageProcess{MessageID: p.MessageID, Tools: []messageProcessTool{}}
	path := e.messageProcessPath(p.SessionID, p.MessageID)
	if path == "" {
		return r.Ok(empty)
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return r.Ok(empty)
	}
	if err != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "对话过程读取失败", true)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, messageProcessMaxBytes+1))
	if err != nil || len(raw) > messageProcessMaxBytes {
		return r.Fail("MESSAGE_PROCESS_INVALID", "对话过程记录无法读取", false)
	}
	var out messageProcess
	if json.Unmarshal(raw, &out) != nil || out.MessageID != p.MessageID || !out.valid() {
		return r.Fail("MESSAGE_PROCESS_INVALID", "对话过程记录不完整", false)
	}
	if out.Tools == nil {
		out.Tools = []messageProcessTool{}
	}
	return r.Ok(out)
}

func (p messageProcess) valid() bool {
	if len(p.Thinking) > messageProcessThinkingBytes || len(p.Tools) > 64 {
		return false
	}
	for _, tool := range p.Tools {
		if tool.CallID == "" || tool.Name == "" || len([]rune(tool.CallID)) > 128 || len([]rune(tool.Name)) > 128 || len(tool.ArgsDigest) > 64 || len([]rune(tool.Summary)) > 512 {
			return false
		}
		switch tool.Status {
		case string(bridge.EventToolStarted), string(bridge.EventToolCompleted), string(bridge.EventApprovalRequired):
		default:
			return false
		}
	}
	if p.Equipment != nil {
		if p.Equipment.Experts == nil {
			return false
		}
		for _, items := range [][]string{p.Equipment.Experts, p.Equipment.Skills, p.Equipment.MissingMcp} {
			if len(items) > 16 {
				return false
			}
			for _, item := range items {
				if len([]rune(item)) > 128 {
					return false
				}
			}
		}
	}
	return true
}
