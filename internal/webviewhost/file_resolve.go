package webviewhost

import "encoding/json"

// ParseFileResolveMessage recognizes the renderer asking for the paths of File
// objects it passed alongside the message. The paths come from WebView2, never
// from the message body, so the page cannot name a file the user did not give it.
func ParseFileResolveMessage(raw string) (string, bool) {
	var msg struct {
		Kind  string `json:"kind"`
		Token string `json:"token"`
	}
	if json.Unmarshal([]byte(raw), &msg) != nil || msg.Kind != "lunitide.files.resolve" || msg.Token == "" || len(msg.Token) > 128 {
		return "", false
	}
	for _, r := range msg.Token {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return "", false
		}
	}
	return msg.Token, true
}
