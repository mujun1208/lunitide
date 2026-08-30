package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/imapp"
)

func (s *Store) ListIMChannels(ctx context.Context) ([]imapp.Channel, error) {
	if err := s.seedIMChannels(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, enabled, webhook_url, inbound_enabled, inbound_allowlist, inbound_auto_run, inbound_app_id, inbound_app_secret, updated_at FROM im_channels ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []imapp.Channel
	for rows.Next() {
		var ch imapp.Channel
		var enabled, inbound, autoRun int
		if err := rows.Scan(&ch.Kind, &enabled, &ch.WebhookURL, &inbound, &ch.InboundAllowlist, &autoRun, &ch.InboundAppID, &ch.InboundAppSecret, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Enabled = enabled == 1
		ch.InboundEnabled = inbound == 1
		ch.InboundAutoRun = autoRun == 1
		out = append(out, imapp.Normalize(ch))
	}
	return out, rows.Err()
}

func (s *Store) UpsertIMChannel(ctx context.Context, kind imapp.Kind, patch imapp.ChannelPatch) ([]imapp.Channel, error) {
	if err := s.seedIMChannels(ctx); err != nil {
		return nil, err
	}
	now := formatTime(time.Now().UTC())
	row := s.db.QueryRowContext(ctx, `SELECT enabled, webhook_url, inbound_enabled, inbound_allowlist, inbound_auto_run, inbound_app_id, inbound_app_secret FROM im_channels WHERE kind=?`, string(kind))
	var curEnabled, curInbound, curAuto int
	var curURL, curAllow, curAppID, curSecret string
	if err := row.Scan(&curEnabled, &curURL, &curInbound, &curAllow, &curAuto, &curAppID, &curSecret); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if patch.Enabled != nil {
		curEnabled = 0
		if *patch.Enabled {
			curEnabled = 1
		}
	}
	if patch.WebhookURL != nil {
		curURL = *patch.WebhookURL
	}
	if patch.InboundEnabled != nil {
		curInbound = 0
		if *patch.InboundEnabled {
			curInbound = 1
		}
	}
	if patch.InboundAllowlist != nil {
		curAllow = *patch.InboundAllowlist
	}
	if patch.InboundAutoRun != nil {
		curAuto = 0
		if *patch.InboundAutoRun {
			curAuto = 1
		}
	}
	if patch.InboundAppID != nil {
		curAppID = *patch.InboundAppID
	}
	if patch.InboundAppSecret != nil && *patch.InboundAppSecret != "" {
		curSecret = *patch.InboundAppSecret
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO im_channels(kind, enabled, webhook_url, inbound_enabled, inbound_allowlist, inbound_auto_run, inbound_app_id, inbound_app_secret, updated_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(kind) DO UPDATE SET enabled=excluded.enabled, webhook_url=excluded.webhook_url, inbound_enabled=excluded.inbound_enabled, inbound_allowlist=excluded.inbound_allowlist, inbound_auto_run=excluded.inbound_auto_run, inbound_app_id=excluded.inbound_app_id, inbound_app_secret=excluded.inbound_app_secret, updated_at=excluded.updated_at`,
		string(kind), curEnabled, curURL, curInbound, curAllow, curAuto, curAppID, curSecret, now); err != nil {
		return nil, err
	}
	return s.ListIMChannels(ctx)
}

func (s *Store) seedIMChannels(ctx context.Context) error {
	now := formatTime(time.Now().UTC())
	for _, kind := range imapp.AllKinds {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO im_channels(kind, enabled, webhook_url, inbound_enabled, inbound_allowlist, inbound_auto_run, inbound_app_id, inbound_app_secret, updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			string(kind), 0, "", 0, "", 0, "", "", now); err != nil {
			return err
		}
	}
	return nil
}
