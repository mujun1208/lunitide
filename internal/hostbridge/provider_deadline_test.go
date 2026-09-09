package hostbridge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
)

func TestGatewayProviderTestAllowsSixMinutesOnlyForDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		method string
		limit  int
		ok     bool
	}{
		{"provider.test", bridge.ProviderTestDeadlineMS, true},
		{"provider.test", bridge.ProviderTestDeadlineMS + 1, false},
		{"provider.model.sync", bridge.ProviderTestDeadlineMS, false},
		{"system.health", bridge.ProviderTestDeadlineMS, false},
	} {
		caller := &callerStub{}
		gateway, err := New("https://app.lunitide.local", caller)
		if err != nil {
			t.Fatal(err)
		}
		var request bridge.Request
		if err := json.Unmarshal(validRequest(t, tc.method), &request); err != nil {
			t.Fatal(err)
		}
		request.DeadlineMS = tc.limit
		request.Payload = json.RawMessage(`{"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","modelId":"doubao-seedance-2-0-260128"}`)
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		response, handled := gateway.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: raw})
		if !handled || response.OK != tc.ok || (caller.calls == 1) != tc.ok {
			t.Fatalf("%s deadline=%d response=%+v calls=%d", tc.method, tc.limit, response, caller.calls)
		}
	}
}
