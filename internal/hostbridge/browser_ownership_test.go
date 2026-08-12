package hostbridge

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
)

type browserHandlerStub struct{ called bool }

func (h *browserHandlerStub) HandleHost(_ context.Context, r bridge.Request) bridge.Response {
	h.called = true
	return bridge.Success(r.ID, map[string]string{"status": "opening"})
}

func TestBrowserMethodsAreEnabledHostOwnedAndNotForwarded(t *testing.T) {
	for _, method := range []bridge.Method{bridge.MethodBrowserOpen, bridge.MethodBrowserClose} {
		metadata := bridge.MethodMetadataByMethod[method]
		if metadata.Owner != "host" || !metadata.Enabled {
			t.Fatalf("%s metadata = %+v", method, metadata)
		}
	}
	handler := &browserHandlerStub{}
	caller := &callerStub{}
	gateway, err := New("https://app.lunitide.local", caller, map[bridge.Method]Handler{bridge.MethodBrowserOpen: handler})
	if err != nil {
		t.Fatal(err)
	}
	response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: validRequest(t, string(bridge.MethodBrowserOpen))})
	if !handled || !response.OK || !handler.called {
		t.Fatalf("host route = (%+v,%v), called=%v", response, handled, handler.called)
	}
	if caller.calls != 0 {
		t.Fatalf("browser request forwarded to engine %d times", caller.calls)
	}
}
