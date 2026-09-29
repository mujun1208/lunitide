package attachmenthost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
)

type fakeEngine struct {
	calls   int
	method  string
	payload []byte
}

func (f *fakeEngine) Call(_ context.Context, req bridge.Request) (bridge.Response, error) {
	f.calls++
	f.method = req.Method
	f.payload = append([]byte(nil), req.Payload...)
	return bridge.Response{OK: true, Payload: map[string]any{
		"attachmentId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "projectId": "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		"originalName": "需求.txt", "mime": "text/plain", "size": 12, "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"parseStatus": "succeeded", "parseErrorCode": "", "parsedTextBytes": 12, "createdAt": "2026-01-01T00:00:00Z",
	}}, nil
}

func TestImportLocalRejectsUnpickedPath(t *testing.T) {
	engine := &fakeEngine{}
	h := &Handler{Allow: func(string) bool { return false }, Engine: engine}
	resp := h.HandleHost(context.Background(), bridge.Request{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Method: "attachment.importLocal",
		Payload: json.RawMessage(`{"projectId":"01ARZ3NDEKTSV4RRFFQ69G5FAW","paths":["C:/secret.txt"]}`),
	})
	if !resp.OK {
		t.Fatalf("host response = %#v", resp.Error)
	}
	if engine.calls != 0 {
		t.Fatal("denied path reached the engine")
	}
	raw, _ := json.Marshal(resp.Payload)
	if !json.Valid(raw) || !contains(string(raw), "不能读取未选择的文件") {
		t.Fatalf("payload = %s", raw)
	}
}

func TestImportLocalForwardsAllowedPath(t *testing.T) {
	engine := &fakeEngine{}
	h := &Handler{Allow: func(path string) bool { return path == `C:\notes.txt` }, Engine: engine}
	resp := h.HandleHost(context.Background(), bridge.Request{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Method: "attachment.importLocal",
		Payload: json.RawMessage(`{"projectId":"01ARZ3NDEKTSV4RRFFQ69G5FAW","sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAX","paths":["C:\\notes.txt"]}`),
	})
	if !resp.OK || engine.calls != 1 || engine.method != "internal.attachment.importPath" {
		t.Fatalf("ok=%v calls=%d method=%s err=%v", resp.OK, engine.calls, engine.method, resp.Error)
	}
	if !contains(string(engine.payload), `C:\\notes.txt`) && !contains(string(engine.payload), `C:\notes.txt`) {
		t.Fatalf("payload = %s", engine.payload)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !contains(string(raw), "01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatalf("payload = %s", raw)
	}
}

func contains(s, part string) bool {
	return len(s) >= len(part) && (s == part || len(part) == 0 || (func() bool {
		for i := 0; i+len(part) <= len(s); i++ {
			if s[i:i+len(part)] == part {
				return true
			}
		}
		return false
	})())
}
