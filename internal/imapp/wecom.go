package imapp

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const WeComOpenWS = "wss://openws.work.weixin.qq.com"

type WeComInboundMessage struct {
	Sender         string
	Text           string
	MessageID      string
	ConversationID string
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

// WeComSendMsgPayload is aibot_send_msg on the inbound long-poll socket.
// chatType 1 = DM (userid), 2 = group (chatid).
func WeComSendMsgPayload(chatID string, group bool, text string) []byte {
	chatType := 1
	if group {
		chatType = 2
	}
	body, _ := json.Marshal(map[string]any{
		"cmd": "aibot_send_msg",
		"headers": map[string]string{
			"req_id": ulid.Make().String(),
		},
		"body": map[string]any{
			"chatid":    strings.TrimSpace(chatID),
			"chat_type": chatType,
			"msgtype":   "markdown",
			"markdown":  map[string]string{"content": strings.TrimSpace(text)},
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
			ChatID string `json:"chatid"`
			Text   struct {
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
	return WeComInboundMessage{
		Sender:         sender,
		Text:           text,
		MessageID:      strings.TrimSpace(env.Body.MsgID),
		ConversationID: strings.TrimSpace(env.Body.ChatID),
	}, true
}
