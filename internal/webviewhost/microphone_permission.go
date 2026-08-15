package webviewhost

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
)

// TrustedVirtualHost is the fixed virtual host backing the trusted origin.
const TrustedVirtualHost = "app.lunitide.local"

// Chromium persists microphone decisions as media_stream_mic content settings.
// A BLOCK entry makes getUserMedia fail inside the renderer without ever
// raising the PermissionRequested event, so the host must clear stale denials
// itself; the deny side is the only value that can wedge voice input.
const contentSettingBlock = 2

// MicrophoneGrantCDPParameters builds DevTools-protocol arguments that pin the
// trusted origin's microphone permission to granted for this browser process.
func MicrophoneGrantCDPParameters(origin string) string {
	parameters, _ := json.Marshal(struct {
		Origin     string         `json:"origin"`
		Permission map[string]any `json:"permission"`
		Setting    string         `json:"setting"`
	}{Origin: origin, Permission: map[string]any{"name": "microphone"}, Setting: "granted"})
	return string(parameters)
}

// MicrophonePreferencesPath returns the default-profile Chromium preferences
// file inside a WebView2 user-data folder.
func MicrophonePreferencesPath(userDataFolder string) string {
	return filepath.Join(userDataFolder, "EBWebView", "Default", "Preferences")
}

// ResetDeniedMicrophoneCache drops persisted microphone denials for the trusted
// virtual host from raw preferences JSON while preserving everything else.
// Malformed or unexpected input is returned unchanged.
func ResetDeniedMicrophoneCache(raw []byte, trustedHost string) ([]byte, bool) {
	var preferences map[string]any
	if err := json.Unmarshal(raw, &preferences); err != nil {
		return raw, false
	}
	exceptions, ok := nestedMap(preferences, "profile", "content_settings", "exceptions", "media_stream_mic")
	if !ok || len(exceptions) == 0 {
		return raw, false
	}
	removed := false
	for key, value := range exceptions {
		if !deniedTrustedMicrophoneEntry(key, value, trustedHost) {
			continue
		}
		delete(exceptions, key)
		removed = true
	}
	if !removed {
		return raw, false
	}
	updated, err := json.Marshal(preferences)
	if err != nil {
		return raw, false
	}
	return updated, true
}

func nestedMap(root map[string]any, path ...string) (map[string]any, bool) {
	current := root
	for _, segment := range path {
		next, ok := current[segment].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func deniedTrustedMicrophoneEntry(key string, value any, trustedHost string) bool {
	entry, ok := value.(map[string]any)
	if !ok {
		return false
	}
	setting, ok := entry["setting"].(float64)
	if !ok || int64(setting) != contentSettingBlock {
		return false
	}
	// Exception keys pair an origin with its embedding origin, e.g.
	// "https://app.lunitide.local:443,*".
	origin, ok := strings.CutSuffix(key, ",*")
	if !ok {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	port := parsed.Port()
	return parsed.Scheme == "https" && (port == "" || port == "443") && strings.EqualFold(parsed.Hostname(), trustedHost)
}
