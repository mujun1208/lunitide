package meetings

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxTranscriptRunes = 1 << 20
const CapacityNotice = "会议内容超过单份原稿容量，未截断或覆盖原稿；已收录音和原始片段仍保留，可分页查看片段、重试补转写或另行整理。"

var ErrCapacity = errors.New("meeting content capacity exceeded")

type boundedSegmentStore interface {
	ReadMeetingSegmentsBounded(context.Context, string) ([]Segment, error)
	AppendMeetingSegmentBounded(context.Context, Segment) (Segment, error)
	MeetingSegmentCoverage(context.Context, string) (int64, int, error)
}

func (s *Service) boundedSegments(ctx context.Context, id string) ([]Segment, error) {
	if store, ok := s.store.(boundedSegmentStore); ok {
		return store.ReadMeetingSegmentsBounded(ctx, id)
	}
	items, err := s.store.ListSegments(ctx, id)
	if err != nil {
		return nil, err
	}
	count := 0
	for i, seg := range items {
		count += utf8.RuneCountInString(seg.Text)
		if i > 0 {
			count++
		}
		if count > maxTranscript || i >= MaxSegments {
			return nil, ErrCapacity
		}
	}
	return items, nil
}

func (s *Service) segmentCoverage(ctx context.Context, m Meeting) (int64, bool, int, error) {
	if store, ok := s.store.(boundedSegmentStore); ok {
		last, n, err := store.MeetingSegmentCoverage(ctx, m.MeetingID)
		if n == 0 {
			n = liveCaptionRunes(m.Transcript)
		}
		return last, n > 0, n, err
	}
	segs, err := s.boundedSegments(ctx, m.MeetingID)
	if err != nil {
		return 0, false, 0, err
	}
	last, has := lastSegmentWatermark(segs, m.Transcript)
	return last, has, captionCoverageRunes(segs, m.Transcript), nil
}

func capacityMarked(m Meeting) bool { return strings.HasPrefix(m.SummaryError, CapacityNotice) }

func (s *Service) finishCapacity(m Meeting) (Meeting, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	ctx, cancel := persistCtx()
	defer cancel()
	current, err := s.store.GetMeeting(ctx, m.MeetingID)
	if err != nil {
		return Meeting{}, err
	}
	if current.Revision != m.Revision {
		return Meeting{}, ErrConflict
	}
	previous := current.UpdatedAt
	current.Status = StatusNeedsSummary
	current.SummaryError = CapacityNotice
	current.UpdatedAt = nextMeetingTime(previous)
	if updated, err := s.store.CompareAndSwapMeeting(ctx, previous, current); err != nil {
		return Meeting{}, err
	} else if !updated {
		return Meeting{}, ErrConflict
	}
	current, err = s.Detail(ctx, m.MeetingID)
	if err != nil {
		return current, err
	}
	return current, ErrCapacity
}
