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
	rows, err := s.db.QueryContext(ctx, `SELECT kind, enabled, webhook_url, updated_at FROM im_channels ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []imapp.Channel
	for rows.Next() {
		var ch imapp.Channel
		var enabled int
		if err := rows.Scan(&ch.Kind, &enabled, &ch.WebhookURL, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		ch.Enabled = enabled == 1
		out = append(out, imapp.Normalize(ch))
	}
	return out, rows.Err()
}

func (s *Store) UpsertIMChannel(ctx context.Context, kind imapp.Kind, enabled *bool, webhookURL *string) ([]imapp.Channel, error) {
	if err := s.seedIMChannels(ctx); err != nil {
		return nil, err
	}
	now := formatTime(time.Now().UTC())
	row := s.db.QueryRowContext(ctx, `SELECT enabled, webhook_url FROM im_channels WHERE kind=?`, string(kind))
	var curEnabled int
	var curURL string
	if err := row.Scan(&curEnabled, &curURL); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if enabled != nil {
		curEnabled = 0
		if *enabled {
			curEnabled = 1
		}
	}
	if webhookURL != nil {
		curURL = *webhookURL
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO im_channels(kind, enabled, webhook_url, updated_at) VALUES(?,?,?,?)
		ON CONFLICT(kind) DO UPDATE SET enabled=excluded.enabled, webhook_url=excluded.webhook_url, updated_at=excluded.updated_at`,
		string(kind), curEnabled, curURL, now); err != nil {
		return nil, err
	}
	return s.ListIMChannels(ctx)
}

func (s *Store) seedIMChannels(ctx context.Context) error {
	now := formatTime(time.Now().UTC())
	for _, kind := range imapp.AllKinds {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO im_channels(kind, enabled, webhook_url, updated_at) VALUES(?,?,?,?)`,
			string(kind), 0, "", now); err != nil {
			return err
		}
	}
	return nil
}
