package imapp

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/lunitide/lunitide/internal/secret"
)

// dpapiSecretMarker is what the im_channels row holds once the inbound app
// secret lives in the DPAPI credential store instead of the database. The
// leading NUL keeps it apart from any real Feishu/WeCom secret (those are
// printable) and from the empty string that means "no secret". Public() still
// reports InboundHasSecret from it being non-empty, so Settings shows a secret
// is configured without ever reading the plaintext.
const dpapiSecretMarker = "\x00lunitide-im-dpapi-v1"

// imSecretRef keys one channel's inbound secret in the DPAPI store. The fields
// only have to be a stable, valid Ref; Origin must parse as a base URL because
// secret.Ref.Validate normalizes it, hence the https sentinel host.
func imSecretRef(kind Kind) secret.Ref {
	return secret.Ref{
		CredentialRef: "im-inbound-" + string(kind),
		ProviderID:    "im-inbound-" + string(kind),
		Origin:        "https://im.inbound.lunitide.local",
		Protocol:      "im-inbound",
	}
}

// WithSecrets wires the DPAPI credential store so inbound app secrets stop
// living in the database as plaintext. When it is never called (unit tests
// without a secure root) the service keeps the old in-column behavior, so the
// seam is opt-in rather than a hard dependency.
func (s *Service) WithSecrets(store secret.Service) *Service {
	if s != nil {
		s.secrets = store
	}
	return s
}

// sealInboundSecret moves a freshly submitted plaintext secret out of the
// patch and into DPAPI, leaving the marker in its place. A nil field (no
// change) and the empty string (leave unchanged, per UpsertIMChannel) both
// pass through untouched. Called before the row is written, so the database
// never sees the plaintext.
func (s *Service) sealInboundSecret(ctx context.Context, kind Kind, patch *ChannelPatch) error {
	if s.secrets == nil || patch.InboundAppSecret == nil {
		return nil
	}
	plain := strings.TrimSpace(*patch.InboundAppSecret)
	if plain == "" || plain == dpapiSecretMarker {
		return nil
	}
	if err := s.secrets.Put(ctx, imSecretRef(kind), []byte(plain)); err != nil {
		return errors.New("imapp: 保存入站密钥失败")
	}
	marker := dpapiSecretMarker
	patch.InboundAppSecret = &marker
	return nil
}

// resolveInboundSecret fills ch.InboundAppSecret with the real plaintext for
// the inbound worker. Three cases:
//   - no secret store wired: leave the column value as-is (tests / legacy).
//   - marker present: read the plaintext back out of DPAPI.
//   - a real secret still in the column: a row written before this change.
//     Migrate it into DPAPI now and rewrite the column to the marker, so the
//     plaintext copy in the database is gone after the first read.
func (s *Service) resolveInboundSecret(ctx context.Context, kind Kind, ch Channel) (Channel, error) {
	if s.secrets == nil {
		return ch, nil
	}
	stored := ch.InboundAppSecret
	if strings.TrimSpace(stored) == "" {
		return ch, nil
	}
	if stored == dpapiSecretMarker {
		plain, err := s.readInboundSecret(ctx, kind)
		if err != nil {
			return ch, err
		}
		ch.InboundAppSecret = plain
		return ch, nil
	}
	// Legacy plaintext row: seal it, then blank the database copy.
	if err := s.secrets.Put(ctx, imSecretRef(kind), []byte(strings.TrimSpace(stored))); err != nil {
		return ch, errors.New("imapp: 迁移入站密钥失败")
	}
	marker := dpapiSecretMarker
	if _, err := s.store.UpsertIMChannel(ctx, kind, ChannelPatch{InboundAppSecret: &marker}); err != nil {
		return ch, err
	}
	ch.InboundAppSecret = strings.TrimSpace(stored)
	return ch, nil
}

func (s *Service) readInboundSecret(ctx context.Context, kind Kind) (string, error) {
	var out string
	err := s.secrets.WithSecret(ctx, imSecretRef(kind), func(plain []byte) error {
		out = string(plain)
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		// Marker without an asset: the credential file was removed under us.
		// Report empty rather than an error so the worker simply stays idle.
		return "", nil
	}
	if err != nil {
		return "", errors.New("imapp: 读取入站密钥失败")
	}
	return out, nil
}
