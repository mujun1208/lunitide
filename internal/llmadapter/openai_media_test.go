package llmadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGPTImageUsesDocumentedRequest(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(200, `{"data":[{"url":"https://cdn.example/image.png"}]}`)}}
	_, err := NewOpenAI(f, Options{}).GenerateImage(context.Background(), nil, "gpt-image-2-all", "blue square")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.NewDecoder(f.requests[0].Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-image-2-all" || body["size"] != "1024x1024" || body["n"] != float64(1) || body["response_format"] != nil || f.requests[0].Header.Get("Accept") != "application/json" {
		t.Fatalf("unexpected image request: %+v", body)
	}
}

func TestImageResponseBudgetIncludesBase64(t *testing.T) {
	data := []byte(strings.Repeat("a", 2<<20))
	body := `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(data) + `"}]}`
	f := &fakeConnector{responses: []*http.Response{response(200, body)}}
	out, err := NewOpenAI(f, Options{}).GenerateImage(context.Background(), nil, "gpt-image-2-all", "blue square")
	if err != nil || len(out.Data) != len(data) {
		t.Fatalf("base64 response rejected: bytes=%d err=%v", len(out.Data), err)
	}
	f = &fakeConnector{responses: []*http.Response{response(200, strings.Repeat(" ", (8<<20)+1))}}
	_, err = NewOpenAI(f, Options{}).GenerateImage(context.Background(), nil, "gpt-image-2-all", "blue square")
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "RESPONSE_TOO_LARGE" || len(f.requests) != 1 {
		t.Fatalf("oversized response not bounded: %v", err)
	}
}

func TestSeedreamImageUsesSupportedSize(t *testing.T) {
	for model, size := range map[string]string{"doubao-seedream-5-0-260128": "2K", "doubao-seedream-3-0-t2i": "1024x1024"} {
		f := &fakeConnector{responses: []*http.Response{response(200, `{"data":[{"url":"https://cdn.example/x.png"}]}`)}}
		if _, err := NewOpenAI(f, Options{}).GenerateImage(context.Background(), nil, model, "blue square"); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.NewDecoder(f.requests[0].Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["size"] != size || body["response_format"] != "url" || body["n"] != nil {
			t.Fatalf("invalid Seedream request: %+v", body)
		}
	}
}

func TestOpenAIGenerateImageParsesURL(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(200, `{"data":[{"url":"https://cdn.example/x.png"}]}`)}}
	out, err := NewOpenAI(f, Options{}).GenerateImage(context.Background(), []byte("k"), "dall-e-3", "a cat")
	if err != nil || out.URL != "https://cdn.example/x.png" {
		t.Fatalf("out=%#v err=%v", out, err)
	}
	if f.requests[0].URL.Path != "/v1/images/generations" {
		t.Fatalf("path=%s", f.requests[0].URL.Path)
	}
	body, _ := io.ReadAll(f.requests[0].Body)
	if !strings.Contains(string(body), "dall-e-3") || !strings.Contains(string(body), "a cat") {
		t.Fatalf("body=%s", body)
	}
}

func TestOpenAIGenerateVideoFallsThroughPaths(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{
		response(404, `{"error":{"message":"nope"}}`),
		response(200, `{"id":"vid_1","url":"https://cdn.example/v.mp4"}`),
	}}
	out, err := NewOpenAI(f, Options{}).GenerateVideo(context.Background(), nil, "sora", "waves")
	if err != nil || out.URL != "https://cdn.example/v.mp4" || out.ID != "vid_1" {
		t.Fatalf("out=%#v err=%v", out, err)
	}
	if len(f.requests) != 2 {
		t.Fatalf("tried %d paths", len(f.requests))
	}
}

func TestOpenAIGenerateVolcVideoCreatesAndPollsTask(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{
		response(200, `{"id":"task_1","status":"submitted"}`),
		response(200, `{"id":"task_1","status":"succeeded","content":{"video_url":"https://cdn.example/seedance.mp4"}}`),
	}}
	out, err := NewOpenAI(f, Options{}).GenerateVideo(context.Background(), []byte("k"), "doubao-seedance-1-5-pro", "waves")
	if err != nil || out.URL != "https://cdn.example/seedance.mp4" || out.ID != "task_1" {
		t.Fatalf("out=%#v err=%v", out, err)
	}
	if len(f.requests) != 2 || f.requests[0].URL.Path != "/v1/contents/generations/tasks" || f.requests[1].Method != http.MethodGet {
		t.Fatalf("requests=%#v", f.requests)
	}
	body, _ := io.ReadAll(f.requests[0].Body)
	if !strings.Contains(string(body), `"type":"text"`) || !strings.Contains(string(body), "waves") {
		t.Fatalf("body=%s", body)
	}
}

func TestVolcVideoDoesNotResubmitOnFailure(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{503, `{"error":"unavailable"}`},
		{429, `{"error":"quota"}`},
		{200, `{"id":"../escape"}`},
		{200, `{"id":"job","status":"unknown"}`},
		{200, `{"id":"job","status":"failed","content":{"video_url":"https://cdn.example/input.mp4"}}`},
	} {
		f := &fakeConnector{responses: []*http.Response{response(tc.status, tc.body)}}
		out, err := NewOpenAI(f, Options{}).GenerateVideo(context.Background(), nil, "doubao-seedance-2-0-260128", "waves")
		if err == nil || out.URL != "" || len(f.requests) != 1 {
			t.Fatalf("body=%s out=%+v err=%v requests=%d", tc.body, out, err, len(f.requests))
		}
	}
}

func TestVolcVideoQueuedReferenceIsNotOutput(t *testing.T) {
	out, _, _, ok := parseVolcVideoTask([]byte(`{"id":"job","status":"queued","url":"https://cdn.example/status","content":{"video_url":"https://cdn.example/reference.mp4"}}`))
	if !ok || out.URL != "" {
		t.Fatalf("unexpected output: %+v", out)
	}
}
