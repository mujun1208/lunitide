package scheduler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestValidateWebhookURLAcceptsVendorHosts(t *testing.T) {
	for _, ok := range []string{
		"",
		"https://open.feishu.cn/open-apis/bot/v2/hook/abc",
		"https://open.larksuite.com/open-apis/bot/v2/hook/abc",
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x",
		"https://oapi.dingtalk.com/robot/send?access_token=x",
	} {
		if err := ValidateWebhookURL(ok); err != nil {
			t.Fatalf("expected ok for %q: %v", ok, err)
		}
	}
}

func TestValidateWebhookURLRejectsSSRFShapes(t *testing.T) {
	for _, bad := range []string{
		"http://open.feishu.cn/hook",            // plain http
		"https://127.0.0.1/hook",                // loopback literal
		"https://10.1.2.3/hook",                 // private literal
		"https://[::1]/hook",                    // v6 literal
		"https://localhost/hook",                // localhost name
		"https://intranet.internal/hook",        // internal suffix
		"https://user:pass@open.feishu.cn/hook", // userinfo
		"https://a.cn/" + strings.Repeat("x", 600), // over rune budget
	} {
		if err := ValidateWebhookURL(bad); err == nil {
			t.Fatalf("expected rejection for %q", bad)
		}
	}
}

// newTestWebhook wires a notifier straight at the httptest server (the
// production constructor refuses loopback literals by design, so tests
// construct the struct directly).
func newTestWebhook(serverURL, targetHost string) *webhookNotifier {
	u, _ := url.Parse(serverURL)
	// Point the endpoint at the test server while classifying like the
	// production host so payload shapes stay covered.
	kind := webhookGeneric
	switch {
	case strings.Contains(targetHost, "feishu"), strings.Contains(targetHost, "lark"):
		kind = webhookLark
	case strings.Contains(targetHost, "qyapi.weixin"):
		kind = webhookWeCom
	case strings.Contains(targetHost, "dingtalk"):
		kind = webhookDingTalk
	}
	return &webhookNotifier{endpoint: *u, kind: kind, client: &http.Client{Timeout: webhookHTTPTimeout}}
}

func TestWebhookNotifierPostsLarkPayload(t *testing.T) {
	var body map[string]any
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()
	n := newTestWebhook(srv.URL, "open.feishu.cn")
	if err := n.Notify("Lunitide 自动化", "任务「日报」已完成"); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if body["msg_type"] != "text" {
		t.Fatalf("msg_type = %v", body["msg_type"])
	}
	content, _ := body["content"].(map[string]any)
	if content == nil || content["text"] != "Lunitide 自动化\n任务「日报」已完成" {
		t.Fatalf("content = %+v", body)
	}
}

func TestWebhookNotifierSurfacesVendorErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":19021,"msg":"sign match fail"}`))
	}))
	defer srv.Close()
	n := newTestWebhook(srv.URL, "open.feishu.cn")
	err := n.Notify("t", "b")
	if err == nil {
		t.Fatal("expected vendor error surfaced")
	}
}

func TestWebhookNotifierRejectsHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	n := newTestWebhook(srv.URL, "hook.example.com")
	if err := n.Notify("t", "b"); err == nil {
		t.Fatal("expected status error")
	}
}

func TestWebhookPayloadShapesPerVendor(t *testing.T) {
	g := &webhookNotifier{kind: webhookGeneric}
	l := &webhookNotifier{kind: webhookLark}
	w := &webhookNotifier{kind: webhookWeCom}
	d := &webhookNotifier{kind: webhookDingTalk}
	if _, ok := g.buildPayload("t", "b")["text"]; !ok {
		t.Fatal("generic shape")
	}
	if l.buildPayload("t", "b")["msg_type"] != "text" {
		t.Fatal("lark shape")
	}
	if w.buildPayload("t", "b")["msgtype"] != "text" {
		t.Fatal("wecom shape")
	}
	if d.buildPayload("t", "b")["msgtype"] != "text" {
		t.Fatal("dingtalk shape")
	}
}

func TestJobWebhookURLRoundTripAndValidation(t *testing.T) {
	s := newTestStore(t)
	job := validJob("带通知的日报", "30 8 * * *")
	job.WebhookURL = "https://open.feishu.cn/open-apis/bot/v2/hook/abc"
	if err := s.PutJob(job); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := s.GetJob(job.ID)
	if !ok || got.WebhookURL != job.WebhookURL {
		t.Fatalf("webhook round trip: %+v", got)
	}
	bad := validJob("x", "* * * * *")
	bad.WebhookURL = "http://insecure.example.com/hook"
	if err := s.PutJob(bad); err == nil {
		t.Fatal("invalid webhook accepted")
	}
}
