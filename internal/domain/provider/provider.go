package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// OriginFingerprint binds a credential to its protocol and canonical origin.
// The URL path deliberately does not participate in this security boundary.
func OriginFingerprint(protocol Protocol, rawBaseURL string) (string, error) {
	bound := rawBaseURL
	if protocol == ProtocolVolcSpeech {
		canon, err := CanonicalVolcSpeechURL(rawBaseURL)
		if err != nil {
			return "", err
		}
		bound = canon
	}
	origin, err := NormalizeOrigin(bound)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(string(protocol) + "\x00" + origin))
	return hex.EncodeToString(sum[:]), nil
}

type Protocol string

const (
	ProtocolOpenAICompatible Protocol = "openai_compatible"
	ProtocolAnthropic        Protocol = "anthropic"
	ProtocolVolcSpeech       Protocol = "volc_speech"
)

// VolcSpeechOrigin is the only stored 基础 URL for Agent Plan speech.
// Official wss/http paths are appended by the engine, not typed by the user.
const VolcSpeechOrigin = "https://openspeech.bytedance.com"

// ValidProtocol is the stored-provider enum, including speech-only Volc.
func ValidProtocol(p Protocol) bool {
	return p == ProtocolOpenAICompatible || p == ProtocolAnthropic || p == ProtocolVolcSpeech
}

type CredentialState string

const (
	CredentialConfigured      CredentialState = "configured"
	CredentialMissing         CredentialState = "missing"
	CredentialUnavailable     CredentialState = "unavailable"
	CredentialRequiresReentry CredentialState = "requires_reentry"
)

type Status string

const (
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
)

var (
	ErrNotFound                  = errors.New("provider not found")
	ErrConflict                  = errors.New("provider version conflict")
	ErrCredentialReentryRequired = errors.New("origin or protocol change requires a new credential reference")
)

type Model struct {
	ModelID        string `json:"modelId"`
	DisplayName    string `json:"displayName"`
	IsDefault      bool   `json:"isDefault"`
	ContextWindow  int64  `json:"contextWindow,omitempty"`
	Kind           Kind   `json:"kind,omitempty"`
	SupportsVision bool   `json:"supportsVision,omitempty"`
	KindDefault    bool   `json:"kindDefault,omitempty"`
}

