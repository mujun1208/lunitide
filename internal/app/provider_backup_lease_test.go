package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
)

type recordingLease struct {
	mu   sync.Mutex
	refs []string
}

func (l *recordingLease) WithLease(_ context.Context, req secretlease.Request, fn func([]byte) error) error {
	l.mu.Lock()
	l.refs = append(l.refs, req.CredentialRef)
	l.mu.Unlock()
	return fn([]byte(req.CredentialRef))
}

func (l *recordingLease) seen() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.refs...)
}

type rotateStreamAdapter struct {
	mu       sync.Mutex
	secrets  []string
	fail401  bool
	emitThen bool
}

func (a *rotateStreamAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *rotateStreamAdapter) TestConnection(context.Context, []byte, gateway.Request) error {
	return errors.New("not used")
}
func (a *rotateStreamAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *rotateStreamAdapter) Stream(_ context.Context, secret []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	a.mu.Lock()
	a.secrets = append(a.secrets, string(secret))
	a.mu.Unlock()
	if a.fail401 {
		return gateway.Response{}, &gateway.Error{Code: "HTTP_401", HTTPStatus: 401, Message: "unauthorized"}
	}
	if a.emitThen {
		if err := emit(gateway.Delta{Text: "partial"}); err != nil {
			return gateway.Response{}, err
		}
		return gateway.Response{}, &gateway.Error{Code: "HTTP_429", HTTPStatus: 429, Message: "insufficient_quota"}
	}
	if string(secret) == "primary-ref" {
		return gateway.Response{}, &gateway.Error{Code: "HTTP_429", HTTPStatus: 429, Message: "insufficient_quota"}
	}
	if err := emit(gateway.Delta{Text: "ok-from-backup"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Message: gateway.Message{Content: "ok-from-backup"}}, nil
}

func backupProvider() provider.Provider {
	return provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://api.example.com", CredentialRef: "primary-ref",
		CredentialRefBackups: []string{"backup-ref"},
		CredentialState:      provider.CredentialConfigured, Status: provider.StatusEnabled,
	}
}

func runBackupStream(t *testing.T, adapter *rotateStreamAdapter, lease *recordingLease) []bridge.Event {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", lease)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return adapter, nil
	})
	_, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-backup"
	e.streams[id] = state
	var events []bridge.Event
	e.runStream(context.Background(), id, state, backupProvider(), gateway.Request{Model: "m"}, func(event bridge.Event) error {
		events = append(events, event)
		return nil
	}, "")
	return events
}

func TestLeaseRotatesOnZeroDelta429(t *testing.T) {
	lease := &recordingLease{}
	adapter := &rotateStreamAdapter{}
	events := runBackupStream(t, adapter, lease)
	if got := lease.seen(); len(got) < 2 || got[0] != "primary-ref" || got[1] != "backup-ref" {
		t.Fatalf("D-F1 refs=%v", got)
	}
	var text strings.Builder
	var failed bool
	for _, ev := range events {
		if ev.Type == bridge.EventDelta && ev.Delta != nil {
			text.WriteString(ev.Delta.Text)
		}
		if ev.Type == bridge.EventFailed {
			failed = true
		}
	}
	if failed || !strings.Contains(text.String(), "ok-from-backup") {
		t.Fatalf("D-F1 events=%#v", events)
	}
}

func TestLeaseDoesNotRotateOn401(t *testing.T) {
	lease := &recordingLease{}
	adapter := &rotateStreamAdapter{fail401: true}
	events := runBackupStream(t, adapter, lease)
	if got := lease.seen(); len(got) != 1 || got[0] != "primary-ref" {
		t.Fatalf("D-F2 refs=%v", got)
	}
	last := events[len(events)-1]
	if last.Type != bridge.EventFailed || last.Error == nil || last.Error.Code != "PROVIDER_AUTHENTICATION_FAILED" {
		t.Fatalf("D-F2 terminal=%#v", last)
	}
}

func TestLeaseDoesNotRotateAfterEventDelta(t *testing.T) {
	lease := &recordingLease{}
	adapter := &rotateStreamAdapter{emitThen: true}
	events := runBackupStream(t, adapter, lease)
	if got := lease.seen(); len(got) != 1 || got[0] != "primary-ref" {
		t.Fatalf("D-F5 refs=%v", got)
	}
	last := events[len(events)-1]
	if last.Type != bridge.EventFailed || last.Error == nil || last.Error.Code != "PROVIDER_RATE_LIMITED" {
		t.Fatalf("D-F5 terminal=%#v", last)
	}
}

func TestPublicProviderOmitsCredentialRefs(t *testing.T) {
	dto := publicProvider(backupProvider())
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "primary-ref") || strings.Contains(string(raw), "backup-ref") || strings.Contains(string(raw), "credentialRef") {
		t.Fatalf("D-F3 leaked ref: %s", raw)
	}
	if dto.CredentialBackupCount != 1 {
		t.Fatalf("D-F3 count=%d", dto.CredentialBackupCount)
	}
}

func TestSharedLeaseRotateStateAcrossComplete(t *testing.T) {
	p := backupProvider()
	p.CredentialRefBackups = []string{"backup-ref", "backup-2"}
	rot := &leaseRotateState{swaps: 1}
	adapter := &rotateCompleteAdapter{}
	e := NewEngineWithGateway(nil, "test", &recordingLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return adapter, nil
	})
	ctx := withLeaseRotate(context.Background(), p, rot, func() bool { return false })
	resp, err := e.completeMaybeRotate(ctx, adapter, []byte("backup-ref"), gateway.Request{Model: "m"})
	if err != nil {
		t.Fatalf("shared rotate: %v", err)
	}
	if rot.swaps != 2 || resp.Message.Content != "from-backup-2" {
		t.Fatalf("plan.verify/council must reuse swap counter, swaps=%d content=%q secrets=%v", rot.swaps, resp.Message.Content, adapter.secrets)
	}
}

type rotateCompleteAdapter struct {
	secrets []string
}

func (a *rotateCompleteAdapter) Complete(_ context.Context, secret []byte, _ gateway.Request) (gateway.Response, error) {
	a.secrets = append(a.secrets, string(secret))
	if string(secret) == "backup-2" {
		return gateway.Response{Message: gateway.Message{Content: "from-backup-2"}}, nil
	}
	return gateway.Response{}, &gateway.Error{Code: "HTTP_429", HTTPStatus: 429, Message: "insufficient_quota"}
}
func (a *rotateCompleteAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
}
func (a *rotateCompleteAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, nil
}

func TestIsQuotaRotateError(t *testing.T) {
	if !isQuotaRotateError(&gateway.Error{HTTPStatus: 429, Message: "slow down"}) {
		t.Fatal("429 must rotate")
	}
	if !isQuotaRotateError(errors.New("insufficient_quota")) {
		t.Fatal("insufficient_quota must rotate")
	}
	if isQuotaRotateError(&gateway.Error{HTTPStatus: 401, Message: "unauthorized"}) {
		t.Fatal("401 must not rotate")
	}
}
