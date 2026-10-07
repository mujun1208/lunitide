package remotegateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/oklog/ulid/v2"
)

const (
	// DefaultPort 是远程网关固定监听端口（PRD §4.1）。
	DefaultPort = 47651
	// PairCodeTTL 是一次性配对码有效期。
	PairCodeTTL = 5 * time.Minute
	// DeviceTokenTTL 与 TRAE 对齐：180 天授权。
	DeviceTokenTTL = 180 * 24 * time.Hour
	// pairMaxFailures / pairLockout 控制配对码爆破锁定。
	pairMaxFailures = 5
	pairLockout     = 15 * time.Minute
)

// ErrDisabled 表示远程访问未开启，配对/连接一律拒绝。
var ErrDisabled = errors.New("remotegateway: remote access is disabled")

// Service 是手机伴侣远程网关的引擎侧宿主。零依赖注入：数据根、bridge
// handler（引擎本身）、产品版本。Enable 后在同一进程内提供 HTTPS+WSS。
type Service struct {
	root    SecureRoot
	handler ipc.Handler
	version string
	store   *Store

	mu        sync.Mutex
	enabled   bool
	keepAwake bool
	port      int
	server    *http.Server
	listener  net.Listener
	certFP    string

	// rendererDirOverride 覆盖渲染目录（同包测试注入；空则按可执行
	// 文件位置解析 web/dist）。
	rendererDirOverride string

	keepAwakeChan chan bool
	keepAwakeOnce sync.Once

	// activeConns 是在线连接注册表（M4）：Revoke 时按设备立即断开既有
	// 连接（PRD 验收：吊销后 30 秒内断开——这里直接断），remote.sessions.list
	// 展示在线会话。key 是 deviceID。
	activeMu    sync.Mutex
	activeConns map[string][]*activeConn

	// pairFailures 是来源 IP 维度的配对失败锁定（内存态，重启即清零；
	// 配对码本身 5 分钟过期且单次使用，重启清零可接受）。
	pairMu       sync.Mutex
	pairFailures map[string]*pairLockState
}

// ActiveSession 是一条在线远程连接（remote.sessions.list 展示项）。
type ActiveSession struct {
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	IP         string    `json:"ip,omitempty"`
	Since      time.Time `json:"since"`
}

type activeConn struct {
	ip    string
	since time.Time
	close func()
}

type pairLockState struct {
	failures    int
	lockedUntil time.Time
}

// New 构造服务并恢复持久化开关状态。不启动监听——由 StartIfEnabled
// 在引擎启动收尾时调用（远程能力永远不阻塞引擎就绪）。
func New(ctx context.Context, root SecureRoot, handler ipc.Handler, version string) (*Service, error) {
	store, err := OpenStore(ctx, root)
	if err != nil {
		return nil, err
	}
	enabled, err := store.ConfigGet(ctx, "enabled")
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	keepAwake, err := store.ConfigGet(ctx, "keepAwake")
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	s := &Service{
		root:          root,
		handler:       handler,
		version:       version,
		store:         store,
		enabled:       enabled,
		keepAwake:     keepAwake,
		port:          DefaultPort,
		keepAwakeChan: make(chan bool, 4),
		activeConns:   map[string][]*activeConn{},
		pairFailures:  map[string]*pairLockState{},
	}
	go s.keepAwakeLoop()
	s.keepAwakeChan <- keepAwake
	return s, nil
}

// StartIfEnabled 在引擎启动时恢复监听（上次会话开着远程访问就自动回来）。
func (s *Service) StartIfEnabled(ctx context.Context) {
	s.mu.Lock()
	want := s.enabled
	s.mu.Unlock()
	if !want {
		return
	}
	if err := s.startLockedServer(ctx, false); err != nil {
		log.Printf("remotegateway: autostart failed: %v", err)
	}
}

