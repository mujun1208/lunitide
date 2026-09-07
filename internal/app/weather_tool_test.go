package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestWeatherStructuredToolActualChatRoundTrip(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	r, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	e.SetToolRuntime(r)
	requests := 0
	r.SetWebFetcher(func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		requests++
		if !strings.HasPrefix(raw, "https://api.met.no/") {
			return networkpolicy.FetchResult{}, errors.New("weather scraped a webpage")
		}
		now := time.Now().UTC()
		data, _ := json.Marshal(map[string]any{"properties": map[string]any{"meta": map[string]any{"updated_at": now, "units": map[string]string{"air_temperature": "celsius"}}, "timeseries": []any{map[string]any{"time": now, "data": map[string]any{"instant": map[string]any{"details": map[string]int{"air_temperature": 23}}}}}}})
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: data}, nil
	})
	modelCalls := 0
	a := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
		modelCalls++
		if modelCalls == 1 {
			if !routedRequestHasTool(req, "weather.get") || !strings.Contains(req.Messages[0].Content, "weather.get") {
				return llmadapter.Response{}, errors.New("weather route missing")
			}
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "weather-test", Name: "weather.get", Arguments: json.RawMessage(`{"place":"合肥","country":"CN","days":1}`)}}}}, nil
		}
		for _, message := range req.Messages {
			if message.Role == llmadapter.RoleTool && message.ToolCallID == "weather-test" {
				var value struct {
					Kind   string `json:"kind"`
					Source string `json:"source"`
				}
				if json.Unmarshal([]byte(message.Content), &value) != nil || value.Kind != "weather_forecast" || !strings.HasPrefix(value.Source, "https://api.met.no/") {
					return llmadapter.Response{}, errors.New("missing real structured evidence")
				}
				return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "合肥当前预报采样为23摄氏度，数据来自MET Norway。"}}, nil
			}
		}
		return llmadapter.Response{}, errors.New("no weather result")
	}}
	frames := runRoutedExecution(t, e, "合肥今天天气怎么样", a)
	for _, frame := range frames {
		if frame.Type == bridge.EventApprovalRequired {
			t.Fatal("read-only weather needs write approval")
		}
	}
	if modelCalls != 2 || requests != 1 {
		t.Fatalf("model=%d requests=%d", modelCalls, requests)
	}
}

func TestWeatherRespectsExistingNetworkGrantAndVoiceParity(t *testing.T) {
	if ids := m8app.ToolPluginIDs("weather.get", nil); len(ids) != 1 || ids[0] != "web-fetch" {
		t.Fatalf("network grant missing: %v", ids)
	}
	for _, profile := range []toolProfile{toolProfileDefault, toolProfileMinimal, toolProfileCoding} {
		for _, voice := range []bool{false, true} {
			defs := applyToolProfile(engineToolDefinitions(), profile)
			if voice {
				defs = filterCompanionDefaultTools(defs)
			}
			found := false
			for _, d := range defs {
				if d.Name == "weather.get" {
					found = true
				}
			}
			if !found {
				t.Fatalf("weather absent profile=%v voice=%v", profile, voice)
			}
		}
	}
	r, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetExecutionGate(func(context.Context, string, json.RawMessage) error { return errors.New("web disabled") })
	r.SetWebFetcher(func(context.Context, string) (networkpolicy.FetchResult, error) {
		t.Fatal("revoked weather reached network")
		return networkpolicy.FetchResult{}, nil
	})
	if _, err := r.Execute(context.Background(), toolruntime.Approval, "test", "weather.get", json.RawMessage(`{"place":"合肥"}`), false); err == nil {
		t.Fatal("revocation bypassed")
	}
}
