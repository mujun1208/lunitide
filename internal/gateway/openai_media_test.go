package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
