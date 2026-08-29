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
)

const (
	AudioMicrophone          = "microphone"
	AudioMicrophoneAndSystem = "microphone_and_system"
)

const (
	maxTitle      = 200
	maxSegment    = 16384
	maxTranscript = 1 << 20
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
	MeetingID    string    `json:"meetingId"`
	Title        string    `json:"title"`
	Status       Status    `json:"status"`
	AudioSource  string    `json:"audioSource"`
	StartedAt    string    `json:"startedAt"`
	EndedAt      string    `json:"endedAt"`
	DurationMS   int64     `json:"durationMs"`
	Summary      string    `json:"summary"`
	Actions      string    `json:"actions"`
	Transcript   string    `json:"transcript"`
	SummaryError string    `json:"summaryError,omitempty"`
	CreatedAt    string    `json:"createdAt"`
	UpdatedAt    string    `json:"updatedAt"`
	Segments     []Segment `json:"segments,omitempty"`
	Docs         []Doc     `json:"docs,omitempty"`
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

type Store interface {
	InsertMeeting(ctx context.Context, m Meeting) error
	UpdateMeeting(ctx context.Context, m Meeting) error
	GetMeeting(ctx context.Context, id string) (Meeting, error)
	ListMeetings(ctx context.Context, limit int) ([]Meeting, error)
	InsertSegment(ctx context.Context, seg Segment) error
	CountSegments(ctx context.Context, meetingID string) (int, error)
	ListSegments(ctx context.Context, meetingID string) ([]Segment, error)
	ReplaceDocs(ctx context.Context, meetingID string, docs []Doc) error
	ListDocs(ctx context.Context, meetingID string) ([]Doc, error)
	HasRecording(ctx context.Context) (bool, error)
	DeleteMeeting(ctx context.Context, id string) error
}

type Service struct {
	store       Store
	complete    Completer
	transcribe  AudioTranscriber
	audioRoot   string
	mu          sync.Mutex
	audioMu     sync.Mutex
	recording   string
	summarizing map[string]bool
	sinks       map[string]*audioSink
}

func New(store Store) *Service {
	return &Service{store: store, summarizing: map[string]bool{}, sinks: map[string]*audioSink{}}
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
		next, changed, reclaimErr := s.maybeReclaimSummarizing(m)
		if reclaimErr == nil && changed {
			items[i] = next
			items[i].Segments = nil
			items[i].Docs = nil
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
	segs, err := s.store.ListSegments(ctx, id)
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
	busy, err := s.store.HasRecording(ctx)
	if err != nil {
		return Meeting{}, err
	}
	if busy {
		return Meeting{}, ErrBusy
	}
	audioSource, err = NormalizeAudioSource(audioSource)
	if err != nil {
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
		Title:       title,
		Status:      StatusRecording,
		AudioSource: audioSource,
		StartedAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.InsertMeeting(ctx, m); err != nil {
		return Meeting{}, err
	}
	s.mu.Lock()
	s.recording = m.MeetingID
	s.mu.Unlock()
	return m, nil
}

func (s *Service) Append(ctx context.Context, meetingID, text string, startedMS int64) (Segment, error) {
	if err := s.ready(); err != nil {
		return Segment{}, err
	}
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Segment{}, ErrInvalid
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxSegment {
		return Segment{}, ErrInvalid
	}
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
	m.UpdatedAt = now
	_ = s.store.UpdateMeeting(ctx, m)
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
	m.UpdatedAt = now.Format(time.RFC3339Nano)
	if err := s.store.UpdateMeeting(ctx, m); err != nil {
		return Meeting{}, err
	}
	return m, nil
}

func (s *Service) Stop(ctx context.Context, meetingID string) (Meeting, error) {
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
		return m, nil
	}
	segs, err := s.store.ListSegments(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	s.closeSink(meetingID)
	ended := time.Now().UTC()
	now := ended.Format(time.RFC3339Nano)
	m.Status = StatusTranscribed
	m.EndedAt = now
	m.DurationMS = recordingDurationMS(m.StartedAt, ended, s.audioDurationMS(meetingID))
	m.Transcript = assembleTranscript(segs)
	m.UpdatedAt = now
	if err := s.store.UpdateMeeting(ctx, m); err != nil {
		return Meeting{}, err
	}
	s.mu.Lock()
	if s.recording == meetingID {
		s.recording = ""
	}
	s.mu.Unlock()
	m.Segments = segs
	return m, nil
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
	audioMS, err := sink.appendPCM(pcm)
	if err != nil {
		return audioMS, err
	}
	return audioMS, nil
}

func persistCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

func (s *Service) Summarize(ctx context.Context, meetingID string) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Meeting{}, ErrInvalid
	}
	m, err := s.Get(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if m.Status == StatusRecording {
		return Meeting{}, ErrNotRecording
	}
	s.mu.Lock()
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
	persist, persistCancel := persistCtx()
	defer persistCancel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m.Status = StatusSummarizing
	m.UpdatedAt = now
	if err := s.store.UpdateMeeting(persist, m); err != nil {
		return Meeting{}, err
	}
	if strings.TrimSpace(m.Transcript) == "" {
		m.Status = StatusNeedsSummary
		m.SummaryError = "没有可用的逐字稿，无法生成摘要"
		m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		_ = s.store.UpdateMeeting(persist, m)
		return s.Get(persist, meetingID)
	}
	if s.complete == nil {
		return s.finishNeedsSummary(m, "尚未配置可用模型，逐字稿已保存。配置模型后可重试生成摘要。")
	}
	workCtx, workCancel := context.WithTimeout(context.Background(), summarizeJobDeadline)
	defer workCancel()
	stop := context.AfterFunc(ctx, workCancel)
	defer stop()
	notes, err := SummarizeLong(workCtx, s.complete, m.Title, CleanTranscript(m.Transcript))
	if err != nil {
		return s.finishNeedsSummary(m, "尚未生成摘要："+err.Error())
	}
	title := strings.TrimSpace(notes.Title)
	if title != "" && utf8.RuneCountInString(title) <= maxTitle {
		m.Title = title
	}
	m.Summary = clipRunes(strings.TrimSpace(notes.Summary), maxSummary)
	m.Actions = clipRunes(strings.TrimSpace(notes.Actions), maxActions)
	m.SummaryError = ""
	m.Status = StatusReady
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.store.UpdateMeeting(persist, m); err != nil {
		return Meeting{}, err
	}
	if err := s.persistDocs(persist, m); err != nil {
		return Meeting{}, err
	}
	return s.Get(persist, meetingID)
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
	m.Status = StatusNeedsSummary
	m.SummaryError = clipRunes("摘要生成中断，逐字稿已保存。可重试生成摘要。", 1024)
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.store.UpdateMeeting(persist, m); err != nil {
		return Meeting{}, false, err
	}
	_ = s.persistDocs(persist, m)
	return m, true, nil
}

func (s *Service) finishNeedsSummary(m Meeting, msg string) (Meeting, error) {
	persist, cancel := persistCtx()
	defer cancel()
	m.Status = StatusNeedsSummary
	m.SummaryError = clipRunes(msg, 1024)
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.store.UpdateMeeting(persist, m); err != nil {
		return Meeting{}, err
	}
	_ = s.persistDocs(persist, m)
	return s.Get(persist, meetingIDOf(m))
}

func meetingIDOf(m Meeting) string { return m.MeetingID }

func (s *Service) persistDocs(ctx context.Context, m Meeting) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	md := RenderMarkdown(m)
	htmlBody := RenderHTML(m)
	return s.store.ReplaceDocs(ctx, m.MeetingID, []Doc{
		{DocID: ulid.Make().String(), MeetingID: m.MeetingID, Kind: "markdown", Body: md, CreatedAt: now},
		{DocID: ulid.Make().String(), MeetingID: m.MeetingID, Kind: "html", Body: htmlBody, CreatedAt: now},
	})
}

type MeetingPatch struct {
	Title      *string
	Summary    *string
	Actions    *string
	Transcript *string
}

func (s *Service) Update(ctx context.Context, meetingID string, patch MeetingPatch) (Meeting, error) {
	if err := s.ready(); err != nil {
		return Meeting{}, err
	}
	if _, err := ulid.ParseStrict(meetingID); err != nil {
		return Meeting{}, ErrInvalid
	}
	if patch.Title == nil && patch.Summary == nil && patch.Actions == nil && patch.Transcript == nil {
		return Meeting{}, ErrInvalid
	}
	m, err := s.Get(ctx, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	if m.Status == StatusRecording {
		return Meeting{}, ErrNotRecording
	}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" || utf8.RuneCountInString(title) > maxTitle {
			return Meeting{}, ErrInvalid
		}
		m.Title = title
	}
	if patch.Summary != nil {
		m.Summary = clipRunes(strings.TrimSpace(*patch.Summary), maxSummary)
	}
	if patch.Actions != nil {
		m.Actions = clipRunes(strings.TrimSpace(*patch.Actions), maxActions)
	}
	if patch.Transcript != nil {
		m.Transcript = clipRunes(strings.TrimSpace(*patch.Transcript), maxTranscript)
	}
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.store.UpdateMeeting(ctx, m); err != nil {
		return Meeting{}, err
	}
	if err := s.persistDocs(ctx, m); err != nil {
		return Meeting{}, err
	}
	return s.Get(ctx, meetingID)
}

func (s *Service) Delete(ctx context.Context, meetingID string) error {
	if err := s.ready(); err != nil {
		return err
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
	if err := s.store.DeleteMeeting(ctx, meetingID); err != nil {
		return err
	}
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
	m, err := s.Get(ctx, meetingID)
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
	fmt.Fprintf(&b, "- 音频：%s\n\n", audioSourceLabel(m.AudioSource))
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
