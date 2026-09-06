package meetings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const SummarySourcePageRunes = 16_384

type SummarySourcePage struct {
	MeetingID      string `json:"meetingId"`
	SourceRevision int64  `json:"sourceRevision"`
	SourceDigest   string `json:"sourceDigest"`
	Title          string `json:"title"`
	Transcript     string `json:"transcript"`
	Offset         int    `json:"offset"`
	NextOffset     int    `json:"nextOffset"`
	TotalRunes     int    `json:"totalRunes"`
}

// The digest pins every page to the same output's actual source. Regeneration
// while the reader navigates returns a conflict instead of mixing snapshots.
func (s *Service) SummarySource(ctx context.Context, meetingID, digest string, offset int) (SummarySourcePage, error) {
	if err := s.ready(); err != nil {
		return SummarySourcePage{}, err
	}
	if !digestPattern.MatchString(digest) || offset < 0 {
		return SummarySourcePage{}, ErrInvalid
	}
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return SummarySourcePage{}, err
	}
	if m.SummarySourceRevision == 0 || m.SummarySourceDigest != digest {
		return SummarySourcePage{}, ErrConflict
	}
	runes := []rune(m.SummarySourceTranscript)
	if offset > len(runes) {
		return SummarySourcePage{}, ErrInvalid
	}
	end := min(offset+SummarySourcePageRunes, len(runes))
	next := 0
	if end < len(runes) {
		next = end
	}
	return SummarySourcePage{MeetingID: meetingID, SourceRevision: m.SummarySourceRevision, SourceDigest: digest, Title: m.SummarySourceTitle,
		Transcript: string(runes[offset:end]), Offset: offset, NextOffset: next, TotalRunes: len(runes)}, nil
}

// Bind the full cleaned input, before deterministic map/reduce splitting. The
// source and resulting summary are committed in the same meeting CAS. A failed
// or superseded generation cannot replace the previous output's source.
func (m *Meeting) bindSummarySource(title, transcript string) {
	m.SummarySourceRevision = m.TranscriptRevision
	m.SummarySourceTitle = title
	m.SummarySourceTranscript = transcript
	raw, _ := json.Marshal(struct {
		Title      string `json:"title"`
		Transcript string `json:"transcript"`
	}{title, transcript})
	sum := sha256.Sum256(raw)
	m.SummarySourceDigest = hex.EncodeToString(sum[:])
	m.SummaryEdited = false
}

func summarySourceDescription(m Meeting) string {
	if m.Summary == "" && m.Actions == "" {
		return "尚未生成摘要"
	}
	if m.SummarySourceRevision == 0 || m.SummarySourceDigest == "" {
		return "摘要输入版本未知，请核对或重新生成"
	}
	line := fmt.Sprintf("摘要依据逐字稿版本 %d；当前逐字稿版本 %d", m.SummarySourceRevision, m.TranscriptRevision)
	if m.SummarySourceRevision != m.TranscriptRevision {
		line += "。原稿已变更，保留的旧摘要需要重新生成"
	}
	if m.SummaryEdited {
		line += "。摘要或待办已人工编辑"
	}
	return line
}
