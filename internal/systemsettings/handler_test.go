package systemsettings

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
)

func TestHandlerOnlyOpensMicrophonePrivacyPage(t *testing.T) {
	calls := 0
	handler := &Handler{OpenMicrophone: func() error { calls++; return nil }}
	request := bridge.Request{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Payload: []byte(`{"page":"privacy-microphone"}`)}
	response := handler.HandleHost(context.Background(), request)
	if !response.OK || calls != 1 {
		t.Fatalf("unexpected response=%#v calls=%d", response, calls)
	}
	for _, payload := range []string{`{}`, `{"page":"privacy-camera"}`, `{"page":"ms-settings:privacy-microphone"}`, `{"page":"privacy-microphone","uri":"calc.exe"}`, `{"page":"privacy-microphone"}{}`} {
		request.Payload = []byte(payload)
		response = handler.HandleHost(context.Background(), request)
		if response.OK || calls != 1 || response.Error == nil || response.Error.Code != "INVALID_SYSTEM_SETTINGS_PAGE" {
			t.Fatalf("untrusted payload %s was not rejected: %#v calls=%d", payload, response, calls)
		}
	}
}

func TestHandlerSanitizesShellFailure(t *testing.T) {
	handler := &Handler{OpenMicrophone: func() error { return errors.New("sensitive shell detail") }}
	request := bridge.Request{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Payload: []byte(`{"page":"privacy-microphone"}`)}
	response := handler.HandleHost(context.Background(), request)
	if response.OK || response.Error == nil || response.Error.Code != "SYSTEM_SETTINGS_OPEN_FAILED" || response.Error.Message != "无法打开 Windows 麦克风设置" {
		t.Fatalf("unexpected response: %#v", response)
	}
}