// Close 停止监听、解除防休眠并关闭存储。
func (s *Service) Close() {
	s.mu.Lock()
	server := s.server
	s.server = nil
	listener := s.listener
	s.listener = nil
	s.enabled = false
	s.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
	s.keepAwakeOnce.Do(func() {
		select {
		case s.keepAwakeChan <- false:
		default:
		}
		close(s.keepAwakeChan)
	})
	_ = s.store.Close()
}

// Enable 开启远程访问：确保证书、起 HTTPS 监听、持久化开关。
func (s *Service) Enable(ctx context.Context) error {
	s.mu.Lock()
	if s.server != nil {
		s.enabled = true
		s.mu.Unlock()
		return s.store.ConfigSet(ctx, "enabled", true)
	}
	s.mu.Unlock()
	if err := s.startLockedServer(ctx, true); err != nil {
		return err
	}
	return s.store.ConfigSet(ctx, "enabled", true)
}

// Disable 关闭远程访问：断开全部连接并持久化开关。
func (s *Service) Disable(ctx context.Context) error {
	s.mu.Lock()
	server := s.server
	s.server = nil
	listener := s.listener
	s.listener = nil
	s.enabled = false
	s.mu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
	// 关闭路径静默删除防火墙规则：删除同样需要管理员权限，但不弹 UAC
	// （监听已停，残留规则无放行目标；下次 Enable 会重建）。
	go func() {
		if err := removeFirewallRule(s.port); err != nil {
			log.Printf("remotegateway: firewall rule remove: %v", err)
		}
	}()
	return s.store.ConfigSet(ctx, "enabled", false)
}

// startLockedServer 起监听；allowElevate 决定防火墙规则缺失败时是否允许
// 弹 UAC 请求一次性管理员授权（仅用户显式 Enable 的路径为 true）。
func (s *Service) startLockedServer(ctx context.Context, allowElevate bool) error {
	cert, fp, err := ensureCertificate(s.root)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("remotegateway: listen %d: %w", DefaultPort, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/info", s.handleInfo)
	mux.HandleFunc("/api/pair", s.handlePair)
	mux.HandleFunc("/bridge", s.serveBridgeWS)
	mux.HandleFunc("/", s.handleStatic)
	server := &http.Server{
		Handler:           mux,
		TLSConfig:         tlsConfig(cert),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("remotegateway: serve: %v", err)
		}
	}()
	s.mu.Lock()
	s.server = server
	s.listener = listener
	s.enabled = true
	s.certFP = fp
	port := s.port
	s.mu.Unlock()
	// 防火墙规则异步确保：netsh 慢或失败都不阻塞网关启动（规则已存在时
	// 幂等跳过；失败只记日志，局域网直连可能仍可用）。
	go func() {
		if err := ensureFirewallRule(port, allowElevate); err != nil {
			log.Printf("remotegateway: firewall rule: %v", err)
		}
	}()
	return nil
}

// StatusSummary 是 remote.access.status 的响应负载。
type StatusSummary struct {
	Enabled          bool   `json:"enabled"`
	Port             int    `json:"port"`
	Addresses        []string `json:"addresses"`
	CertFingerprint  string `json:"certFingerprint,omitempty"`
	KeepAwake        bool   `json:"keepAwake"`
	ActiveDevices    int    `json:"activeDevices"`
}

// listenerAddr 暴露实际监听地址（测试拨号用；生产即 :47651）。
func (s *Service) listenerAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Status 汇总当前状态（bridged：桌面设置页展示用）。
func (s *Service) Status(ctx context.Context) (StatusSummary, error) {
	s.mu.Lock()
	enabled := s.enabled && s.server != nil
	fp := s.certFP
	keepAwake := s.keepAwake
	port := s.port
	s.mu.Unlock()
	active := 0
	if devices, err := s.store.DeviceList(ctx); err == nil {
		now := time.Now()
		for _, d := range devices {
			if d.RevokedAt == nil && now.Before(d.ExpiresAt) {
				active++
			}
		}
	}
	return StatusSummary{
		Enabled:         enabled,
		Port:            port,
		Addresses:       hostAddresses(),
		CertFingerprint: fp,
		KeepAwake:       keepAwake,
		ActiveDevices:   active,
	}, nil
}

