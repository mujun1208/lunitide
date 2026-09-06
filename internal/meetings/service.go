// Package meetings is this-PC meeting notes: microphone, optionally mixed with
// this-PC system audio, then a generated document. It never captures another
// machine's audio.
package meetings

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

var (
	ErrUnavailable  = errors.New("meetings unavailable")
	ErrNotFound     = errors.New("meeting not found")
	ErrInvalid      = errors.New("meeting request invalid")
	ErrBusy         = errors.New("a meeting is already recording")
	ErrNotRecording = errors.New("meeting is not recording")
	ErrCanceled     = errors.New("meeting picker canceled")
	ErrUnsupported  = errors.New("meeting save dialog unsupported")
	ErrConflict     = errors.New("meeting changed during operation")
)

const (
	AudioMicrophone          = "microphone"
	AudioMicrophoneAndSystem = "microphone_and_system"
)

const (
	maxTitle      = 200
	maxSegment    = 16384
	maxTranscript = MaxTranscriptRunes
	maxSummary    = 65536
	maxActions    = 32768
	maxList       = 200
	// 1–2 hour meetings at ~1s ASR chops still sit well under this.
	// Audio files on disk are the backup if the cap is ever hit.
	MaxSegments = 100_000
)

type Status string

const (
	StatusRecording    Status = "recording"
	StatusTranscribed  Status = "transcribed"
	StatusSummarizing  Status = "summarizing"
	StatusReady        Status = "ready"
	StatusNeedsSummary Status = "needs_summary"
)

type Meeting struct {
	MeetingID               string    `json:"meetingId"`
	Revision                int64     `json:"revision"`
	TranscriptRevision      int64     `json:"transcriptRevision"`
	SummarySourceRevision   int64     `json:"summarySourceRevision"`
	SummarySourceDigest     string    `json:"summarySourceDigest,omitempty"`
	SummarySourceTitle      string    `json:"summarySourceTitle,omitempty"`
	SummarySourceTranscript string    `json:"summarySourceTranscript,omitempty"`
	SummaryEdited           bool      `json:"summaryEdited"`
	Title                   string    `json:"title"`
	Status                  Status    `json:"status"`
	AudioSource             string    `json:"audioSource"`
	StartedAt               string    `json:"startedAt"`
	EndedAt                 string    `json:"endedAt"`
	DurationMS              int64     `json:"durationMs"`
	Summary                 string    `json:"summary"`
	Actions                 string    `json:"actions"`
	Transcript              string    `json:"transcript"`
	SummaryError            string    `json:"summaryError,omitempty"`
	CreatedAt               string    `json:"createdAt"`
	UpdatedAt               string    `json:"updatedAt"`
	Segments                []Segment `json:"segments,omitempty"`
	Docs                    []Doc     `json:"docs,omitempty"`
}

type Segment struct {
	SegmentID string `json:"segmentId"`
	MeetingID string `json:"meetingId"`
	Seq       int    `json:"seq"`
	StartedMS int64  `json:"startedMs"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

type Doc struct {
	DocID     string `json:"docId"`
	MeetingID string `json:"meetingId"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
}

type Notes struct {
	Title   string
	Summary string
	Actions string
}

type Completer func(ctx context.Context, title, transcript string) (Notes, error)

var meetingClock atomic.Int64

// A revision token must change even when the wall clock stalls or moves back.
func nextMeetingTime(previous string) string {
	floor := int64(0)
	if parsed, err := time.Parse(time.RFC3339Nano, previous); err == nil {
		floor = parsed.UnixNano()
	}
	for {
		last := meetingClock.Load()
		next := time.Now().UnixNano()
		if next <= floor {
			next = floor + 1
		}
		if next <= last {
			next = last + 1
		}
		if meetingClock.CompareAndSwap(last, next) {
			return time.Unix(0, next).UTC().Format(time.RFC3339Nano)
		}
	}
}

