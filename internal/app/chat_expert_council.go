// chat_expert_council.go runs the Expert Council Engine: when two or more
// experts are mounted, each expert gets an independent deliberation pass,
// then 月汐 synthesizes one best recommendation for the user.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const (
	councilMaxExperts      = 8
	councilParallelExperts = 3
	councilExpertMaxTokens = 4096
	councilExpertMaxBody   = 6000
	councilExpertMaxSteps  = 6
)

type councilExpert struct {
	ID   string
	Name string
	Body string
}

type councilOpinion struct {
	ExpertID   string
	ExpertName string
	Text       string
	Err        error
}

type expertCouncilConfig struct {
	Enabled    bool
	Question   string
	PhaseLabel string
	Companion  bool
	SessionID  string
	Mode       executionMode
	Experts    []councilExpert
}

type expertCouncilInputs struct {
	SessionID    string
	ProjectID    string
	PhaseLabel   string
	Companion    bool
	TurnText     string
	ExplicitMsgs []gateway.Message
}

func phaseKeyFromWorkbenchLabel(label string) string {
	switch strings.TrimSpace(label) {
	case "需求架构规范":
		return m8core.PhaseRequirementDefinition
	case "方案和UI设计":
		return m8core.PhaseSolutionExperience
	case "数据库", "接口":
		return m8core.PhaseArchitecturePlan
	case "开发", "集成":
		return m8core.PhaseDevelopmentChange
	case "测试":
		return m8core.PhaseVerificationAcceptance
	case "发布":
		return m8core.PhaseReleaseDelivery
	default:
		return ""
	}
}

func appendUniqueExpertIDs(ids []string, extra ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids)+len(extra))
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range ids {
		add(id)
	}
	for _, id := range extra {
		add(id)
	}
	if len(out) > councilMaxExperts {
		return out[:councilMaxExperts]
	}
	return out
}

// selectedTurnExpertIDs is the spawn filter for both 普通对话 and 项目管理.
// Composer chips ride as `[引用专家 name|id]` on the current turn; those IDs
// win over a stale session mount (PM used to silently session.experts.set the
// first four conversation specialists). With no turn refs, only the session
// chips are used. The 13-specialist catalog and phase-matrix defaults are
// never unioned in.
func selectedTurnExpertIDs(mounted []string, turnTexts ...string) []string {
	for _, text := range turnTexts {
		if refs := extractExpertRefIDs(text); len(refs) > 0 {
			return appendUniqueExpertIDs(nil, refs...)
		}
	}
	if len(mounted) == 1 {
		return appendUniqueExpertIDs(nil, mounted[0])
	}
	return nil
}

func (e *Engine) collectCouncilExpertIDs(ctx context.Context, in expertCouncilInputs) []string {
	var mounted []string
	if in.SessionID != "" && e.sessionExperts != nil {
		if ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, in.SessionID); err == nil {
			mounted = ids
		}
	}
	return selectedTurnExpertIDs(mounted, e.priorTurnTexts(ctx, in.SessionID, in.TurnText)...)
}

func (e *Engine) resolveCouncilExperts(ctx context.Context, ids []string) []councilExpert {
	if e.m8expert == nil {
		return nil
	}
	out := make([]councilExpert, 0, len(ids))
	for _, id := range ids {
		detail, err := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: id})
		if err != nil {
			continue
		}
		name, _ := detail.Expert["name"].(string)
		if name == "" {
			name = id
		}
		out = append(out, councilExpert{
			ID:   id,
			Name: name,
			Body: clipExpertCouncilBody(detail.SixSection),
		})
	}
	return out
}

func expertDeliberateDigest(expertID, question string) string {
	args, _ := json.Marshal(map[string]string{"expertId": expertID, "question": question})
	return toolruntime.Digest("expert.deliberate", args)
}

func clipExpertCouncilBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	r := []rune(s)
	if len(r) <= councilExpertMaxBody {
		return s
	}
	return string(r[:councilExpertMaxBody]) + "\n…（岗位说明书已截断）"
}

func (e *Engine) buildExpertCouncilConfig(ctx context.Context, in expertCouncilInputs) *expertCouncilConfig {
	// Voice companion is single-persona (月汐); expert deliberation adds
	// latency before the first spoken token and breaks the phone-call feel.
	if in.Companion {
		return nil
	}
	if e.m8expert == nil || skipExpertCouncil(in.TurnText) {
		return nil
	}
	ids := e.collectCouncilExpertIDs(ctx, in)
	if len(ids) < 2 {
		return nil
	}
	experts := e.resolveCouncilExperts(ctx, ids)
	if len(experts) < 2 {
		return nil
	}
	question := strings.TrimSpace(in.TurnText)
	if question == "" && in.SessionID != "" {
		question = e.peekLastUserMessage(ctx, in.SessionID)
	}
	if question == "" {
		return nil
	}
	return &expertCouncilConfig{
		Enabled:    true,
		Question:   question,
		PhaseLabel: in.PhaseLabel,
		Companion:  in.Companion,
		SessionID:  in.SessionID,
		Experts:    experts,
	}
}

