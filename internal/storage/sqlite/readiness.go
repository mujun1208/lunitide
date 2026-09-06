package sqlite

import (
	"context"
	"errors"
)

// CheckReadiness validates the live connection and its safety settings. Startup
// already performs the full schema/integrity validation; health polling stays
// cheap and never repairs or changes user data.
func (s *Store) CheckReadiness(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("storage unavailable")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var one, foreignKeys, trustedSchema int
	if err := conn.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA trusted_schema`).Scan(&trustedSchema); err != nil {
		return err
	}
	if one != 1 || foreignKeys != 1 || trustedSchema != 0 {
		return errors.New("storage connection safety settings unavailable")
	}
	return nil
}