// PairCodeInfo 是 remote.pair.code 的响应负载：码、配对 URL、QR PNG。
type PairCodeInfo struct {
	Code         string    `json:"code"`
	URL          string    `json:"url"`
	ExpiresAt    time.Time `json:"expiresAt"`
	QRPngBase64  string    `json:"qrPngBase64"`
	Addresses    []string  `json:"addresses"`
	Fingerprint  string    `json:"fingerprint"`
}

// IssuePairCode 签发一次性配对码并生成二维码内容。仅在远程访问开启时
// 可用；二维码 URL 携带首选地址 + 码 + 证书指纹（16 位短指纹）+ 桌面当前
// 语言（lang=zh-CN/en，配对页据此把语言写入手机 localStorage）。
func (s *Service) IssuePairCode(ctx context.Context, lang string) (PairCodeInfo, error) {
	s.mu.Lock()
	enabled := s.enabled && s.server != nil
	fp := s.certFP
	s.mu.Unlock()
	if !enabled {
		return PairCodeInfo{}, ErrDisabled
	}
	code, err := randomPairCode()
	if err != nil {
		return PairCodeInfo{}, err
	}
	now := time.Now()
	expires := now.Add(PairCodeTTL)
	if err := s.store.PairingCodeSave(ctx, hashToken(code), now, expires); err != nil {
		return PairCodeInfo{}, err
	}
	addresses := hostAddresses()
	host := "127.0.0.1"
	if len(addresses) > 0 {
		host = addresses[0]
	}
	url := fmt.Sprintf("https://%s/pair#c=%s&fp=%s", joinHostPort(host), code, fmtFingerprint(fp))
	if lang == "zh-CN" || lang == "en" {
		url += "&lang=" + lang
	}
	png, err := qrcode.Encode(url, qrcode.Medium, 512)
	if err != nil {
		return PairCodeInfo{}, err
	}
	return PairCodeInfo{
		Code:        code,
		URL:         url,
		ExpiresAt:   expires,
		QRPngBase64: base64.StdEncoding.EncodeToString(png),
		Addresses:   addresses,
		Fingerprint: fmtFingerprint(fp),
	}, nil
}

