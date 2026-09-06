package meetings

import (
	"context"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const TranscriptPageRunes = 16_384
const SegmentPageSize = 10

type TranscriptPage struct {
	MeetingID          string `json:"meetingId"`
	TranscriptRevision int64  `json:"transcriptRevision"`
	Text               string `json:"text"`
	Offset             int    `json:"offset"`
	NextOffset         int    `json:"nextOffset"`
	TotalRunes         int    `json:"totalRunes"`
}

type SegmentPage struct {
	Items      []Segment `json:"items"`
	NextSeq    int       `json:"nextSeq"`
	HasMore    bool      `json:"hasMore"`
	ThroughSeq int       `json:"throughSeq"`
	Revision   int64     `json:"revision"`
}

type TranscriptEdit struct {
	TranscriptRevision int64  `json:"transcriptRevision"`
	Offset             int    `json:"offset"`
	DeleteRunes        int    `json:"deleteRunes"`
	Text               string `json:"text"`
}

type segmentPageStore interface {
	ReadMeetingSegmentPage(context.Context, string, int64, int, int) (SegmentPage, error)
}

// Metadata loads no document copies or unbounded segment collection. Internal
// export/edit paths keep the complete authoritative transcript in this row.
func (s *Service) Metadata(ctx context.Context, id string) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	if _, err := ulid.ParseStrict(id); err != nil {
		return Meeting{}, ErrInvalid
	}
	m, err := s.store.GetMeeting(ctx, id)
	if err != nil {
		return Meeting{}, err
	}
	if next, changed, reclaimErr := s.maybeReclaimSummarizing(m); reclaimErr == nil && changed {
		m = next
	}
	return m, nil
}

func (s *Service) Detail(ctx context.Context, id string) (Meeting, error) {
	m, err := s.Metadata(ctx, id)
	if err != nil {
		return Meeting{}, err
	}
	page, err := s.Segments(ctx, id, m.Revision, 0, 0)
	if err != nil {
		return Meeting{}, err
	}
	m.Segments = page.Items
	return m, nil
}

func (s *Service) Transcript(ctx context.Context, id string, revision int64, offset int) (TranscriptPage, error) {
	if offset < 0 || revision < 0 {
		return TranscriptPage{}, ErrInvalid
	}
	m, err := s.Metadata(ctx, id)
	if err != nil {
		return TranscriptPage{}, err
	}
	if m.TranscriptRevision != revision {
		return TranscriptPage{}, ErrConflict
	}
	text := []rune(m.Transcript)
	if offset > len(text) {
		return TranscriptPage{}, ErrInvalid
	}
	end := min(offset+TranscriptPageRunes, len(text))
	next := 0
	if end < len(text) {
		next = end
	}
	return TranscriptPage{MeetingID: id, TranscriptRevision: revision, Text: string(text[offset:end]), Offset: offset, NextOffset: next, TotalRunes: len(text)}, nil
}

func (s *Service) Segments(ctx context.Context, id string, revision int64, afterSeq, throughSeq int) (SegmentPage, error) {
	if err := s.ready(); err != nil {
		return SegmentPage{}, err
	}
	if _, err := ulid.ParseStrict(id); err != nil || revision < 1 || afterSeq < 0 || throughSeq < 0 || (throughSeq > 0 && afterSeq > throughSeq) {
		return SegmentPage{}, ErrInvalid
	}
	if store, ok := s.store.(segmentPageStore); ok {
		return store.ReadMeetingSegmentPage(ctx, id, revision, afterSeq, throughSeq)
	}
	// Compatibility for small in-memory adapters; production uses a bounded
	// SQL query and verifies the revision in that same read transaction.
	m, err := s.store.GetMeeting(ctx, id)
	if err != nil {
		return SegmentPage{}, err
	}
	if m.Revision != revision {
		return SegmentPage{}, ErrConflict
	}
	all, err := s.store.ListSegments(ctx, id)
	if err != nil {
		return SegmentPage{}, err
	}
	if throughSeq == 0 {
		for _, seg := range all {
			throughSeq = max(throughSeq, seg.Seq)
		}
	}
	page := SegmentPage{Items: []Segment{}, ThroughSeq: throughSeq, Revision: revision}
	for _, seg := range all {
		if seg.Seq <= afterSeq || seg.Seq > throughSeq {
			continue
		}
		if len(page.Items) == SegmentPageSize {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, seg)
	}
	if page.HasMore {
		page.NextSeq = page.Items[len(page.Items)-1].Seq
	}
	return page, nil
}

func applyTranscriptEdit(m Meeting, edit TranscriptEdit) (string, error) {
	if edit.TranscriptRevision != m.TranscriptRevision {
		return "", ErrConflict
	}
	if edit.Offset < 0 || edit.DeleteRunes < 0 || edit.DeleteRunes > TranscriptPageRunes || !utf8.ValidString(edit.Text) || utf8.RuneCountInString(edit.Text) > TranscriptPageRunes {
		return "", ErrInvalid
	}
	old := []rune(m.Transcript)
	if edit.Offset > len(old) || edit.DeleteRunes > len(old)-edit.Offset {
		return "", ErrInvalid
	}
	next := string(old[:edit.Offset]) + edit.Text + string(old[edit.Offset+edit.DeleteRunes:])
	if utf8.RuneCountInString(next) > maxTranscript {
		return "", ErrInvalid
	}
	return next, nil
}
