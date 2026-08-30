package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

type callerStub struct {
	calls int
	err   error
}

func (c *callerStub) Call(_ context.Context, request bridge.Request) (bridge.Response, error) {
	c.calls++
	if c.err != nil {
		return bridge.Response{}, c.err
	}
	return bridge.Success(request.ID, map[string]string{"status": "ok"}), nil
}

func TestGatewayRejectsUntrustedOriginAndChildFrameBeforeRPC(t *testing.T) {
	caller := &callerStub{}
	gateway, err := New("https://app.lunitide.local", caller)
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest(t, "system.health")
	for _, message := range []Message{
		{SourceURL: "https://evil.example/", TopFrame: true, JSON: request},
		{SourceURL: "https://app.lunitide.local/", TopFrame: false, JSON: request},
		{SourceURL: "data:text/html,test", TopFrame: true, JSON: request},
	} {
		if _, handled := gateway.Handle(context.Background(), message); handled {
			t.Fatalf("untrusted message was handled: %#v", message)
		}
	}
	if caller.calls != 0 {
		t.Fatalf("untrusted messages reached RPC: %d calls", caller.calls)
	}
}

func TestGatewayRejectsOversizedMessageBeforeRPC(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	if _, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: make([]byte, MaxMessageBytes+1)}); handled {
		t.Fatal("oversized message was handled")
	}
	if caller.calls != 0 {
		t.Fatal("oversized message reached RPC")
	}
}

func TestGatewayReturnsSchemaErrorWithoutCallingRPC(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	raw := append(validRequest(t, "system.health"), []byte(` {}`)...)
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/index.html", TopFrame: true, JSON: raw})
	if !handled || response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if caller.calls != 0 {
		t.Fatal("invalid schema reached RPC")
	}
}

func TestGatewayRejectsIncompleteEnvelopeBeforeRPC(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: []byte(`{"method":"system.health"}`)})
	if !handled || response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if strings.Contains(response.Error.Message, "请求协议无效") {
		t.Fatalf("envelope copy still uses protocol jargon: %q", response.Error.Message)
	}
	if caller.calls != 0 {
		t.Fatal("incomplete envelope reached RPC")
	}
}

func TestEnvelopeErrorMessageIsHuman(t *testing.T) {
	if got := envelopeErrorMessage(errInvalidDeadline); got != "请求超时参数无效" {
		t.Fatalf("deadline = %q", got)
	}
	if got := envelopeErrorMessage(errInvalidSentAt); !strings.Contains(got, "时间") {
		t.Fatalf("clock = %q", got)
	}
	if strings.Contains(envelopeErrorMessage(errors.New("nope")), "请求协议无效") {
		t.Fatal("fallback must not say 请求协议无效")
	}
}

func TestGatewayAcceptsTrustedDocumentQueryAndFragment(t *testing.T) {
	for _, source := range []string{
		"https://app.lunitide.local/index.html?route=settings",
		"https://app.lunitide.local/index.html#settings",
	} {
		caller := &callerStub{}
		gateway, _ := New("https://app.lunitide.local", caller)
		response, handled := gateway.Handle(context.Background(), Message{SourceURL: source, TopFrame: true, JSON: validRequest(t, "system.health")})
		if !handled || !response.OK || caller.calls != 1 {
			t.Fatalf("trusted source %q lost Bridge access: handled=%v response=%#v calls=%d", source, handled, response, caller.calls)
		}
	}
}

func TestGatewayRejectsNullPayloadBeforeRPC(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	var request map[string]any
	if err := json.Unmarshal(validRequest(t, "system.health"), &request); err != nil {
		t.Fatal(err)
	}
	request["payload"] = nil
	raw, _ := json.Marshal(request)
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: raw})
	if !handled || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" || caller.calls != 0 {
		t.Fatalf("null payload was not rejected: %#v", response)
	}
}

func TestGatewayRejectsDisabledMethod(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: validRequest(t, "diagnostics.export")})
	if !handled || response.OK || response.Error == nil || response.Error.Code != "BRIDGE_METHOD_NOT_ALLOWED" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if caller.calls != 0 {
		t.Fatal("unknown method reached RPC")
	}
}

func TestGatewayForwardsEnabledProviderWriteMethod(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: validRequest(t, "provider.delete")})
	if !handled || !response.OK || caller.calls != 1 {
		t.Fatalf("enabled write was not forwarded: response=%#v calls=%d", response, caller.calls)
	}
}

