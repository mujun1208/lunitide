package app

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/voice"
)

func handleMeetingsList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.list 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	items, err := e.meetings.List(ctx)
	if err != nil {
		return meetingsFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, publicMeeting(m, false))
	}
	return bridge.Success(r.ID, map[string]any{"items": out})
}

func handleMeetingsStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Title       string `json:"title"`
		AudioSource string `json:"audioSource"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.start 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	m, err := e.meetings.Start(ctx, p.Title, p.AudioSource)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicMeeting(m, true))
}

func handleMeetingsAppend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
		Text      string `json:"text"`
		StartedMS int64  `json:"startedMs"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.append 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	seg, err := e.meetings.Append(ctx, p.MeetingID, p.Text, p.StartedMS)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicSegment(seg))
}

func handleMeetingsAudioAppend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
		PCM       string `json:"pcm"`
	}
	if decodePayload(r.Payload, &p) != nil || p.MeetingID == "" || p.PCM == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.audio.append 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	pcm, err := base64.StdEncoding.DecodeString(p.PCM)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.audio.append 音频编码无效", false)
	}
	audioMS, err := e.meetings.AppendAudio(ctx, p.MeetingID, pcm)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"meetingId": p.MeetingID, "audioMs": audioMS})
}

func handleMeetingsLoopbackPoll(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.MeetingID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.loopback.poll 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	pcm, active, err := e.meetings.PollLoopback(ctx, p.MeetingID)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{
		"meetingId": p.MeetingID,
		"active":    active,
		"pcm":       base64.StdEncoding.EncodeToString(pcm),
	})
}

func handleMeetingsCatchup(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.catchup 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	m, err := e.meetings.CatchUp(ctx, p.MeetingID)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicMeeting(m, true))
}

func handleMeetingsStop(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.stop 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	m, err := e.meetings.Stop(ctx, p.MeetingID)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicMeeting(m, true))
}

func handleMeetingsHeartbeat(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.heartbeat 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	m, err := e.meetings.Heartbeat(ctx, p.MeetingID)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicMeeting(m, false))
}

func handleMeetingsGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.get 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	m, err := e.meetings.Get(ctx, p.MeetingID)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicMeeting(m, true))
}

func handleMeetingsSummarize(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.summarize 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	m, err := e.meetings.Summarize(ctx, p.MeetingID)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicMeeting(m, true))
}

func handleMeetingsUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID  string  `json:"meetingId"`
		Title      *string `json:"title"`
		Summary    *string `json:"summary"`
		Actions    *string `json:"actions"`
		Transcript *string `json:"transcript"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.update 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	m, err := e.meetings.Update(ctx, p.MeetingID, meetings.MeetingPatch{
		Title: p.Title, Summary: p.Summary, Actions: p.Actions, Transcript: p.Transcript,
	})
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, publicMeeting(m, true))
}

func handleMeetingsDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.delete 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	if err := e.meetings.Delete(ctx, p.MeetingID); err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"meetingId": p.MeetingID})
}

func handleMeetingsExport(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID string `json:"meetingId"`
		Format    string `json:"format"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "meetings.export 参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	path, format, err := e.meetings.Export(ctx, p.MeetingID, p.Format, "")
	if err != nil {
		return meetingsFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"path": path, "format": format})
}

func meetingsUnavailable(r bridge.Request) bridge.Response {
	return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "会议记录暂时不可用", true)
}

func meetingsFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, meetings.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "MEETING_NOT_FOUND", "会议不存在", false)
	case errors.Is(err, meetings.ErrInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "会议请求无效", false)
	case errors.Is(err, meetings.ErrBusy):
		return bridge.Failure(r.ID, r.TraceID, "MEETING_BUSY", "已有一场会议正在录制", false)
	case errors.Is(err, meetings.ErrNotRecording):
		return bridge.Failure(r.ID, r.TraceID, "MEETING_NOT_RECORDING", "当前会议未在录制", false)
	case errors.Is(err, meetings.ErrCanceled):
		return bridge.Failure(r.ID, r.TraceID, "MEETING_CANCELED", "已取消导出", false)
	case errors.Is(err, meetings.ErrUnsupported):
		return bridge.Failure(r.ID, r.TraceID, "MEETING_PICKER_UNSUPPORTED", "当前系统没有可用的保存对话框", false)
	case errors.Is(err, meetings.ErrUnavailable):
		return meetingsUnavailable(r)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "会议记录暂时不可用", true)
	}
}

func publicMeeting(m meetings.Meeting, detail bool) map[string]any {
	out := map[string]any{
		"meetingId": m.MeetingID, "title": m.Title, "status": string(m.Status), "audioSource": m.AudioSource,
		"startedAt": m.StartedAt, "endedAt": m.EndedAt, "durationMs": m.DurationMS,
		"summary": m.Summary, "actions": m.Actions, "transcript": m.Transcript,
		"createdAt": m.CreatedAt, "updatedAt": m.UpdatedAt,
	}
	if m.SummaryError != "" {
		out["summaryError"] = m.SummaryError
	}
	if detail {
		segs := m.Segments
		if len(segs) > 4000 {
			segs = segs[len(segs)-4000:]
		}
		outSegs := make([]map[string]any, 0, len(segs))
		for _, seg := range segs {
			outSegs = append(outSegs, publicSegment(seg))
		}
		docs := make([]map[string]any, 0, len(m.Docs))
		for _, doc := range m.Docs {
			docs = append(docs, map[string]any{
				"docId": doc.DocID, "meetingId": doc.MeetingID, "kind": doc.Kind, "body": doc.Body, "createdAt": doc.CreatedAt,
			})
		}
		out["segments"] = outSegs
		out["docs"] = docs
	}
	return out
}

