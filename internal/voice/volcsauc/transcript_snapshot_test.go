package volcsauc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lunitide/lunitide/internal/voice"
)

func TestSessionKeepsAudioPositionsAndDoesNotTreatOldDefiniteAsCurrentFinal(t *testing.T) {
	frames := []string{
		`{"result":{"text":"我们开会。","utterances":[{"text":"我们开会。","start_time":0,"end_time":1000,"definite":true}]}}`,
		`{"result":{"text":"我们现在开会议。先看一下本周","utterances":[{"text":"我们现在开会议。","start_time":0,"end_time":1000,"definite":true},{"text":"先看一下本周","start_time":1500,"end_time":2400,"definite":false}]}}`,
		`{"result":{"text":"我们现在开会议。先看一下本周进度。","utterances":[{"text":"我们现在开会议。","start_time":0,"end_time":1000,"definite":true},{"text":"先看一下本周进度。","start_time":1500,"end_time":3000,"definite":true}]}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for _, raw := range frames {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
			if err = conn.WriteMessage(websocket.BinaryMessage, pack(msgFullServer, 0, serialJSON, 0, 0, []byte(raw))); err != nil {
				return
			}
		}
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	got := make(chan voice.Transcript, len(frames))
	b := New(Config{BaseURL: server.URL, APIKey: "test-only", Dial: insecureWSDial(server)})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := b.Start(ctx, voice.SessionOptions{OnTranscript: func(tr voice.Transcript) { got <- tr }})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	reader := sess.(interface{ LatestTranscript() voice.Transcript })
	for i := range frames {
		if i > 0 {
			if err := sess.Append(ctx, make([]byte, voice.FrameBytes*2)); err != nil {
				t.Fatal(err)
			}
		}
		waitTranscript(t, got)
		tr := reader.LatestTranscript()
		if tr.Final != (i != 1) {
			t.Fatalf("step %d final=%v", i, tr.Final)
		}
		if i > 0 && (len(tr.Utterances) != 2 || tr.Utterances[1].StartMs != 1500) {
			t.Fatalf("step %d lost positions: %+v", i, tr)
		}
		if again := reader.LatestTranscript(); again.Text != "" || len(again.Utterances) != 0 {
			t.Fatalf("step %d replayed consumed snapshot", i)
		}
	}
}

func TestSnapshotWindowDoesNotResendAnEntireLongMeeting(t *testing.T) {
	var utterances []map[string]any
	for i := 0; i < 500; i++ {
		utterances = append(utterances, map[string]any{"text": fmt.Sprintf("这是第%d个分句。", i), "start_time": i * 1000, "end_time": i*1000 + 900, "definite": i < 499})
	}
	raw, err := json.Marshal(map[string]any{"result": map[string]any{"text": strings.Repeat("旧的全文。", 1000), "utterances": utterances}})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := snapshotFromJSON(raw)
	if !ok || len(tr.Utterances) != 64 || tr.Utterances[0].StartMs != 436000 || tr.Final {
		t.Fatalf("bad bounded snapshot: len=%d final=%v ok=%v", len(tr.Utterances), tr.Final, ok)
	}
	if strings.Contains(tr.Text, "旧的全文") || !strings.HasSuffix(tr.Text, "这是第499个分句。") {
		t.Fatal("text was not the recent timeline window")
	}
}

func TestSnapshotWithoutValidPositionsUsesCompatibleWholeText(t *testing.T) {
	tr, ok := snapshotFromJSON([]byte(`{"result":{"text":"第一句。第二句。","utterances":[{"text":"第一句。","definite":true},{"text":"第二句。","definite":true}]}}`))
	if !ok || tr.Text != "第一句。第二句。" || !tr.Final || len(tr.Utterances) != 0 {
		t.Fatalf("fallback=%+v", tr)
	}
}
