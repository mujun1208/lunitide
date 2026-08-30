package imapp

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const WeComOpenWS = "wss://openws.work.weixin.qq.com"

type WeComInboundMessage struct {
	Sender    string
	Text      string
	MessageID string
}

func WeComSubscribePayload(botID, secret string) []byte {
	body, _ := json.Marshal(map[string]any{
		"cmd": "aibot_subscribe",
		"headers": map[string]string{
			"req_id": ulid.Make().String(),
		},
		"body": map[string]string{
			"bot_id": strings.TrimSpace(botID),
			"secret": strings.TrimSpace(secret),
		},
	})
	return body
}

func WeComPingPayload() []byte {
	body, _ := json.Marshal(map[string]any{
		"cmd": "ping",
		"headers": map[string]string{
			"req_id": ulid.Make().String(),
		},
	})
	return body
}

func ParseWeComMessageEvent(raw []byte) (WeComInboundMessage, bool) {
	payload := extractJSONObject(raw)
	if len(payload) == 0 {
		return WeComInboundMessage{}, false
	}
	var env struct {
		Cmd  string `json:"cmd"`
		Body struct {
			MsgID   string `json:"msgid"`
			MsgType string `json:"msgtype"`
			From    struct {
				UserID string `json:"userid"`
			} `json:"from"`
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"body"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return WeComInboundMessage{}, false
	}
	if env.Cmd != "" && env.Cmd != "aibot_msg_callback" {
		return WeComInboundMessage{}, false
	}
	if env.Body.MsgType != "" && env.Body.MsgType != "text" {
		return WeComInboundMessage{}, false
	}
	text := strings.TrimSpace(env.Body.Text.Content)
	sender := strings.TrimSpace(env.Body.From.UserID)
	if text == "" || sender == "" || utf8.RuneCountInString(text) > MaxInboundTextRunes {
		return WeComInboundMessage{}, false
	}
	return WeComInboundMessage{Sender: sender, Text: text, MessageID: strings.TrimSpace(env.Body.MsgID)}, true
}
