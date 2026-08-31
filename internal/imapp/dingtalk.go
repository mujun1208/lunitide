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
	DingTalkOpenAPI         = "https://api.dingtalk.com"
	DingTalkConnectionPath  = "/v1.0/gateway/connections/open"
	DingTalkCallbackTopic   = "/v1.0/im/bot/messages/get"
	DingTalkStreamUserAgent = "lunitide/gateway-48"
)

type DingTalkEndpoint struct {
	URL    string
	Ticket string
}

type DingTalkInboundMessage struct {
	Sender         string
	Text           string
	MessageID      string
	ConversationID string
}

type dingTalkConnectionResp struct {
	Endpoint string `json:"endpoint"`
	Ticket   string `json:"ticket"`
}

func FetchDingTalkEndpoint(ctx context.Context, client *http.Client, domain, appKey, appSecret string) (DingTalkEndpoint, error) {
	domain = strings.TrimRight(strings.TrimSpace(domain), "/")
	appKey = strings.TrimSpace(appKey)
	appSecret = strings.TrimSpace(appSecret)
	if domain == "" || appKey == "" || appSecret == "" {
		return DingTalkEndpoint{}, fmt.Errorf("imapp: dingtalk app credentials missing")
	}
	u, err := url.Parse(domain + DingTalkConnectionPath)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return DingTalkEndpoint{}, fmt.Errorf("imapp: dingtalk endpoint url invalid")
	}
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]any{
		"clientId":     appKey,
		"clientSecret": appSecret,
		"ua":           DingTalkStreamUserAgent,
		"subscriptions": []map[string]string{
			{"type": "CALLBACK", "topic": DingTalkCallbackTopic},
		},
	})
	if err != nil {
		return DingTalkEndpoint{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return DingTalkEndpoint{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return DingTalkEndpoint{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return DingTalkEndpoint{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return DingTalkEndpoint{}, fmt.Errorf("imapp: dingtalk endpoint http %d", resp.StatusCode)
	}
	var parsed dingTalkConnectionResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return DingTalkEndpoint{}, err
	}
	if strings.TrimSpace(parsed.Endpoint) == "" || strings.TrimSpace(parsed.Ticket) == "" {
		return DingTalkEndpoint{}, fmt.Errorf("imapp: dingtalk endpoint: empty websocket url")
	}
	ws, err := url.Parse(parsed.Endpoint)
	if err != nil || (ws.Scheme != "wss" && ws.Scheme != "https") {
		return DingTalkEndpoint{}, fmt.Errorf("imapp: dingtalk websocket url invalid")
	}
	return DingTalkEndpoint{URL: parsed.Endpoint, Ticket: parsed.Ticket}, nil
}

func DingTalkStreamURL(ep DingTalkEndpoint) string {
	base := strings.TrimSpace(ep.URL)
	ticket := strings.TrimSpace(ep.Ticket)
	if base == "" || ticket == "" {
		return ""
	}
	if strings.Contains(base, "ticket=") {
		return base
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "ticket=" + url.QueryEscape(ticket)
}

func ParseDingTalkMessageEvent(raw []byte) (DingTalkInboundMessage, bool) {
	payload := extractJSONObject(raw)
	if len(payload) == 0 {
		return DingTalkInboundMessage{}, false
	}
	var env struct {
		Type    string `json:"type"`
		Headers struct {
			Topic     string `json:"topic"`
			MessageID string `json:"messageId"`
		} `json:"headers"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return DingTalkInboundMessage{}, false
	}
	if env.Type != "" && !strings.EqualFold(env.Type, "CALLBACK") {
		return DingTalkInboundMessage{}, false
	}
	if env.Headers.Topic != "" && env.Headers.Topic != DingTalkCallbackTopic {
		return DingTalkInboundMessage{}, false
	}
	data := bytes.TrimSpace(env.Data)
	if len(data) == 0 {
		data = payload
	}
	if len(data) > 0 && data[0] == '"' {
		var inner string
		if json.Unmarshal(data, &inner) == nil {
			data = []byte(inner)
		}
	}
	var body struct {
		MsgID            string `json:"msgId"`
		MsgType          string `json:"msgtype"`
		SenderStaffID    string `json:"senderStaffId"`
		SenderNick       string `json:"senderNick"`
		ConversationID   string `json:"conversationId"`
		ConversationType string `json:"conversationType"`
		Text             struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	if json.Unmarshal(data, &body) != nil {
		return DingTalkInboundMessage{}, false
	}
	if body.MsgType != "" && body.MsgType != "text" {
		return DingTalkInboundMessage{}, false
	}
	text := strings.TrimSpace(body.Text.Content)
	sender := strings.TrimSpace(body.SenderStaffID)
	if sender == "" {
		sender = strings.TrimSpace(body.SenderNick)
	}
	if text == "" || sender == "" || utf8.RuneCountInString(text) > MaxInboundTextRunes {
		return DingTalkInboundMessage{}, false
	}
	messageID := strings.TrimSpace(body.MsgID)
	if messageID == "" {
		messageID = strings.TrimSpace(env.Headers.MessageID)
	}
	return DingTalkInboundMessage{
		Sender:         sender,
		Text:           text,
		MessageID:      messageID,
		ConversationID: strings.TrimSpace(body.ConversationID),
	}, true
}

func DingTalkStreamAck(messageID string) []byte {
	body, _ := json.Marshal(map[string]any{
		"code": 200,
		"headers": map[string]string{
			"messageId":   strings.TrimSpace(messageID),
			"contentType": "application/json",
		},
		"message": "OK",
		"data":    `{"success":true}`,
	})
	return body
}