type Store interface {
	InsertMeeting(ctx context.Context, m Meeting) error
	UpdateMeeting(ctx context.Context, m Meeting) error
	CompareAndSwapMeeting(ctx context.Context, previousUpdatedAt string, m Meeting) (bool, error)
	TouchRecording(ctx context.Context, meetingID string, durationMS int64, updatedAt string) error
	GetMeeting(ctx context.Context, id string) (Meeting, error)
	ListMeetings(ctx context.Context, limit int) ([]Meeting, error)
	InsertSegment(ctx context.Context, seg Segment) error
	DeleteSegments(ctx context.Context, meetingID string) error
	CountSegments(ctx context.Context, meetingID string) (int, error)
	LastSegment(ctx context.Context, meetingID string) (Segment, bool, error)
	ListSegments(ctx context.Context, meetingID string) ([]Segment, error)
	ReplaceDocs(ctx context.Context, meetingID string, docs []Doc) error
	ListDocs(ctx context.Context, meetingID string) ([]Doc, error)
	HasRecording(ctx context.Context) (bool, error)
	DeleteMeeting(ctx context.Context, id string) error
	DeleteMeetingVersion(ctx context.Context, id string, revision int64) error
}

type Service struct {
	executionScope func(context.Context, string) (context.Context, func(), error)
	store          Store
	complete       Completer
	transcribe     AudioTranscriber
	audioRoot      string
	mu             sync.Mutex
	mutationMu     sync.Mutex // Short recording/edit mutations; never held during model I/O.
	audioMu        sync.Mutex
	recording      string
	summarizing    map[string]bool
	catchingUp     map[string]bool
	sinks          map[string]*audioSink
	loopback       *loopbackSession
}

func New(store Store) *Service {
	return &Service{store: store, summarizing: map[string]bool{}, catchingUp: map[string]bool{}, sinks: map[string]*audioSink{}}
}

func (s *Service) SetExecutionScope(scope func(context.Context, string) (context.Context, func(), error)) {
	s.executionScope = scope
}
func (s *Service) jobScope(ctx context.Context, capability string) (context.Context, func(), error) {
	parent := context.WithoutCancel(ctx)
	if s.executionScope != nil {
		return s.executionScope(parent, capability)
	}
	return parent, func() {}, nil
}

func (s *Service) SetCompleter(fn Completer) { s.complete = fn }

func (s *Service) ready() error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Meeting, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	items, err := s.store.ListMeetings(ctx, maxList)
	if err != nil {
		return nil, err
	}
	for i, m := range items {
		if m.Status != StatusSummarizing {
			continue
		}
		// List projections omit the potentially large source snapshot. A
		// recovery CAS must load the complete row before preserving it.
		m, err = s.store.GetMeeting(ctx, m.MeetingID)
		if err != nil {
			return nil, err
		}
		next, changed, reclaimErr := s.maybeReclaimSummarizing(m)
		if reclaimErr == nil && changed {
			items[i] = next
			items[i].Segments = nil
			items[i].Docs = nil
			items[i].Summary, items[i].Actions, items[i].Transcript, items[i].SummarySourceTranscript = "", "", "", ""
		}
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id string) (Meeting, error) {
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
	segs, err := s.boundedSegments(ctx, id)
	if err != nil {
		return Meeting{}, err
	}
	docs, err := s.store.ListDocs(ctx, id)
	if err != nil {
		return Meeting{}, err
	}
	m.Segments = segs
	m.Docs = docs
	return m, nil
}

