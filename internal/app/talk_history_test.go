package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/talk"
)

func TestTalkFinalHistoryTwentyRoundsReplayAndReopen(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	session := &talkSession{talkID: "talk-reconnect-stable", sessionID: sessionID, providerProtocol: "openai_compatible", modelID: "realtime"}
	e.messages = &committedWithoutAck{MessageService: e.messages, loseAck: true}
	for i := 0; i < 20; i++ {
		for _, role := range []string{"user", "assistant"} {
			ev := talk.ServerEvent{Kind: "transcript", Final: true, Role: role, ItemID: fmt.Sprintf("item-%s-%d", role, i), Transcript: fmt.Sprintf("%s turn %d", role, i)}
			a, err := e.persistTalkTranscript(session, ev)
			if err != nil {
				t.Fatal(err)
			}
			b, err := e.persistTalkTranscript(session, ev)
			if err != nil || a.ID != b.ID {
				t.Fatalf("duplicate turn: %v", err)
			}
		}
	}
	reopened, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	messages, err := messageapp.New(reopened, reopened, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID, Direction: messageapp.Forward, Limit: 64, ByteBudget: messageapp.MaxByteBudget})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 40 {
		t.Fatalf("wanted 40 durable messages: %d", len(page.Items))
	}
	for i, m := range page.Items {
		role := message.RoleUser
		if i%2 == 1 {
			role = message.RoleAssistant
		}
		if m.Role != role || m.Sequence != int64(i+1) {
			t.Fatalf("order %d: %#v", i, m)
		}
	}
	e.messages = messages
	changed := talk.ServerEvent{Kind: "transcript", Final: true, Role: "assistant", ItemID: "item-assistant-0", Transcript: "changed content"}
	if _, err = e.persistTalkTranscript(session, changed); !errors.Is(err, messageapp.ErrIdempotencyConflict) {
		t.Fatalf("identity reused: %v", err)
	}
	unknown := talkSession{talkID: session.talkID, sessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", providerProtocol: session.providerProtocol, modelID: session.modelID}
	if _, err = e.persistTalkTranscript(&unknown, changed); err == nil {
		t.Fatal("missing session accepted")
	}
	if _, err = e.persistTalkTranscript(session, talk.ServerEvent{Role: "assistant", Transcript: "only a delta"}); err == nil {
		t.Fatal("delta accepted")
	}
}

type transcriptConn struct {
	frames    [][]byte
	afterRead func()
	once      sync.Once
	closed    chan struct{}
}

func (s *transcriptConn) WriteMessage(int, []byte) error { return nil }
func (s *transcriptConn) Close() error                   { s.once.Do(func() { close(s.closed) }); return nil }
func (s *transcriptConn) ReadMessage() (int, []byte, error) {
	if len(s.frames) == 0 {
		return 0, nil, io.EOF
	}
	raw := s.frames[0]
	s.frames = s.frames[1:]
	if s.afterRead != nil {
		s.afterRead()
	}
	return 1, raw, nil
}

func TestTalkStreamOnlyFinalTranscriptsCommitBeforeAck(t *testing.T) {
	for _, cancelAtRead := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelAtRead), func(t *testing.T) {
			e, _, sessionID, _ := messageEngine(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			frames := [][]byte{[]byte(`{"type":"response.audio_transcript.delta","delta":"temporary half sentence"}`), []byte(`{"type":"conversation.item.input_audio_transcription.completed","item_id":"user-1","transcript":"打开网页"}`), []byte(`{"type":"response.output_audio_transcript.done","item_id":"assistant-1","response_id":"response-1","transcript":"已为你打开网页"}`)}
			conn := &transcriptConn{frames: frames, closed: make(chan struct{})}
			if cancelAtRead {
				conn.afterRead = cancel
			}
			s := &talkSession{talkID: "stable-talk", streamID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", sessionID: sessionID, conn: conn, cancel: cancel}
			state := &streamState{cancel: cancel, talk: true}
			e.streams[s.streamID] = state
			e.putTalk(s)
			acknowledgements := 0
			e.runTalkStream(ctx, s, state, func(event bridge.Event) error {
				if event.Talk != nil && event.Talk.MessageID != "" {
					acknowledgements++
					page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID, Direction: messageapp.Forward})
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, m := range page.Items {
						if m.ID == event.Talk.MessageID && m.Text == event.Talk.Text {
							found = true
						}
					}
					if !found {
						t.Fatalf("ACK preceded commit: %#v", event)
					}
				}
				return nil
			})
			page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID, Direction: messageapp.Forward})
			if err != nil {
				t.Fatal(err)
			}
			if cancelAtRead {
				if len(page.Items) != 0 || acknowledgements != 0 {
					t.Fatal("cancel converted a delta to history")
				}
			} else if len(page.Items) != 2 || acknowledgements != 3 {
				raw, _ := json.Marshal(page)
				t.Fatalf("finals/handoff: acks=%d history=%s", acknowledgements, raw)
			}
			if e.talkBySession(sessionID) != nil {
				t.Fatal("disconnected session retained")
			}
			select {
			case <-conn.closed:
			default:
				t.Fatal("socket not released")
			}
		})
	}
}

func TestTalkFinalReceivedAtCancellationStillPersists(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &transcriptConn{frames: [][]byte{[]byte(`{"type":"conversation.item.input_audio_transcription.completed","item_id":"user-1","transcript":"完成的一句话"}`)}, afterRead: cancel, closed: make(chan struct{})}
	s := &talkSession{talkID: "cancel-talk", streamID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", sessionID: sessionID, conn: conn, cancel: cancel}
	state := &streamState{cancel: cancel, talk: true}
	e.streams[s.streamID] = state
	e.putTalk(s)
	e.runTalkStream(ctx, s, state, func(bridge.Event) error { return nil })
	page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("received final lost: %d %v", len(page.Items), err)
	}
}

type unavailableTalkMessages struct{ MessageService }

func (s unavailableTalkMessages) AppendAssistant(context.Context, string, string, string, string, messageapp.AssistantUsage) (message.Message, error) {
	return message.Message{}, errors.New("disk unavailable")
}

func TestTalkHistoryFailureDoesNotEmitFinalAckOrContinue(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	e.messages = unavailableTalkMessages{e.messages}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &transcriptConn{frames: [][]byte{[]byte(`{"type":"response.output_audio_transcript.done","item_id":"assistant-1","transcript":"无法持久的完整响应"}`), []byte(`{"type":"conversation.item.input_audio_transcription.completed","item_id":"user-2","transcript":"后续不应继续"}`)}, closed: make(chan struct{})}
	s := &talkSession{talkID: "failed-talk", streamID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", sessionID: sessionID, conn: conn, cancel: cancel}
	state := &streamState{cancel: cancel, talk: true}
	e.streams[s.streamID] = state
	e.putTalk(s)
	sawFailure, sawTerminal := false, false
	e.runTalkStream(ctx, s, state, func(event bridge.Event) error {
		if event.Talk != nil && event.Talk.MessageID != "" {
			t.Fatal("uncommitted final acknowledged")
		}
		if event.Talk != nil && event.Talk.Code == "TALK_HISTORY_SAVE_FAILED" {
			sawFailure = true
		}
		if event.Type == bridge.EventFailed {
			sawTerminal = true
		}
		return nil
	})
	if !sawFailure || !sawTerminal || len(conn.frames) != 1 {
		t.Fatalf("failure hidden or stream continued: %v %v %d", sawFailure, sawTerminal, len(conn.frames))
	}
	page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("unexpected history: %d %v", len(page.Items), err)
	}
}
