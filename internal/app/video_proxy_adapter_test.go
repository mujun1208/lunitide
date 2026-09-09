package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
)

func TestVideoProxyAdapterUsesSeparateSameOriginConnector(t *testing.T) {
	for _, tc := range []struct {
		name           string
		nativeStatus   int
		nativeBody     string
		nativePollCode int
		proxyPostCode  int
		proxyPollCode  int
		wantOK         bool
		wantPaths      []string
	}{
		{"fallback", 404, `{"error":"Invalid URL"}`, 0, 200, 200, true, []string{"POST /api/v3/contents/generations/tasks", "POST /v1/video/generations", "GET /v1/video/generations/task_proxy"}},
		{"native success", 200, `{"id":"task_native","status":"queued"}`, 200, 0, 0, true, []string{"POST /api/v3/contents/generations/tasks", "GET /api/v3/contents/generations/tasks/task_native"}},
		{"native poll404", 200, `{"id":"task_native","status":"queued"}`, 404, 0, 0, false, []string{"POST /api/v3/contents/generations/tasks", "GET /api/v3/contents/generations/tasks/task_native"}},
		{"native poll500", 200, `{"id":"task_native","status":"queued"}`, 500, 0, 0, false, []string{"POST /api/v3/contents/generations/tasks", "GET /api/v3/contents/generations/tasks/task_native"}},
		{"native500", 500, `{}`, 0, 0, 0, false, []string{"POST /api/v3/contents/generations/tasks"}},
		{"nativeHTML", 200, `<html>not API</html>`, 0, 0, 0, false, []string{"POST /api/v3/contents/generations/tasks"}},
		{"ambiguous404", 404, `{"id":"task_native","status":"queued"}`, 0, 0, 0, false, []string{"POST /api/v3/contents/generations/tasks"}},
		{"proxy post404", 404, `{}`, 0, 404, 0, false, []string{"POST /api/v3/contents/generations/tasks", "POST /v1/video/generations"}},
		{"proxy poll404", 404, `{}`, 0, 200, 404, false, []string{"POST /api/v3/contents/generations/tasks", "POST /v1/video/generations", "GET /v1/video/generations/task_proxy"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				paths = append(paths, r.Method+" "+r.URL.Path)
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing credential")
				}
				code, body := 500, `{}`
				switch r.Method + " " + r.URL.Path {
				case "POST /api/v3/contents/generations/tasks":
					code, body = tc.nativeStatus, tc.nativeBody
				case "GET /api/v3/contents/generations/tasks/task_native":
					code, body = tc.nativePollCode, `{"id":"task_native","status":"succeeded","content":{"video_url":"https://cdn.example/final.mp4"}}`
				case "POST /v1/video/generations":
					code, body = tc.proxyPostCode, `{"id":"task_proxy","task_id":"task_proxy","status":"queued"}`
					var params map[string]any
					if err := json.NewDecoder(r.Body).Decode(&params); err != nil || params["duration"] != float64(5) || params["prompt"] != "blue square" || params["model"] != "doubao-seedance-2-0-260128" {
						t.Errorf("invalid proxy payload: %+v err=%v", params, err)
					}
				case "GET /v1/video/generations/task_proxy":
					code, body = tc.proxyPollCode, `{"code":"success","data":{"id":154,"task_id":"task_proxy","status":"SUCCESS","result_url":"https://cdn.example/final.mp4","data":{"id":"different_upstream_id","status":"succeeded"}}}`
				default:
					t.Errorf("unexpected route: %s %s", r.Method, r.URL.Path)
				}
				if code == 0 {
					code = 500
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			e := NewEngine(providerRepositoryStub{}, "test")
			p := provider.Provider{ID: "video-test", Protocol: provider.ProtocolOpenAICompatible, BaseURL: server.URL + "/v1"}
			model := provider.Model{ModelID: "doubao-seedance-2-0-260128", Kind: provider.KindVideo}
			a, err := e.adapterForModel(context.Background(), p, model)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			out, err := a.(llmadapter.VideoGenerator).GenerateVideo(ctx, []byte("test-key"), model.ModelID, "blue square")
			if (err == nil) != tc.wantOK || (tc.wantOK && out.URL != "https://cdn.example/final.mp4") {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(paths, tc.wantPaths) {
				t.Fatalf("paths=%v want=%v", paths, tc.wantPaths)
			}
			if p.BaseURL != server.URL+"/v1" {
				t.Fatal("stored provider mutated")
			}
		})
	}
}

type videoTestProviders struct {
	providerRepositoryStub
	items []provider.Provider
}

func (p videoTestProviders) Get(_ context.Context, id string) (provider.Provider, error) {
	for _, item := range p.items {
		if item.ID == id {
			return item, nil
		}
	}
	return provider.Provider{}, provider.ErrNotFound
}

func (p videoTestProviders) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return p.items, nil
}