func (s *Service) Start(ctx context.Context, title, audioSource string) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	busy, err := s.store.HasRecording(ctx)
	if err != nil {
		return Meeting{}, err
	}
	if busy {
		return Meeting{}, ErrBusy
	}
	if _, err = NormalizeAudioSource(audioSource); err != nil {
		return Meeting{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "会议 " + time.Now().Format("2006-01-02 15:04")
	}
	if utf8.RuneCountInString(title) > maxTitle {
		return Meeting{}, ErrInvalid
	}
	m := Meeting{
		MeetingID:   ulid.Make().String(),
		Revision:    1,
		Title:       title,
		Status:      StatusRecording,
		AudioSource: AudioMicrophone,
		StartedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.InsertMeeting(ctx, m); err != nil {
		return Meeting{}, err
	}
	if s.startLoopback(m.MeetingID) == nil {
		m.AudioSource = AudioMicrophoneAndSystem
		m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := s.store.UpdateMeeting(ctx, m); err != nil {
			s.stopLoopback(m.MeetingID)
			return Meeting{}, err
		}
		m.Revision++
	}
	s.mu.Lock()
	s.recording = m.MeetingID
	s.mu.Unlock()
	return m, nil
}

// resolveAgainstLastSegment folds a fresh recognizer final into what is already
// committed. It answers with the text still worth storing, or with the existing
// segment when the final carried nothing new: the same line twice (a repeated
// flush, or a restart replaying its last result), or a growing final whose
// prefix is already on record.
func (s *Service) resolveAgainstLastSegment(ctx context.Context, meetingID, text string) (string, Segment, bool) {
	prev, ok, err := s.store.LastSegment(ctx, meetingID)
	if err != nil || !ok {
		return text, Segment{}, false
	}
	prevText := strings.TrimSpace(prev.Text)
	if text == prevText {
		return "", prev, true
	}
	rest, skip := peelMeetingPrefix(prevText, text)
	if skip || strings.TrimSpace(rest) == "" {
		return "", prev, true
	}
	return rest, prev, false
}

func (s *Service) Append(ctx context.Context, meetingID, text string, startedMS int64) (Segment, error) {
	if err := s.ready(); err != nil {
		return Segment{}, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Segment{}, ErrInvalid
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxSegment {
		return Segment{}, ErrInvalid
	}
	kept, prev, settled := s.resolveAgainstLastSegment(ctx, meetingID, text)
	if settled {
		return prev, nil
	}
	text = kept
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return Segment{}, err
	}
	if m.Status != StatusRecording {
		return Segment{}, ErrNotRecording
	}
	n, err := s.store.CountSegments(ctx, meetingID)
	if err != nil {
		return Segment{}, err
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
	if store, ok := s.store.(boundedSegmentStore); ok {
		seg, err = store.AppendMeetingSegmentBounded(ctx, seg)
	} else {
		var existing []Segment
		existing, err = s.boundedSegments(ctx, meetingID)
		if err == nil {
			_, err = assembleTranscript(append(existing, seg))
		}
		if err == nil {
			err = s.store.InsertSegment(ctx, seg)
		}
	}
	if err != nil {
		return Segment{}, err
	}
	// A caption append must never write an old recording snapshot over Stop.
	_ = s.store.TouchRecording(ctx, meetingID, m.DurationMS, nextMeetingTime(m.UpdatedAt))
	return seg, nil
}

func (s *Service) Heartbeat(ctx context.Context, meetingID string) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Meeting{}, ErrInvalid
	}
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if m.Status != StatusRecording {
		return Meeting{}, ErrNotRecording
	}
	now := time.Now().UTC()
	m.DurationMS = recordingDurationMS(m.StartedAt, now, s.audioDurationMS(meetingID))
	m.UpdatedAt = nextMeetingTime(m.UpdatedAt)
	if err := s.store.TouchRecording(ctx, meetingID, m.DurationMS, m.UpdatedAt); err != nil {
		return Meeting{}, err
	}
	return m, nil
}

