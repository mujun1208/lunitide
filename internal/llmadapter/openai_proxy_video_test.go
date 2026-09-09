package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProxyVideoGenerationTracksOuterTaskUntilSuccess(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{
		response(200, `{"id":"task_proxy","task_id":"task_proxy","object":"video","status":"queued","result_url":"https://cdn.example/reference.mp4"}`),
		response(200, `{"code":"success","data":{"id":154,"task_id":"task_proxy","status":"IN_PROGRESS","result_url":"https://cdn.example/reference.mp4","data":{"id":"upstream_other","status":"succeeded","content":{"video_url":"https://cdn.example/reference.mp4"}}}}`),
		response(200, `{"code":"success","data":{"task_id":"task_proxy","status":"SUCCESS","result_url":"https://cdn.example/final.mp4","data":{"id":"upstream_other","status":"succeeded","content":{"video_url":"https://cdn.example/final.mp4"},"duration":5}}}`),
	}}
	out, err := NewOpenAI(f, Options{}).GenerateProxyVideo(context.Background(), []byte("test-key"), "doubao-seedance-2-0-260128", "stationary blue square")
	if err != nil || out.ID != "task_proxy" || out.URL != "https://cdn.example/final.mp4" || out.MIME != "video/mp4" {
		t.Fatalf("result=%+v err=%v", out, err)
	}
	if len(f.requests) != 3 || f.requests[0].Method != http.MethodPost || f.requests[0].URL.Path != "/v1/video/generations" {
		t.Fatalf("unexpected requests: %+v", f.requests)
	}
	for i, req := range f.requests {
		if !f.authSeen[i] || req.Header.Get("Authorization") != "" {
			t.Fatal("credential was missing or retained")
		}
		if i > 0 && (req.Method != http.MethodGet || req.URL.Path != "/v1/video/generations/task_proxy") {
			t.Fatalf("unexpected poll: %s %s", req.Method, req.URL.Path)
		}
	}
	var params map[string]any
	if err := json.NewDecoder(f.requests[0].Body).Decode(&params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 3 || params["duration"] != float64(5) || params["model"] != "doubao-seedance-2-0-260128" || params["prompt"] != "stationary blue square" {
		t.Fatalf("request params=%+v", params)
	}
}

func TestProxyVideoTaskValidation(t *testing.T) {
	for _, tc := range []struct {
		name, raw, expected string
		valid, done         bool
	}{
		{"nested output", `{"task_id":"task_1","status":"SUCCESS","data":{"id":"different","status":"succeeded","content":{"video_url":"https://cdn.example/final.mp4"}}}`, "task_1", true, true},
		{"queued url", `{"task_id":"task_1","status":"queued","result_url":"https://cdn.example/input.mp4"}`, "task_1", true, false},
		{"queued nested success", `{"task_id":"task_1","status":"queued","data":{"id":"other","status":"succeeded","content":{"video_url":"https://cdn.example/input.mp4"}}}`, "task_1", true, false},
		{"wrong outer id", `{"task_id":"other","status":"SUCCESS","result_url":"https://cdn.example/final.mp4"}`, "task_1", false, false},
		{"nested id only", `{"status":"SUCCESS","data":{"id":"task_1","content":{"video_url":"https://cdn.example/final.mp4"}}}`, "task_1", false, false},
		{"unsafe id", `{"task_id":"../escape","status":"queued"}`, "", false, false},
		{"escaped id", `{"task_id":"task%2F1","status":"queued"}`, "", false, false},
		{"missing status", `{"task_id":"task_1"}`, "", false, false},
		{"unknown status", `{"task_id":"task_1","status":"complete"}`, "", false, false},
		{"missing result", `{"task_id":"task_1","status":"SUCCESS"}`, "task_1", false, false},
		{"failed with url", `{"task_id":"task_1","status":"FAILURE","result_url":"https://cdn.example/input.mp4"}`, "task_1", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var task proxyVideoTask
			if err := json.Unmarshal([]byte(tc.raw), &task); err != nil {
				t.Fatal(err)
			}
			out, done, err := task.result(tc.expected)
			if (err == nil) != tc.valid || done != tc.done || (!done && out.URL != "") {
				t.Fatalf("out=%+v done=%v err=%v", out, done, err)
			}
		})
	}
	for _, rawURL := range []string{"javascript:alert(1)", "file:///C:/private.mp4", "//cdn.example/x.mp4", "https://user:secret@cdn.example/x.mp4", "https://cdn.example/x.mp4#fragment", "https://cdn.example/x\n.mp4", "https://cdn.example/x\\y.mp4", "https:///missing-host", "https://:443/x"} {
		if out, _, err := (proxyVideoTask{TaskID: "task_1", Status: "SUCCESS", ResultURL: rawURL}).result("task_1"); err == nil || out.URL != "" {
			t.Fatalf("unsafe URL accepted: %q", rawURL)
		}
	}
}

