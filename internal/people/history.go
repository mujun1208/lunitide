package people

import (
	"context"
	"encoding/json"
)

const maxHistoryBytes = 512 << 10

type historyReader interface {
	ListPeopleMessagesBefore(context.Context, string, string, int) ([]Message, error)
}

// History is cursor-based and binds the cursor to the already authorized thread.
// It does not mark older messages as newly read or emit additional read notices.
func (s *Service) History(ctx context.Context, threadID, before string) (Thread, []Message, error) {
	t, err := s.PeekThread(ctx, threadID)
	if err != nil {
		return Thread{}, nil, err
	}
	reader, ok := s.store.(historyReader)
	if !ok {
		return Thread{}, nil, ErrUnavailable
	}
	items, err := reader.ListPeopleMessagesBefore(ctx, threadID, before, maxMessages)
	if err != nil {
		return Thread{}, nil, err
	}
	items, err = boundHistory(items)
	return t, items, err
}

// Preserve full message bodies; page at a message boundary instead of silently
// clipping a message or overflowing the bridge envelope with 200 large texts.
func boundHistory(items []Message) ([]Message, error) {
	size, first := 2, len(items)
	for i := len(items) - 1; i >= 0; i-- {
		raw, err := json.Marshal(items[i])
		if err != nil {
			return nil, err
		}
		if len(raw)+2 > maxHistoryBytes {
			return nil, ErrInvalid
		}
		if size+len(raw)+1 > maxHistoryBytes {
			break
		}
		size += len(raw) + 1
		first = i
	}
	return items[first:], nil
}
