package imapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	WeComOpenAPI     = "https://qyapi.weixin.qq.com"
	DingTalkOAPI     = "https://oapi.dingtalk.com"
	maxReplyRunes    = 4000
	defaultReplyHTTP = 1 << 20
)

type ReplyTransport struct {
	HTTP           *http.Client
	FeishuDomain   string
	WeComDomain    string
	DingTalkDomain string
	DingTalkOAPI   string
}

func (t ReplyTransport) client() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return http.DefaultClient
}

func clipReplyText(text string) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= maxReplyRunes {
		return text
	}
	return string([]rune(text)[:maxReplyRunes])
}

func (t ReplyTransport) Send(ctx context.Context, kind Kind, appID, appSecret, sender, conversationID, text string) error {
	text = clipReplyText(text)
	if text == "" {
		return fmt.Errorf("imapp: empty reply")
	}
	switch kind {
	case KindFeishu:
		return t.sendFeishu(ctx, appID, appSecret, sender, text)
	case KindWeCom:
		return t.sendWeCom(ctx, appID, appSecret, sender, text)
	case KindDingTalk:
		return t.sendDingTalk(ctx, appID, appSecret, sender, conversationID, text)
	default:
		return ErrInboundKind
	}
}

func (t ReplyTransport) sendFeishu(ctx context.Context, appID, appSecret, sender, text string) error {
	domain := strings.TrimRight(strings.TrimSpace(t.FeishuDomain), "/")
	if domain == "" {
		domain = FeishuOpenDomain
	}
	token, err := t.postJSON(ctx, domain+"/open-apis/auth/v3/tenant_access_token/internal", "", map[string]string{
		"app_id": appID, "app_secret": appSecret,
	})
	if err != nil {
		return err
	}
	access := strings.TrimSpace(stringFrom(token, "tenant_access_token"))
	if access == "" {
		return fmt.Errorf("imapp: feishu token missing")
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	body, err := t.postJSON(ctx, domain+"/open-apis/im/v1/messages?receive_id_type=open_id", "Bearer "+access, map[string]string{
		"receive_id": sender,
		"msg_type":   "text",
		"content":    string(content),
	})
	if err != nil {
		return err
	}
	if code, ok := body["code"].(float64); ok && code != 0 {
		return fmt.Errorf("imapp: feishu reply: %v", body["msg"])
	}
	return nil
}

func (t ReplyTransport) sendWeCom(ctx context.Context, corpID, secret, sender, text string) error {
	_ = ctx
	_ = corpID
	_ = secret
	_ = sender
	_ = text
	// Inbound WeCom is the AI-bot websocket (aibot_send_msg). The old
	// cgi-bin/message/send + agentid=1 path cannot reach that bot.
	return fmt.Errorf("imapp: wecom reply uses inbound aibot stream")
}

func (t ReplyTransport) sendDingTalk(ctx context.Context, appKey, appSecret, sender, conversationID, text string) error {
	oapi := strings.TrimRight(strings.TrimSpace(t.DingTalkOAPI), "/")
	if oapi == "" {
		oapi = DingTalkOAPI
	}
	api := strings.TrimRight(strings.TrimSpace(t.DingTalkDomain), "/")
	if api == "" {
		api = DingTalkOpenAPI
	}
	q := url.Values{"appkey": {appKey}, "appsecret": {appSecret}}
	token, err := t.getJSON(ctx, oapi+"/gettoken?"+q.Encode())
	if err != nil {
		return err
	}
	access := strings.TrimSpace(stringFrom(token, "access_token"))
	if access == "" {
		return fmt.Errorf("imapp: dingtalk token missing")
	}
	param, _ := json.Marshal(map[string]string{"content": text})
	payload := map[string]any{
		"robotCode": appKey,
		"userIds":   []string{sender},
		"msgKey":    "sampleText",
		"msgParam":  string(param),
	}
	if strings.TrimSpace(conversationID) != "" {
		payload["conversationId"] = conversationID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/v1.0/robot/oToMessages/batchSend", bytes.NewReader(mustJSON(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", access)
	resp, err := t.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, defaultReplyHTTP))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("imapp: dingtalk reply http %d", resp.StatusCode)
	}
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) == nil {
		if code, ok := parsed["code"].(string); ok && code != "" && code != "0" {
			return fmt.Errorf("imapp: dingtalk reply: %v", parsed["message"])
		}
	}
	return nil
}

func (t ReplyTransport) postJSON(ctx context.Context, rawURL, auth string, payload any) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(mustJSON(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return t.doJSON(req)
}

func (t ReplyTransport) getJSON(ctx context.Context, rawURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return t.doJSON(req)
}

func (t ReplyTransport) doJSON(req *http.Request) (map[string]any, error) {
	resp, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, defaultReplyHTTP))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("imapp: reply http %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func stringFrom(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
