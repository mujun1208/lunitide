package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/lunitide/lunitide/internal/secretlease"
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
		segs := make([]map[string]any, 0, len(m.Segments))
		for _, seg := range m.Segments {
			segs = append(segs, publicSegment(seg))
		}
		docs := make([]map[string]any, 0, len(m.Docs))
		for _, doc := range m.Docs {
			docs = append(docs, map[string]any{
				"docId": doc.DocID, "meetingId": doc.MeetingID, "kind": doc.Kind, "body": doc.Body, "createdAt": doc.CreatedAt,
			})
		}
		out["segments"] = segs
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

func (e *Engine) completeMeeting(ctx context.Context, title, transcript string) (meetings.Notes, error) {
	if e.providers == nil {
		return meetings.Notes{}, errors.New("没有可用模型")
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return meetings.Notes{}, err
	}
	var chosen provider.Provider
	for _, p := range items {
		if p.Status == provider.StatusEnabled && p.CredentialState == provider.CredentialConfigured && len(p.Models) > 0 {
			chosen = p
			break
		}
	}
	if chosen.ID == "" {
		return meetings.Notes{}, errors.New("没有已启用的模型")
	}
	model := chosen.Models[0].ModelID
	for _, m := range chosen.Models {
		if m.IsDefault {
			model = m.ModelID
			break
		}
	}
	user := "会议标题：" + title + "\n\n以下是本机转写（已去掉部分语气词）。请按系统要求输出 JSON。逐字稿很短时也要分「背景 / 讨论要点 / 结论」写摘要，决议/待办要可执行。\n\n逐字稿：\n" + transcript
	var content string
	err = e.withProviderLease(ctx, chosen, secretlease.OperationChat, func(ctx context.Context, secret []byte) error {
		adapter, adapterErr := e.adapter(ctx, chosen)
		if adapterErr != nil {
			return adapterErr
		}
		resp, completeErr := adapter.Complete(ctx, secret, gateway.Request{
			Model: model,
			Messages: []gateway.Message{
				{Role: gateway.RoleSystem, Content: meetingNotesSystem},
				{Role: gateway.RoleUser, Content: user},
			},
			MaxTokens:   4096,
			MaxAttempts: 1,
		})
		if completeErr != nil {
			return completeErr
		}
		content = strings.TrimSpace(resp.Message.Content)
		return nil
	})
	if err != nil {
		return meetings.Notes{}, err
	}
	return meetings.ParseNotes(content, title), nil
}
