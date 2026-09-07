package app

import (
	"context"
	"slices"

	"github.com/lunitide/lunitide/internal/contextapp"
)

// chatHistoryReader projects persisted rows before envelope validation. Stored
// tool results have no paired provider tool call, so they are historical notes,
// not live protocol tool messages. The database and its audit rows stay intact.
type chatHistoryReader struct {
	contextapp.Reader
	afterSequence int64
}

func (r chatHistoryReader) ListMessages(ctx context.Context, session, direction string, limit int) ([]contextapp.Message, error) {
	rows, err := r.Reader.ListMessages(ctx, session, direction, limit)
	if err != nil {
		return nil, err
	}
	rows = append([]contextapp.Message(nil), rows...)
	if direction == "backward" {
		slices.Reverse(rows)
	}
	out := make([]contextapp.Message, 0, len(rows))
	for _, row := range rows {
		if r.afterSequence > 0 && row.Sequence <= r.afterSequence {
			continue
		}
		if row.Role == "tool" {
			row.Role = "user"
			row.Content = foldHistoricalToolResult(row.Content)
			row.TokenCount = 0
		}
		// Interrupted/failed assistant persistence may leave adjacent assistant
		// rows. They are one historical answer; retain both bodies without inventing
		// a user turn or dropping the failed response.
		if n := len(out); n > 0 && row.Role == "assistant" && out[n-1].Role == "assistant" {
			out[n-1].Content += "\n\n" + row.Content
			out[n-1].TokenCount = 0
			continue
		}
		out = append(out, row)
	}
	if direction == "backward" {
		slices.Reverse(out)
	}
	return out, nil
}
