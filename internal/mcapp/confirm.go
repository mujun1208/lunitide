package mcapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// Confirm-method identifiers bound into each token.
const (
	ConfirmMethodInstall   = "mc.connector.install"
	ConfirmMethodUninstall = "mc.connector.uninstall"
	ConfirmMethodUpdate    = "mc.connector.update"
)

// ConfirmTokenRow is one persisted confirmation token (hash only - the
// raw token never touches storage).
type ConfirmTokenRow struct {
	TokenHash  string
	Method     string
	Target     string
	Digest     string
	IssuedAt   string
	ExpiresAt  string
	ConsumedAt string
}

// ── mc.confirm.token ────────────────────────────────────────────────────────

// IssueConfirmToken mints one single-use token bound to method+target.
func (s *Service) IssueConfirmToken(ctx context.Context, method, target, digest string) (string, string, error) {
	if s == nil || s.uow == nil {
		return "", "", ErrMcNotFound
	}
	if method != ConfirmMethodInstall && method != ConfirmMethodUninstall && method != ConfirmMethodUpdate {
		return "", "", fmt.Errorf("%w: method %q", ErrMcSchema, method)
	}
	if len(target) < 1 || len(target) > 256 {
		return "", "", fmt.Errorf("%w: target length", ErrMcSchema)
	}
	now := s.clock.Now().UTC()
	if !s.limiter.allow(now) {
		return "", "", ErrMcRateLimited
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	row := ConfirmTokenRow{
		TokenHash: hex.EncodeToString(sum[:]),
		Method:    method,
		Target:    target,
		Digest:    digest,
		IssuedAt:  now.Format(time.RFC3339),
		ExpiresAt: now.Add(McConfirmTTL).Format(time.RFC3339),
	}
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		if err := tx.PutConfirmToken(row); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "mc.confirm.issued",
			ResourceType: "mc_confirm_token", ResourceID: row.TokenHash[:16],
			Actor: "renderer", CreatedAt: row.IssuedAt,
		})
		return err
	})
	if err != nil {
		return "", "", err
	}
	return token, row.ExpiresAt, nil
}

// consumeConfirm validates and burns one token inside the caller's
// transaction (single-use gate: exactly one row may flip consumed_at).
func (s *Service) consumeConfirm(tx Tx, method, target, token string, now time.Time) error {
	if token == "" {
		return fmt.Errorf("%w: token missing", ErrMcConfirm)
	}
	sum := sha256.Sum256([]byte(token))
	row, err := tx.GetConfirmToken(hex.EncodeToString(sum[:]))
	if errors.Is(err, m7flow.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: unknown token", ErrMcConfirm)
	}
	if err != nil {
		return err
	}
	if row.Method != method || row.Target != target {
		return fmt.Errorf("%w: token bound to %s/%s", ErrMcConfirm, row.Method, row.Target)
	}
	if err := tx.ConsumeConfirmToken(row.TokenHash, now.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("%w: token expired or already used", ErrMcConfirm)
	}
	return nil
}

// installTarget derives the canonical confirm-target for an install:
// the market item id, or the fingerprint digest for manual configs.
func installTarget(in InstallInput) string {
	if in.MarketItemID != "" {
		return in.MarketItemID
	}
	return "fp:" + fingerprintDigest(in.Transport, in.Command, in.URL)
}

func fingerprintDigest(transport, command, urlRef string) string {
	sum := sha256.Sum256([]byte(transport + "|" + command + "|" + urlRef))
	return hex.EncodeToString(sum[:])
}
