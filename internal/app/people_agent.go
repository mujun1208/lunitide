package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
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
	peopleAgentMaxSteps       = 8
	peopleAgentMaxTokens      = 1600
	peopleAgentTimeout        = 3 * time.Minute
	peopleAgentReplyMaxRunes  = 8000
	peopleAgentMaxHandoffHops = 2
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
		e.registerAgentContactForExpert(ctx, item.ExpertID, item.Name, item.Division, item.State, item.CatalogItemID)
	}
	return nil
}

func (e *Engine) registerAgentContactForExpert(ctx context.Context, expertID, name, division, state, catalogItemID string) {
	if e == nil || e.people == nil {
		return
	}
	if m8app.ExpertKindForExpert(name, catalogItemID) != m8app.ExpertKindAgent || state == "archived" {
		return
	}
	emoji, bio := "🌙", ""
	if item, ok := m8app.ResolveConversationExpert(name, catalogItemID); ok {
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
	mentions := ParseTurnMentions(body)
	var out []people.Contact
	seen := map[string]bool{}
	match := func(agent people.Contact) bool {
		name := strings.TrimSpace(agent.Nickname)
		short := strings.TrimSuffix(name, "专家")
		for _, m := range mentions {
			if m.Kind != "expert" && m.Kind != "member" {
				continue
			}
			if m.ID != "" && m.ID == agent.SubjectID {
				return true
			}
			if m.Name != "" && (m.Name == name || m.Name == short || strings.HasPrefix(name, m.Name)) {
				return true
			}
		}
		return false
	}
	for _, agent := range agents {
		if seen[agent.SubjectID] || !match(agent) {
			continue
		}
		seen[agent.SubjectID] = true
		out = append(out, agent)
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

func peopleAgentJobTargets(job peopleAgentJob, body string, thread people.Thread, agents []people.Contact) []people.Contact {
	if id := strings.TrimSpace(job.targetID); id != "" {
		for _, agent := range agents {
			if agent.SubjectID == id {
				return []people.Contact{agent}
			}
		}
		return nil
	}
	targets := parseAgentMentions(body, agents)
	if len(targets) == 0 && thread.Kind != "group" && len(agents) == 1 {
		return agents[:1]
	}
	return targets
}

func peopleAgentHandoffTargets(reply string, agents []people.Contact, fromID string) []people.Contact {
	var out []people.Contact
	for _, agent := range parseAgentMentions(reply, agents) {
		if agent.SubjectID == fromID {
			continue
		}
		out = append(out, agent)
	}
	return out
}

func peopleAgentHandoffBody(fromName, reply string) string {
	fromName = strings.TrimSpace(fromName)
	if fromName == "" {
		fromName = "同事"
	}
	return "【" + fromName + " 交接】\n" + strings.TrimSpace(reply)
}

func (e *Engine) enqueuePeopleAgentHandoffs(thread people.Thread, posted people.Message, from people.Contact, hop int) {
	if e == nil || hop >= peopleAgentMaxHandoffHops {
		return
	}
	for _, next := range peopleAgentHandoffTargets(posted.Body, peopleAgentMembers(thread), from.SubjectID) {
		_, _, dropped := enqueuePeopleAgentJob(peopleAgentJob{
			threadID:  thread.ThreadID,
			messageID: posted.MessageID,
			body:      peopleAgentHandoffBody(from.Nickname, posted.Body),
			targetID:  next.SubjectID,
			hop:       hop + 1,
		})
		if dropped != "" {
			e.notifyPeopleAgentDropped(context.Background(), thread.ThreadID, dropped)
		}
	}
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
	job, started, dropped := enqueuePeopleAgentTurn(threadID, user.MessageID, user.Body)
	if dropped != "" {
		e.notifyPeopleAgentDropped(ctx, threadID, dropped)
	}
	if !started {
		return
	}
	e.runPeopleAgentJob(ctx, job)
	e.drainPeopleAgentQueue(ctx, threadID)
}

func (e *Engine) runPeopleAgentJob(ctx context.Context, job peopleAgentJob) {
	threadID := job.threadID
	user := people.Message{MessageID: job.messageID, ThreadID: threadID, Kind: "text", Body: job.body}
	thread, err := e.people.PeekThread(ctx, threadID)
	if err != nil {
		return
	}
	agents := peopleAgentMembers(thread)
	if len(agents) == 0 {
		return
	}
	targets := peopleAgentJobTargets(job, user.Body, thread, agents)
	if len(targets) == 0 {
		if thread.Kind == "group" && e.people != nil && validCanonicalULID(threadID) {
			_, _ = e.people.SendSystem(ctx, threadID, peopleAgentNoTargetUserError())
		}
		return
	}
	workCtx, cancel := context.WithTimeout(ctx, peopleAgentTimeout)
	defer cancel()
	for _, agent := range targets {
		if task := parseClaimTaskKey(user.Body); task != "" && e.claims != nil {
			owner, created, claimErr := e.claims.TryClaimExpertTask(workCtx, threadID, task, agent.SubjectID)
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
				_, _ = e.people.SendAs(workCtx, agent.SubjectID, threadID, "这个任务已经由「"+name+"」认领。")
				continue
			}
		}
		if e.people != nil {
			if msgs, listErr := e.people.ListMessages(workCtx, threadID, 40); listErr == nil {
				if peopleAgentReplyStale(msgs, user) {
					continue
				}
				others := map[string]bool{}
				for _, member := range agents {
					if member.SubjectID != agent.SubjectID {
						others[member.SubjectID] = true
					}
				}
				if peopleAgentCollision(msgs, user.MessageID, agent.SubjectID, others) {
					reply := "（有同事先回了这一句，我这边不重复开口。）"
					if _, err := e.people.SendAs(workCtx, agent.SubjectID, threadID, reply); err != nil {
						log.Printf("people agent collision: %v", err)
					}
					continue
				}
			}
		}
		reply, err := e.completePeopleAgentTurn(workCtx, agent, threadID, user.Body)
		if err != nil || strings.TrimSpace(reply) == "" {
			continue
		}
		if utf8.RuneCountInString(reply) > peopleAgentReplyMaxRunes {
			reply = string([]rune(reply)[:peopleAgentReplyMaxRunes])
		}
		posted, err := e.people.SendAs(workCtx, agent.SubjectID, threadID, reply)
		if err != nil {
			log.Printf("people agent reply: %v", err)
			if e.people != nil && validCanonicalULID(threadID) {
				_, _ = e.people.SendSystem(workCtx, threadID, peopleAgentSendFailedUserError())
			}
			continue
		}
		if ident, bindErr := e.conversationIdentityForPeople(workCtx, threadID, agent.Nickname, []string{agent.SubjectID}); bindErr == nil {
			e.recordPeopleTurnMemory(workCtx, ident.BoundSessionID, agent.SubjectID, user.Body, reply, posted.MessageID)
		}
		e.enqueuePeopleAgentHandoffs(thread, posted, agent, job.hop)
	}
}

// peopleAgentExecutionMode is the authority an inbound colleague message gets.
// Deliberately not full-access: nobody is watching this turn, and full-access
// is also the switch that turns on unconfined whole-disk tool execution
// (fullDiskChat), so a message from outside would reach the entire filesystem.
// auto-edit still lets the agent read, search and write inside the workspace,
// which is all a colleague reply needs.
func peopleAgentExecutionMode() executionMode { return executionModeAutoEdit }

func peopleAgentAllowedTool(name string) bool {
	switch name {
	case "user.ask", "computer.act", "desktop.open", "desktop.type", "im.send",
		"skill.create", "expert.create", "plugin.create", "plan.run", "skill.manage",
		// A shell is the one tool that makes every other restriction here
		// decorative, and this turn is driven by a message from outside.
		"command.run":
		return false
	}
	if strings.HasPrefix(name, "cc.") {
		return false
	}
	if name == "mcp.search" || name == "mcp.call" || strings.HasPrefix(name, mcpToolPrefix) {
		return true
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

func peopleBoundSessionUserError() string {
	return "这条同事对话还没绑上会话，工具这轮先不用。请再发一次。"
}

func peopleAgentNoReplyUserError() string {
	return "这轮没有生成回复。请先在设置 → 模型与供应商里启用一个对话模型，再发一次。"
}

func peopleAgentNoTargetUserError() string {
	return "没有找到你 @ 的同事，请检查一下昵称。"
}

func peopleAgentSendFailedUserError() string {
	return "回复生成好了但没能发出来，请再发一次。"
}

var errPeopleAgentNoReply = errors.New("people agent produced no reply")

func (e *Engine) completePeopleAgentTurn(ctx context.Context, agent people.Contact, threadID, userText string) (string, error) {
	ident, bindErr := e.conversationIdentityForPeople(ctx, threadID, agent.Nickname, []string{agent.SubjectID})
	if bindErr != nil || ident.BoundSessionID == "" {
		note := peopleBoundSessionUserError()
		if e != nil && e.people != nil && validCanonicalULID(threadID) {
			_, _ = e.people.SendSystem(ctx, threadID, note)
		}
		if bindErr == nil {
			bindErr = errPeopleBoundSession
		}
		return note, bindErr
	}
	sessionID := ident.BoundSessionID
	intent := turnIntentForPeople(userText)
	eq := e.equipmentForNames(ctx, []string{agent.Nickname})
	if eq.Brain != BrainLunitide {
		prompt := e.localBrainPrompt(ctx, agent, threadID, sessionID, intent.Text)
		text, err := runLocalBrain(ctx, eq.Brain, prompt, localBrainWorkDir(agent.SubjectID), intent.Text)
		if err == nil && strings.TrimSpace(text) != "" {
			return localBrainPrefix(eq.Brain) + text, nil
		}
		note := localBrainFallbackNotice(eq.Brain, err)
		if e.tools != nil && sessionID != "" {
			if out, tErr := e.completePeopleAgentWithTools(ctx, agent, sessionID, intent.Text); tErr == nil && strings.TrimSpace(out) != "" {
				return lockLocalBrainFallback(note, out), nil
			}
		}
		if out, tErr := e.completePeopleAgentText(ctx, agent, sessionID, intent.Text); tErr == nil && strings.TrimSpace(out) != "" {
			return lockLocalBrainFallback(note, out), nil
		}
		if e.people != nil && threadID != "" {
			_, _ = e.people.SendSystem(ctx, threadID, strings.TrimSpace(note))
		}
		return note, err
	}
	if e.tools != nil && sessionID != "" {
		text, err := e.completePeopleAgentWithTools(ctx, agent, sessionID, intent.Text)
		if err == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
		if err != nil {
			log.Printf("people agent tools: %v", err)
		}
	}
	text, err := e.completePeopleAgentText(ctx, agent, sessionID, intent.Text)
	if strings.TrimSpace(text) != "" {
		return text, err
	}
	note := peopleAgentNoReplyUserError()
	if e.people != nil && validCanonicalULID(threadID) {
		_, _ = e.people.SendSystem(ctx, threadID, note)
	}
	if err == nil {
		err = errPeopleAgentNoReply
	}
	return note, err
}

func (e *Engine) peopleAgentPrompt(ctx context.Context, agent people.Contact) string {
	return e.peopleAgentTurnPrompt(ctx, agent, "", "")
}

func (e *Engine) peopleAgentTurnPrompt(ctx context.Context, agent people.Contact, sessionID, userText string) string {
	var b strings.Builder
	b.WriteString("你是同事专家「")
	b.WriteString(agent.Nickname)
	b.WriteString("」（同一月汐引擎上的人设和工具，不是独立进程）。这是同事聊天：按岗位把事做完，最后用中文纯文本回复。生成的文件写在本机工作区或桌面（desktop=true），并在回复里给出路径。群聊里可以用 @同事昵称 把未完成的部分交给对方；不要 @ 自己，不要来回互 @。不要说你是月汐主编排。不要向同事要审批。\n")
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
	var preferred []string
	if e.m8expert != nil {
		if agent.SubjectID != "" {
			if keys, err := e.m8expert.ListBoundSkills(ctx, agent.SubjectID); err == nil && len(keys) > 0 {
				preferred = keys
			}
		}
		if len(preferred) == 0 {
			preferred = e.m8expert.ComposeSkillsForNames(ctx, []string{agent.Nickname})
		}
	}
	b.WriteString(expertComposeHint([]string{agent.Nickname}, published, e.connectedComposeMcpIDs(), preferred))
	b.WriteString(e.peopleCompanionMemoryHint(ctx, sessionID, userText, agent.SubjectID))
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
	entry, ok := e.resolvePreferredChatModel(items)
	if !ok {
		return "", nil
	}
	tools := e.peopleAgentToolList(ctx, agent)
	allowed := toolNameSet(tools)
	system := e.peopleAgentTurnPrompt(ctx, agent, sessionID, userText)
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
				summary := e.runPeopleAgentTool(op, sessionID, agent, call)
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

func (e *Engine) peopleAgentToolList(ctx context.Context, agent people.Contact) []gateway.ToolDefinition {
	tools := peopleAgentToolDefinitions(e.engineToolDefinitionsFor(peopleAgentExecutionMode()))
	tools = append(tools, peopleAgentToolDefinitions(e.skillToolDefinitions())...)
	eq := e.equipmentForNames(ctx, []string{agent.Nickname})
	tools = append(tools, peopleAgentToolDefinitions(e.mcpToolDefinitionsRestricted(eq.McpIDs, true))...)
	return tools
}

func (e *Engine) runPeopleAgentTool(ctx context.Context, sessionID string, agent people.Contact, call gateway.ToolCall) string {
	if !peopleAgentAllowedTool(call.Name) {
		return "ok:false\n同事聊天不能用这个工具。"
	}
	if reason, deny := unattendedMcpDenied(call.Name, call.Arguments); deny {
		return reason
	}
	eq := e.equipmentForNames(ctx, []string{agent.Nickname})
	var r toolruntime.Result
	var err error
	switch call.Name {
	case "skill.invoke":
		r, err = e.invokeSkillTool(ctx, peopleAgentExecutionMode(), sessionID, call.Arguments)
	case "skill.view":
		r, err = e.invokeSkillViewTool(ctx, call.Arguments)
	case "browser.act":
		r, err = e.invokeBrowserAct(ctx, peopleAgentExecutionMode(), sessionID, call.Arguments)
	case "image.generate", "video.generate":
		r, err = e.invokeMediaGenerate(ctx, call.Name, call.Arguments)
	default:
		if call.Name == "mcp.search" {
			out, sErr := e.searchMcpToolsFiltered(call.Arguments, eq.McpIDs, true)
			if sErr != nil {
				return "ok:false\n" + sErr.Error()
			}
			return out
		}
		if call.Name == "mcp.call" {
			out, cErr := e.callMcpToolByNameGuarded(ctx, call.Arguments, eq.McpIDs, true)
			if cErr != nil {
				return "ok:false\n" + cErr.Error()
			}
			return out
		}
		if eid, tool, ok := parseMcpToolName(call.Name); ok {
			if !e.mcpNameAllowed(call.Name, eid, eq.McpIDs, true) {
				return "ok:false\n未授权这个 MCP。"
			}
			out, mErr := e.invokeMcpTool(ctx, eid, tool, call.Arguments)
			if mErr != nil {
				return "ok:false\n" + mErr.Error()
			}
			return out
		}
		if e.tools == nil {
			return "ok:false\n工具运行时不可用。"
		}
		r, err = e.executeUserTool(ctx, peopleAgentExecutionMode(), sessionID, call.Name, call.Arguments)
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

func (e *Engine) completePeopleAgentText(ctx context.Context, agent people.Contact, sessionID, userText string) (string, error) {
	if e.providers == nil {
		return "", nil
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	entry, ok := e.resolvePreferredChatModel(items)
	if !ok {
		return "", nil
	}
	system := e.peopleAgentTurnPrompt(ctx, agent, sessionID, userText)
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
