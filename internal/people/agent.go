package people

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

// AgentOrgName marks locally registered conversation specialists in the roster.
// They are trusted without LAN pairing so they can join 同事群.
const AgentOrgName = "月汐智能体"

func IsAgentContact(c Contact) bool {
	return strings.TrimSpace(c.OrgName) == AgentOrgName
}

func (s *Service) UpsertAgentContact(ctx context.Context, c Contact) error {
	if err := s.readyUnlocked(); err != nil {
		return err
	}
	c.SubjectID = strings.TrimSpace(c.SubjectID)
	c.Nickname = strings.TrimSpace(c.Nickname)
	if c.SubjectID == "" || c.Nickname == "" || utf8.RuneCountInString(c.Nickname) > 64 {
		return ErrInvalid
	}
	now := nowRFC3339()
	if existing, err := s.store.GetContact(ctx, c.SubjectID); err == nil {
		c.CreatedAt = existing.CreatedAt
		c.Remark = existing.Remark
		c.Blocked = existing.Blocked
	} else {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.LastSeenAt == "" {
		c.LastSeenAt = now
	}
	if c.Status == "" {
		c.Status = "online"
	}
	c.OrgName = AgentOrgName
	c.TrustState = "trusted"
	c.PublicKey = ""
	c.PairingHash = ""
	c.HostAddr = ""
	c.Self = false
	return s.store.UpsertContact(ctx, c)
}

func (s *Service) SendAs(ctx context.Context, senderID, threadID, body string) (Message, error) {
	if err := s.readyUnlocked(); err != nil {
		return Message{}, err
	}
	senderID = strings.TrimSpace(senderID)
	threadID = strings.TrimSpace(threadID)
	body = strings.TrimSpace(body)
	if senderID == "" || threadID == "" || body == "" || utf8.RuneCountInString(body) > maxText {
		return Message{}, ErrInvalid
	}
	t, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return Message{}, ErrNotFound
	}
	sender, err := s.store.GetContact(ctx, senderID)
	if err != nil {
		return Message{}, ErrNotFound
	}
	if !IsAgentContact(sender) || sender.Blocked {
		return Message{}, ErrInvalid
	}
	if !threadHasMember(t, senderID) {
		return Message{}, ErrNotFound
	}
	msg := Message{
		MessageID: ulid.Make().String(),
		ThreadID:  t.ThreadID,
		SenderID:  senderID,
		Kind:      "text",
		Body:      body,
		CreatedAt: nowRFC3339(),
	}
	if err := s.store.InsertMessage(ctx, msg, nil); err != nil {
		return Message{}, err
	}
	go s.deliverMessage(t, msg, "")
	return msg, nil
}

func (s *Service) SendSystem(ctx context.Context, threadID, body string) (Message, error) {
	if err := s.readyUnlocked(); err != nil {
		return Message{}, err
	}
	threadID = strings.TrimSpace(threadID)
	body = strings.TrimSpace(body)
	if threadID == "" || body == "" || utf8.RuneCountInString(body) > maxText {
		return Message{}, ErrInvalid
	}
	t, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return Message{}, ErrNotFound
	}
	msg := Message{
		MessageID: ulid.Make().String(),
		ThreadID:  t.ThreadID,
		SenderID:  s.identity.SubjectID(),
		Kind:      "system",
		Body:      body,
		CreatedAt: nowRFC3339(),
	}
	if err := s.store.InsertMessage(ctx, msg, nil); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func threadHasMember(t Thread, subjectID string) bool {
	for _, member := range t.Members {
		if member.SubjectID == subjectID {
			return true
		}
	}
	return false
}
