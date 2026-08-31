package app

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const (
	peopleAgentReplyMinInterval = 3 * time.Second
	peopleAgentMaxSteps         = 8
	peopleAgentMaxTokens        = 1600
	peopleAgentTimeout          = 3 * time.Minute
	peopleAgentReplyMaxRunes    = 8000
)

var (
	peopleAgentReplyMu   sync.Mutex
	peopleAgentReplyLast = map[string]time.Time{}
)

func (e *Engine) RegisterExpertAgentContacts(ctx context.Context) error {
	if e == nil || e.people == nil || e.m8expert == nil {
		return nil
	}
	listed, err := e.m8expert.List(ctx, m8app.ExpertFilter{})
	if err != nil {
		return err
	}
	for _, item := range listed.Experts {
		e.registerAgentContactForExpert(ctx, item.ExpertID, item.Name, item.Division, item.State)
	}
	return nil
}

func (e *Engine) registerAgentContactForExpert(ctx context.Context, expertID, name, division, state string) {
	if e == nil || e.people == nil {
		return
	}
	if m8app.ExpertKindForName(name) != m8app.ExpertKindAgent || state == "archived" {
		return
	}
	emoji, bio := "🌙", ""
	if item, ok := m8app.ConversationExpertByName(name); ok {
		if item.Emoji != "" {
			emoji = item.Emoji
		}
		bio = strings.TrimSpace(item.Description)
		if utf8.RuneCountInString(bio) > 2000 {
			bio = string([]rune(bio)[:2000])
		}
	}
	if utf8.RuneCountInString(name) > 64 {
		name = string([]rune(name)[:64])
	}
	if err := e.people.UpsertAgentContact(ctx, people.Contact{
		SubjectID:  expertID,
		Nickname:   name,
		Avatar:     emoji,
		Status:     "online",
		Department: division,
		Title:      m8app.DivisionRole(division),
		Bio:        bio,
	}); err != nil {
		log.Printf("people agent roster: %v", err)
	}
}

func parseAgentMentions(body string, agents []people.Contact) []people.Contact {
	body = strings.TrimSpace(body)
	if body == "" || len(agents) == 0 {
		return nil
	}
	var out []people.Contact
	seen := map[string]bool{}
	for _, agent := range agents {
		name := strings.TrimSpace(agent.Nickname)
		if name == "" || seen[agent.SubjectID] {
			continue
		}
		if strings.Contains(body, "@"+name) || strings.Contains(body, "@"+strings.TrimSuffix(name, "专家")) {
			seen[agent.SubjectID] = true
			out = append(out, agent)
		}
	}
	return out
}

func parseClaimTaskKey(body string) string {
	trimmed := strings.TrimSpace(body)
	idx := strings.Index(trimmed, "认领")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(trimmed[idx+len("认领"):])
	rest = strings.TrimLeft(rest, "：: ")
	if rest == "" || utf8.RuneCountInString(rest) > 128 {
		return ""
	}
	return rest
}

func peopleAgentMembers(t people.Thread) []people.Contact {
	var out []people.Contact
	for _, member := range t.Members {
		if people.IsAgentContact(member) && !member.Self && !member.Blocked {
			out = append(out, member)
		}
	}
	return out
}

func (e *Engine) maybePeopleAgentReply(ctx context.Context, threadID string, user people.Message) {
	if e == nil || e.people == nil || user.Kind != "text" || strings.TrimSpace(user.Body) == "" {
		return
	}
	if !peopleAgentReplyDue(threadID) {
		return
	}
	thread, err := e.people.PeekThread(ctx, threadID)
	if err != nil {
		return
	}
	agents := peopleAgentMembers(thread)
	if len(agents) == 0 {
		return
	}
	targets := parseAgentMentions(user.Body, agents)
	if len(targets) == 0 && thread.Kind != "group" && len(agents) == 1 {
		targets = agents[:1]
	}
	if len(targets) == 0 {
		return
	}
	agent := targets[0]
	if task := parseClaimTaskKey(user.Body); task != "" && e.claims != nil {
		owner, created, claimErr := e.claims.TryClaimExpertTask(ctx, threadID, task, agent.SubjectID)
		if claimErr != nil {
			log.Printf("people agent claim: %v", claimErr)
		} else if !created && owner != "" && owner != agent.SubjectID {
			name := owner
			for _, member := range thread.Members {
				if member.SubjectID == owner && member.Nickname != "" {
					name = member.Nickname
					break
				}
			}
			_, _ = e.people.SendAs(ctx, agent.SubjectID, threadID, "这个任务已经由「"+name+"」认领。")
			return
		}
	}
	workCtx, cancel := context.WithTimeout(ctx, peopleAgentTimeout)
	defer cancel()
	reply, err := e.completePeopleAgentTurn(workCtx, agent, threadID, user.Body)
	if err != nil || strings.TrimSpace(reply) == "" {
		return
	}
	if utf8.RuneCountInString(reply) > peopleAgentReplyMaxRunes {
		reply = string([]rune(reply)[:peopleAgentReplyMaxRunes])
	}
	if _, err := e.people.SendAs(ctx, agent.SubjectID, threadID, reply); err != nil {
		log.Printf("people agent reply: %v", err)
	}
}