func expertDeliberateSystemPrompt(name, body string) string {
	return fmt.Sprintf("你是专家「%s」。请严格以该岗位说明书的专业视角独立作答，不要模拟其他角色，也不要替用户做最终拍板。\n\n岗位说明书：\n%s\n\n%s\n\n输出格式（中文，简洁）：\n【立场】一句话\n【建议】3-6 条要点\n【风险】主要风险或反对点\n【前提】关键假设\n需要事实或素材时先调用 web.search（必要时 web.fetch）；需要成文时调用对应 *.gen（桌面 desktop=true）；结构图用 mermaid；匹配技能立刻 skill.invoke。不要倾倒 200 页全书。", name, body, specialistPersonaCapabilityLine())
}

func expertDeliberateUserPrompt(question, phaseLabel string) string {
	var b strings.Builder
	b.WriteString("用户问题：\n")
	b.WriteString(strings.TrimSpace(question))
	if phaseLabel != "" {
		b.WriteString("\n\n当前项目阶段：")
		b.WriteString(phaseLabel)
	}
	b.WriteString("\n\n请给出你的独立专业意见。")
	return b.String()
}

func (e *Engine) deliberateExpert(ctx context.Context, a gateway.Adapter, credential []byte, model string, expert councilExpert, question, phaseLabel string, companion bool, mode executionMode, sessionID string) councilOpinion {
	op := councilOpinion{ExpertID: expert.ID, ExpertName: expert.Name}
	eq := e.equipmentForNames(ctx, []string{expert.Name})
	tools := specialistToolDefinitions(e.engineToolDefinitionsFor(mode))
	if e.skills != nil {
		for _, d := range e.skillToolDefinitions() {
			if d.Name == "skill.invoke" || d.Name == "skill.view" {
				tools = append(tools, d)
			}
		}
	}
	tools = append(tools, e.mcpToolDefinitionsRestricted(eq.McpIDs, true)...)
	req := gateway.Request{
		Model:            model,
		MaxTokens:        councilExpertMaxTokens,
		MaxAttempts:      1,
		DisableReasoning: companion,
		Tools:            tools,
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: expertDeliberateSystemPrompt(expert.Name, expert.Body)},
			{Role: gateway.RoleUser, Content: expertDeliberateUserPrompt(question, phaseLabel)},
		},
	}
	allowed := toolNameSet(tools)
	var lastText string
	steps := councilExpertMaxSteps
	if e.tools == nil || len(tools) == 0 {
		steps = 1
		req.Tools = nil
	}
	for step := 0; step < steps; step++ {
		resp, err := a.Complete(ctx, credential, req)
		if err != nil {
			if lastText != "" {
				op.Text = lastText
				return op
			}
			op.Err = err
			op.Text = "（该专家本轮未能完成发言）"
			return op
		}
		if len(resp.Message.ToolCalls) == 0 {
			lastText = strings.TrimSpace(resp.Message.Content)
			break
		}
		req.Messages = append(req.Messages, resp.Message)
		req.Messages = append(req.Messages, e.runCouncilToolCalls(ctx, mode, sessionID, expert.Name, allowed, resp.Message.ToolCalls)...)
	}
	if lastText == "" {
		op.Text = "（该专家未返回有效意见）"
		return op
	}
	op.Text = stripOfficeGenLecture(lastText)
	if strings.Contains(op.Text, "写到桌面请用对应") {
		op.Text = "（该专家本轮未能完成发言）"
	}
	return op
}

func (e *Engine) runCouncilToolCalls(ctx context.Context, mode executionMode, sessionID, expertName string, allowed map[string]bool, calls []gateway.ToolCall) []gateway.Message {
	eq := e.equipmentForNames(ctx, []string{expertName})
	out := make([]gateway.Message, len(calls))
	for i, call := range calls {
		summary := "refused: tool not allowed for expert.deliberate"
		if reason, deny := ungatedEngineToolDenied(mode, false, call.Name, call.Arguments); deny {
			// Same gate the main chat loop applies before dispatch: in approval
			// mode a council specialist cannot reach out through mcp.call / mcp_*
			// or actuate the browser either — this path has no approval prompt.
			summary = reason
		} else if reason, deny := e.denyRestrictedMCP(call.Name, call.Arguments, true, eq.McpIDs); deny {
			summary = reason
		} else if call.Name == "mcp.search" {
			outText, err := e.searchMcpToolsFiltered(call.Arguments, eq.McpIDs, true)
			if err != nil {
				summary = err.Error()
			} else {
				summary = outText
			}
		} else if call.Name == "mcp.call" {
			outText, err := e.callMcpToolByNameGuarded(ctx, call.Arguments, eq.McpIDs, true)
			if err != nil {
				summary = err.Error()
			} else {
				summary = outText
			}
		} else if endpointID, tool, ok := parseMcpToolName(call.Name); ok {
			if !e.mcpNameAllowed(call.Name, endpointID, eq.McpIDs, true) {
				summary = "未授权这个 MCP"
			} else if text, err := e.invokeMcpTool(ctx, endpointID, tool, call.Arguments); err != nil {
				summary = err.Error()
			} else {
				summary = text
			}
		} else if allowed[call.Name] {
			r, err := e.executeUserTool(ctx, mode, sessionID, call.Name, call.Arguments)
			if err != nil {
				summary = err.Error()
			} else {
				summary = r.Output
			}
		}
		if len(summary) > 4096 {
			summary = summary[:4096]
		}
		out[i] = gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary}
	}
	return out
}