func publicSegment(seg meetings.Segment) map[string]any {
	return map[string]any{
		"segmentId": seg.SegmentID, "meetingId": seg.MeetingID, "seq": seg.Seq,
		"startedMs": seg.StartedMS, "text": seg.Text, "createdAt": seg.CreatedAt,
	}
}

const meetingNotesSystem = `你是月汐的会议纪要助手。根据本机转写的逐字稿生成中文会议文档。
只输出一个 JSON 对象，不要 Markdown 围栏：
{"title":"简短标题","summary":"会议摘要","actions":["待办1","待办2"]}

会议摘要（summary）必须写清三块，即使用逐字稿很短也不要写成一句带过：
- 背景：开会目的或起因（仅当逐字稿能支持；没有就写「未说明背景」）
- 讨论要点：实际说到的议题，分点列出
- 结论：达成的共识或明确下一步；没有就写「未形成明确结论」

决议/待办（actions）：
- 每条必须可执行：做什么；谁来做（仅当逐字稿出现人名或角色）；截止时间（仅当逐字稿提到）
- 写成「谁 + 做什么 + 截止（若有）」；没有责任人就写事项本身
- 没有明确待办时返回空数组，不要编造

规则：
- 只使用逐字稿里出现的事实；不要把「呃」「啊」写进摘要
- 不要编造未出现的人名、部门、数字或决议
- 字母缩写按常见写法还原（如 brd / b r d → BRD）
- 全文逐字稿由系统另行保存，不要在 JSON 里重复逐字稿
- 逐字稿可能混有本机麦克风与扬声器对面的声音，不要臆测发言人`

func meetingSummarySlow(modelID string) bool {
	id := strings.ToLower(modelID)
	return strings.Contains(id, "r1") || strings.Contains(id, "reasoner") || strings.Contains(id, "thinking")
}

func meetingSummaryCandidates(items []provider.Provider) []provider.CatalogEntry {
	catalog := provider.CatalogForKind(items, provider.KindLLM)
	preferred := make([]provider.CatalogEntry, 0, len(catalog))
	rest := make([]provider.CatalogEntry, 0)
	for _, entry := range catalog {
		if meetingSummarySlow(entry.Model.ModelID) {
			rest = append(rest, entry)
			continue
		}
		preferred = append(preferred, entry)
	}
	out := append(preferred, rest...)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func (e *Engine) completeMeeting(ctx context.Context, title, transcript string) (meetings.Notes, error) {
	if e.providers == nil {
		return meetings.Notes{}, errors.New("没有可用模型")
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return meetings.Notes{}, err
	}
	candidates := meetingSummaryCandidates(items)
	if len(candidates) == 0 {
		return meetings.Notes{}, errors.New("没有已启用的模型")
	}
	user := "会议标题：" + title + "\n\n以下是本机转写（已去掉部分语气词）。请按系统要求输出 JSON。逐字稿很短时也要分「背景 / 讨论要点 / 结论」写摘要，决议/待办要可执行。\n\n逐字稿：\n" + transcript
	var last error
	for _, entry := range candidates {
		var content string
		err = e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(ctx context.Context, secret []byte) error {
			adapter, adapterErr := e.adapter(ctx, entry.Provider)
			if adapterErr != nil {
				return adapterErr
			}
			resp, completeErr := adapter.Complete(ctx, secret, gateway.Request{
				Model: entry.Model.ModelID,
				Messages: []gateway.Message{
					{Role: gateway.RoleSystem, Content: meetingNotesSystem},
					{Role: gateway.RoleUser, Content: user},
				},
				MaxTokens:        4096,
				MaxAttempts:      2,
				DisableReasoning: true,
			})
			if completeErr != nil {
				return completeErr
			}
			content = strings.TrimSpace(resp.Message.Content)
			return nil
		})
		if err == nil {
			return meetings.ParseNotes(content, title), nil
		}
		last = err
	}
	if last == nil {
		return meetings.Notes{}, errors.New("没有已启用的模型")
	}
	return meetings.Notes{}, last
}

func (e *Engine) transcribeMeetingPCM(ctx context.Context, pcm []byte) (string, error) {
	if e.voice == nil || e.voice.backend == nil {
		return "", errors.New("本机识别不可用")
	}
	if len(pcm) < 2 {
		return "", nil
	}
	if err := e.voice.backend.Ready(ctx); err != nil {
		return "", err
	}
	session, err := e.voice.backend.Start(ctx, voice.SessionOptions{Language: "zh-CN"})
	if err != nil {
		return "", err
	}
	defer session.Close()
	frame := voice.FrameBytes
	if frame < 2 {
		frame = 3200
	}
	for i := 0; i < len(pcm); {
		end := i + frame
		if end > len(pcm) {
			end = len(pcm)
		}
		if (end-i)%2 != 0 {
			end--
		}
		if end <= i {
			break
		}
		if err := session.Append(ctx, pcm[i:end]); err != nil {
			return "", err
		}
		i = end
	}
	text, err := session.Finish(ctx)
	if err != nil && strings.TrimSpace(text) == "" {
		if errors.Is(err, voice.ErrNoAudio) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(text), nil
}