func peopleAgentReplyDue(threadID string) bool {
	peopleAgentReplyMu.Lock()
	defer peopleAgentReplyMu.Unlock()
	last := peopleAgentReplyLast[threadID]
	if !last.IsZero() && time.Since(last) < peopleAgentReplyMinInterval {
		return false
	}
	peopleAgentReplyLast[threadID] = time.Now()
	return true
}

func peopleAgentSessionID(threadID string) string {
	if len(threadID) == 26 && !strings.ContainsAny(threadID, `/\`) {
		return threadID
	}
	return ""
}

func peopleAgentAllowedTool(name string) bool {
	switch name {
	case "user.ask", "computer.act", "desktop.open", "desktop.type", "im.send",
		"skill.create", "expert.create", "plugin.create", "plan.run":
		return false
	}
	if strings.HasPrefix(name, "cc.") {
		return false
	}
	return specialistToolAllow[name]
}

func peopleAgentToolDefinitions(all []gateway.ToolDefinition) []gateway.ToolDefinition {
	var out []gateway.ToolDefinition
	for _, d := range all {
		if peopleAgentAllowedTool(d.Name) {
			out = append(out, d)
		}
	}
	return out
}

func (e *Engine) completePeopleAgentTurn(ctx context.Context, agent people.Contact, threadID, userText string) (string, error) {
	sessionID := peopleAgentSessionID(threadID)
	if e.tools != nil && sessionID != "" {
		text, err := e.completePeopleAgentWithTools(ctx, agent, sessionID, userText)
		if err == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
		if err != nil {
			log.Printf("people agent tools: %v", err)
		}
	}
	return e.completePeopleAgentText(ctx, agent, userText)
}

func (e *Engine) peopleAgentPrompt(ctx context.Context, agent people.Contact) string {
	var b strings.Builder
	b.WriteString("你是独立智能体「")
	b.WriteString(agent.Nickname)
	b.WriteString("」。这是同事聊天：按岗位把事做完，最后用中文纯文本回复。生成的文件写在本机工作区或桌面（desktop=true），并在回复里给出路径。不要说你是月汐主编排。不要向同事要审批。\n")
	if e.m8expert != nil {
		if detail, dErr := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: agent.SubjectID}); dErr == nil {
			var six struct {
				Identity            string `json:"identity"`
				Mission             string `json:"mission"`
				Rules               string `json:"rules"`
				Workflow            string `json:"workflow"`
				DeliverableTemplate string `json:"deliverableTemplate"`
				SuccessMetrics      string `json:"successMetrics"`
			}
			_ = json.Unmarshal(detail.SixSection, &six)
			write := func(title, body string) {
				body = strings.TrimSpace(body)
				if body == "" {
					return
				}
				b.WriteString("\n[")
				b.WriteString(title)
				b.WriteString("]\n")
				b.WriteString(clipRunes(body, 4000))
				b.WriteByte('\n')
			}
			write("稳定身份", six.Identity)
			write("使命", six.Mission)
			write("规则", six.Rules)
			write("流程", six.Workflow)
			write("交付", six.DeliverableTemplate)
			write("成败", six.SuccessMetrics)
		}
	}
	b.WriteString(specialistRuntimeInstruction())
	var published []skill.Skill
	if skillServiceAvailable(e.skills) {
		if items, err := e.skills.List(ctx, skill.SkillStatusPublished); err == nil {
			published = items
		}
	}
	b.WriteString(expertComposeHint([]string{agent.Nickname}, published, e.connectedComposeMcpIDs()))
	return b.String()
}

func (e *Engine) completePeopleAgentWithTools(ctx context.Context, agent people.Contact, sessionID, userText string) (string, error) {
	if e.providers == nil {
		return "", nil
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	catalog := provider.CatalogForKind(items, provider.KindLLM)
	if len(catalog) == 0 {
		return "", nil
	}
	entry := catalog[0]
	tools := peopleAgentToolDefinitions(e.engineToolDefinitionsFor(executionModeFullAccess))
	allowed := toolNameSet(tools)
	system := e.peopleAgentPrompt(ctx, agent)
	var text string
	leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		a, aErr := e.adapter(op, entry.Provider)
		if aErr != nil {
			return aErr
		}
		req := gateway.Request{
			Model: entry.Model.ModelID, MaxTokens: peopleAgentMaxTokens, MaxAttempts: 1,
			Messages: []gateway.Message{
				{Role: gateway.RoleSystem, Content: system},
				{Role: gateway.RoleUser, Content: userText},
			},
			Tools: tools,
		}
		var paths []string
		for step := 0; step < peopleAgentMaxSteps; step++ {
			resp, cErr := a.Complete(op, secret, req)
			if cErr != nil {
				return cErr
			}
			if len(resp.Message.ToolCalls) == 0 {
				text = strings.TrimSpace(resp.Message.Content)
				break
			}
			req.Messages = append(req.Messages, resp.Message)
			for _, call := range resp.Message.ToolCalls {
				if !allowed[call.Name] {
					req.Messages = append(req.Messages, gateway.Message{
						Role: gateway.RoleTool, ToolCallID: call.ID, Content: "ok:false\n同事聊天不能用这个工具。",
					})
					continue
				}
				summary := e.runPeopleAgentTool(op, sessionID, call)
				if len(summary) > 4096 {
					summary = summary[:4096]
				}
				paths = append(paths, extractDeliverablePaths(summary)...)
				req.Messages = append(req.Messages, gateway.Message{
					Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary,
				})
			}
		}
		if strings.TrimSpace(text) == "" {
			req.Tools = nil
			req.Messages = append(req.Messages, gateway.Message{
				Role: gateway.RoleUser, Content: "步数用尽。用中文告诉同事你做成了什么、文件在哪、还缺什么。不要再调用工具。",
			})
			resp, cErr := a.Complete(op, secret, req)
			if cErr != nil {
				return cErr
			}
			text = strings.TrimSpace(resp.Message.Content)
		}
		if note := formatDeliverablePaths(paths, text); note != "" {
			if text != "" {
				text += "\n"
			}
			text += note
		}
		return nil
	})
	return text, leaseErr
}

func (e *Engine) runPeopleAgentTool(ctx context.Context, sessionID string, call gateway.ToolCall) string {
	if !peopleAgentAllowedTool(call.Name) {
		return "ok:false\n同事聊天不能用这个工具。"
	}
	var r toolruntime.Result
	var err error
	switch call.Name {
	case "skill.invoke":
		r, err = e.invokeSkillTool(ctx, executionModeFullAccess, sessionID, call.Arguments)
	case "skill.view":
		r, err = e.invokeSkillViewTool(ctx, call.Arguments)
	case "browser.act":
		r, err = e.invokeBrowserAct(ctx, executionModeFullAccess, sessionID, call.Arguments)
	case "image.generate", "video.generate":
		r, err = e.invokeMediaGenerate(ctx, call.Name, call.Arguments)
	default:
		if e.tools == nil {
			return "ok:false\n工具运行时不可用。"
		}
		r, err = e.executeUserTool(ctx, executionModeFullAccess, sessionID, call.Name, call.Arguments)
	}
	if err != nil {
		msg := err.Error()
		if !strings.HasPrefix(msg, "ok:false") {
			return "ok:false\n" + msg
		}
		return msg
	}
	return r.Output
}

func extractDeliverablePaths(summary string) []string {
	var out []string
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasSuffix(lower, ".pptx") || strings.HasSuffix(lower, ".docx") ||
			strings.HasSuffix(lower, ".xlsx") || strings.HasSuffix(lower, ".html") ||
			strings.HasSuffix(lower, ".pdf") || strings.HasSuffix(lower, ".md") {
			out = append(out, line)
		}
	}
	return out
}

func formatDeliverablePaths(paths []string, already string) string {
	seen := map[string]bool{}
	var uniq []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] || strings.Contains(already, p) {
			continue
		}
		seen[p] = true
		uniq = append(uniq, p)
	}
	if len(uniq) == 0 {
		return ""
	}
	return "文件：" + strings.Join(uniq, "；")
}

func (e *Engine) completePeopleAgentText(ctx context.Context, agent people.Contact, userText string) (string, error) {
	if e.providers == nil {
		return "", nil
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	catalog := provider.CatalogForKind(items, provider.KindLLM)
	if len(catalog) == 0 {
		return "", nil
	}
	entry := catalog[0]
	system := e.peopleAgentPrompt(ctx, agent)
	var text string
	leaseErr := e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		a, aErr := e.adapter(op, entry.Provider)
		if aErr != nil {
			return aErr
		}
		resp, cErr := a.Complete(op, secret, gateway.Request{
			Model: entry.Model.ModelID, MaxTokens: 800, MaxAttempts: 1,
			Messages: []gateway.Message{
				{Role: gateway.RoleSystem, Content: system},
				{Role: gateway.RoleUser, Content: userText},
			},
		})
		if cErr != nil {
			return cErr
		}
		text = strings.TrimSpace(resp.Message.Content)
		return nil
	})
	return text, leaseErr
}
