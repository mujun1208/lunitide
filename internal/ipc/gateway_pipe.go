package ipc

import (
	"os"
	"strings"
	"unicode"
)

// GatewayPipeName is the per-user stable engine pipe. DACL still limits the
// pipe to the current user; the name is predictable so a second client can
// reconnect after the first window closes.
func GatewayPipeName(username string) string {
	user := sanitizePipeToken(username)
	if user == "" {
		user = sanitizePipeToken(os.Getenv("USERNAME"))
	}
	if user == "" {
		user = "local"
	}
	return `\\.\pipe\lunitide-gateway-` + user
}

func sanitizePipeToken(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	var b strings.Builder
	for _, r := range raw {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(strings.ReplaceAll(b.String(), "--", "-"), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}