func (s *Service) Stop(ctx context.Context, meetingID string, expectedRevision ...int64) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Meeting{}, ErrInvalid
	}
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if m.Status != StatusRecording {
		return m, nil
	}
	if len(expectedRevision) > 0 && (expectedRevision[0] < 1 || m.Revision != expectedRevision[0]) {
		return Meeting{}, ErrConflict
	}
	// Stop is the only path that ends a meeting. WAV rotate, ASR death,
	// and display-track recycle must never call this.
	segs, err := s.boundedSegments(ctx, meetingID)
	if err != nil && !errors.Is(err, ErrCapacity) {
		return Meeting{}, err
	}
	capacity := errors.Is(err, ErrCapacity) || capacityMarked(m)
	s.stopLoopback(meetingID)
	s.closeSink(meetingID)
	ended := time.Now().UTC()
	now := ended.Format(time.RFC3339Nano)
	m.Status = StatusTranscribed
	m.EndedAt = now
	m.DurationMS = recordingDurationMS(m.StartedAt, ended, s.audioDurationMS(meetingID))
	transcript := m.Transcript
	if !capacity {
		transcript, err = assembleTranscript(segs)
		capacity = errors.Is(err, ErrCapacity)
	}
	if capacity {
		m.Status = StatusNeedsSummary
		m.SummaryError = CapacityNotice
		transcript = m.Transcript
	}
	if transcript != m.Transcript {
		m.TranscriptRevision++
	}
	m.Transcript = transcript
	previousUpdatedAt := m.UpdatedAt
	m.UpdatedAt = nextMeetingTime(m.UpdatedAt)
	if changed, err := s.store.CompareAndSwapMeeting(ctx, previousUpdatedAt, m); err != nil {
		return Meeting{}, err
	} else if !changed {
		current, err := s.store.GetMeeting(ctx, meetingID)
		if err == nil && current.Status != StatusRecording {
			return current, nil
		}
		return Meeting{}, ErrConflict
	}
	s.mu.Lock()
	if s.recording == meetingID {
		s.recording = ""
	}
	s.mu.Unlock()
	// A concurrent heartbeat can advance duration while the content revision
	// remains valid. Return the actual committed row, including source version.
	committed, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	committed.Segments = segs
	if capacity {
		return committed, ErrCapacity
	}
	return committed, nil
}

func recordingDurationMS(startedAt string, now time.Time, audioMS int64) int64 {
	started, parseErr := time.Parse(time.RFC3339Nano, startedAt)
	wall := int64(0)
	if parseErr == nil && !now.Before(started) {
		wall = now.Sub(started).Milliseconds()
	}
	if audioMS > wall {
		return audioMS
	}
	return wall
}

// AppendAudio writes 16 kHz mono s16le PCM to rolling WAV files. Live ASR is
// captions; this recording is the source of truth for long sessions.
func (s *Service) AppendAudio(ctx context.Context, meetingID string, pcm []byte) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return 0, ErrInvalid
	}
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return 0, err
	}
	if m.Status != StatusRecording {
		return 0, ErrNotRecording
	}
	sink, err := s.ensureSink(meetingID)
	if err != nil {
		return 0, err
	}
	if mixed := s.takeMixPCM(meetingID, len(pcm)); len(mixed) > 0 {
		pcm = mixS16le(pcm, mixed)
	}
	// Old callers retain their unkeyed behavior. Once keyed recording begins,
	// keep its ordered immutable read view instead of extending an earlier WAV.
	sink.mu.Lock()
	if err := sink.loadBatchesLocked(); err != nil {
		sink.mu.Unlock()
		return 0, err
	}
	if len(sink.batches) > 0 {
		defer sink.mu.Unlock()
		pcm = pcm[:len(pcm)/2*2]
		id := AudioBatchIdentity{CaptureSessionID: ulid.Make().String()}
		for len(pcm) > 0 {
			n := min(len(pcm), 24576*2)
			id.SampleCount, id.Digest = int64(n/2), pcmDigest(pcm[:n])
			if _, err := sink.appendBatchLocked(meetingID, pcm[:n], id); err != nil {
				return pcmDurationMS(sink.totalBytes), err
			}
			pcm = pcm[n:]
			id.ChunkSeq++
			id.SampleStart += id.SampleCount
		}
		return pcmDurationMS(sink.totalBytes), nil
	}
	sink.mu.Unlock()
	audioMS, err := sink.appendPCM(pcm)
	if err != nil {
		return audioMS, err
	}
	return audioMS, nil
}

func persistCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), persistTimeout)
}

var persistTimeout = 20 * time.Second

// SetPersistTimeoutForTest shortens the per-write store budget. Restore via the returned func.
func SetPersistTimeoutForTest(d time.Duration) func() {
	prev := persistTimeout
	if d > 0 {
		persistTimeout = d
	}
	return func() { persistTimeout = prev }
}

