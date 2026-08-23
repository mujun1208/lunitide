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
	councilMaxExperts       = 8
	councilParallelExperts  = 3
	councilExpertMaxTokens  = 1400
	councilExpertMaxBody    = 6000
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
	Experts    []councilExpert
}

type expertCouncilInputs struct {
	SessionID      string
	ProjectID      string
	PhaseLabel     string
	Companion      bool
	TurnText       string
	ExplicitMsgs   []gateway.Message
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

func (e *Engine) collectCouncilExpertIDs(ctx context.Context, in expertCouncilInputs) []string {
	var mounted []string
	if in.SessionID != "" && e.sessionExperts != nil {
		if ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, in.SessionID); err == nil {
			mounted = ids
		}
	}
	var texts []string
	for _, m := range in.ExplicitMsgs {
		texts = append(texts, m.Content)
	}
	if in.SessionID != "" && e.messageReader != nil {
		texts = append(texts, e.peekLastUserMessage(ctx, in.SessionID))
	}
	ids := collectExpertIDs(mounted, texts...)
	phaseKey := phaseKeyFromWorkbenchLabel(in.PhaseLabel)
	if in.ProjectID != "" && phaseKey != "" && e.m8expert != nil {
		matrix, err := e.m8expert.MountingGet(ctx, m8app.MountingGetInput{ProjectID: in.ProjectID, PhaseKey: phaseKey})
		if err == nil {
			for _, row := range matrix.Matrix {
				for _, m := range row.Mountings {
					if m.State == m8core.MountingMounted && m.ExpertState == m8core.ExpertEnabled {
						ids = appendUniqueExpertIDs(ids, m.ExpertID)
					}
				}
			}
		}
	}
	return ids
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
		Experts:    experts,
	}
}

func expertDeliberateSystemPrompt(name, body string) string {
	return fmt.Sprintf("你是专家「%s」。请严格以该岗位说明书的专业视角独立作答，不要模拟其他角色，也不要替用户做最终拍板。\n\n岗位说明书：\n%s\n\n输出格式（中文，简洁）：\n【立场】一句话\n【建议】3-6 条要点\n【风险】主要风险或反对点\n【前提】关键假设", name, body)
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

func (e *Engine) deliberateExpert(ctx context.Context, a gateway.Adapter, credential []byte, model string, expert councilExpert, question, phaseLabel string, companion bool) councilOpinion {
	op := councilOpinion{ExpertID: expert.ID, ExpertName: expert.Name}
	resp, err := a.Complete(ctx, credential, gateway.Request{
		Model:            model,
		MaxTokens:        councilExpertMaxTokens,
		MaxAttempts:      1,
		DisableReasoning: companion,
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: expertDeliberateSystemPrompt(expert.Name, expert.Body)},
			{Role: gateway.RoleUser, Content: expertDeliberateUserPrompt(question, phaseLabel)},
		},
	})
	if err != nil {
		op.Err = err
		op.Text = "（该专家本轮未能完成发言）"
		return op
	}
	text := strings.TrimSpace(resp.Message.Content)
	if text == "" {
		op.Text = "（该专家未返回有效意见）"
		return op
	}
	op.Text = text
	return op
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
		"综合后如需执行再调用工具；讨论阶段不要为写文件而打断结论。\n"
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
				opinions[idx] = e.deliberateExpert(ctx, a, credential, model, expert, cfg.Question, cfg.PhaseLabel, cfg.Companion)
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
