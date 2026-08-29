package imapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/scheduler"
)

var (
	ErrKind = errors.New("imapp: unknown channel")
	ErrOff  = errors.New("imapp: channel is off")
)

// Kind is one of the five front-door IM channels.
type Kind string

const (
	KindFeishu   Kind = "feishu"
	KindWeCom    Kind = "wecom"
	KindDingTalk Kind = "dingtalk"
	KindWeChat   Kind = "wechat"
	KindQQ       Kind = "qq"
)

// AllKinds is the stable Settings order.
var AllKinds = []Kind{KindFeishu, KindWeCom, KindDingTalk, KindWeChat, KindQQ}

// Channel is one persisted IM destination.
type Channel struct {
	Kind       Kind   `json:"kind"`
	Label      string `json:"label"`
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhookUrl"`
	Mode       string `json:"mode"`
	DesktopApp string `json:"desktopApp"`
	UpdatedAt  string `json:"updatedAt"`
}

// Store persists the five channel rows.
type Store interface {
	ListIMChannels(ctx context.Context) ([]Channel, error)
	UpsertIMChannel(ctx context.Context, kind Kind, enabled *bool, webhookURL *string) ([]Channel, error)
}

// Service is the Settings + im.send surface.
type Service struct {
	store Store
}

func New(store Store) *Service { return &Service{store: store} }

func ParseKind(raw string) (Kind, error) {
	k := Kind(strings.ToLower(strings.TrimSpace(raw)))
	for _, known := range AllKinds {
		if k == known {
			return k, nil
		}
	}
	return "", ErrKind
}

func Label(kind Kind) string {
	switch kind {
	case KindFeishu:
		return "飞书"
	case KindWeCom:
		return "企业微信"
	case KindDingTalk:
		return "钉钉"
	case KindWeChat:
		return "微信"
	case KindQQ:
		return "QQ"
	default:
		return string(kind)
	}
}

func DesktopApp(kind Kind) string {
	switch kind {
	case KindFeishu:
		return "飞书"
	case KindWeCom:
		return "企业微信"
	case KindDingTalk:
		return "钉钉"
	case KindWeChat:
		return "微信"
	case KindQQ:
		return "QQ"
	default:
		return ""
	}
}

func webhookAllowed(kind Kind) bool {
	switch kind {
	case KindFeishu, KindWeCom, KindDingTalk:
		return true
	default:
		return false
	}
}

func Normalize(ch Channel) Channel {
	ch.Kind, _ = ParseKind(string(ch.Kind))
	ch.Label = Label(ch.Kind)
	ch.DesktopApp = DesktopApp(ch.Kind)
	ch.WebhookURL = strings.TrimSpace(ch.WebhookURL)
	if !webhookAllowed(ch.Kind) {
		ch.WebhookURL = ""
	}
	ch.Mode = "off"
	if ch.Enabled && ch.WebhookURL != "" {
		ch.Mode = "webhook"
	} else if ch.Enabled {
		ch.Mode = "desktop"
	}
	return ch
}

func (s *Service) List(ctx context.Context) ([]Channel, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("imapp: store unavailable")
	}
	items, err := s.store.ListIMChannels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(AllKinds))
	byKind := map[Kind]Channel{}
	for _, ch := range items {
		byKind[ch.Kind] = Normalize(ch)
	}
	for _, kind := range AllKinds {
		if ch, ok := byKind[kind]; ok {
			out = append(out, ch)
			continue
		}
		out = append(out, Normalize(Channel{Kind: kind}))
	}
	return out, nil
}

func (s *Service) Set(ctx context.Context, kind Kind, enabled *bool, webhookURL *string) ([]Channel, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("imapp: store unavailable")
	}
	if _, err := ParseKind(string(kind)); err != nil {
		return nil, err
	}
	if webhookURL != nil {
		url := strings.TrimSpace(*webhookURL)
		if url != "" {
			if !webhookAllowed(kind) {
				return nil, fmt.Errorf("imapp: %s uses the desktop client, not a webhook", Label(kind))
			}
			if utf8.RuneCountInString(url) > 512 {
				return nil, scheduler.ErrWebhookInvalid
			}
			if err := scheduler.ValidateWebhookURL(url); err != nil {
				return nil, err
			}
		}
		webhookURL = &url
	}
	return s.store.UpsertIMChannel(ctx, kind, enabled, webhookURL)
}

func (s *Service) Lookup(ctx context.Context, kind Kind) (Channel, error) {
	items, err := s.List(ctx)
	if err != nil {
		return Channel{}, err
	}
	for _, ch := range items {
		if ch.Kind == kind {
			return ch, nil
		}
	}
	return Channel{}, ErrKind
}

// Send posts to a webhook channel. Desktop channels return a sentinel the
// tool runtime turns into desktop.open + type.
func (s *Service) Send(ctx context.Context, kind Kind, to, text string) (Channel, string, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 4000 {
		return Channel{}, "", errors.New("imapp: empty message")
	}
	ch, err := s.Lookup(ctx, kind)
	if err != nil {
		return Channel{}, "", err
	}
	if !ch.Enabled {
		return ch, "", fmt.Errorf("%w: 请先在设置 → 消息通道启用%s", ErrOff, ch.Label)
	}
	if ch.Mode == "webhook" {
		n, err := scheduler.NewWebhookNotifier(ch.WebhookURL)
		if err != nil {
			return ch, "", err
		}
		title := ch.Label
		if strings.TrimSpace(to) != "" {
			title = ch.Label + " · " + strings.TrimSpace(to)
		}
		if err := n.Notify(title, text); err != nil {
			return ch, "", err
		}
		return ch, "sent via " + ch.Label + " webhook", nil
	}
	return ch, "desktop:" + ch.DesktopApp, nil
}
