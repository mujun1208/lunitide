package meetings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type catchupResult struct {
	StartedMS int64  `json:"startedMs"`
	Samples   int    `json:"samples"`
	Digest    string `json:"digest"`
	Text      string `json:"text"`
	Error     string `json:"error,omitempty"`
}

type catchupJournal struct {
	Version            int                      `json:"version"`
	MeetingID          string                   `json:"meetingId"`
	FromMS             int64                    `json:"fromMs"`
	ReplaceLive        bool                     `json:"replaceLive"`
	OriginalTranscript string                   `json:"originalTranscript"`
	LastTranscript     string                   `json:"lastTranscript"`
	PreparedTranscript *string                  `json:"preparedTranscript,omitempty"`
	PreparedUpdatedAt  string                   `json:"preparedUpdatedAt,omitempty"`
	Results            map[string]catchupResult `json:"results"`
}

func pcmDigest(pcm []byte) string { sum := sha256.Sum256(pcm); return hex.EncodeToString(sum[:]) }

func (s *Service) CatchUp(ctx context.Context, meetingID string, expectedRevision ...int64) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	m, err := s.Metadata(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if m.Status == StatusRecording {
		return Meeting{}, ErrNotRecording
	}
	if len(expectedRevision) > 0 && (expectedRevision[0] < 1 || m.Revision != expectedRevision[0]) {
		return Meeting{}, ErrConflict
	}
	s.mu.Lock()
	if s.catchingUp[meetingID] || s.summarizing[meetingID] {
		s.mu.Unlock()
		return Meeting{}, ErrBusy
	}
	s.catchingUp[meetingID] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.catchingUp, meetingID); s.mu.Unlock() }()
	if strings.TrimSpace(s.audioRoot) == "" {
		if capacityMarked(m) {
			return m, ErrCapacity
		}
		return m, nil
	}
	path := filepath.Join(audioDir(s.audioRoot, meetingID), "catchup.json")
	var journal catchupJournal
	exists, err := readMeetingJSON(path, &journal)
	if err != nil {
		return Meeting{}, err
	}
	if !exists {
		audioMS := s.audioDurationMS(meetingID)
		lastMS, hasText, cover, coverageErr := s.segmentCoverage(ctx, m)
		if coverageErr != nil {
			if !errors.Is(coverageErr, ErrCapacity) {
				return Meeting{}, coverageErr
			}
			if !capacityMarked(m) {
				return s.finishCapacity(m)
			}
			lastMS, hasText, cover = 0, false, 0
		}
		if !capacityMarked(m) && !needsCatchupRunes(audioMS, lastMS, hasText, cover) {
			return m, nil
		}
		journal = catchupJournal{Version: 1, MeetingID: meetingID, OriginalTranscript: m.Transcript, LastTranscript: m.Transcript, ReplaceLive: shouldReplaceLiveCaptions(audioMS, lastMS, cover), Results: map[string]catchupResult{}}
		if hasText && lastMS > 0 && !journal.ReplaceLive {
			journal.FromMS = lastMS + catchupOverlapMS
		}
		if capacityMarked(m) {
			journal.FromMS = 0
			journal.ReplaceLive = true
		}
		if err := writeMeetingJSON(path, journal); err != nil {
			return Meeting{}, err
		}
	}
	if journal.Version != 1 || journal.MeetingID != meetingID || journal.Results == nil {
		return Meeting{}, ErrInvalid
	}
	resultRunes := 0
	if len(journal.Results) > 4096 {
		return s.finishCapacity(m)
	}
	for _, result := range journal.Results {
		resultRunes += utf8.RuneCountInString(result.Text)
		if resultRunes > maxTranscript {
			return s.finishCapacity(m)
		}
	}
	if m.Transcript != journal.LastTranscript {
		if journal.PreparedTranscript == nil || m.Transcript != *journal.PreparedTranscript || m.UpdatedAt != journal.PreparedUpdatedAt {
			return Meeting{}, ErrConflict
		}
		// SQLite committed but the final journal rename was interrupted. The
		// prepared source version proves this is our output, not a manual edit.
		journal.LastTranscript = m.Transcript
		journal.PreparedTranscript = nil
		journal.PreparedUpdatedAt = ""
		if err := writeMeetingJSON(path, journal); err != nil {
			return Meeting{}, err
		}
	}
	if s.transcribe == nil {
		return s.finishNeedsSummary(m, "本机补转写不可用，原始逐字稿和音频已保留。装好本机识别后可重试。")
	}
	lifetime, release, scopeErr := s.jobScope(ctx, "stt")
	if scopeErr != nil {
		return Meeting{}, scopeErr
	}
	defer release()
	work, cancel := context.WithTimeout(lifetime, catchupJobDeadline)
	defer cancel()
	// Persist every span result independently. A successful tail never hides a
	// failed earlier span, and restarting the process retries only missing spans.
	for pass := 0; pass < 2; pass++ {
		empty := false
		err = walkAudioSpans(audioDir(s.audioRoot, meetingID), journal.FromMS, func(span audioSpan) error {
			if err := work.Err(); err != nil {
				return err
			}
			digest := pcmDigest(span.pcm)
			key := fmt.Sprintf("%d:%d", span.startedMS, len(span.pcm)/2)
			previous, found := journal.Results[key]
			if !found && len(journal.Results) >= 4096 {
				return ErrCapacity
			}
			if found && previous.Digest != digest {
				return ErrConflict
			}
			if found && previous.Error == "" && previous.Text != "" {
				return nil
			}
			// Retry empty first-pass results once, but leave decoder failures for
			// the next explicit retry instead of paying for them in a tight loop.
			if pass > 0 && found && previous.Error != "" {
				return nil
			}
			text, decodeErr := s.transcribe(work, span.pcm)
			if work.Err() != nil {
				decodeErr = work.Err()
				text = ""
			}
			result := catchupResult{StartedMS: span.startedMS, Samples: len(span.pcm) / 2, Digest: digest, Text: strings.TrimSpace(text)}
			nextRunes := resultRunes - utf8.RuneCountInString(previous.Text) + utf8.RuneCountInString(result.Text)
			capacity := nextRunes > maxTranscript
			if capacity {
				decodeErr = ErrCapacity
			}
			if decodeErr != nil {
				result.Error = clipRunes(decodeErr.Error(), 128)
				result.Text = ""
			}
			if result.Text == "" && result.Error == "" {
				empty = true
			}
			journal.Results[key] = result
			resultRunes = resultRunes - utf8.RuneCountInString(previous.Text) + utf8.RuneCountInString(result.Text)
			if err := writeMeetingJSON(path, journal); err != nil {
				return err
			}
			if capacity {
				return ErrCapacity
			}
			return nil
		})
		if err != nil || !empty {
			break
		}
	}
	if err != nil && work.Err() == nil {
		if errors.Is(err, ErrCapacity) {
			return s.finishCapacity(m)
		}
		return Meeting{}, err
	}
	var ordered []catchupResult
	for _, result := range journal.Results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].StartedMS < ordered[j].StartedMS })
	var generated []Segment
	var gaps []string
	for _, result := range ordered {
		if result.Error != "" {
			gaps = append(gaps, fmt.Sprintf("%d–%d秒", result.StartedMS/1000, (result.StartedMS+int64(result.Samples)*1000/audioSampleRate)/1000))
			continue
		}
		if result.Text != "" {
			generated = append(generated, Segment{Text: result.Text, StartedMS: result.StartedMS})
		}
	}
	transcript, aggregateErr := assembleTranscript(generated)
	if aggregateErr != nil {
		return s.finishCapacity(m)
	}
	if !journal.ReplaceLive || len(gaps) > 0 || work.Err() != nil || transcript == "" {
		transcript, aggregateErr = assembleTranscript(append([]Segment{{Text: journal.OriginalTranscript}}, generated...))
		if aggregateErr != nil {
			return s.finishCapacity(m)
		}
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	persist, persistCancel := persistCtx()
	defer persistCancel()
	current, readErr := s.store.GetMeeting(persist, meetingID)
	if readErr != nil {
		return Meeting{}, readErr
	}
	if current.UpdatedAt != m.UpdatedAt {
		return Meeting{}, ErrConflict
	}
	if transcript == current.Transcript && len(gaps) == 0 && work.Err() == nil && (current.Status == StatusReady || current.Status == StatusTranscribed) {
		return s.Detail(persist, meetingID)
	}
	previousUpdatedAt := current.UpdatedAt
	transcriptChanged := current.Transcript != transcript
	if transcriptChanged {
		current.TranscriptRevision++
	}
	current.Transcript = transcript
	current.UpdatedAt = nextMeetingTime(previousUpdatedAt)
	if len(gaps) > 0 || work.Err() != nil {
		current.Status = StatusNeedsSummary
		current.SummaryError = "转写补全存在缺口，原稿已保留，请重试补转写。"
		if len(gaps) > 0 {
			current.SummaryError += " 未完成：" + strings.Join(gaps, "、")
		}
		if work.Err() != nil {
			current.SummaryError += " 本次处理已中断。"
		}
		current.SummaryError = clipRunes(current.SummaryError, 1024)
	} else if strings.TrimSpace(transcript) != "" {
		current.Status = StatusTranscribed
		current.SummaryError = ""
		if transcriptChanged && (current.Summary != "" || current.Actions != "") {
			current.Status = StatusNeedsSummary
			current.SummaryError = "补转写已更新逐字稿，旧摘要已保留；请基于当前原稿重新生成。"
		}
	}
	journal.PreparedTranscript = &transcript
	journal.PreparedUpdatedAt = current.UpdatedAt
	if err := writeMeetingJSON(path, journal); err != nil {
		return Meeting{}, err
	}
	if err := s.persistMeetingVersion(current, previousUpdatedAt); err != nil {
		return Meeting{}, err
	}
	journal.LastTranscript = transcript
	journal.PreparedTranscript = nil
	journal.PreparedUpdatedAt = ""
	if err := writeMeetingJSON(path, journal); err != nil {
		return Meeting{}, err
	}
	return s.Detail(persist, meetingID)
}
