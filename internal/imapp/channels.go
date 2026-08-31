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
	ErrKind            = errors.New("imapp: unknown channel")
	ErrOff             = errors.New("imapp: channel is off")
	ErrWebhookRequired = errors.New("imapp: webhook url required")
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
	Kind             Kind   `json:"kind"`
	Label            string `json:"label"`
	Enabled          bool   `json:"enabled"`
	WebhookURL       string `json:"webhookUrl"`
	Mode             string `json:"mode"`
	DesktopApp       string `json:"desktopApp"`
	InboundEnabled   bool   `json:"inboundEnabled"`
	InboundAllowlist string `json:"inboundAllowlist"`
	InboundAutoRun   bool   `json:"inboundAutoRun"`
	InboundAppID     string `json:"inboundAppId"`
	InboundHasSecret bool   `json:"inboundHasSecret"`
	InboundAppSecret string `json:"-"`
	UpdatedAt        string `json:"updatedAt"`
}

// Store persists the five channel rows.
type Store interface {
	ListIMChannels(ctx context.Context) ([]Channel, error)
	UpsertIMChannel(ctx context.Context, kind Kind, patch ChannelPatch) ([]Channel, error)
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
	ch.InboundAllowlist = NormalizeAllowlist(ch.InboundAllowlist)
	ch.InboundAppID = strings.TrimSpace(ch.InboundAppID)
	if !inboundAllowed(ch.Kind) {
		ch.InboundEnabled = false
		ch.InboundAllowlist = ""
		ch.InboundAutoRun = false
		ch.InboundAppID = ""
		ch.InboundAppSecret = ""
		ch.InboundHasSecret = false
	}
	if !ch.InboundEnabled {
		ch.InboundAutoRun = false
	}
	if len(ParseAllowlist(ch.InboundAllowlist)) == 0 {
		ch.InboundAutoRun = false
	}
	ch.Mode = "off"
	if ch.Enabled && ch.WebhookURL != "" {
		ch.Mode = "webhook"
	} else if ch.Enabled && !webhookAllowed(ch.Kind) {
		ch.Mode = "desktop"
	} else if ch.Enabled {
		ch.Enabled = false
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
			out = append(out, ch.Public())
			continue
		}
		out = append(out, Normalize(Channel{Kind: kind}).Public())
	}
	return out, nil
}

func (s *Service) Set(ctx context.Context, kind Kind, patch ChannelPatch) ([]Channel, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("imapp: store unavailable")
	}
	if _, err := ParseKind(string(kind)); err != nil {
		return nil, err
	}
	if patch.WebhookURL != nil {
		url := strings.TrimSpace(*patch.WebhookURL)
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
		patch.WebhookURL = &url
	}
	if patch.InboundAllowlist != nil {
		joined := NormalizeAllowlist(*patch.InboundAllowlist)
		patch.InboundAllowlist = &joined
	}
	if patch.InboundAppID != nil {
		id := strings.TrimSpace(*patch.InboundAppID)
		patch.InboundAppID = &id
	}
	if patch.InboundAppSecret != nil {
		secret := strings.TrimSpace(*patch.InboundAppSecret)
		patch.InboundAppSecret = &secret
	}
	cur, err := s.Lookup(ctx, kind)
	if err != nil && !errors.Is(err, ErrKind) {
		return nil, err
	}
	enabled := cur.InboundEnabled
	if patch.InboundEnabled != nil {
		enabled = *patch.InboundEnabled
	}
	allow := cur.InboundAllowlist
	if patch.InboundAllowlist != nil {
		allow = *patch.InboundAllowlist
	}
	appID := cur.InboundAppID
	if patch.InboundAppID != nil {
		appID = *patch.InboundAppID
	}
	secret := ""
	if patch.InboundAppSecret != nil {
		secret = *patch.InboundAppSecret
	}
	if err := validateInboundFields(kind, enabled, allow, appID, secret); err != nil {
		return nil, err
	}
	wantEnabled := cur.Enabled
	if patch.Enabled != nil {
		wantEnabled = *patch.Enabled
	}
	url := cur.WebhookURL
	if patch.WebhookURL != nil {
		url = strings.TrimSpace(*patch.WebhookURL)
	}
	if wantEnabled && webhookAllowed(kind) && url == "" {
		return nil, ErrWebhookRequired
	}
	if patch.InboundAutoRun != nil && *patch.InboundAutoRun && len(ParseAllowlist(allow)) == 0 {
		off := false
		patch.InboundAutoRun = &off
	}
	items, err := s.store.UpsertIMChannel(ctx, kind, patch)
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(items))
	for _, ch := range items {
		out = append(out, ch.Public())
	}
	return out, nil
}

// LookupSecret returns the row including inbound_app_secret for the Feishu
// long-connection worker. Bridge List/Set never expose the secret.
func (s *Service) LookupSecret(ctx context.Context, kind Kind) (Channel, error) {
	if s == nil || s.store == nil {
		return Channel{}, errors.New("imapp: store unavailable")
	}
	items, err := s.store.ListIMChannels(ctx)
	if err != nil {
		return Channel{}, err
	}
	for _, ch := range items {
		if ch.Kind == kind {
			return Normalize(ch), nil
		}
	}
	return Channel{}, ErrKind
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
