// Package talk implements the OpenAI-shaped realtime WebSocket used by talk.*.
// It does not add a ProviderProtocol and does not reuse omni.* or volc SAUC.
package talk

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// RealtimeWebSocketURL maps an openai_compatible HTTPS origin onto the
// standard /v1/realtime WebSocket. The model id is queried, never invented.
func RealtimeWebSocketURL(baseURL, modelID string) (string, error) {
	raw := strings.TrimSpace(baseURL)
	id := strings.TrimSpace(modelID)
	if raw == "" || id == "" {
		return "", errors.New("talk realtime url needs baseUrl and modelId")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("talk realtime baseUrl is invalid")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", errors.New("talk realtime baseUrl scheme is invalid")
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/realtime"):
	case path == "", path == "/v1":
		path = "/v1/realtime"
	default:
		path += "/realtime"
	}
	u.Path = path
	query := u.Query()
	query.Set("model", id)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func SessionUpdateMessage(instructions string) []byte {
	body := map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"modalities":          []string{"audio", "text"},
			"input_audio_format":  "pcm16",
			"output_audio_format": "pcm16",
			"input_audio_transcription": map[string]any{
				"model": "whisper-1",
			},
			"turn_detection": map[string]any{"type": "server_vad"},
			"instructions":   instructions,
		},
	}
	raw, _ := json.Marshal(body)
	return raw
}

func AppendAudioMessage(pcmBase64 string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": UpsamplePCM16kBase64To24k(pcmBase64),
	})
	return raw
}

func CancelOutputMessage() []byte {
	raw, _ := json.Marshal(map[string]any{"type": "response.cancel"})
	return raw
}

func CreateUserTextMessage(text string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"type": "conversation.item.create",
		"item": map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]string{{
				"type": "input_text",
				"text": text,
			}},
		},
	})
	return raw
}

func ResponseCreateMessage() []byte {
	raw, _ := json.Marshal(map[string]any{"type": "response.create"})
	return raw
}

// ServerEvent is one OpenAI-shaped realtime frame, mapped onto talk.* events.
type ServerEvent struct {
	Kind       string
	Audio      string
	Transcript string
	Role       string
	Code       string
	Message    string
}

func ParseServerEvent(raw []byte) ServerEvent {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return ServerEvent{Kind: "error", Code: "TALK_SESSION_FAILED", Message: "通话核事件无法解析"}
	}
	typ, _ := payload["type"].(string)
	switch typ {
	case "response.audio.delta", "response.output_audio.delta":
		delta, _ := payload["delta"].(string)
		return ServerEvent{Kind: "audio", Audio: delta}
	case "response.audio_transcript.delta", "response.output_audio_transcript.delta":
		delta, _ := payload["delta"].(string)
		return ServerEvent{Kind: "transcript", Transcript: delta, Role: "assistant"}
	case "conversation.item.input_audio_transcription.completed":
		text, _ := payload["transcript"].(string)
		return ServerEvent{Kind: "transcript", Transcript: text, Role: "user"}
	case "input_audio_buffer.speech_started":
		return ServerEvent{Kind: "barge"}
	case "error":
		code, message := "TALK_SESSION_FAILED", "通话核失败"
		if nested, ok := payload["error"].(map[string]any); ok {
			if c, _ := nested["code"].(string); c != "" {
				code = c
			}
			if m, _ := nested["message"].(string); m != "" {
				message = m
			}
		}
		return ServerEvent{Kind: "error", Code: code, Message: message}
	case "session.created", "session.updated":
		return ServerEvent{Kind: "ready"}
	default:
		return ServerEvent{Kind: "ignore"}
	}
}
