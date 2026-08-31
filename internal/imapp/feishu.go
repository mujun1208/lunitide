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
	FeishuOpenDomain   = "https://open.feishu.cn"
	FeishuEndpointPath = "/callback/ws/endpoint"
)

type FeishuEndpoint struct {
	URL string `json:"URL"`
}

type feishuEndpointResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data *FeishuEndpoint `json:"data"`
}

type FeishuInboundMessage struct {
	Sender         string
	Text           string
	MessageID      string
	ConversationID string
}

func FetchFeishuEndpoint(ctx context.Context, client *http.Client, domain, appID, appSecret string) (FeishuEndpoint, error) {
	domain = strings.TrimRight(strings.TrimSpace(domain), "/")
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if domain == "" || appID == "" || appSecret == "" {
		return FeishuEndpoint{}, fmt.Errorf("imapp: feishu app credentials missing")
	}
	u, err := url.Parse(domain + FeishuEndpointPath)
	if err != nil || u.Scheme != "https" {
		return FeishuEndpoint{}, fmt.Errorf("imapp: feishu endpoint url invalid")
	}
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]string{"AppID": appID, "AppSecret": appSecret})
	if err != nil {
		return FeishuEndpoint{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return FeishuEndpoint{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "zh")
	resp, err := client.Do(req)
	if err != nil {
		return FeishuEndpoint{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return FeishuEndpoint{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return FeishuEndpoint{}, fmt.Errorf("imapp: feishu endpoint http %d", resp.StatusCode)
	}
	var parsed feishuEndpointResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return FeishuEndpoint{}, err
	}
	if parsed.Code != 0 || parsed.Data == nil || strings.TrimSpace(parsed.Data.URL) == "" {
		msg := strings.TrimSpace(parsed.Msg)
		if msg == "" {
			msg = "empty websocket url"
		}
		return FeishuEndpoint{}, fmt.Errorf("imapp: feishu endpoint: %s", msg)
	}
	ws, err := url.Parse(parsed.Data.URL)
	if err != nil || (ws.Scheme != "wss" && ws.Scheme != "https") {
		return FeishuEndpoint{}, fmt.Errorf("imapp: feishu websocket url invalid")
	}
	return *parsed.Data, nil
}

func ParseFeishuMessageEvent(raw []byte) (FeishuInboundMessage, bool) {
	payload := extractJSONObject(raw)
	if len(payload) == 0 {
		return FeishuInboundMessage{}, false
	}
	var env struct {
		Header struct {
			EventType string `json:"event_type"`
		} `json:"header"`
		Event struct {
			Sender struct {
				SenderID struct {
					OpenID  string `json:"open_id"`
					UserID  string `json:"user_id"`
					UnionID string `json:"union_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				MessageID   string `json:"message_id"`
				ChatID      string `json:"chat_id"`
				MessageType string `json:"message_type"`
				Content     string `json:"content"`
			} `json:"message"`
		} `json:"event"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return FeishuInboundMessage{}, false
	}
	if env.Header.EventType != "" && env.Header.EventType != "im.message.receive_v1" {
		return FeishuInboundMessage{}, false
	}
	if env.Event.Message.MessageType != "" && env.Event.Message.MessageType != "text" {
		return FeishuInboundMessage{}, false
	}
	text := strings.TrimSpace(env.Event.Message.Content)
	if strings.HasPrefix(text, "{") {
		var body struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(text), &body) == nil {
			text = strings.TrimSpace(body.Text)
		}
	}
	if text == "" || utf8.RuneCountInString(text) > MaxInboundTextRunes {
		return FeishuInboundMessage{}, false
	}
	sender := strings.TrimSpace(env.Event.Sender.SenderID.OpenID)
	if sender == "" {
		sender = strings.TrimSpace(env.Event.Sender.SenderID.UserID)
	}
	if sender == "" {
		sender = strings.TrimSpace(env.Event.Sender.SenderID.UnionID)
	}
	if sender == "" {
		return FeishuInboundMessage{}, false
	}
	return FeishuInboundMessage{
		Sender:         sender,
		Text:           text,
		MessageID:      strings.TrimSpace(env.Event.Message.MessageID),
		ConversationID: strings.TrimSpace(env.Event.Message.ChatID),
	}, true
}

func extractJSONObject(raw []byte) []byte {
	start := bytes.IndexByte(raw, '{')
	if start < 0 {
		return nil
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return nil
}
