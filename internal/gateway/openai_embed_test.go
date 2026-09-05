package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestOpenAIEmbedRequestShapeAndBLOB(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(200, `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`)}}
	out, err := NewOpenAI(f, Options{}).Embed(context.Background(), []byte("k"), "text-embedding-3-small", []string{"ATA", "gear"})
	if err != nil || len(out) != 2 || out[0][0] != 1 || out[1][1] != 1 {
		t.Fatalf("out=%#v err=%v", out, err)
	}
	if f.requests[0].URL.Path != "/v1/embeddings" {
		t.Fatalf("path=%s", f.requests[0].URL.Path)
	}
	body, _ := io.ReadAll(f.requests[0].Body)
	var payload struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Model != "text-embedding-3-small" || len(payload.Input) != 2 {
		t.Fatalf("body=%s", body)
	}
	blob := EncodeEmbeddingBLOB(out[0])
	got, ok := DecodeEmbeddingBLOB(blob)
	if !ok || len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("blob roundtrip %v ok=%v", got, ok)
	}
	if _, ok := DecodeEmbeddingBLOB([]byte{0, 0, 0, 0}); ok {
		t.Fatal("dim 0 must skip")
	}
	if _, ok := DecodeEmbeddingBLOB(bytes.Repeat([]byte{1}, 3)); ok {
		t.Fatal("short blob must skip")
	}
}

func TestOpenAIEmbedRejectsEmpty(t *testing.T) {
	f := &fakeConnector{}
	if _, err := NewOpenAI(f, Options{}).Embed(context.Background(), nil, "m", nil); err == nil {
		t.Fatal("empty texts must fail")
	}
}