func videoTestProvider() provider.Provider {
	return provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "Video Test", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://video.invalid/v1", Status: provider.StatusEnabled, CredentialState: provider.CredentialConfigured, CredentialRef: "test-ref", Models: []provider.Model{{ModelID: "doubao-seedance-2-0-260128", Kind: provider.KindVideo, IsDefault: true, KindDefault: true}}}
}

type videoLeaseRecorder struct {
	requests []secretlease.Request
}

func (l *videoLeaseRecorder) WithLease(ctx context.Context, req secretlease.Request, fn func([]byte) error) error {
	l.requests = append(l.requests, req)
	return fn([]byte("test-key"))
}

type videoDeadlineAdapter struct {
	mediaProbeAdapter
	deadline time.Time
	calls    int
	err      error
}

func (a *videoDeadlineAdapter) GenerateVideo(ctx context.Context, _ []byte, _, _ string) (llmadapter.MediaResult, error) {
	a.deadline, _ = ctx.Deadline()
	a.calls++
	return llmadapter.MediaResult{URL: "https://cdn.example/final.mp4"}, a.err
}

func TestProviderVideoTestDeadlineReachesCredentialLeaseAndAdapter(t *testing.T) {
	p := videoTestProvider()
	lease := &videoLeaseRecorder{}
	a := &videoDeadlineAdapter{}
	e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p}}, "test", lease)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	for _, limit := range []int{bridge.ProviderTestDeadlineMS, 4000} {
		r := validRequest("provider.test", `{"providerId":"`+p.ID+`","modelId":"`+p.Models[0].ModelID+`"}`)
		r.DeadlineMS = limit
		response := e.Handle(context.Background(), r)
		if !response.OK {
			t.Fatalf("response=%+v", response)
		}
		var dto diagnosticDTO
		if err := json.Unmarshal(mustJSON(response.Payload), &dto); err != nil || dto.Status != "passed" {
			t.Fatalf("diagnostic=%+v err=%v", dto, err)
		}
		remaining := time.Until(a.deadline)
		if remaining < time.Duration(limit-1000)*time.Millisecond || remaining > time.Duration(limit)*time.Millisecond {
			t.Fatalf("adapter deadline incorrectly bounded: %v for %dms", remaining, limit)
		}
		req := lease.requests[len(lease.requests)-1]
		if req.Operation != secretlease.OperationProviderTest || !req.Deadline.Equal(a.deadline) || req.Origin != "https://video.invalid" {
			t.Fatalf("lease binding/deadline=%+v", req)
		}
	}
	for _, operation := range []secretlease.Operation{secretlease.OperationModelDiscover, secretlease.OperationProviderTest} {
		if err := e.withProviderLease(context.Background(), p, operation, func(ctx context.Context, _ []byte) error {
			d, _ := ctx.Deadline()
			remaining := time.Until(d)
			if operation == secretlease.OperationModelDiscover && remaining > 30*time.Second {
				t.Fatal("discovery lease was widened")
			}
			if operation == secretlease.OperationProviderTest && remaining < 5*time.Minute {
				t.Fatal("video diagnostic lease is too short")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVideoGenerationNeverRetriesAnotherCatalogModelAfterUncertainFailure(t *testing.T) {
	p := videoTestProvider()
	backup := p
	backup.ID, backup.Name = "01ARZ3NDEKTSV4RRFFQ69G5FAA", "Backup Video"
	for _, failure := range []error{context.DeadlineExceeded, &llmadapter.Error{Code: "HTTP_404", Stage: llmadapter.StageHTTP, HTTPStatus: 404}, &llmadapter.Error{Code: "HTTP_500", Stage: llmadapter.StageHTTP, HTTPStatus: 500}} {
		a := &videoDeadlineAdapter{err: failure}
		e := NewEngineWithGateway(videoTestProviders{items: []provider.Provider{p, backup}}, "test", &videoLeaseRecorder{})
		e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
		_, err := e.invokeMediaGenerate(context.Background(), "", "video.generate", json.RawMessage(`{"prompt":"blue square"}`))
		if err == nil || a.calls != 1 {
			t.Fatalf("failure=%v err=%v calls=%d", failure, err, a.calls)
		}
	}
}