func (s *Service) Summarize(ctx context.Context, meetingID string, expectedRevision ...int64) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Meeting{}, ErrInvalid
	}
	m, err := s.Metadata(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if len(expectedRevision) > 0 && (expectedRevision[0] < 1 || m.Revision != expectedRevision[0]) {
		return Meeting{}, ErrConflict
	}
	if m.Status == StatusRecording {
		return Meeting{}, ErrNotRecording
	}
	if capacityMarked(m) {
		return Meeting{}, ErrCapacity
	}
	if strings.TrimSpace(m.Transcript) == "" {
		segs, readErr := s.boundedSegments(ctx, meetingID)
		if readErr != nil {
			if errors.Is(readErr, ErrCapacity) {
				return s.finishCapacity(m)
			}
			return Meeting{}, readErr
		}
		m.Transcript, err = assembleTranscript(segs)
		if err != nil {
			return s.finishCapacity(m)
		}
	}
	if m.Status == StatusNeedsSummary && strings.HasPrefix(m.SummaryError, "转写补全存在缺口") {
		return m, nil
	}
	s.mu.Lock()
	if s.catchingUp[meetingID] {
		s.mu.Unlock()
		return Meeting{}, ErrBusy
	}
	if s.summarizing[meetingID] {
		s.mu.Unlock()
		return m, nil
	}
	if s.summarizing == nil {
		s.summarizing = map[string]bool{}
	}
	s.summarizing[meetingID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.summarizing, meetingID)
		s.mu.Unlock()
	}()
	previousUpdatedAt := m.UpdatedAt
	m.Status = StatusSummarizing
	m.UpdatedAt = nextMeetingTime(previousUpdatedAt)
	if err := s.persistMeetingVersion(m, previousUpdatedAt); err != nil {
		return Meeting{}, err
	}
	m.Revision++
	if strings.TrimSpace(m.Transcript) == "" {
		return s.finishNeedsSummary(m, "没有可用的逐字稿，无法生成摘要")
	}
	if s.complete == nil {
		return s.finishNeedsSummary(m, "尚未配置可用模型，逐字稿已保存。配置模型后可重试生成摘要。")
	}
	// Keep the job off the Bridge request ctx: WebView timeouts and page
	// unmount must not leave the meeting stuck in 生成纪要中.
	lifetime, release, scopeErr := s.jobScope(ctx, "llm")
	if scopeErr != nil {
		return s.finishNeedsSummary(m, summarizeErrMessage(scopeErr))
	}
	defer release()
	workCtx, workCancel := context.WithTimeout(lifetime, summarizeJobDeadline)
	defer workCancel()
	sourceTitle, sourceTranscript := m.Title, CleanTranscript(m.Transcript)
	notes, err := SummarizeLong(workCtx, s.complete, sourceTitle, sourceTranscript)
	if workCtx.Err() != nil {
		err = workCtx.Err()
	}
	if err != nil {
		return s.finishNeedsSummary(m, summarizeErrMessage(err))
	}
	title := strings.TrimSpace(notes.Title)
	if title != "" && utf8.RuneCountInString(title) <= maxTitle {
		m.Title = title
	}
	m.Summary = clipRunes(strings.TrimSpace(notes.Summary), maxSummary)
	m.Actions = clipRunes(strings.TrimSpace(notes.Actions), maxActions)
	if m.Summary == "" && m.Actions == "" {
		return s.finishNeedsSummary(m, "模型没有写出摘要，逐字稿已保存。可重试生成摘要。")
	}
	m.SummaryError = ""
	m.Status = StatusReady
	m.bindSummarySource(sourceTitle, sourceTranscript)
	previousUpdatedAt = m.UpdatedAt
	m.UpdatedAt = nextMeetingTime(previousUpdatedAt)
	s.mutationMu.Lock()
	err = s.persistMeetingVersionContext(workCtx, m, previousUpdatedAt)
	if err != nil {
		s.mutationMu.Unlock()
		if errors.Is(err, ErrConflict) {
			return s.finishNeedsSummary(m, "会议内容已修改，已保留人工修订。请基于最新内容重新生成纪要。")
		}
		return Meeting{}, err
	}
	defer s.mutationMu.Unlock()
	persist, persistCancel := persistCtx()
	defer persistCancel()
	if err := s.persistDocs(persist, m); err != nil {
		return Meeting{}, err
	}
	result, err := s.Detail(persist, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	// Keep the internal service result compatible without loading every raw
	// segment. Public bridge replies omit these derived document copies.
	result.Docs, err = s.store.ListDocs(persist, meetingID)
	return result, err
}

