package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/oklog/ulid/v2"
)

var errPeopleBoundSession = errors.New("people bound session unavailable")

type sessionByID interface {
	Get(context.Context, string) (session.Session, error)
}

func (e *Engine) ensurePeopleBoundSession(ctx context.Context, threadID, titleHint string) (string, error) {
	if e == nil || e.people == nil || e.sessions == nil || e.projects == nil {
		return "", errPeopleBoundSession
	}
	if !validCanonicalULID(threadID) {
		return "", errPeopleBoundSession
	}
	if existing, ok, err := e.people.ThreadSession(ctx, threadID); err != nil {
		return "", err
	} else if ok {
		if e.peopleBoundSessionExists(ctx, existing) {
			return existing, nil
		}
		_ = e.people.ClearThreadSession(ctx, threadID)
	}
	projectID, err := e.ensurePersonalChatProject(ctx)
	if err != nil {
		return "", err
	}
	title, err := session.NormalizeTitle(peopleBoundSessionTitle(titleHint))
	if err != nil {
		return "", err
	}
	created, err := e.sessions.Create(ctx, ulid.Make().String(), "people-agent", map[string]string{
		"projectId": projectID, "title": title,
	}, session.Session{ProjectID: projectID, Title: title})
	if err != nil {
		return "", err
	}
	if _, dirErr := e.sessionOutputDir(created.ID); dirErr != nil {
		_ = dirErr
	}
	if err := e.people.BindThreadSession(ctx, threadID, created.ID); err != nil {
		if existing, ok, lookErr := e.people.ThreadSession(ctx, threadID); lookErr == nil && ok {
			return existing, nil
		}
		return "", err
	}
	if bound, ok, lookErr := e.people.ThreadSession(ctx, threadID); lookErr == nil && ok {
		return bound, nil
	}
	return created.ID, nil
}

func (e *Engine) peopleBoundSessionExists(ctx context.Context, sessionID string) bool {
	if e == nil || sessionID == "" {
		return false
	}
	getter, ok := e.sessions.(sessionByID)
	if !ok {
		return validCanonicalULID(sessionID)
	}
	_, err := getter.Get(ctx, sessionID)
	return err == nil
}

func (e *Engine) noticePeopleBoundSessionFailure(ctx context.Context, threadID string, msgs []people.Message) []people.Message {
	note := peopleBoundSessionUserError()
	for _, m := range msgs {
		if m.Kind == "system" && m.Body == note {
			return msgs
		}
	}
	if e == nil || e.people == nil || !validCanonicalULID(threadID) {
		return msgs
	}
	if _, err := e.people.SendSystem(ctx, threadID, note); err != nil {
		return msgs
	}
	if next, err := e.people.ListMessages(ctx, threadID, 64); err == nil {
		return next
	}
	return msgs
}

func peopleBoundSessionTitle(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return "同事对话"
	}
	hint = strings.TrimPrefix(hint, "同事 · ")
	hint = strings.TrimPrefix(hint, "同事 ")
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return "同事对话"
	}
	return hint
}

func isColleagueChatTitle(title string) bool {
	t := strings.TrimSpace(title)
	if t == "同事对话" {
		return true
	}
	return strings.HasPrefix(t, "同事 · ") || strings.HasPrefix(t, "同事·")
}

func (e *Engine) peopleBoundSessionSet(ctx context.Context) map[string]struct{} {
	out := map[string]struct{}{}
	if e == nil || e.people == nil {
		return out
	}
	ids, err := e.people.ListBoundSessionIDs(ctx)
	if err != nil {
		return out
	}
	for _, id := range ids {
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func (e *Engine) ordinaryChatSessions(ctx context.Context, items []session.Session) []session.Session {
	bound := e.peopleBoundSessionSet(ctx)
	out := make([]session.Session, 0, len(items))
	for _, item := range items {
		if _, ok := bound[item.ID]; ok {
			continue
		}
		if isColleagueChatTitle(item.Title) {
			continue
		}
		out = append(out, item)
	}
	return out
}
