package remotegateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

// fakeRoot 是测试用的数据根：临时目录 + no-op ACL 保护。
type fakeRoot struct{ dir string }

func (r fakeRoot) FilePath(name string) (string, error) {
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("unsafe name %q", name)
	}
	return filepath.Join(r.dir, name), nil
}

func (r fakeRoot) ProtectRegularFile(string) error { return nil }

func newTestRoot(t *testing.T) fakeRoot {
	t.Helper()
	return fakeRoot{dir: t.TempDir()}
}

// fakeHandler 扮演引擎：直接回成功，记录收到的请求。
type fakeHandler struct {
	lastMethod string
}

func (h *fakeHandler) Handle(_ context.Context, request bridge.Request) bridge.Response {
	h.lastMethod = request.Method
	return request.Ok(map[string]any{"engine": "fake", "ok": true})
}

func newRequest(t *testing.T, method string, payload any) bridge.Request {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return bridge.Request{
		Version:  bridge.Version,
		Kind:     "request",
		ID:       ulid.Make().String(),
		TraceID:  ulid.Make().String(),
		Method:   method,
		SentAt:   time.Now().UTC(),
		Payload:  raw,
	}
}

func TestStoreDeviceRoundtrip(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, newTestRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	device := Device{
		DeviceID:  "01TESTDEVICE",
		Name:      "测试手机",
		Platform:  "ios-pwa",
		TokenHash: "abc",
		Scopes:    DefaultScopes,
		GrantedAt: now,
		ExpiresAt: now.Add(DeviceTokenTTL),
		LastIP:    "192.0.2.9",
	}
	if err := store.DeviceSave(ctx, device); err != nil {
		t.Fatal(err)
	}
	got, err := store.DeviceByTokenHash(ctx, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != device.DeviceID || got.Name != device.Name || got.Platform != device.Platform {
		t.Fatalf("device mismatch: %+v", got)
	}
	if len(got.Scopes) != len(DefaultScopes) {
		t.Fatalf("scopes mismatch: %v", got.Scopes)
	}
	if err := store.DeviceRevoke(ctx, device.DeviceID, now); err != nil {
		t.Fatal(err)
	}
	// 吊销后令牌查询直接找不到（DeviceByTokenHash 过滤 revoked）。
	if _, err := store.DeviceByTokenHash(ctx, "abc"); err == nil {
		t.Fatal("revoked device still authenticates")
	}
}

func TestStorePairingCodeSingleUse(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, newTestRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	hash := hashToken("12345678")
	if err := store.PairingCodeSave(ctx, hash, now, now.Add(PairCodeTTL)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.PairingCodeConsume(ctx, hash, now); !ok {
		t.Fatal("valid code not consumed")
	}
	if ok, _ := store.PairingCodeConsume(ctx, hash, now); ok {
		t.Fatal("code consumed twice")
	}
	if ok, _ := store.PairingCodeConsume(ctx, hashToken("00000000"), now); ok {
		t.Fatal("unknown code consumed")
	}
	expired := hashToken("87654321")
	if err := store.PairingCodeSave(ctx, expired, now.Add(-time.Hour), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.PairingCodeConsume(ctx, expired, now); ok {
		t.Fatal("expired code consumed")
	}
}

func TestStoreConfigRoundtrip(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, newTestRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if on, _ := store.ConfigGet(ctx, "enabled"); on {
		t.Fatal("default enabled should be false")
	}
	if err := store.ConfigSet(ctx, "enabled", true); err != nil {
		t.Fatal(err)
	}
	if on, _ := store.ConfigGet(ctx, "enabled"); !on {
		t.Fatal("enabled not persisted")
	}
}

func TestMethodScopeClassification(t *testing.T) {
	cases := map[string]string{
		"chat.start":            scopeChat,
		"conversation.list":     scopeChat,
		"message.send":          scopeChat,
		"talk.start":            scopeChat,
		"system.health":         scopeChat,
		"fs.read":               scopeFiles,
		"workspace.list":        scopeFiles,
		"office.artifact.open":  scopeFiles,
		"attachment.upload":     scopeFiles,
		"productHub.overview":   scopeHub,
		"settings.get":          scopeSettings,
		"remote.access.status":  scopeSettings,
		"power.keepAwake.set":   scopeSettings,
		// 模型列表只读放行（手机端选模型的前提）；管理面仍拒绝。
		"provider.list":         scopeChat,
		"provider.get":          scopeChat,
		"provider.create":       scopeSettings,
		"provider.credential.submit": scopeSettings,
		"mcp.invoke":            scopeTools,
		"skill.package.list":    scopeTools,
		"plugin.pack.install":   scopeTools,
		"capability.roles.set":  scopeTools,
		"agentHub.thread.create": scopeTools,
		"capability.list":        scopeTools,
	}
	for method, want := range cases {
		if got := methodScope(method); got != want {
			t.Errorf("methodScope(%q) = %q, want %q", method, got, want)
		}
	}
	// 默认 scope 集合：settings 拒绝，其余放行。
	defaults := DefaultScopes
	if deviceAllows(defaults, "chat.start") != true {
		t.Error("chat denied by default scopes")
	}
	if deviceAllows(defaults, "fs.read") != true {
		t.Error("files denied by default scopes")
	}
	if deviceAllows(defaults, "productHub.overview") != true {
		t.Error("hub denied by default scopes")
	}
	if deviceAllows(defaults, "settings.get") != false {
		t.Error("settings allowed by default scopes")
	}
	if deviceAllows(defaults, "remote.access.enable") != false {
		t.Error("remote management allowed by default scopes")
	}
}

func TestPairCodeFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := randomPairCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 8 {
			t.Fatalf("code length = %d", len(code))
		}
		for _, c := range code {
			if c < '0' || c > '9' {
				t.Fatalf("non-digit code %q", code)
			}
		}
		seen[code] = true
	}
	if len(seen) < 90 {
		t.Fatalf("pair codes lack entropy: %d unique in 100", len(seen))
	}
}

func TestServicePairFlowAndLockout(t *testing.T) {
	ctx := context.Background()
	handler := &fakeHandler{}
	svc, err := New(ctx, newTestRoot(t), handler, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	svc.port = 0
	// 未开启时配对码签发必须拒绝。
	if _, err := svc.IssuePairCode(ctx, ""); err == nil {
		t.Fatal("pair code issued while disabled")
	}
	if err := svc.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.CertFingerprint == "" {
		t.Fatalf("status after enable: %+v", status)
	}
	info, err := svc.IssuePairCode(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.URL, "https://") || !strings.Contains(info.URL, info.Code) || !strings.Contains(info.URL, info.Fingerprint) {
		t.Fatalf("pair url malformed: %s", info.URL)
	}
	if !strings.Contains(info.URL, "&lang=zh-CN") {
		t.Fatalf("pair url missing desktop language: %s", info.URL)
	}
	if len(info.QRPngBase64) < 64 {
		t.Fatal("qr png missing")
	}
	// 错码 5 次 → 锁定。
	for i := 0; i < pairMaxFailures; i++ {
		if _, err := svc.HandlePair(ctx, "00000000", "攻击者", "other", "203.0.113.7"); err == nil {
			t.Fatal("wrong code paired")
		}
	}
	if _, err := svc.HandlePair(ctx, info.Code, "我的手机", "ios-pwa", "203.0.113.7"); err == nil {
		t.Fatal("locked IP still pairing")
	}
	// 正常来源 IP 不受影响。
	result, err := svc.HandlePair(ctx, info.Code, "我的手机", "ios-pwa", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if result.DeviceToken == "" || result.Product != "Lunitide" {
		t.Fatalf("pair result: %+v", result)
	}
	// 码已消费：同码再用失败。
	if _, err := svc.HandlePair(ctx, info.Code, "我的手机", "ios-pwa", "192.0.2.10"); err == nil {
		t.Fatal("pair code reused")
	}
	// 设备列表 + 吊销。
	devices, err := svc.Devices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices: %v %v", devices, err)
	}
	if devices[0].RevokedAt != nil {
		t.Fatal("fresh device already revoked")
	}
	if err := svc.Revoke(ctx, devices[0].DeviceID); err != nil {
		t.Fatal(err)
	}
	devices, _ = svc.Devices(ctx)
	if devices[0].RevokedAt == nil {
		t.Fatal("revoke not persisted")
	}
	// 禁用后状态翻转。
	if err := svc.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	status, _ = svc.Status(ctx)
	if status.Enabled {
		t.Fatal("still enabled after disable")
	}
}

// TestHandleApk 钉住 Android 壳安装包分发路由：仅 GET/HEAD 放行、仅精确
// /app/lunitide.apk、Content-Type 为 APK MIME、无包/路径穿越一律 404。
// 顺带断言配对结果携带指纹与候选地址（壳 APP 直连所需）。
func TestHandleApk(t *testing.T) {
	ctx := context.Background()
	root := newTestRoot(t)
	svc, err := New(ctx, root, &fakeHandler{}, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	svc.port = 0
	if err := svc.Enable(ctx); err != nil {
		t.Fatal(err)
	}

	// 布局：<base>\web\dist 为 rendererDirOverride，APK 在其兄弟 app\ 下。
	base := t.TempDir()
	renderer := filepath.Join(base, "web", "dist")
	if err := os.MkdirAll(renderer, 0o755); err != nil {
		t.Fatal(err)
	}
	svc.rendererDirOverride = renderer

	do := func(method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		rec := httptest.NewRecorder()
		svc.handleApk(rec, req)
		return rec
	}

	// 包未布置 → 404（配对页 apkAvailable 探测落空，走浏览器引导分支）。
	if rec := do(http.MethodGet, "/app/lunitide.apk"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing apk status = %d, want 404", rec.Code)
	}

	payload := []byte("PK\x03\x04 fake android package")
	if err := os.MkdirAll(filepath.Join(base, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "app", "lunitide.apk"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	// GET：200 + APK MIME + 逐字节一致。
	rec := do(http.MethodGet, "/app/lunitide.apk")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Fatalf("content-type = %q", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Fatalf("apk body mismatch: %d bytes", rec.Body.Len())
	}
	// HEAD：配对页可用性探测依赖。
	if rec := do(http.MethodHead, "/app/lunitide.apk"); rec.Code != http.StatusOK {
		t.Fatalf("head status = %d", rec.Code)
	}
	// 其他方法拒绝。
	if rec := do(http.MethodPost, "/app/lunitide.apk"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post status = %d, want 405", rec.Code)
	}
	// 仅精确路径放行（含路径穿越规整后不匹配）。
	for _, target := range []string{"/app/other.apk", "/app/lunitide.apk/x", "/app/../lunitide.apk", "/app/", "/app"} {
		if rec := do(http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Fatalf("target %q status = %d, want 404", target, rec.Code)
		}
	}

	// 配对结果必须带指纹（确定性非空）；候选地址存在时须为合法 IP。
	info, err := svc.IssuePairCode(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.HandlePair(ctx, info.Code, "我的手机", "android-shell", "192.0.2.11")
	if err != nil {
		t.Fatal(err)
	}
	if result.Fingerprint == "" {
		t.Fatal("pair result missing fingerprint")
	}
	for _, a := range result.Addresses {
		if net.ParseIP(a) == nil {
			t.Fatalf("pair result address %q is not an IP", a)
		}
	}
}

// dialer 按指纹锁定证书（手机端语义）：VerifyPeerCertificate 比对期望指纹。
func pinnedDialer(t *testing.T, expectFP string) *websocket.Dialer {
	t.Helper()
	return &websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if len(rawCerts) == 0 {
					return fmt.Errorf("no peer certificate")
				}
				sum := sha256.Sum256(rawCerts[0])
				if hex.EncodeToString(sum[:]) != expectFP {
					return fmt.Errorf("certificate fingerprint mismatch")
				}
				return nil
			},
		},
	}
}

func TestBridgeWSSSession(t *testing.T) {
	ctx := context.Background()
	handler := &fakeHandler{}
	root := newTestRoot(t)
	svc, err := New(ctx, root, handler, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	svc.port = 0
	if err := svc.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	addr := svc.listenerAddr()
	if addr == "" {
		t.Fatal("no listener")
	}
	info, err := svc.IssuePairCode(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.HandlePair(ctx, info.Code, "测试手机", "android-pwa", "192.0.2.20")
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	wsURL := fmt.Sprintf("wss://%s/bridge", net.JoinHostPort(host, portOf(addr)))

	// 1. 错误令牌 → 401。
	badHeader := http.Header{"Authorization": []string{"Bearer deadbeef"}}
	if _, resp, err := pinnedDialer(t, fullFP(t, root)).Dial(wsURL, badHeader); err == nil {
		t.Fatal("bad token accepted")
	} else if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d", resp.StatusCode)
	}

	// 2. 错误指纹 → 握手拒绝（M0 验收：错误指纹被拒）。
	wrongFPDialer := &websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func([][]byte, [][]*x509.Certificate) error {
				return fmt.Errorf("fingerprint mismatch")
			},
		},
	}
	goodHeader := http.Header{"Authorization": []string{"Bearer " + result.DeviceToken}}
	if _, _, err := wrongFPDialer.Dial(wsURL, goodHeader); err == nil {
		t.Fatal("wrong fingerprint accepted")
	}

	// 3. 正确指纹 + 正确令牌 → WSS 会话，system.health 一去一回。
	conn, _, err := pinnedDialer(t, fullFP(t, root)).Dial(wsURL, goodHeader)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	request := newRequest(t, "system.health", map[string]any{})
	raw, _ := json.Marshal(request)
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatal(err)
	}
	var response bridge.Response
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.RequestID != request.ID {
		t.Fatalf("unexpected response: %+v", response)
	}
	if handler.lastMethod != "system.health" {
		t.Fatalf("handler saw %q", handler.lastMethod)
	}

	// 4. scope 拒绝：settings 类方法 → REMOTE_SCOPE_DENIED。
	settingsRequest := newRequest(t, "settings.get", map[string]any{})
	raw, _ = json.Marshal(settingsRequest)
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatal(err)
	}
	var denied bridge.Response
	if err := conn.ReadJSON(&denied); err != nil {
		t.Fatal(err)
	}
	if denied.OK || denied.Error == nil || denied.Error.Code != "REMOTE_SCOPE_DENIED" {
		t.Fatalf("scope denial mismatch: %+v", denied)
	}
	conn.Close()

	// 5. 浏览器语义：?token= 查询参数鉴权（PWA 无法发送 Authorization 头）。
	queryURL := fmt.Sprintf("wss://%s/bridge?token=%s", net.JoinHostPort(host, portOf(addr)), result.DeviceToken)
	qconn, _, err := pinnedDialer(t, fullFP(t, root)).Dial(queryURL, nil)
	if err != nil {
		t.Fatalf("query-token dial: %v", err)
	}
	defer qconn.Close()
	healthRequest := newRequest(t, "system.health", map[string]any{})
	raw, _ = json.Marshal(healthRequest)
	if err := qconn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatal(err)
	}
	var healthResponse bridge.Response
	if err := qconn.ReadJSON(&healthResponse); err != nil {
		t.Fatal(err)
	}
	if !healthResponse.OK || healthResponse.RequestID != healthRequest.ID {
		t.Fatalf("query-token response mismatch: %+v", healthResponse)
	}
}

