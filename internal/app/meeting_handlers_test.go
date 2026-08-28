package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newMeetingsEngine(t *testing.T) (*Engine, *meetings.Service) {
	t.Helper()
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "meetings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := meetings.New(store)
	e := NewEngine(nil, "test")
	e.SetMeetingsService(svc)
	return e, svc
}

func meetingsCall(t *testing.T, e *Engine, method string, payload any) bridge.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := bridge.Request{ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", TraceID: "01BBBBBBBBBBBBBBBBBBBBBBB", Method: method, Payload: raw}
	handler, ok := RuntimeHandlers[bridge.Method(method)]
	if !ok || handler == nil {
		t.Fatalf("method %s missing from RuntimeHandlers", method)
	}
	return handler(e, context.Background(), r)
}

func meetingsOK[Out any](t *testing.T, e *Engine, method string, payload any) Out {
	t.Helper()
	resp := meetingsCall(t, e, method, payload)
	if !resp.OK {
		t.Fatalf("%s failed: %+v", method, resp.Error)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v raw=%s", method, err, raw)
	}
	return out
}

func TestMeetingsHandlersStartStopSummarizeWithoutModel(t *testing.T) {
	e, _ := newMeetingsEngine(t)
	started := meetingsOK[map[string]any](t, e, "meetings.start", map[string]any{"title": "周会"})
	id, _ := started["meetingId"].(string)
	if started["status"] != "recording" || started["audioSource"] != "microphone" || id == "" {
		t.Fatalf("start = %#v", started)
	}
	seg := meetingsOK[map[string]any](t, e, "meetings.append", map[string]any{"meetingId": id, "text": "先对齐范围"})
	if seg["text"] != "先对齐范围" {
		t.Fatalf("append = %#v", seg)
	}
	stopped := meetingsOK[map[string]any](t, e, "meetings.stop", map[string]any{"meetingId": id})
	if stopped["status"] != "transcribed" || !strings.Contains(stopped["transcript"].(string), "对齐") {
		t.Fatalf("stop = %#v", stopped)
	}
	sum := meetingsOK[map[string]any](t, e, "meetings.summarize", map[string]any{"meetingId": id})
	if sum["status"] != "needs_summary" {
		t.Fatalf("summarize without model = %#v", sum)
	}
	if !strings.Contains(sum["summaryError"].(string), "尚未") && !strings.Contains(sum["summaryError"].(string), "模型") {
		t.Fatalf("honest error = %#v", sum["summaryError"])
	}
	listed := meetingsOK[struct {
		Items []map[string]any `json:"items"`
	}](t, e, "meetings.list", map[string]any{})
	if len(listed.Items) != 1 {
		t.Fatalf("list = %#v", listed.Items)
	}
	got := meetingsOK[map[string]any](t, e, "meetings.get", map[string]any{"meetingId": id})
	if got["meetingId"] != id {
		t.Fatalf("get = %#v", got)
	}
}

func TestMeetingsHandlersBusyAndUnavailable(t *testing.T) {
	e, _ := newMeetingsEngine(t)
	meetingsOK[map[string]any](t, e, "meetings.start", map[string]any{})
	resp := meetingsCall(t, e, "meetings.start", map[string]any{"title": "第二场"})
	if resp.OK || resp.Error == nil || resp.Error.Code != "MEETING_BUSY" {
		t.Fatalf("busy = %+v", resp)
	}
	bare := NewEngine(nil, "test")
	missing := meetingsCall(t, bare, "meetings.list", map[string]any{})
	if missing.OK || missing.Error == nil || missing.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("unwired = %+v", missing)
	}
}

func TestMeetingsHandlersStartSystemAudioAndRejectRemote(t *testing.T) {
	e, _ := newMeetingsEngine(t)
	resp := meetingsCall(t, e, "meetings.start", map[string]any{"audioSource": "remote"})
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("remote = %+v", resp)
	}
	started := meetingsOK[map[string]any](t, e, "meetings.start", map[string]any{"title": "周会", "audioSource": "microphone_and_system"})
	if started["audioSource"] != "microphone_and_system" {
		t.Fatalf("start mix = %#v", started)
	}
}

func TestMeetingsHandlersSummarizeWithCompleter(t *testing.T) {
	e, svc := newMeetingsEngine(t)
	svc.SetCompleter(func(ctx context.Context, title, transcript string) (meetings.Notes, error) {
		return meetings.Notes{Title: "评审纪要", Summary: "已对齐。", Actions: "- 导出文档"}, nil
	})
	started := meetingsOK[map[string]any](t, e, "meetings.start", map[string]any{})
	id, _ := started["meetingId"].(string)
	meetingsOK[map[string]any](t, e, "meetings.append", map[string]any{"meetingId": id, "text": "下周发布"})
	meetingsOK[map[string]any](t, e, "meetings.stop", map[string]any{"meetingId": id})
	sum := meetingsOK[map[string]any](t, e, "meetings.summarize", map[string]any{"meetingId": id})
	if sum["status"] != "ready" || sum["title"] != "评审纪要" {
		t.Fatalf("summarize = %#v", sum)
	}
}

func TestMeetingsHandlersUpdateAndDelete(t *testing.T) {
	e, _ := newMeetingsEngine(t)
	started := meetingsOK[map[string]any](t, e, "meetings.start", map[string]any{"title": "评审"})
	id, _ := started["meetingId"].(string)
	meetingsOK[map[string]any](t, e, "meetings.append", map[string]any{"meetingId": id, "text": "大家好"})
	meetingsOK[map[string]any](t, e, "meetings.stop", map[string]any{"meetingId": id})
	updated := meetingsOK[map[string]any](t, e, "meetings.update", map[string]any{"meetingId": id, "summary": "改过的摘要", "actions": "- 新待办"})
	if updated["summary"] != "改过的摘要" || updated["actions"] != "- 新待办" {
		t.Fatalf("update = %#v", updated)
	}
	deleted := meetingsOK[map[string]any](t, e, "meetings.delete", map[string]any{"meetingId": id})
	if deleted["meetingId"] != id {
		t.Fatalf("delete = %#v", deleted)
	}
	resp := meetingsCall(t, e, "meetings.get", map[string]any{"meetingId": id})
	if resp.OK || resp.Error == nil || resp.Error.Code != "MEETING_NOT_FOUND" {
		t.Fatalf("get after delete = %+v", resp)
	}
}

func TestMeetingNotesSystemAsksForActionsAndConclusions(t *testing.T) {
	for _, needle := range []string{"待办", "结论", "背景", "讨论要点", "不要编造"} {
		if !strings.Contains(meetingNotesSystem, needle) {
			t.Fatalf("prompt missing %q:\n%s", needle, meetingNotesSystem)
		}
	}
}
