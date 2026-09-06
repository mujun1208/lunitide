package meetings

import (
	"context"

	"strings"
	"time"
	"unicode/utf8"
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

func assembleTranscript(segs []Segment) (string, error) {
	var lines []string
	total := 0
	for _, seg := range segs {
		text := strings.TrimSpace(seg.Text)
		total += utf8.RuneCountInString(text) + 1
		if total > maxTranscript+1 {
			return "", ErrCapacity
		}
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
		return "", ErrCapacity
	}
	return transcript, nil
}