func formatCouncilBrief(question string, opinions []councilOpinion) string {
	var b strings.Builder
	b.WriteString("\n\n[专家理事会 · 征询记录]\n")
	b.WriteString("用户问题：")
	b.WriteString(strings.TrimSpace(question))
	b.WriteByte('\n')
	for _, op := range opinions {
		b.WriteString("\n---\n【")
		b.WriteString(op.ExpertName)
		b.WriteString("】\n")
		if op.Err != nil {
			b.WriteString(op.Text)
			b.WriteByte('\n')
			continue
		}
		b.WriteString(op.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func councilChairInstruction(brief string, companion bool) string {
	if companion {
		return brief + "\n\n你是月汐（会议主席）。上面是各位专家的独立意见。请综合分歧与共识，给用户一份最优方案：先 1-2 句结论（适合语音朗读），再简短说明关键分歧与推荐取舍。不要逐条复读每位专家原文，不要拆成多条助手消息。\n"
	}
	return brief + "\n\n你是月汐（会议主席）。上面是各位专家的独立征询结果。请输出一份给用户的最优方案，结构如下：\n" +
		"## 综合结论\n（明确推荐方案）\n\n" +
		"## 主要分歧\n（列出专家间冲突点及你的取舍理由）\n\n" +
		"## 待你拍板\n（仍需用户决定的问题，若无写“无”）\n\n" +
		"## 各专家要点\n（每位专家 1-3 行摘要，不要全文粘贴）\n\n" +
		"综合后必须把交付做完：需要网上事实就 web.search / web.fetch；结构图画 mermaid；成文用 docx.gen / excel.gen / pptx.gen / html.gen（桌面 desktop=true）；匹配技能立刻 skill.invoke。不要只给口头结论交差。\n"
}

func injectCouncilChairBrief(req *gateway.Request, brief string, companion bool) {
	if req == nil || brief == "" {
		return
	}
	chair := councilChairInstruction(brief, companion)
	if len(req.Messages) == 0 || req.Messages[0].Role != gateway.RoleSystem {
		req.Messages = append([]gateway.Message{{Role: gateway.RoleSystem, Content: chair}}, req.Messages...)
		return
	}
	req.Messages[0].Content += chair
}

func (e *Engine) runExpertCouncil(ctx context.Context, a gateway.Adapter, credential []byte, model string, cfg expertCouncilConfig, send func(bridge.Event) error) (string, error) {
	if !cfg.Enabled || len(cfg.Experts) < 2 {
		return "", nil
	}
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: fmt.Sprintf("正在召开专家理事会（%d 位专家）…\n", len(cfg.Experts))}})
	opinions := make([]councilOpinion, len(cfg.Experts))
	for start := 0; start < len(cfg.Experts); start += councilParallelExperts {
		end := start + councilParallelExperts
		if end > len(cfg.Experts) {
			end = len(cfg.Experts)
		}
		var wg sync.WaitGroup
		for i := start; i < end; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				expert := cfg.Experts[idx]
				callID := ulid.Make().String()
				digest := expertDeliberateDigest(expert.ID, cfg.Question)
				_ = send(bridge.Event{
					Type: bridge.EventToolStarted,
					Tool: &bridge.ToolEvent{CallID: callID, Name: "expert.deliberate", ArgsDigest: digest, Summary: "专家「" + expert.Name + "」发言中…"},
				})
				opinions[idx] = e.deliberateExpert(ctx, a, credential, model, expert, cfg.Question, cfg.PhaseLabel, cfg.Companion, cfg.Mode, cfg.SessionID)
				summary := truncateUTF8Bytes(opinions[idx].Text, 480)
				if opinions[idx].Err != nil {
					summary = opinions[idx].Text
				}
				_ = send(bridge.Event{
					Type: bridge.EventToolCompleted,
					Tool: &bridge.ToolEvent{CallID: callID, Name: "expert.deliberate", ArgsDigest: digest, Summary: "专家「" + expert.Name + "」：" + summary},
				})
			}(i)
		}
		wg.Wait()
	}
	brief := formatCouncilBrief(cfg.Question, opinions)
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: "专家征询完成，月汐正在综合最优方案…\n"}})
	return brief, nil
}

func (e *Engine) applyExpertCouncil(ctx context.Context, a gateway.Adapter, credential []byte, model string, cfg *expertCouncilConfig, req *gateway.Request, companion bool, send func(bridge.Event) error) {
	if cfg == nil || !cfg.Enabled {
		return
	}
	brief, err := e.runExpertCouncil(ctx, a, credential, model, *cfg, send)
	if err != nil {
		log.Printf("expert council failed: %v", err)
		return
	}
	if brief != "" {
		injectCouncilChairBrief(req, brief, companion)
	}
}
