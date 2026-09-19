package agenthub

const EventLunitideSession = "lunitide_session"

func LunitideSessionID(events []ThreadEvent) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == EventLunitideSession && events[i].Detail != "" {
			return events[i].Detail
		}
	}
	return ""
}

func (s *Service) BindLunitideSession(threadID, sessionID string) error {
	if s == nil || s.Threads == nil || threadID == "" || sessionID == "" {
		return nil
	}
	events, err := s.Threads.ListEvents(threadID)
	if err != nil {
		return err
	}
	if existing := LunitideSessionID(events); existing != "" {
		return nil
	}
	return insertThreadEvent(s.Threads, threadID, AgentEvent{
		Type:   EventLunitideSession,
		Title:  "对话投影",
		Detail: sessionID,
	})
}

func AttachLunitideSession(detail ThreadDetail) ThreadDetail {
	if detail.SessionID == "" {
		detail.SessionID = LunitideSessionID(detail.Events)
	}
	return detail
}
