package volcsauc

import (
	"strings"
	"time"
	"unicode"
)

const (
	// Name is the backend key the bridge reports. Stable across versions.
	Name = "volc-sauc"

	DefaultHost        = "openspeech.bytedance.com"
	DefaultResourceID  = "volc.seedasr.sauc.duration"
	PathOptimized      = "/api/v3/sauc/bigmodel_async"
	PathBidirectional  = "/api/v3/sauc/bigmodel"
	DefaultEndWindowMS = 400
	DefaultModelName   = "bigmodel"
	connectIDBytes     = 16
	handshakeTimeout   = 1500 * time.Millisecond
	firstDialBudget    = 800 * time.Millisecond
)

// DefaultHotwords are product names the ASR should prefer over near-homophones.
var DefaultHotwords = []string{
	"月汐", "月伴", "Lunitide", "GPT-SoVITS", "WebView2", "BYOK", "sherpa",
}

// Config is everything needed to open one Volc SAUC stream.
type Config struct {
	// BaseURL is the stored provider origin, https://openspeech.bytedance.com.
	// The websocket path is appended here; wss is derived from the host.
	BaseURL string
	// APIKey is X-Api-Key for the new console. Empty when using AppKey+AccessKey.
	APIKey string
	// AppKey is X-Api-App-Key (old console).
	AppKey string
	// AccessKey is X-Api-Access-Key (old console).
	AccessKey string
	// ResourceID selects the 2.0 SKU. Empty becomes DefaultResourceID.
	ResourceID string
	// UID is a stable-enough user tag Volc wants on the full client request.
	UID string
	// EndWindowMS is VAD silence before definite. Floor 200; product default 400.
	EndWindowMS int
	// Dial, when set, replaces the default websocket dialer (tests).
	Dial DialFunc
}

// HandshakeError is a refused upgrade or a SAUC error frame.
type HandshakeError struct {
	Status  int
	Code    int
	Message string
}

func (e *HandshakeError) Error() string {
	if e == nil {
		return "volc sauc handshake failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Status != 0 {
		return "volc sauc handshake status"
	}
	return "volc sauc handshake failed"
}

// ParseCredential splits a stored secret into old-console AppId + token, or
// a new-console API key.
//
// Accepted forms: a bare key; "appId:token" when appId is all digits;
// "appId\\ntoken".
func ParseCredential(secret string) (appKey, token string) {
	s := strings.TrimSpace(secret)
	if s == "" {
		return "", ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	if i := strings.IndexByte(s, ':'); i > 0 {
		head := s[:i]
		if isAllDigits(head) && len(head) >= 6 {
			return head, s[i+1:]
		}
	}
	return "", s
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// ResourceIDFromModel uses the model id when it already is a Volc resource
// id, otherwise the 2.0 duration SKU.
func ResourceIDFromModel(modelID string) string {
	id := strings.TrimSpace(modelID)
	if strings.HasPrefix(id, "volc.seedasr.") || strings.HasPrefix(id, "volc.bigasr.") {
		return id
	}
	return DefaultResourceID
}

// StreamURL is the websocket endpoint. optimized prefers bigmodel_async.
func StreamURL(baseURL string, optimized bool) string {
	host := hostOf(baseURL)
	path := PathOptimized
	if !optimized {
		path = PathBidirectional
	}
	return "wss://" + host + path
}

// AllowedSpeechHost is the only origin the production dialer will open.
// Tests inject Dial and may use httptest hosts.
func AllowedSpeechHost(host string) bool {
	return strings.EqualFold(strings.TrimSpace(host), DefaultHost)
}

func hostOf(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "wss://")
	s = strings.TrimPrefix(s, "ws://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, ":443")
	s = strings.TrimSuffix(s, ":80")
	if s == "" {
		return DefaultHost
	}
	return strings.ToLower(s)
}

func (c Config) resourceID() string {
	if strings.TrimSpace(c.ResourceID) == "" {
		return DefaultResourceID
	}
	return strings.TrimSpace(c.ResourceID)
}

func (c Config) endWindowMS() int {
	if c.EndWindowMS < 200 {
		return DefaultEndWindowMS
	}
	if c.EndWindowMS > 3000 {
		return 3000
	}
	return c.EndWindowMS
}

func (c Config) uid() string {
	if strings.TrimSpace(c.UID) != "" {
		return strings.TrimSpace(c.UID)
	}
	return "lunitide"
}

// ConfigFromSecret maps a stored API secret onto SAUC headers.
func ConfigFromSecret(baseURL, modelID, secret string) Config {
	app, token := ParseCredential(secret)
	cfg := Config{
		BaseURL:    baseURL,
		ResourceID: ResourceIDFromModel(modelID),
	}
	if app != "" {
		cfg.AppKey, cfg.AccessKey = app, token
	} else {
		cfg.APIKey = token
	}
	return cfg
}

// LooksLikeLegacyASRResource is the 1.0 SKU that 403s on 2.0 apps.
func LooksLikeLegacyASRResource(id string) bool {
	return strings.Contains(id, "bigasr")
}

// SanitizeProbeError is the settings-page sentence for a failed handshake.
func SanitizeProbeError(err error) string {
	if err == nil {
		return ""
	}
	var he *HandshakeError
	if asHandshake(err, &he) {
		if he.Status == 403 || he.Code == 403 {
			if LooksLikeLegacyASRResource(he.Message) || strings.Contains(he.Message, "bigasr") {
				return "资源 ID 像是 1.0（volc.bigasr.*）。2.0 应用请用 volc.seedasr.sauc.duration"
			}
			return "火山语音鉴权失败（403）。请核对 API Key 与 Resource-Id（2.0 用 volc.seedasr.sauc.duration）"
		}
		if he.Status == 401 || he.Code == 401 {
			return "火山语音鉴权失败。请核对 API Key"
		}
	}
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "timeout") || strings.Contains(msg, "deadline") {
		return "火山语音连接超时"
	}
	if strings.Contains(msg, "unsupported volc speech host") {
		return "火山语音只连接 openspeech.bytedance.com"
	}
	return "火山语音连接失败"
}

func asHandshake(err error, target **HandshakeError) bool {
	if err == nil {
		return false
	}
	he, ok := err.(*HandshakeError)
	if !ok {
		return false
	}
	*target = he
	return true
}
