// P3-1 IM webhook notifier: automation run results fan out to a user-pasted
// custom-bot webhook (Lark/Feishu, WeCom, DingTalk text bots). Payload shape
// is picked from the host so the user only pastes one URL; hosts that are
// not recognized get the generic {"text": ...} body. SSRF guards: https
// only, no IP literals, no localhost names, and a hard 5s timeout so a dead
// endpoint can never stall the scheduler loop.
package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxWebhookURLRunes = 512
	webhookHTTPTimeout = 5 * time.Second
)

// ErrWebhookInvalid answers a webhook URL that fails the SSRF guard.
var ErrWebhookInvalid = errors.New("scheduler: webhook url invalid")

// webhookKind picks the payload family from the URL host.
type webhookKind int

const (
	webhookGeneric webhookKind = iota
	webhookLark
	webhookWeCom
	webhookDingTalk
)

// classifyWebhook maps known IM hosts onto payload kinds.
func classifyWebhook(u *url.URL) webhookKind {
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.HasSuffix(host, "feishu.cn"), strings.HasSuffix(host, "larksuite.com"), strings.HasSuffix(host, "lark.com"):
		return webhookLark
	case strings.HasSuffix(host, "qyapi.weixin.qq.com"):
		return webhookWeCom
	case strings.HasSuffix(host, "dingtalk.com"):
		return webhookDingTalk
	}
	return webhookGeneric
}

// ValidateWebhookURL enforces the production SSRF guard: https scheme, no
// userinfo, no port tricks, no IP literals (v4/v6, public or private) and
// no localhost names. Webhook endpoints from the big IM vendors are all
// https hostnames, so the strict rule costs nothing real.
func ValidateWebhookURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len([]rune(raw)) > maxWebhookURLRunes || strings.ContainsRune(raw, 0) || strings.ContainsRune(raw, '\n') || strings.ContainsRune(raw, '\r') {
		return fmt.Errorf("%w: length/characters", ErrWebhookInvalid)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: parse", ErrWebhookInvalid)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%w: https only", ErrWebhookInvalid)
	}
	if u.User != nil {
		return fmt.Errorf("%w: userinfo forbidden", ErrWebhookInvalid)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: host empty", ErrWebhookInvalid)
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("%w: local name", ErrWebhookInvalid)
	}
	if ip := net.ParseIP(host); ip != nil {
		return fmt.Errorf("%w: ip literal", ErrWebhookInvalid)
	}
	return nil
}

// webhookNotifier POSTs one text message per Notify call.
type webhookNotifier struct {
	endpoint url.URL
	kind     webhookKind
	client   *http.Client
}

// NewWebhookNotifier validates (production SSRF guard) and wires a webhook
// notifier. It answers ErrWebhookInvalid for anything the guard rejects.
func NewWebhookNotifier(rawURL string) (Notifier, error) {
	if err := ValidateWebhookURL(rawURL); err != nil {
		return nil, err
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &webhookNotifier{endpoint: *u, kind: classifyWebhook(u), client: &http.Client{Timeout: webhookHTTPTimeout}}, nil
}

// Notify posts title+body to the IM webhook. It never panics and always
// answers an error instead of blocking callers beyond the client timeout.
func (w *webhookNotifier) Notify(title, body string) error {
	payload := w.buildPayload(title, body)
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), webhookHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.endpoint.String(), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("scheduler: webhook status %d", resp.StatusCode)
	}
	// Vendors answer 200 with an embedded code (0 = ok); a non-zero code is
	// surfaced so the caller can see the bot rejected the message (bad
	// secret, disabled bot, rate limit).
	var probe struct {
		Code int64  `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&probe); err == nil && probe.Code != 0 {
		msg := probe.Msg
		if msg == "" {
			msg = "bot rejected the message"
		}
		return fmt.Errorf("scheduler: webhook code %d: %s", probe.Code, msg)
	}
	return nil
}

// buildPayload shapes the body per vendor family.
func (w *webhookNotifier) buildPayload(title, body string) map[string]any {
	text := title
	if body != "" {
		text = title + "\n" + body
	}
	switch w.kind {
	case webhookLark:
		return map[string]any{"msg_type": "text", "content": map[string]any{"text": text}}
	case webhookWeCom:
		return map[string]any{"msgtype": "text", "text": map[string]any{"content": text}}
	case webhookDingTalk:
		return map[string]any{"msgtype": "text", "text": map[string]any{"content": text}}
	}
	return map[string]any{"text": text}
}
