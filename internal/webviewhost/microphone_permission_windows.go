//go:build windows

package webviewhost

import (
	"log"
	"os"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

// resetDeniedMicrophoneCache runs before the WebView2 browser process starts,
// while it still has exclusive access to the preferences file, and removes
// persisted microphone denials for the trusted origin left by older builds.
func resetDeniedMicrophoneCache(userDataFolder string) {
	path := MicrophonePreferencesPath(userDataFolder)
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	updated, changed := ResetDeniedMicrophoneCache(raw, TrustedVirtualHost)
	if !changed {
		return
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		log.Printf("clearing cached microphone denial in %s failed: %v", path, err)
		return
	}
	log.Printf("cleared cached microphone denial for %s", TrustedOrigin)
}

// grantTrustedMicrophone pins the microphone permission for the trusted origin
// through the DevTools protocol, so a cached or ASK state can no longer fail
// getUserMedia inside the renderer before the PermissionRequested handler has
// a chance to allow it.
func (h *Host) grantTrustedMicrophone() {
	handler := wv2.NewICoreWebView2CallDevToolsProtocolMethodCompletedHandlerByFunc(func(code com.Error, _ string) com.Error {
		if failed(win32.HRESULT(code)) {
			log.Printf("microphone DevTools permission override failed: 0x%x", uint32(code))
		}
		return com.Error(win32.S_OK)
	}, true)
	if result := h.core.CallDevToolsProtocolMethod("Browser.setPermission", MicrophoneGrantCDPParameters(TrustedOrigin), handler); failed(win32.HRESULT(result)) {
		log.Printf("microphone DevTools permission override request failed: 0x%x", uint32(result))
	}
}