// TestRemoteSessionsAndRevokeDisconnect 覆盖 M4 验收：在线会话登记、
// 审计可查、吊销后既有连接立即断开。
func TestRemoteSessionsAndRevokeDisconnect(t *testing.T) {
	ctx := context.Background()
	handler := &fakeHandler{}
	root := newTestRoot(t)
	svc, err := New(ctx, root, handler, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	svc.port = 0
	if err := svc.Enable(ctx); err != nil {
		t.Fatal(err)
	}
	addr := svc.listenerAddr()
	info, err := svc.IssuePairCode(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.HandlePair(ctx, info.Code, "测试手机", "ios-pwa", "192.0.2.30")
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	wsURL := fmt.Sprintf("wss://%s/bridge", net.JoinHostPort(host, portOf(addr)))
	conn, _, err := pinnedDialer(t, fullFP(t, root)).Dial(wsURL, http.Header{"Authorization": []string{"Bearer " + result.DeviceToken}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 在线会话登记 + 审计可见（允许事件异步落库的短暂竞态）。
	deadline := time.Now().Add(5 * time.Second)
	var summary SessionsSummary
	for {
		summary, err = svc.Sessions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(summary.Sessions) == 1 && len(summary.Audit) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sessions/audit not visible: %+v", summary)
		}
		time.Sleep(20 * time.Millisecond)
	}
	deviceID := summary.Sessions[0].DeviceID
	if summary.Sessions[0].DeviceName != "测试手机" || summary.Sessions[0].IP == "" {
		t.Fatalf("active session shape: %+v", summary.Sessions[0])
	}
	foundPair := false
	for _, entry := range summary.Audit {
		if entry.Kind == "pair" && entry.DeviceID == deviceID {
			foundPair = true
		}
	}
	if !foundPair {
		t.Fatalf("pair audit missing: %+v", summary.Audit)
	}

	// 吊销 → 既有连接立即断开（M4 验收 ≤30s，这里同步断）。
	if err := svc.Revoke(ctx, deviceID); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var response bridge.Response
	if err := conn.ReadJSON(&response); err == nil {
		t.Fatalf("revoked connection still readable: %+v", response)
	}
	// 新连接 401。
	if _, resp, err := pinnedDialer(t, fullFP(t, root)).Dial(wsURL, http.Header{"Authorization": []string{"Bearer " + result.DeviceToken}}); err == nil {
		t.Fatal("revoked token accepted")
	} else if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d", resp.StatusCode)
	}
}

func TestKeepAwakePersistAndRestore(t *testing.T) {
	ctx := context.Background()
	root := newTestRoot(t)
	svc, err := New(ctx, root, &fakeHandler{}, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetKeepAwake(ctx, true); err != nil {
		t.Fatal(err)
	}
	svc.Close()
	// 重新打开同一数据根：keepAwake 恢复为 true。
	restored, err := New(ctx, root, &fakeHandler{}, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	status, _ := restored.Status(ctx)
	if !status.KeepAwake {
		t.Fatal("keepAwake not restored across restart")
	}
}

func fullFP(t *testing.T, root fakeRoot) string {
	t.Helper()
	certPath, err := root.FilePath("remote-cert.pem")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert file: %v", err)
	}
	cert, err := tls.LoadX509KeyPair(certPath, mustPath(t, root, "remote-tls-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	fp, err := certificateFingerprint(cert)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func mustPath(t *testing.T, root fakeRoot, name string) string {
	t.Helper()
	path, err := root.FilePath(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "47651"
	}
	return port
}

// TestHandleStaticPairFallback 在 Windows 上复现 v0.16.0/0.16.1 的配对页
// 回退 bug：handleStatic 曾用 filepath.Clean 清洗 URL 路径，Windows 下
// "/pair" 变成 "\pair"，== "/pair" 永假，配对页全部落到 index.html
// （手机扫码因此看不到配对界面）。该断言在 Windows 本机必须通过。
func TestHandleStaticPairFallback(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, newTestRoot(t), &fakeHandler{}, "0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	// 最小 dist：两页内容可区分，用于断言回退目标。
	dist := t.TempDir()
	indexHTML := `<!doctype html><html lang="en"><title>app-index</title></html>`
	pairHTML := `<!doctype html><html lang="zh-CN"><title>pair-page</title></html>`
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "pair.html"), []byte(pairHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.rendererDirOverride = dist

	get := func(target string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		svc.handleStatic(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	// /pair（含尾斜杠变体）必须回退到配对页。
	for _, target := range []string{"/pair", "/pair/"} {
		if body := get(target).Body.String(); !strings.Contains(body, "pair-page") {
			t.Fatalf("GET %s fell back to index.html, want pair.html (Windows path-clean regression)", target)
		}
	}
	// 其余 SPA 路由回 index.html，且带 no-cache。
	for _, target := range []string{"/", "/anything"} {
		rec := get(target)
		if body := rec.Body.String(); !strings.Contains(body, "app-index") {
			t.Fatalf("GET %s returned pair/other page, want index.html", target)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("GET %s cache-control = %q, want no-cache", target, got)
		}
	}
	// /assets/ 长缓存头在 Windows 上同样曾被 filepath.Clean 破坏。
	assetsDir := filepath.Join(dist, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "main-abc123.js"), []byte("// js"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := get("/assets/main-abc123.js").Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("assets cache-control = %q, want immutable long cache", got)
	}
	// 直达 pair.html 文件本身也必须正常。
	if body := get("/pair.html").Body.String(); !strings.Contains(body, "pair-page") {
		t.Fatal("GET /pair.html did not serve pair.html")
	}
}