// PairResult 是配对成功返回给手机的信息。
type PairResult struct {
	DeviceToken string    `json:"deviceToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Product     string    `json:"product"`
	Version     string    `json:"version"`
}

// HandlePair 消费配对码并签发设备令牌。错误路径全部走 IP 锁定计数。
func (s *Service) HandlePair(ctx context.Context, code, deviceName, platform, ip string) (PairResult, error) {
	s.mu.Lock()
	enabled := s.enabled && s.server != nil
	s.mu.Unlock()
	if !enabled {
		return PairResult{}, ErrDisabled
	}
	if s.pairLocked(ip) {
		_ = s.store.AuditAppend(ctx, "anonymous", "pair-locked", "", ip, time.Now())
		return PairResult{}, errors.New("配对失败次数过多，已临时锁定")
	}
	now := time.Now()
	ok, err := s.store.PairingCodeConsume(ctx, hashToken(code), now)
	if err != nil {
		return PairResult{}, err
	}
	if !ok {
		s.recordPairFailure(ip)
		_ = s.store.AuditAppend(ctx, "anonymous", "pair-fail", "", ip, time.Now())
		return PairResult{}, errors.New("配对码无效或已过期")
	}
	token, err := randomToken()
	if err != nil {
		return PairResult{}, err
	}
	deviceID := ulid.Make().String()
	device := Device{
		DeviceID:  deviceID,
		Name:      sanitizeDeviceName(deviceName),
		Platform:  sanitizePlatform(platform),
		TokenHash: hashToken(token),
		Scopes:    DefaultScopes,
		GrantedAt: now,
		ExpiresAt: now.Add(DeviceTokenTTL),
		LastIP:    ip,
	}
	if err := s.store.DeviceSave(ctx, device); err != nil {
		return PairResult{}, err
	}
	_ = s.store.AuditAppend(ctx, deviceID, "pair", "", device.Name+" / "+device.Platform, now)
	s.pairMu.Lock()
	delete(s.pairFailures, ip)
	s.pairMu.Unlock()
	return PairResult{
		DeviceToken: token,
		ExpiresAt:   device.ExpiresAt,
		Product:     "Lunitide",
		Version:     s.version,
	}, nil
}

// Devices 列出全部已配对设备（含已吊销，桌面管理页展示用）。
func (s *Service) Devices(ctx context.Context) ([]Device, error) {
	return s.store.DeviceList(ctx)
}

// Revoke 吊销设备令牌：立即断开该设备全部在线连接（M4 验收），新连接 401。
func (s *Service) Revoke(ctx context.Context, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return errors.New("deviceId is required")
	}
	if err := s.store.DeviceRevoke(ctx, deviceID, time.Now()); err != nil {
		return err
	}
	s.disconnectDevice(deviceID)
	_ = s.store.AuditAppend(ctx, deviceID, "revoke", "", "", time.Now())
	return nil
}

// registerActiveSession 登记一条在线连接并返回注销函数。closeConn 由
// WS 会话提供（conn.Close），Revoke 时用于立即断开。
func (s *Service) registerActiveSession(device *Device, ip string, closeConn func()) func() {
	entry := &activeConn{ip: ip, since: time.Now(), close: closeConn}
	s.activeMu.Lock()
	s.activeConns[device.DeviceID] = append(s.activeConns[device.DeviceID], entry)
	s.activeMu.Unlock()
	return func() {
		s.activeMu.Lock()
		conns := s.activeConns[device.DeviceID]
		for i, c := range conns {
			if c == entry {
				s.activeConns[device.DeviceID] = append(conns[:i], conns[i+1:]...)
				break
			}
		}
		if len(s.activeConns[device.DeviceID]) == 0 {
			delete(s.activeConns, device.DeviceID)
		}
		s.activeMu.Unlock()
	}
}

// disconnectDevice 关闭该设备全部在线连接（Revoke 路径）。
func (s *Service) disconnectDevice(deviceID string) {
	s.activeMu.Lock()
	conns := s.activeConns[deviceID]
	delete(s.activeConns, deviceID)
	s.activeMu.Unlock()
	for _, c := range conns {
		c.close()
	}
}

// ActiveSessions 返回当前在线会话快照。
func (s *Service) ActiveSessions() []ActiveSession {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	sessions := []ActiveSession{}
	for deviceID, conns := range s.activeConns {
		for _, c := range conns {
			sessions = append(sessions, ActiveSession{DeviceID: deviceID, IP: c.ip, Since: c.since})
		}
	}
	return sessions
}

// SessionsSummary 是 remote.sessions.list 的响应负载（PRD §4.3）：
// 当前在线远程会话与审计摘要。
type SessionsSummary struct {
	Sessions []ActiveSession `json:"sessions"`
	Audit    []AuditEntry    `json:"audit"`
}

// Sessions 汇总在线会话与最近审计（桌面管理页展示）。
func (s *Service) Sessions(ctx context.Context) (SessionsSummary, error) {
	sessions := s.ActiveSessions()
	if len(sessions) > 0 {
		devices, err := s.store.DeviceList(ctx)
		if err == nil {
			names := map[string]string{}
			for _, d := range devices {
				names[d.DeviceID] = d.Name
			}
			for i := range sessions {
				sessions[i].DeviceName = names[sessions[i].DeviceID]
			}
		}
	}
	audit, err := s.store.AuditRecent(ctx, 100)
	if err != nil {
		return SessionsSummary{}, err
	}
	return SessionsSummary{Sessions: sessions, Audit: audit}, nil
}

// SetKeepAwake 切换防休眠并持久化。
func (s *Service) SetKeepAwake(ctx context.Context, on bool) error {
	if err := s.store.ConfigSet(ctx, "keepAwake", on); err != nil {
		return err
	}
	s.mu.Lock()
	s.keepAwake = on
	s.mu.Unlock()
	select {
	case s.keepAwakeChan <- on:
	default:
	}
	return nil
}

func (s *Service) keepAwakeLoop() {
	// SetThreadExecutionState 的 ES_CONTINUOUS 状态粘在调用线程上，
	// 清除必须发生在同一线程：钉住本 goroutine 的 OS 线程。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for on := range s.keepAwakeChan {
		if err := setKeepAwake(on); err != nil {
			log.Printf("remotegateway: keepAwake=%v failed: %v", on, err)
		}
	}
}

func (s *Service) pairLocked(ip string) bool {
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	state, ok := s.pairFailures[ip]
	if !ok {
		return false
	}
	return time.Now().Before(state.lockedUntil)
}

func (s *Service) recordPairFailure(ip string) {
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	state, ok := s.pairFailures[ip]
	if !ok {
		state = &pairLockState{}
		s.pairFailures[ip] = state
	}
	state.failures++
	if state.failures >= pairMaxFailures {
		state.lockedUntil = time.Now().Add(pairLockout)
		state.failures = 0
	}
}

// auditQuiet 记审计不阻塞调用方（写路径上的鉴权失败/越权尝试）。
func (s *Service) auditQuiet(deviceID, kind, method, detail string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.store.AuditAppend(ctx, deviceID, kind, method, detail, time.Now())
	}()
}

func (s *Service) auditMethod(deviceID, method string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.store.AuditAppend(ctx, deviceID, "method", method, "", time.Now())
	}()
}

func (s *Service) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"product": "Lunitide", "version": s.version})
}

func (s *Service) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"deviceName"`
		Platform   string `json:"platform"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := s.HandlePair(ctx, body.Code, body.DeviceName, body.Platform, remoteIP(r))
	if err != nil {
		if errors.Is(err, ErrDisabled) {
			http.Error(w, "remote access disabled", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// handleStatic 服务 web 构建产物（手机 PWA 即同一份前端）。SPA 回退：
// /pair 落到配对落地页 pair.html，其余路由回 index.html；带哈希的
// assets 静态资源允许长缓存。
func (s *Service) handleStatic(w http.ResponseWriter, r *http.Request) {
	dir := s.rendererDirOverride
	if dir == "" {
		dir = rendererDir()
	}
	if dir == "" {
		http.NotFound(w, r)
		return
	}
	// path.Clean 是 URL 路径语义（永远 '/' 分隔）；此处若用
	// filepath.Clean，Windows 会把 "/pair" 清洗成 "\pair"，导致下面的
	// == "/pair" 与 HasPrefix("/assets/") 判断在 Windows 上永假——
	// v0.16.0/0.16.1 的配对页回退因此全部落到 index.html。
	clean := path.Clean("/" + r.URL.Path)
	full := filepath.Join(dir, filepath.FromSlash(clean))
	if st, err := os.Stat(full); err == nil && !st.IsDir() {
		if strings.HasPrefix(clean, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, full)
		return
	}
	fallback := "index.html"
	if clean == "/pair" || strings.HasPrefix(clean, "/pair/") {
		fallback = "pair.html"
	}
	page := filepath.Join(dir, fallback)
	if _, err := os.Stat(page); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, page)
}

func rendererDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "web", "dist")
}

func joinHostPort(host string) string {
	if strings.Contains(host, ":") {
		return net.JoinHostPort(host, fmt.Sprint(DefaultPort))
	}
	return net.JoinHostPort(host, fmt.Sprint(DefaultPort))
}
