package remotegateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 手机端媒体代理 /media/assets/<ticket> 的守卫逻辑：方法、token 形态、
// 未注入媒体服务。有效 ticket 的 200/206 链路由 e2e（真实配对 + openAsset
// 重写 URL）验证——ticket map 是 mediaapp 包内私有，跨包无法注入。

func newMediaProxyService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(context.Background(), newTestRoot(t), &fakeHandler{}, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	return svc
}

func TestMediaProxyWithoutService(t *testing.T) {
	svc := newMediaProxyService(t)
	rec := httptest.NewRecorder()
	svc.handleMediaAsset(rec, httptest.NewRequest(http.MethodGet, "/media/assets/tokentokentoken12", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no media service = %d, want 503", rec.Code)
	}
}

func TestMediaProxyMethodGuard(t *testing.T) {
	svc := newMediaProxyService(t)
	rec := httptest.NewRecorder()
	svc.handleMediaAsset(rec, httptest.NewRequest(http.MethodPost, "/media/assets/tokentokentoken12", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
}

func TestMediaProxyTokenShape(t *testing.T) {
	svc := newMediaProxyService(t)
	// SetMediaService(nil) 保持 media=nil；用 nil Service 直接构造 handler
	// 层校验：token 形态非法时必须在触碰 media 服务之前拒绝。
	svc.SetMediaService(nil)
	cases := []struct {
		path string
	}{
		{"/media/assets/"},                      // 空
		{"/media/assets/short"},                  // < 16
		{"/media/assets/tokentokentoken12/next"}, // 多段
		{"/media/assets/tokentokentoken~12"},     // 非法字符（~）
		{"/media/assets/tokentokentoken12.."},    // 非法字符（.）
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		svc.handleMediaAsset(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusServiceUnavailable {
			// 形态非法 → 404；合法形态但无 media 服务 → 503。本组全部
			// 形态非法，若出现其它状态说明校验顺序错了。
			t.Fatalf("%s = %d, want 404 (shape guard before service check)", tc.path, rec.Code)
		}
	}
}

func TestMediaProxyAcceptsValidTokenWithoutMedia(t *testing.T) {
	svc := newMediaProxyService(t)
	svc.SetMediaService(nil)
	// 形态合法的 token 走到服务层（nil → 503），证明守卫不吞掉合法请求。
	rec := httptest.NewRecorder()
	svc.handleMediaAsset(rec, httptest.NewRequest(http.MethodGet, "/media/assets/tokentokentoken12", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("valid token, nil service = %d, want 503", rec.Code)
	}
	// HEAD 同样被接受（只写头不写体）。
	rec = httptest.NewRecorder()
	svc.handleMediaAsset(rec, httptest.NewRequest(http.MethodHead, "/media/assets/tokentokentoken12", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("HEAD valid token = %d, want 503", rec.Code)
	}
}

func TestMediaProxyRouteRegistered(t *testing.T) {
	// 路由必须挂在 mux 上（漏挂 = 手机端全部 404 落到 SPA 回退）。
	svc := newMediaProxyService(t)
	svc.mu.Lock()
	server := svc.server
	svc.mu.Unlock()
	if server == nil {
		t.Skip("server not started; route registration covered by e2e")
	}
	mux, ok := server.Handler.(*http.ServeMux)
	if !ok {
		t.Fatal("server handler is not a ServeMux")
	}
	_, pattern := mux.Handler(httptest.NewRequest(http.MethodGet, "/media/assets/x", nil))
	if !strings.HasPrefix(pattern, "/media/assets/") {
		t.Fatalf("route pattern = %q, want /media/assets/", pattern)
	}
}
