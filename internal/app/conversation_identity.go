package app

import (
	"context"
	"strings"
)

type Surface string

const (
	SurfaceSession   Surface = "session"
	SurfaceCompanion Surface = "companion"
	SurfacePeople    Surface = "people"
)

const (
	identityKindSession = "session"
	identityKindPeople  = "people_thread"
)

// ConversationIdentity is the shared "who is this turn" record for session,
// companion, and colleague-thread surfaces. BoundSessionID is a real sessions
// row; it must never equal a people thread ULID.
type ConversationIdentity struct {
	Kind                  string // session | people_thread
	ContainerID           string
	LocalSubjectID        string
	MemorySubjectID       string
	MountedExpertIDs      []string
	ParticipantSubjectIDs []string
	BoundSessionID        string
}

type TurnMention struct {
	Kind string // expert | skill | member
	ID   string
	Name string
}

type TurnIntent struct {
	Surface       Surface
	Text          string
	Mentions      []TurnMention
	ContextRefs   []string
	ExecutionMode string
	Companion     bool
	ProjectID     string
}

func (e *Engine) conversationIdentityForSession(ctx context.Context, sessionID string, companion bool) ConversationIdentity {
	_ = companion
	subject := ""
	if e != nil {
		subject = e.memorySubjectID()
	}
	var mounted []string
	if e != nil && strings.TrimSpace(sessionID) != "" {
		mounted = e.sessionMountedExpertIDs(ctx, sessionID)
	}
	return ConversationIdentity{
		Kind:             identityKindSession,
		ContainerID:      sessionID,
		LocalSubjectID:   subject,
		MemorySubjectID:  subject,
		MountedExpertIDs: mounted,
		BoundSessionID:   sessionID,
	}
}

func (e *Engine) conversationIdentityForPeople(ctx context.Context, threadID, titleHint string, participants []string) (ConversationIdentity, error) {
	subject := ""
	if e != nil {
		subject = e.memorySubjectID()
	}
	bound, err := e.ensurePeopleBoundSession(ctx, threadID, titleHint)
	return ConversationIdentity{
		Kind:                  identityKindPeople,
		ContainerID:           threadID,
		LocalSubjectID:        subject,
		MemorySubjectID:       subject,
		MountedExpertIDs:      append([]string{}, participants...),
		ParticipantSubjectIDs: append([]string{}, participants...),
		BoundSessionID:        bound,
	}, err
}

func turnIntentForChat(companion bool, text, projectID, mode string, refs []string) TurnIntent {
	surface := SurfaceSession
	if companion {
		surface = SurfaceCompanion
	}
	return TurnIntent{
		Surface:       surface,
		Text:          text,
		Mentions:      ParseTurnMentions(text),
		ContextRefs:   append([]string{}, refs...),
		ExecutionMode: mode,
		Companion:     companion,
		ProjectID:     projectID,
	}
}

func turnIntentForPeople(text string) TurnIntent {
	return TurnIntent{
		Surface:  SurfacePeople,
		Text:     text,
		Mentions: ParseTurnMentions(text),
	}
}

func (ident ConversationIdentity) sessionKey(fallback string) string {
	if id := strings.TrimSpace(ident.BoundSessionID); id != "" {
		return id
	}
	return strings.TrimSpace(fallback)
}
