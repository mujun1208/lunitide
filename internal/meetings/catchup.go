package meetings

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// Live captions that lag the recording clock by less than this are treated
	// as complete. A 1–2 hour session whose recognizer died 20 minutes in is
	// well above this slack.
	catchupSlackMS = 15_000
	// Skip the last live utterance's start so catch-up does not repeat it.
	catchupOverlapMS = 4_000
	// Keep live captions at or above this size. A 10-minute meeting with a
	// screen of lyrics is well above it; two short fragments are not.
	minKeepLiveCaptions = 80
)

var catchupJobDeadline = 9 * time.Minute

var errCatchupInterrupted = errors.New("meetings: catch-up interrupted")

// AudioTranscriber turns a PCM span (16 kHz mono s16le) into text. Nil means
// catch-up cannot run (no sherpa/cloud ASR).
type AudioTranscriber func(ctx context.Context, pcm []byte) (string, error)

func (s *Service) SetAudioTranscriber(fn AudioTranscriber) { s.transcribe = fn }

func (s *Service) SetAudioRoot(dir string) { s.audioRoot = strings.TrimSpace(dir) }

func liveCaptionRunes(transcript string) int {
	return utf8.RuneCountInString(strings.TrimSpace(transcript))
}

func captionCoverageRunes(segs []Segment, transcript string) int {
	n := 0
	for _, seg := range segs {
		n += utf8.RuneCountInString(strings.TrimSpace(seg.Text))
	}
	if n > 0 {
		return n
	}
	return liveCaptionRunes(transcript)
}

// shouldReplaceLiveCaptions drops scraps only when they kept pace with the
// clock. A large audio gap still needs append-only catch-up so earlier live
// lines are kept. Do not wipe a usable transcript to re-decode the whole WAV.
func shouldReplaceLiveCaptions(audioMS, lastSegmentMS int64, coverRunes int) bool {
	if coverRunes >= minKeepLiveCaptions {
		return false
	}
	return audioMS-lastSegmentMS <= catchupSlackMS
}

// NeedsCatchup reports whether leftover audio should be transcribed after stop.
func NeedsCatchup(audioMS, lastSegmentMS int64, hasTranscript bool, transcript string) bool {
	return needsCatchupRunes(audioMS, lastSegmentMS, hasTranscript, liveCaptionRunes(transcript))
}

func needsCatchupRunes(audioMS, lastSegmentMS int64, hasTranscript bool, coverRunes int) bool {
	if audioMS <= 3_000 {
		return false
	}
	if !hasTranscript {
		return true
	}
	if audioMS-lastSegmentMS > catchupSlackMS {
		return true
	}
	return coverRunes < minKeepLiveCaptions
}

func lastSegmentWatermark(segs []Segment, transcript string) (int64, bool) {
	has := strings.TrimSpace(transcript) != "" || len(segs) > 0
	if len(segs) == 0 {
		return 0, has
	}
	last := segs[len(segs)-1].StartedMS
	for _, seg := range segs {
		if seg.StartedMS > last {
			last = seg.StartedMS
		}
	}
	return last, has
}

func (s *Service) CatchUp(ctx context.Context, meetingID string) (Meeting, error) {
	m, err := s.catchUpOnce(ctx, meetingID)
	if err != nil {
		return m, err
	}
	audioMS := s.audioDurationMS(meetingID)
	lastMS, hasText := lastSegmentWatermark(m.Segments, m.Transcript)
	if !needsCatchupRunes(audioMS, lastMS, hasText, captionCoverageRunes(m.Segments, m.Transcript)) {
		return m, nil
	}
	// A long session whose live ASR died mid-way can leave large audio gaps.
	// One 9-minute pass may not finish every span; retry once from the new watermark.
	return s.catchUpOnce(ctx, meetingID)
}

