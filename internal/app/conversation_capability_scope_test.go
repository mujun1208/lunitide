package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/voice"
)

func scopedConversationPlugins(t *testing.T, e *Engine) func(string) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user")
	e.SetM8PluginService(svc)
	if err = m8app.EnsureBuiltinPlugins(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	return func(plugin string) {
		listed, err := svc.List(context.Background(), "", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range listed.Plugins {
			if p.PluginID == plugin {
				raw, _ := json.Marshal(map[string]any{"installId": p.InstallID, "enabled": false})
				if r := handlePluginToggle(e, context.Background(), bridge.Request{Payload: raw}); !r.OK {
					t.Fatalf("toggle: %#v", r.Error)
				}
				return
			}
		}
		t.Fatalf("missing plugin %s", plugin)
	}
}

type delayedVoiceFinal struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (s *delayedVoiceFinal) Append(context.Context, []byte) error { return nil }
func (s *delayedVoiceFinal) Finish(ctx context.Context) (string, error) {
	close(s.started)
	<-ctx.Done()
	return "停用后迟到的完整文字", nil
}
func (s *delayedVoiceFinal) Close() error { s.once.Do(func() { close(s.closed) }); return nil }

func TestVoiceCapabilityDisableCancelsInFlightAndRejectsLateFinal(t *testing.T) {
	e := NewEngine(nil, "test")
	disable := scopedConversationPlugins(t, e)
	e.voice = &VoiceService{sessions: map[string]voice.Session{}}
	underlying := &delayedVoiceFinal{started: make(chan struct{}), closed: make(chan struct{})}
	id, err := e.startScopedVoice(context.Background(), func(context.Context) (voice.Session, error) { return underlying, nil })
	if err != nil {
		t.Fatal(err)
	}
	session, _ := e.voiceSession(id)
	result := make(chan error, 1)
	go func() {
		text, err := session.Finish(context.Background())
		if text != "" {
			result <- errors.New("late final accepted")
			return
		}
		result <- err
	}()
	<-underlying.started
	disable("stt")
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("finish: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finish not canceled")
	}
	select {
	case <-underlying.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("recognizer not released")
	}
	if _, ok := e.voiceSession(id); ok {
		t.Fatal("revoked recognizer retained")
	}
	if _, err = e.startScopedVoice(context.Background(), func(context.Context) (voice.Session, error) {
		t.Fatal("disabled recognizer started")
		return underlying, nil
	}); err == nil {
		t.Fatal("disabled scope accepted")
	}
}
