package app

import (
	"context"
	"github.com/lunitide/lunitide/internal/bridge"
)

func handleMeetingsTranscriptGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID          string `json:"meetingId"`
		TranscriptRevision int64  `json:"transcriptRevision"`
		Offset             int    `json:"offset"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "会议原稿分页参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	page, err := e.meetings.Transcript(ctx, p.MeetingID, p.TranscriptRevision, p.Offset)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return r.Ok(page)
}

func handleMeetingsSegmentsList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MeetingID        string `json:"meetingId"`
		ExpectedRevision int64  `json:"expectedRevision"`
		AfterSeq         int    `json:"afterSeq"`
		ThroughSeq       int    `json:"throughSeq"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "会议片段分页参数无效", false)
	}
	if e.meetings == nil {
		return meetingsUnavailable(r)
	}
	page, err := e.meetings.Segments(ctx, p.MeetingID, p.ExpectedRevision, p.AfterSeq, p.ThroughSeq)
	if err != nil {
		return meetingsFailure(r, err)
	}
	return r.Ok(page)
}