func TestProxyVideoInvalidOrFailedPollNeverResubmits(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"poll404", `{"error":"missing"}`, 404},
		{"poll500", `{"error":"unavailable"}`, 500},
		{"html", `<html>not an API</html>`, 200},
		{"missing envelope", `{"task_id":"task_1","status":"SUCCESS","result_url":"https://cdn.example/v.mp4"}`, 200},
		{"failed envelope", `{"code":"failure","data":{"task_id":"task_1","status":"SUCCESS","result_url":"https://cdn.example/v.mp4"}}`, 200},
		{"wrong task", `{"code":"success","data":{"task_id":"other","status":"SUCCESS","result_url":"https://cdn.example/v.mp4"}}`, 200},
		{"numeric row wrong task", `{"code":"success","data":{"id":154,"task_id":"other","status":"SUCCESS","result_url":"https://cdn.example/v.mp4","data":{"id":"task_1"}}}`, 200},
		{"numeric row missing task", `{"code":"success","data":{"id":154,"status":"SUCCESS","result_url":"https://cdn.example/v.mp4","data":{"id":"task_1"}}}`, 200},
		{"failed task", `{"code":"success","data":{"task_id":"task_1","status":"FAILURE"}}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeConnector{responses: []*http.Response{response(200, `{"id":"task_1","task_id":"task_1","status":"queued"}`), response(tc.status, tc.body)}}
			out, err := NewOpenAI(f, Options{}).GenerateProxyVideo(context.Background(), nil, "seedance", "blue")
			if err == nil || out.URL != "" || IsVideoEndpointNotFound(err) || len(f.requests) != 2 || f.requests[1].Method != http.MethodGet {
				t.Fatalf("out=%+v err=%v requests=%d", out, err, len(f.requests))
			}
		})
	}
}

func TestProxyVideoRecordedPollIgnoresNumericDatabaseID(t *testing.T) {
	const taskID = "task_gdtUzbGS2qbg5q9UP2AH2ZSzrlYhqT6g"
	const recordedPoll = `{"code":"success","data":{"id":154,"task_id":"task_gdtUzbGS2qbg5q9UP2AH2ZSzrlYhqT6g","status":"SUCCESS","result_url":"https://cdn.example/video.mp4","data":{"id":"cgt-20260908230859-p6tvm","status":"succeeded","content":{"video_url":"https://cdn.example/video.mp4"},"duration":5}}}`
	f := &fakeConnector{responses: []*http.Response{
		response(200, `{"id":"`+taskID+`","task_id":"`+taskID+`","object":"video","status":"queued"}`),
		response(200, recordedPoll),
	}}
	out, err := NewOpenAI(f, Options{}).GenerateProxyVideo(context.Background(), nil, "doubao-seedance-2-0-260128", "blue square")
	if err != nil || out.ID != taskID || out.URL != "https://cdn.example/video.mp4" || out.MIME != "video/mp4" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if len(f.requests) != 2 || f.requests[0].Method != http.MethodPost || f.requests[1].Method != http.MethodGet || f.requests[1].URL.Path != "/v1/video/generations/"+taskID {
		t.Fatalf("unexpected submission or polling target: %+v", f.requests)
	}
}

func TestProxyVideoCreationStillRequiresMatchingStringID(t *testing.T) {
	for _, raw := range []string{
		`{"id":154,"task_id":"task_1","status":"queued"}`,
		`{"id":"other","task_id":"task_1","status":"queued"}`,
		`{"id":"task_1","task_id":154,"status":"queued"}`,
	} {
		f := &fakeConnector{responses: []*http.Response{response(200, raw)}}
		out, err := NewOpenAI(f, Options{}).GenerateProxyVideo(context.Background(), nil, "seedance", "blue square")
		if err == nil || out.URL != "" || IsVideoEndpointNotFound(err) || len(f.requests) != 1 {
			t.Fatalf("malformed creation accepted or retried: out=%+v err=%v requests=%d", out, err, len(f.requests))
		}
	}
}

func TestProxyVideoPollingCancellationDoesNotResubmit(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{
		response(200, `{"task_id":"task_1","status":"queued"}`),
		response(200, `{"code":"success","data":{"task_id":"task_1","status":"IN_PROGRESS"}}`),
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := NewOpenAI(f, Options{}).GenerateProxyVideo(ctx, nil, "seedance", "blue")
	if err == nil || IsVideoEndpointNotFound(err) || len(f.requests) != 2 {
		t.Fatalf("err=%v requests=%d", err, len(f.requests))
	}
}

type videoErrorBody struct{}

func (videoErrorBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (videoErrorBody) Close() error             { return nil }

type videoTransportError struct {
	fakeConnector
	failAt int
}

func (f *videoTransportError) Do(r *http.Request) (*http.Response, error) {
	if len(f.requests) < f.failAt {
		return f.fakeConnector.Do(r)
	}
	f.requests = append(f.requests, r)
	return nil, context.DeadlineExceeded
}

func TestNativeVideoFallbackMarkerOnlyBeforeAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		responses []*http.Response
		fallback  bool
	}{
		{"initial404", []*http.Response{response(404, `{"error":"Invalid URL"}`)}, true},
		{"initial500", []*http.Response{response(500, `{"error":"unavailable"}`)}, false},
		{"initial429", []*http.Response{response(429, `{"error":"quota"}`)}, false},
		{"initialHTML", []*http.Response{response(200, `<html>not API</html>`)}, false},
		{"ambiguous404", []*http.Response{response(404, `{"id":"task_1","status":"queued"}`)}, false},
		{"ambiguousNested404", []*http.Response{response(404, `{"data":{"task_id":"task_1"}}`)}, false},
		{"readFailure404", []*http.Response{{StatusCode: 404, Body: videoErrorBody{}}}, false},
		{"oversize404", []*http.Response{response(404, strings.Repeat(" ", 2<<20)+`{"id":"task_1"}`)}, false},
		{"poll404", []*http.Response{response(200, `{"id":"task_1","status":"queued"}`), response(404, `{}`)}, false},
		{"poll500", []*http.Response{response(200, `{"id":"task_1","status":"queued"}`), response(500, `{}`)}, false},
		{"pollReadFailure404", []*http.Response{response(200, `{"id":"task_1","status":"queued"}`), {StatusCode: 404, Body: videoErrorBody{}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantRequests := len(tc.responses)
			f := &fakeConnector{responses: tc.responses}
			_, err := NewOpenAI(f, Options{}).GenerateVideo(context.Background(), nil, "seedance", "blue")
			if err == nil || IsVideoEndpointNotFound(err) != tc.fallback || len(f.requests) != wantRequests {
				t.Fatalf("err=%v fallback=%v requests=%d", err, IsVideoEndpointNotFound(err), len(f.requests))
			}
			if tc.fallback {
				var upstream *Error
				if !errors.As(err, &upstream) || upstream.HTTPStatus != 404 {
					t.Fatal("structured diagnostic lost")
				}
			}
		})
	}
	for _, failAt := range []int{0, 1} {
		f := &videoTransportError{failAt: failAt, fakeConnector: fakeConnector{responses: []*http.Response{response(200, `{"id":"task_1","status":"queued"}`)}}}
		_, err := NewOpenAI(f, Options{}).GenerateVideo(context.Background(), nil, "seedance", "blue")
		if err == nil || IsVideoEndpointNotFound(err) || len(f.requests) != failAt+1 || strings.Contains(err.Error(), "secret") {
			t.Fatalf("timeout resubmitted: %v", err)
		}
	}
}

func TestProxyVideoInitialFailuresNeverResubmit(t *testing.T) {
	for _, resp := range []*http.Response{response(404, `{}`), response(500, `{}`), response(200, `<html>not API</html>`), {StatusCode: 404, Body: videoErrorBody{}}} {
		f := &fakeConnector{responses: []*http.Response{resp}}
		_, err := NewOpenAI(f, Options{}).GenerateProxyVideo(context.Background(), nil, "seedance", "blue")
		if err == nil || IsVideoEndpointNotFound(err) || len(f.requests) != 1 {
			t.Fatalf("proxy submission retried: %v", err)
		}
	}
	for _, failAt := range []int{0, 1} {
		f := &videoTransportError{failAt: failAt, fakeConnector: fakeConnector{responses: []*http.Response{response(200, `{"task_id":"task_1","status":"queued"}`)}}}
		_, err := NewOpenAI(f, Options{}).GenerateProxyVideo(context.Background(), nil, "seedance", "blue")
		if err == nil || IsVideoEndpointNotFound(err) || len(f.requests) != failAt+1 {
			t.Fatalf("proxy timeout retried: %v", err)
		}
	}
}