func TestEngineForwardableRequiresEnabledEngineOwnership(t *testing.T) {
	tests := []struct {
		name     string
		metadata bridge.MethodMetadata
		want     bool
	}{
		{name: "enabled engine-owned", metadata: bridge.MethodMetadata{Owner: "engine", Enabled: true}, want: true},
		{name: "disabled engine-owned", metadata: bridge.MethodMetadata{Owner: "engine", Enabled: false}, want: false},
		{name: "enabled host-owned", metadata: bridge.MethodMetadata{Owner: "host", Enabled: true}, want: false},
		{name: "disabled host-owned", metadata: bridge.MethodMetadata{Owner: "host", Enabled: false}, want: false},
		{name: "enabled unknown owner", metadata: bridge.MethodMetadata{Owner: "other", Enabled: true}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isEngineForwardable(test.metadata); got != test.want {
				t.Fatalf("isEngineForwardable(%+v) = %t, want %t", test.metadata, got, test.want)
			}
		})
	}
}

func TestGatewayDoesNotForwardEnabledHostOwnedMethod(t *testing.T) {
	method := bridge.MethodDiagnosticsExport
	original := bridge.MethodMetadataByMethod[method]
	bridge.MethodMetadataByMethod[method] = bridge.MethodMetadata{Owner: "host", Enabled: true}
	t.Cleanup(func() { bridge.MethodMetadataByMethod[method] = original })

	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: validRequest(t, string(method))})
	if !handled || response.OK || response.Error == nil || response.Error.Code != "BRIDGE_METHOD_NOT_ALLOWED" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if caller.calls != 0 {
		t.Fatalf("enabled host-owned method reached engine RPC: %d calls", caller.calls)
	}
}

func TestGatewayMapsEngineFailureWithoutLeakingDetails(t *testing.T) {
	caller := &callerStub{err: errors.New("pipe \\.\\pipe\\secret failed")}
	gateway, _ := New("https://app.lunitide.local", caller)
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: validRequest(t, "system.health")})
	if !handled || response.OK || response.Error == nil || response.Error.Code != "ENGINE_UNAVAILABLE" || response.Error.Message != "核心引擎暂时不可用" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestGatewayReturnsExplicitFailureForOversizedTrustedMessage(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	request := bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "attachment.ingest", SentAt: time.Now().UTC(), Payload: json.RawMessage(`{"contentBase64":"` + strings.Repeat("A", MaxMessageBytes) + `"}`), DeadlineMS: 30000}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: raw})
	if !handled || response.OK || response.Error == nil || response.Error.Code != "BRIDGE_REQUEST_TOO_LARGE" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if caller.calls != 0 {
		t.Fatalf("oversized request reached engine RPC: %d calls", caller.calls)
	}
}

func TestGatewayAcceptsLongMeetingDeadline(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	id := ulid.Make().String()
	trace := ulid.Make().String()
	request := bridge.Request{
		Version: bridge.Version, Kind: "request", ID: id, TraceID: trace,
		Method: "meetings.append", SentAt: time.Now().UTC(),
		Payload: json.RawMessage(`{"meetingId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","text":"x"}`), DeadlineMS: 120000,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: raw})
	if !handled || !response.OK || caller.calls != 1 {
		t.Fatalf("long meeting deadline rejected: handled=%v resp=%#v calls=%d", handled, response, caller.calls)
	}
	health := bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: "system.health", SentAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), DeadlineMS: 120000,
	}
	healthRaw, err := json.Marshal(health)
	if err != nil {
		t.Fatal(err)
	}
	denied, handledHealth := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: healthRaw})
	if !handledHealth || denied.OK || denied.Error == nil || denied.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("health must keep the 30s cap: %+v", denied)
	}
	if denied.Error.Message != "请求超时参数无效" {
		t.Fatalf("deadline mismatch message = %q", denied.Error.Message)
	}
}

func TestGatewayAcceptsPeopleScreenCaptureDeadline(t *testing.T) {
	caller := &callerStub{}
	gateway, _ := New("https://app.lunitide.local", caller)
	request := bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: "people.screen.capture", SentAt: time.Now().UTC(),
		Payload: json.RawMessage(`{"region":true}`), DeadlineMS: bridge.PeopleCaptureDeadlineMS,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: raw})
	if !handled || !response.OK || caller.calls != 1 {
		t.Fatalf("people screen capture deadline rejected: handled=%v resp=%#v calls=%d", handled, response, caller.calls)
	}
}

func validRequest(t *testing.T, method string) []byte {
	t.Helper()
	raw, err := json.Marshal(bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: method, SentAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
