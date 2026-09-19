package app

import (
	"context"
	"log"
	"strings"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
)

func (e *Engine) attachHubSession(_ context.Context, detail agenthub.ThreadDetail) agenthub.ThreadDetail {
	return agenthub.AttachLunitideSession(detail)
}

func (e *Engine) ensureHubSession(ctx context.Context, detail agenthub.ThreadDetail) agenthub.ThreadDetail {
	detail = e.attachHubSession(ctx, detail)
	if detail.SessionID != "" {
		return detail
	}
	sid, err := e.bindHubChatSession(ctx, detail.Thread.ID, detail.Thread.Title)
	if err != nil || sid == "" {
		return detail
	}
	detail.SessionID = sid
	return detail
}

func (e *Engine) bindHubChatSession(ctx context.Context, threadID, titleHint string) (string, error) {
	if e == nil || e.agentHub == nil || e.sessions == nil || e.projects == nil || threadID == "" {
		return "", nil
	}
	projectID, err := e.ensurePersonalChatProject(ctx)
	if err != nil {
		return "", err
	}
	titleHint = strings.TrimSpace(titleHint)
	if titleHint == "" {
		titleHint = "Agent Hub"
	}
	title, err := session.NormalizeTitle(titleHint)
	if err != nil {
		title = "Agent Hub"
	}
	created, err := e.sessions.Create(ctx, "hub:"+threadID, "hub-agent", map[string]string{
		"projectId": projectID,
	}, session.Session{ProjectID: projectID, Title: title})
	if err != nil {
		return "", err
	}
	if bindErr := e.agentHub.BindLunitideSession(threadID, created.ID); bindErr != nil {
		log.Printf("hub session bind skipped: %v", bindErr)
	}
	return created.ID, nil
}

func (e *Engine) projectHubThread(ctx context.Context, detail agenthub.ThreadDetail) {
	if e == nil || e.messages == nil || detail.SessionID == "" {
		return
	}
	text := hubProjectionText(detail)
	if text == "" {
		return
	}
	key := "hub:" + detail.Thread.ID
	_, err := e.messages.Append(ctx, key, "hub-project", map[string]string{
		"sessionId": detail.SessionID,
	}, message.Message{SessionID: detail.SessionID, Role: message.RoleAssistant, Status: message.StatusCompleted, Text: text})
	if err != nil {
		log.Printf("hub session project skipped: %v", err)
	}
}

func hubProjectionText(detail agenthub.ThreadDetail) string {
	var lastUser, lastOut string
	for _, m := range detail.Messages {
		switch m.Role {
		case "user":
			lastUser = strings.TrimSpace(m.Content)
		case "assistant", "notice":
			lastOut = strings.TrimSpace(m.Content)
		}
	}
	if lastUser == "" && lastOut == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Agent Hub · ")
	b.WriteString(strings.TrimSpace(detail.Thread.HarnessID))
	if lastUser != "" {
		b.WriteString("\n用户：")
		b.WriteString(clipRunes(lastUser, 400))
	}
	if lastOut != "" {
		b.WriteString("\n结果：")
		b.WriteString(clipRunes(lastOut, 800))
	}
	return b.String()
}