func (s *Service) persistMeetingVersion(m Meeting, previousUpdatedAt string) error {
	ctx, cancel := persistCtx()
	defer cancel()
	return s.persistMeetingVersionContext(ctx, m, previousUpdatedAt)
}
func (s *Service) persistMeetingVersionContext(ctx context.Context, m Meeting, previousUpdatedAt string) error {
	updated, err := s.store.CompareAndSwapMeeting(ctx, previousUpdatedAt, m)
	if err != nil {
		return err
	}
	if !updated {
		return ErrConflict
	}
	return nil
}

func summarizeErrMessage(err error) string {
	if err == nil {
		return "尚未生成摘要，逐字稿已保存。可重试生成摘要。"
	}
	msg := strings.TrimSpace(err.Error())
	msg = strings.ReplaceAll(msg, "\n", " ")
	if utf8.RuneCountInString(msg) > 160 {
		msg = string([]rune(msg)[:160]) + "…"
	}
	if msg == "" {
		return "尚未生成摘要，逐字稿已保存。可重试生成摘要。"
	}
	if looksLikeBillingFailure(msg) {
		return "尚未生成摘要：这个对话模型余额不足。逐字稿已保存。请换一个已启用的模型再重试。"
	}
	return "尚未生成摘要：" + msg + "。逐字稿已保存，可重试。"
}

func looksLikeBillingFailure(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "payment required") ||
		strings.Contains(lower, "insufficient") ||
		strings.Contains(lower, "402") ||
		strings.Contains(msg, "余额不足")
}

func (s *Service) maybeReclaimSummarizing(m Meeting) (Meeting, bool, error) {
	if m.Status != StatusSummarizing {
		return m, false, nil
	}
	s.mu.Lock()
	busy := s.summarizing[m.MeetingID]
	s.mu.Unlock()
	if busy {
		return m, false, nil
	}
	persist, cancel := persistCtx()
	defer cancel()
	previousUpdatedAt := m.UpdatedAt
	m.Status = StatusNeedsSummary
	m.SummaryError = clipRunes("摘要生成中断，逐字稿已保存。可重试生成摘要。", 1024)
	m.UpdatedAt = nextMeetingTime(previousUpdatedAt)
	if updated, err := s.store.CompareAndSwapMeeting(persist, previousUpdatedAt, m); err != nil {
		return Meeting{}, false, err
	} else if !updated {
		current, readErr := s.store.GetMeeting(persist, m.MeetingID)
		return current, readErr == nil, readErr
	}
	_ = s.persistDocs(persist, m)
	m.Revision++
	return m, true, nil
}

func (s *Service) finishNeedsSummary(m Meeting, msg string) (Meeting, error) {
	persist, cancel := persistCtx()
	defer cancel()
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	// Failure recovery must also preserve edits made while the model ran.
	current, err := s.store.GetMeeting(persist, m.MeetingID)
	if err != nil {
		return Meeting{}, err
	}
	previousUpdatedAt := current.UpdatedAt
	m = current
	m.Status = StatusNeedsSummary
	m.SummaryError = clipRunes(msg, 1024)
	m.UpdatedAt = nextMeetingTime(previousUpdatedAt)
	if updated, err := s.store.CompareAndSwapMeeting(persist, previousUpdatedAt, m); err != nil {
		return Meeting{}, err
	} else if !updated {
		return Meeting{}, ErrConflict
	}
	_ = s.persistDocs(persist, m)
	return s.Detail(persist, meetingIDOf(m))
}

func meetingIDOf(m Meeting) string { return m.MeetingID }

