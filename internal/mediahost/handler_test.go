package mediahost

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/desktopfiles"
	"github.com/oklog/ulid/v2"
)

type stubEngine struct {
	calls []bridge.Request
}

func (s *stubEngine) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	s.calls = append(s.calls, r)
	var p struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(r.Payload, &p)
	id := ulid.Make().String()
	return bridge.Success(r.ID, map[string]any{
		"assetId":    id,
		"sourceKind": "user_selected",
		"kind":       "audio",
		"title":      filepath.Base(p.Path),
		"mime":       "audio/mpeg",
		"size":       12,
		"state":      "ready",
		"revision":   1,
	}), nil
}

func TestPickCanceled(t *testing.T) {
	h := &Handler{Pick: func(bool, bool) ([]desktopfiles.Item, []string, error) {
		return nil, nil, desktopfiles.ErrCanceled
	}}
	resp := h.HandleHost(context.Background(), pickReq(`{"scopeKind":"user"}`))
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !strings.Contains(string(raw), `"canceled":true`) || !strings.Contains(string(raw), `"assets":[]`) {
		t.Fatalf("%s", raw)
	}
}

func TestPickRegistersAudioAndOmitsPaths(t *testing.T) {
	engine := &stubEngine{}
	audio := filepath.Join(t.TempDir(), "song.mp3")
	text := filepath.Join(t.TempDir(), "note.txt")
	h := &Handler{
		Engine: engine,
		Pick: func(folder, multiple bool) ([]desktopfiles.Item, []string, error) {
			if folder || !multiple {
				t.Fatalf("folder=%v multiple=%v", folder, multiple)
			}
			return []desktopfiles.Item{
				{Path: audio, FileName: "song.mp3", MIME: "audio/mpeg", Size: 12},
				{Path: text, FileName: "note.txt", MIME: "text/plain", Size: 4},
			}, nil, nil
		},
	}
	resp := h.HandleHost(context.Background(), pickReq(`{"scopeKind":"user","multiple":true}`))
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), audio) || strings.Contains(string(raw), `song.mp3`) == false {
		t.Fatalf("must return DTO without path: %s", raw)
	}
	if strings.Contains(string(raw), "note.txt") {
		t.Fatalf("non-media must be skipped: %s", raw)
	}
	if len(engine.calls) != 1 || engine.calls[0].Method != "internal.media.asset.register" {
		t.Fatalf("calls %+v", engine.calls)
	}
	var registered struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(engine.calls[0].Payload, &registered); err != nil || registered.Path != audio {
		t.Fatalf("engine must receive path: %s err=%v", engine.calls[0].Payload, err)
	}
}

func TestPickRejectsUserScopeId(t *testing.T) {
	h := &Handler{Pick: func(bool, bool) ([]desktopfiles.Item, []string, error) {
		return nil, nil, errors.New("should not pick")
	}}
	resp := h.HandleHost(context.Background(), pickReq(`{"scopeKind":"user","scopeId":"x"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("%+v", resp)
	}
}

func TestElementReportPlayingWithoutCommandIsNotAccepted(t *testing.T) {
	engine := &playerEngine{}
	h := &Handler{Player: &Player{Engine: engine, WindowInstanceID: "window-a"}}
	session := ulid.Make().String()
	resp := h.HandleHost(context.Background(), bridge.Request{
		ID: "req", TraceID: "tr", Method: "media.element.report",
		Payload: json.RawMessage(`{"mediaSessionId":"` + session + `","event":"playing","positionMs":0,"durationMs":100}`),
	})
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !strings.Contains(string(raw), `"accepted":false`) {
		t.Fatalf("%s", raw)
	}
	for _, method := range engine.listed() {
		if method == "internal.media.player.report" {
			t.Fatal("host must not report without a claimed command")
		}
	}
}

func TestElementReportPlayingWithCommandIsAccepted(t *testing.T) {
	engine := &playerEngine{nextOp: ulid.Make().String()}
	h := &Handler{Player: &Player{Engine: engine, WindowInstanceID: "window-a"}}
	session := ulid.Make().String()
	resp := h.HandleHost(context.Background(), bridge.Request{
		ID: "req", TraceID: "tr", Method: "media.element.report",
		Payload: json.RawMessage(`{"mediaSessionId":"` + session + `","event":"playing"}`),
	})
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !strings.Contains(string(raw), `"accepted":true`) {
		t.Fatalf("%s", raw)
	}
}

func pickReq(payload string) bridge.Request {
	return bridge.Request{ID: "req", TraceID: "tr", Method: "media.asset.pick", Payload: json.RawMessage(payload)}
}