func (s *Service) catchUpOnce(ctx context.Context, meetingID string) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	m, err := s.Get(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if m.Status == StatusRecording {
		return Meeting{}, ErrNotRecording
	}
	audioMS := s.audioDurationMS(meetingID)
	lastMS, hasText := lastSegmentWatermark(m.Segments, m.Transcript)
	cover := captionCoverageRunes(m.Segments, m.Transcript)
	if !needsCatchupRunes(audioMS, lastMS, hasText, cover) {
		return m, nil
	}
	replaceLive := shouldReplaceLiveCaptions(audioMS, lastMS, cover)
	fromMS := int64(0)
	if hasText && lastMS > 0 && !replaceLive {
		fromMS = lastMS + catchupOverlapMS
	}
	if replaceLive {
		if err := s.store.DeleteSegments(ctx, meetingID); err != nil {
			return Meeting{}, err
		}
		hasText = false
		fromMS = 0
	}
	if strings.TrimSpace(s.audioRoot) == "" || s.transcribe == nil {
		return s.catchupUnavailable(m, hasText)
	}
	workCtx, workCancel := context.WithTimeout(context.Background(), catchupJobDeadline)
	defer workCancel()
	stop := context.AfterFunc(ctx, workCancel)
	defer stop()
	wrote := false
	visited := false
	var lastTranscribe error
	err = walkAudioSpans(audioDir(s.audioRoot, meetingID), fromMS, func(span audioSpan) error {
		visited = true
		if workCtx.Err() != nil {
			return errCatchupInterrupted
		}
		text, transErr := s.transcribe(workCtx, span.pcm)
		if transErr != nil {
			lastTranscribe = transErr
			return nil
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		if _, appendErr := s.appendCatchup(context.Background(), meetingID, text, span.startedMS); appendErr != nil {
			return appendErr
		}
		wrote = true
		return nil
	})
	if err != nil {
		m, _ = s.Get(context.Background(), meetingID)
		if errors.Is(err, errCatchupInterrupted) {
			return s.finishNeedsSummary(m, "转写补全中断，音频已保存。可重试补转写。")
		}
		return s.finishNeedsSummary(m, "转写补全失败："+err.Error())
	}
	if lastTranscribe != nil && !wrote && !hasText {
		m, _ = s.Get(context.Background(), meetingID)
		return s.finishNeedsSummary(m, "转写补全失败："+lastTranscribe.Error())
	}
	if !visited {
		return m, nil
	}
	if !wrote && !hasText {
		return m, nil
	}
	return s.rebuildTranscript(context.Background(), meetingID)
}

func (s *Service) catchupUnavailable(m Meeting, hasText bool) (Meeting, error) {
	if hasText {
		return m, nil
	}
	return s.finishNeedsSummary(m, "实时转写不完整，且补转写只用本机识别。本机识别不可用。音频已保存，装好本机识别后再重试。")
}

func (s *Service) appendCatchup(ctx context.Context, meetingID, text string, startedMS int64) (Segment, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxSegment {
		return Segment{}, ErrInvalid
	}
	kept, prev, settled := s.resolveAgainstLastSegment(ctx, meetingID, text)
	if settled {
		return prev, nil
	}
	text = kept
	n, err := s.store.CountSegments(ctx, meetingID)
	if err != nil {
		return Segment{}, err
	}
	if n >= MaxSegments {
		return Segment{}, ErrInvalid
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seg := Segment{
		SegmentID: ulid.Make().String(),
		MeetingID: meetingID,
		Seq:       n + 1,
		StartedMS: startedMS,
		Text:      text,
		CreatedAt: now,
	}
	if err := s.store.InsertSegment(ctx, seg); err != nil {
		return Segment{}, err
	}
	return seg, nil
}

func (s *Service) rebuildTranscript(ctx context.Context, meetingID string) (Meeting, error) {
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	segs, err := s.store.ListSegments(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	m.Transcript = assembleTranscript(segs)
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if m.Status == StatusNeedsSummary && strings.TrimSpace(m.Transcript) != "" && strings.Contains(m.SummaryError, "没有可用的逐字稿") {
		m.SummaryError = ""
		m.Status = StatusTranscribed
	}
	if err := s.store.UpdateMeeting(ctx, m); err != nil {
		return Meeting{}, err
	}
	return s.Get(ctx, meetingID)
}

func assembleTranscript(segs []Segment) string {
	var lines []string
	for _, seg := range segs {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		if len(lines) == 0 {
			lines = append(lines, text)
			continue
		}
		last := lines[len(lines)-1]
		if text == last || strings.HasPrefix(last, text) && utf8.RuneCountInString(text) >= 4 {
			continue
		}
		if strings.HasPrefix(text, last) && utf8.RuneCountInString(last) >= 4 {
			lines[len(lines)-1] = text
			continue
		}
		if rest, skip := peelMeetingPrefix(last, text); skip {
			continue
		} else if rest != text {
			if rest == "" {
				continue
			}
			lines = append(lines, rest)
			continue
		}
		lines = append(lines, text)
	}
	transcript := strings.Join(lines, "\n")
	if utf8.RuneCountInString(transcript) > maxTranscript {
		transcript = string([]rune(transcript)[:maxTranscript])
	}
	return transcript
}
