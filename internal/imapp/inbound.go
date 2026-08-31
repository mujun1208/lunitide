package imapp

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var (
	ErrInboundKind      = errors.New("imapp: inbound is only for 飞书, 企业微信, and 钉钉")
	ErrInboundAllowlist = errors.New("imapp: inbound requires a sender allowlist")
	ErrInboundOff       = errors.New("imapp: inbound is off")
	ErrInboundDenied    = errors.New("imapp: sender is not on the allowlist")
)

const (
	MaxAllowlistRunes   = 2000
	MaxAllowlistSenders = 32
	MaxSenderRunes      = 128
	MaxInboundTextRunes = 2000
	MaxAppIDRunes       = 64
	MaxAppSecretRunes   = 256

	// PersonalChatProjectName matches web/src/App.tsx PERSONAL_CHAT_PROJECT
	// (U+2063 prefix). Do not rename.
	PersonalChatProjectName = "\u2063月汐·普通对话"
)

// ChannelPatch is one Settings write. Nil fields are left unchanged.
type ChannelPatch struct {
	Enabled          *bool
	WebhookURL       *string
	InboundEnabled   *bool
	InboundAllowlist *string
	InboundAutoRun   *bool
	InboundAppID     *string
	InboundAppSecret *string
}

func inboundAllowed(kind Kind) bool {
	return kind == KindFeishu || kind == KindWeCom || kind == KindDingTalk
}

func InboundSessionTitle(kind Kind) string {
	return Label(kind) + " · 入站"
}

func ParseAllowlist(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, ";", "\n")
	raw = strings.ReplaceAll(raw, ",", "\n")
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		if utf8.RuneCountInString(item) > MaxSenderRunes {
			continue
		}
		seen[key] = true
		out = append(out, item)
		if len(out) >= MaxAllowlistSenders {
			break
		}
	}
	return out
}

func NormalizeAllowlist(raw string) string {
	return strings.Join(ParseAllowlist(raw), "\n")
}

func SenderAllowed(allowlist string, sender string) bool {
	want := strings.ToLower(strings.TrimSpace(sender))
	if want == "" {
		return false
	}
	for _, item := range ParseAllowlist(allowlist) {
		if strings.ToLower(item) == want {
			return true
		}
	}
	return false
}

func (ch Channel) Public() Channel {
	ch.InboundHasSecret = strings.TrimSpace(ch.InboundAppSecret) != ""
	ch.InboundAppSecret = ""
	return Normalize(ch)
}

func (ch Channel) Secret() string {
	return strings.TrimSpace(ch.InboundAppSecret)
}

func validateInboundFields(kind Kind, enabled bool, allowlist, appID, appSecret string) error {
	if !enabled && strings.TrimSpace(allowlist) == "" && appID == "" && appSecret == "" {
		return nil
	}
	if !inboundAllowed(kind) {
		if enabled {
			return ErrInboundKind
		}
		return nil
	}
	if utf8.RuneCountInString(allowlist) > MaxAllowlistRunes {
		return fmt.Errorf("imapp: allowlist too long")
	}
	if utf8.RuneCountInString(appID) > MaxAppIDRunes {
		return fmt.Errorf("imapp: app id too long")
	}
	if utf8.RuneCountInString(appSecret) > MaxAppSecretRunes {
		return fmt.Errorf("imapp: app secret too long")
	}
	if enabled && len(ParseAllowlist(allowlist)) == 0 && strings.TrimSpace(appID) == "" && strings.TrimSpace(appSecret) == "" {
		return ErrInboundAllowlist
	}
	return nil
}

func InboundShouldAutoRun(ch Channel) bool {
	return ch.InboundEnabled && ch.InboundAutoRun && len(ParseAllowlist(ch.InboundAllowlist)) > 0
}

func AdmitInbound(ch Channel, sender, text string) error {
	if !inboundAllowed(ch.Kind) {
		return ErrInboundKind
	}
	if !ch.InboundEnabled {
		return ErrInboundOff
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxInboundTextRunes {
		return errors.New("imapp: empty inbound text")
	}
	if len(ParseAllowlist(ch.InboundAllowlist)) == 0 {
		if strings.TrimSpace(sender) == "" {
			return ErrInboundDenied
		}
		return nil
	}
	if !SenderAllowed(ch.InboundAllowlist, sender) {
		return ErrInboundDenied
	}
	return nil
}