func (s *Service) persistDocs(ctx context.Context, m Meeting) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	md := RenderMarkdown(m)
	htmlBody := RenderHTML(m)
	docs := []Doc{
		{DocID: ulid.Make().String(), MeetingID: m.MeetingID, Kind: "markdown", Body: md, CreatedAt: now},
	}
	// This table is a derived cache with a 2 Mi-rune row limit. HTML escaping
	// can exceed it for a valid transcript; export always renders the complete
	// authoritative row, so omit that cache entry instead of truncating it.
	if utf8.RuneCountInString(htmlBody) <= 2<<20 {
		docs = append(docs, Doc{DocID: ulid.Make().String(), MeetingID: m.MeetingID, Kind: "html", Body: htmlBody, CreatedAt: now})
	}
	return s.store.ReplaceDocs(ctx, m.MeetingID, docs)
}

type MeetingPatch struct {
	ExpectedRevision int64
	TranscriptEdit   *TranscriptEdit
	Title            *string
	Summary          *string
	Actions          *string
	Transcript       *string
}

func (s *Service) Update(ctx context.Context, meetingID string, patch MeetingPatch) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Meeting{}, ErrInvalid
	}
	if patch.Title == nil && patch.Summary == nil && patch.Actions == nil && patch.Transcript == nil && patch.TranscriptEdit == nil {
		return Meeting{}, ErrInvalid
	}
	m, err := s.Metadata(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if m.Status == StatusRecording {
		return Meeting{}, ErrNotRecording
	}
	if patch.ExpectedRevision < 1 || m.Revision != patch.ExpectedRevision {
		return Meeting{}, ErrConflict
	}
	previousUpdatedAt := m.UpdatedAt
	if patch.TranscriptEdit != nil {
		if patch.Transcript != nil {
			return Meeting{}, ErrInvalid
		}
		next, editErr := applyTranscriptEdit(m, *patch.TranscriptEdit)
		if editErr != nil {
			return Meeting{}, editErr
		}
		patch.Transcript = &next
	}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" || utf8.RuneCountInString(title) > maxTitle {
			return Meeting{}, ErrInvalid
		}
		m.Title = title
	}
	if patch.Summary != nil {
		next := clipRunes(strings.TrimSpace(*patch.Summary), maxSummary)
		m.SummaryEdited = m.SummaryEdited || next != m.Summary
		m.Summary = next
	}
	if patch.Actions != nil {
		next := clipRunes(strings.TrimSpace(*patch.Actions), maxActions)
		m.SummaryEdited = m.SummaryEdited || next != m.Actions
		m.Actions = next
	}
	if patch.Transcript != nil {
		next := strings.TrimSpace(*patch.Transcript)
		if patch.TranscriptEdit != nil {
			next = *patch.Transcript
		}
		if !utf8.ValidString(next) || utf8.RuneCountInString(next) > maxTranscript {
			return Meeting{}, ErrCapacity
		}
		if next != m.Transcript {
			m.TranscriptRevision++
			m.Status = StatusNeedsSummary
			m.SummaryError = "逐字稿已修改，旧摘要已保留；请基于当前原稿重新生成。"
		}
		m.Transcript = next
	}
	m.UpdatedAt = nextMeetingTime(previousUpdatedAt)
	if updated, err := s.store.CompareAndSwapMeeting(ctx, previousUpdatedAt, m); err != nil {
		return Meeting{}, err
	} else if !updated {
		return Meeting{}, ErrConflict
	}
	if err := s.persistDocs(ctx, m); err != nil {
		return Meeting{}, err
	}
	return s.Detail(ctx, meetingID)
}