type Provider struct {
	ID              string          `json:"id"`
	LegacyID        string          `json:"legacyId,omitempty"`
	Name            string          `json:"name"`
	Protocol        Protocol        `json:"protocol"`
	BaseURL         string          `json:"baseUrl"`
	Models          []Model         `json:"models"`
	CredentialState       CredentialState `json:"credentialState"`
	CredentialRef         string          `json:"-"`
	CredentialRefBackups  []string        `json:"-"`
	CredentialBackupCount int             `json:"credentialBackupCount,omitempty"`
	Status                Status          `json:"status"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	Version         int64           `json:"version"`
}

type Filter struct {
	Protocol Protocol
}

type Repository interface {
	// Get returns the complete Provider, including CredentialRef, so the owning
	// application service can coordinate secret replacement/deletion. Repository
	// mutations never delete secrets. The application service durably records
	// credential cleanup in its transactional outbox for the Host worker.
	Create(context.Context, Provider) (Provider, error)
	Get(context.Context, string) (Provider, error)
	List(context.Context, Filter) ([]Provider, error)
	Update(context.Context, Provider, int64) (Provider, error)
	Delete(context.Context, string, int64) error
}

// NormalizeBaseURL returns the canonical HTTPS base URL persisted by repositories.
// This is syntactic normalization, not an SSRF defense; it does not resolve or connect.
func NormalizeBaseURL(raw string) (string, error) {
	input := strings.TrimSpace(raw)
	if input == "" || strings.Contains(input, "\\") || !printableASCII(input) {
		return "", errors.New("provider origin is invalid")
	}
	u, err := url.Parse(input)
	if err != nil || (!strings.EqualFold(u.Scheme, "https") && !strings.EqualFold(u.Scheme, "http")) || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("provider origin is invalid")
	}
	hostname := u.Hostname()
	if !validASCIIHostname(hostname) || strings.HasSuffix(hostname, ".") {
		return "", errors.New("provider origin is invalid")
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") || strings.HasSuffix(u.Host, "]:") {
		return "", errors.New("provider origin is invalid")
	}
	if port != "" {
		value, e := strconv.Atoi(port)
		if e != nil || value < 1 || value > 65535 {
			return "", errors.New("provider origin is invalid")
		}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if port == "443" && u.Scheme == "https" {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	if port == "80" && u.Scheme == "http" {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	escaped := path.Clean("/" + strings.TrimPrefix(u.EscapedPath(), "/"))
	if escaped == "/" {
		escaped = ""
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return "", errors.New("provider base URL is invalid")
	}
	u.Path, u.RawPath = decoded, escaped
	if u.EscapedPath() == u.Path {
		u.RawPath = ""
	}
	return u.String(), nil
}

func printableASCII(value string) bool {
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func validASCIIHostname(host string) bool {
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range []byte(label) {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

// CanonicalVolcSpeechURL accepts the official Agent Plan full paths (https/wss)
// and stores only the speech origin. ark.cn-beijing text URLs remap here too.
func CanonicalVolcSpeechURL(raw string) (string, error) {
	input := strings.TrimSpace(raw)
	if input == "" || input == "https://" || input == "http://" {
		return VolcSpeechOrigin, nil
	}
	rewritten := input
	if strings.HasPrefix(strings.ToLower(rewritten), "wss://") {
		rewritten = "https://" + rewritten[6:]
	} else if strings.HasPrefix(strings.ToLower(rewritten), "ws://") {
		rewritten = "http://" + rewritten[5:]
	}
	origin, err := NormalizeOrigin(rewritten)
	if err != nil {
		return "", errors.New("火山语音基础 URL 只接受 openspeech.bytedance.com")
	}
	u, err := url.Parse(origin)
	if err != nil {
		return "", errors.New("火山语音基础 URL 只接受 openspeech.bytedance.com")
	}
	host := strings.ToLower(u.Hostname())
	if host == "openspeech.bytedance.com" || strings.HasSuffix(host, ".volces.com") || host == "volces.com" {
		return VolcSpeechOrigin, nil
	}
	return "", errors.New("火山语音基础 URL 只接受 openspeech.bytedance.com")
}

// NormalizeOrigin returns only the credential security boundary: scheme, host,
// and effective non-default port. Paths never participate in credential binding.
func NormalizeOrigin(raw string) (string, error) {
	base, err := NormalizeBaseURL(raw)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(base)
	u.Path, u.RawPath = "", ""
	return u.String(), nil
}

// ModelIDValid is the single transport/domain policy: printable ASCII only,
// no surrounding whitespace, and the SQLite/schema maximum of 200 bytes.
func ModelIDValid(id string) bool {
	if id == "" || len(id) > 200 || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func (p Provider) Validate() error {
	if p.ID == "" || strings.TrimSpace(p.Name) == "" || len(p.Name) > 500 {
		return errors.New("provider identity or name is invalid")
	}
	if !ValidProtocol(p.Protocol) {
		return errors.New("provider protocol is invalid")
	}
	origin, err := NormalizeBaseURL(p.BaseURL)
	if err != nil || origin != p.BaseURL || len(origin) > 2048 {
		return errors.New("provider base URL is invalid")
	}
	if p.Status != "" && p.Status != StatusEnabled && p.Status != StatusDisabled {
		return errors.New("provider status is invalid")
	}
	if p.CredentialState != "" && p.CredentialState != CredentialConfigured && p.CredentialState != CredentialMissing && p.CredentialState != CredentialUnavailable && p.CredentialState != CredentialRequiresReentry {
		return errors.New("provider credential state is invalid")
	}
	if (p.CredentialRef == "") == (p.CredentialState == CredentialConfigured) {
		return errors.New("credential reference and state are inconsistent")
	}
	if strings.TrimSpace(p.CredentialRef) != p.CredentialRef || len(p.CredentialRef) > 256 {
		return errors.New("credential reference is invalid")
	}
	if err := validateCredentialBackups(p.CredentialRef, p.CredentialRefBackups); err != nil {
		return err
	}
	if len(p.Models) < 1 || len(p.Models) > 50 {
		return errors.New("provider must contain 1 to 50 models")
	}
	seen := make(map[string]struct{}, len(p.Models))
	defaults := 0
	kindDefaults := map[Kind]int{}
	for _, model := range p.Models {
		id := model.ModelID
		if !ModelIDValid(id) {
			return errors.New("model ID is invalid")
		}
		display := strings.TrimSpace(model.DisplayName)
		if display == "" || display != model.DisplayName || len(display) > 200 {
			return errors.New("model display name is invalid")
		}
		if !ValidKind(string(model.Kind)) {
			return errors.New("model kind is invalid")
		}
		if _, exists := seen[id]; exists {
			return errors.New("model IDs must be unique")
		}
		seen[id] = struct{}{}
		if model.IsDefault {
			defaults++
		}
		if model.KindDefault {
			kindDefaults[model.EffectiveKind()]++
			if kindDefaults[model.EffectiveKind()] > 1 {
				return errors.New("each model kind may have at most one catalog default on a provider")
			}
		}
	}
	if defaults != 1 {
		return errors.New("provider must contain exactly one default model")
	}
	if p.Protocol == ProtocolVolcSpeech {
		for _, model := range p.Models {
			if !IsSpeechKind(model.EffectiveKind()) {
				return errors.New("volc speech providers may only contain asr or tts models")
			}
		}
	}
	return nil
}

func validateCredentialBackups(primary string, backups []string) error {
	if len(backups) > 4 {
		return errors.New("at most four backup credentials are allowed")
	}
	seen := map[string]struct{}{}
	for _, ref := range backups {
		if ref == "" || strings.TrimSpace(ref) != ref || len(ref) > 256 {
			return errors.New("backup credential reference is invalid")
		}
		if ref == primary {
			return errors.New("backup credential must differ from the primary")
		}
		if _, ok := seen[ref]; ok {
			return errors.New("backup credential references must be unique")
		}
		seen[ref] = struct{}{}
	}
	return nil
}

// BackupCount is the public DTO count: live refs win, idempotent replay may
// only have the derived count.
func (p Provider) BackupCount() int {
	if n := len(p.CredentialRefBackups); n > 0 {
		return n
	}
	if p.CredentialBackupCount < 0 {
		return 0
	}
	if p.CredentialBackupCount > 4 {
		return 4
	}
	return p.CredentialBackupCount
}

// CredentialRefChain is primary then backups, de-duplicated, max 4 keys.
func (p Provider) CredentialRefChain() []string {
	out := make([]string, 0, 1+len(p.CredentialRefBackups))
	seen := map[string]struct{}{}
	add := func(ref string) {
		if ref == "" {
			return
		}
		if _, ok := seen[ref]; ok {
			return
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	add(p.CredentialRef)
	for _, ref := range p.CredentialRefBackups {
		add(ref)
	}
	return out
}