func (s *Service) Delete(ctx context.Context, meetingID string, expectedRevision ...int64) error {
	if err := s.ready(); err != nil {
		return err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.mu.Lock()
	busy := s.catchingUp[meetingID] || s.summarizing[meetingID]
	s.mu.Unlock()
	if busy {
		return ErrBusy
	}
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return ErrInvalid
	}
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return err
	}
	if m.Status == StatusRecording {
		return ErrBusy
	}
	if len(expectedRevision) != 1 || expectedRevision[0] < 1 || m.Revision != expectedRevision[0] {
		return ErrConflict
	}
	if err := s.store.DeleteMeetingVersion(ctx, meetingID, expectedRevision[0]); err != nil {
		return err
	}
	s.stopLoopback(meetingID)
	s.removeAudio(meetingID)
	s.mu.Lock()
	if s.recording == meetingID {
		s.recording = ""
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) Export(ctx context.Context, meetingID, format, destPath string) (string, string, error) {
	if err := s.ready(); err != nil {
		return "", "", err
	}
	m, err := s.Metadata(ctx, meetingID)
	if err != nil {
		return "", "", err
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "markdown"
	}
	var body, ext string
	switch format {
	case "markdown", "md":
		format, ext, body = "markdown", ".md", RenderMarkdown(m)
	case "html":
		format, ext, body = "html", ".html", RenderHTML(m)
	case "txt":
		format, ext, body = "txt", ".txt", RenderText(m)
	default:
		return "", "", ErrInvalid
	}
	if strings.TrimSpace(destPath) == "" {
		destPath, err = pickSavePath("导出会议记录", defaultExportName(m.Title, ext))
		if err != nil {
			return "", "", err
		}
	}
	if !filepath.IsAbs(destPath) {
		return "", "", ErrInvalid
	}
	if err := os.WriteFile(destPath, []byte(body), 0o600); err != nil {
		return "", "", err
	}
	return destPath, format, nil
}

func defaultExportName(title, ext string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	if clean == "" {
		clean = "会议记录"
	}
	return clean + ext
}

func RenderMarkdown(m Meeting) string {
	summary := strings.TrimSpace(m.Summary)
	if summary == "" {
		summary = "尚未生成摘要。"
		if m.SummaryError != "" {
			summary = m.SummaryError
		}
	}
	actions := strings.TrimSpace(m.Actions)
	if actions == "" {
		actions = "尚未生成待办。"
	}
	transcript := strings.TrimSpace(m.Transcript)
	if transcript == "" {
		transcript = "（空）"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", m.Title)
	fmt.Fprintf(&b, "- 开始：%s\n", m.StartedAt)
	if m.EndedAt != "" {
		fmt.Fprintf(&b, "- 结束：%s\n", m.EndedAt)
	}
	fmt.Fprintf(&b, "- 时长：%s\n", formatDuration(m.DurationMS))
	fmt.Fprintf(&b, "- 音频：%s\n", audioSourceLabel(m.AudioSource))
	fmt.Fprintf(&b, "- 摘要来源：%s\n\n", summarySourceDescription(m))
	b.WriteString("## 会议摘要\n\n")
	b.WriteString(summary)
	b.WriteString("\n\n## 决议/待办\n\n")
	b.WriteString(actions)
	b.WriteString("\n\n## 全文逐字稿\n\n")
	b.WriteString(transcript)
	b.WriteByte('\n')
	return b.String()
}

func RenderHTML(m Meeting) string {
	md := RenderMarkdown(m)
	return "<!DOCTYPE html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\"><title>" +
		html.EscapeString(m.Title) +
		"</title></head><body><pre>" + html.EscapeString(md) + "</pre></body></html>\n"
}

func RenderText(m Meeting) string {
	return RenderMarkdown(m)
}

func formatDuration(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	sec := ms / 1000
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func clipRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func NormalizeAudioSource(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AudioMicrophone, nil
	}
	if raw == AudioMicrophone || raw == AudioMicrophoneAndSystem {
		return raw, nil
	}
	return "", ErrInvalid
}

func audioSourceLabel(src string) string {
	if src == AudioMicrophoneAndSystem {
		return "本机麦克风 + 本机系统声音（未共享给其他电脑）"
	}
	return "本机麦克风（未混录系统扬声器）"
}

func ParseNotes(raw, fallbackTitle string) Notes {
	raw = strings.TrimSpace(raw)
	notes := Notes{Title: fallbackTitle}
	if raw == "" {
		return notes
	}
	if parsed, ok := parseJSONNotes(raw); ok {
		if strings.TrimSpace(parsed.Title) == "" {
			parsed.Title = fallbackTitle
		}
		return parsed
	}
	notes.Summary = sectionBetween(raw, []string{"会议摘要", "摘要", "Summary"}, []string{"决议", "待办", "行动项", "Action"})
	notes.Actions = sectionBetween(raw, []string{"决议", "待办", "行动项", "Action"}, []string{"逐字稿", "全文", "Transcript"})
	if notes.Summary == "" {
		notes.Summary = raw
	}
	return notes
}
