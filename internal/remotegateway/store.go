// Package remotegateway 承载手机伴侣的远程网关：HTTPS + WebSocket 上承载
// 既有 bridge 帧协议，远程设备经过配对令牌鉴权后复用引擎全部能力。
// PRD：docs/design/PRD-mobile-companion-2026-10-07.md。
package remotegateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SecureRoot 是与 storage.sqlite 同一形状的数据根能力：定位文件并施加
// 本机 ACL 保护。生产实现是 *datadir.SecureRoot。
type SecureRoot interface {
	FilePath(name string) (string, error)
	ProtectRegularFile(name string) error
}

// Device 是一台已配对的远程设备。令牌明文只在配对响应里出现一次，
// 落库的是 SHA-256 哈希。
type Device struct {
	DeviceID   string     `json:"deviceId"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	TokenHash  string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	GrantedAt  time.Time  `json:"grantedAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
	LastIP     string     `json:"lastIp,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// AuditEntry 是 remote_audit 的一行：配对、鉴权失败、方法调用、吊销。
type AuditEntry struct {
	ID       int64     `json:"id"`
	DeviceID string    `json:"deviceId"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	Method   string    `json:"method,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

// Store 持有独立的 remote.db，不进主库迁移链：网关的表全部自建自管，
// 主库 schema 不因远程能力变化。
type Store struct {
	db *sql.DB
}

const storeSchema = `
CREATE TABLE IF NOT EXISTS paired_devices (
  device_id    TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  platform     TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  scopes       TEXT NOT NULL,
  granted_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  last_seen_at INTEGER,
  last_ip      TEXT,
  revoked_at   INTEGER
);
CREATE TABLE IF NOT EXISTS pairing_codes (
  code_hash  TEXT PRIMARY KEY,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);
CREATE TABLE IF NOT EXISTS remote_audit (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id TEXT NOT NULL,
  at        INTEGER NOT NULL,
  kind      TEXT NOT NULL,
  method    TEXT,
  detail    TEXT
);
CREATE INDEX IF NOT EXISTS idx_remote_audit_device ON remote_audit(device_id, at);
CREATE TABLE IF NOT EXISTS remote_config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

// OpenStore 打开（必要时创建）remote.db 并确保表结构。database 文件与
// WAL 边车都过 SecureRoot 的 ACL 保护，与主库同等级别。
func OpenStore(ctx context.Context, root SecureRoot) (*Store, error) {
	if root == nil {
		return nil, errors.New("remotegateway: secure root is required")
	}
	path, err := root.FilePath("remote.db")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"remote.db", "remote.db-wal", "remote.db-shm", "remote.db-journal"} {
		if err := root.ProtectRegularFile(name); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("remotegateway: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, storeSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("remotegateway: schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// ConfigGet 读取布尔型开关（enabled / keepAwake）。缺失键返回 false。
func (s *Store) ConfigGet(ctx context.Context, key string) (bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM remote_config WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

func (s *Store) ConfigSet(ctx context.Context, key string, on bool) error {
	value := "0"
	if on {
		value = "1"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO remote_config(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) DeviceSave(ctx context.Context, device Device) error {
	scopes, err := json.Marshal(device.Scopes)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO paired_devices
		(device_id, name, platform, token_hash, scopes, granted_at, expires_at, last_seen_at, last_ip, revoked_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		device.DeviceID, device.Name, device.Platform, device.TokenHash, string(scopes),
		device.GrantedAt.Unix(), device.ExpiresAt.Unix(), unixOrNil(device.LastSeenAt), device.LastIP, unixOrNil(device.RevokedAt))
	return err
}

// DeviceByTokenHash 按令牌哈希取设备（不校验有效期/吊销，由调用方判定）。
func (s *Store) DeviceByTokenHash(ctx context.Context, tokenHash string) (*Device, error) {
	row := s.db.QueryRowContext(ctx, `SELECT device_id, name, platform, token_hash, scopes,
		granted_at, expires_at, last_seen_at, last_ip, revoked_at
		FROM paired_devices WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash)
	return scanDevice(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDevice(row rowScanner) (*Device, error) {
	var device Device
	var scopes string
	var grantedAt, expiresAt int64
	var lastSeen, revoked sql.NullInt64
	var lastIP sql.NullString
	if err := row.Scan(&device.DeviceID, &device.Name, &device.Platform, &device.TokenHash, &scopes,
		&grantedAt, &expiresAt, &lastSeen, &lastIP, &revoked); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopes), &device.Scopes); err != nil {
		return nil, err
	}
	device.GrantedAt = time.Unix(grantedAt, 0)
	device.ExpiresAt = time.Unix(expiresAt, 0)
	if lastSeen.Valid {
		t := time.Unix(lastSeen.Int64, 0)
		device.LastSeenAt = &t
	}
	if lastIP.Valid {
		device.LastIP = lastIP.String
	}
	if revoked.Valid {
		t := time.Unix(revoked.Int64, 0)
		device.RevokedAt = &t
	}
	return &device, nil
}

func (s *Store) DeviceList(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id, name, platform, token_hash, scopes,
		granted_at, expires_at, last_seen_at, last_ip, revoked_at
		FROM paired_devices ORDER BY granted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := []Device{}
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, *device)
	}
	return devices, rows.Err()
}

func (s *Store) DeviceRevoke(ctx context.Context, deviceID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE paired_devices SET revoked_at = ? WHERE device_id = ?`, at.Unix(), deviceID)
	return err
}

func (s *Store) DeviceTouch(ctx context.Context, deviceID, ip string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE paired_devices SET last_seen_at = ?, last_ip = ? WHERE device_id = ?`, at.Unix(), ip, deviceID)
	return err
}

// PairingCodeSave 写入一条一次性配对码（只存哈希）。
func (s *Store) PairingCodeSave(ctx context.Context, codeHash string, createdAt, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pairing_codes(code_hash, created_at, expires_at) VALUES(?, ?, ?)`,
		codeHash, createdAt.Unix(), expiresAt.Unix())
	return err
}

// PairingCodeConsume 原子消费配对码：命中且未使用且未过期才置 used 并返回
// true；过期行顺手清理。单条 UPDATE 的原子性由 sqlite 写锁保证。
func (s *Store) PairingCodeConsume(ctx context.Context, codeHash string, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE pairing_codes SET used_at = ?
		WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`, at.Unix(), codeHash, at.Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM pairing_codes WHERE expires_at <= ?`, at.Unix())
	return n == 1, nil
}

func (s *Store) AuditAppend(ctx context.Context, deviceID, kind, method, detail string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO remote_audit(device_id, at, kind, method, detail) VALUES(?, ?, ?, ?, ?)`,
		deviceID, at.Unix(), kind, method, detail)
	return err
}

func (s *Store) AuditRecent(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, device_id, at, kind, COALESCE(method, ''), COALESCE(detail, '')
		FROM remote_audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []AuditEntry{}
	for rows.Next() {
		var entry AuditEntry
		var at int64
		if err := rows.Scan(&entry.ID, &entry.DeviceID, &at, &entry.Kind, &entry.Method, &entry.Detail); err != nil {
			return nil, err
		}
		entry.At = time.Unix(at, 0)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func unixOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

// sanitizeDeviceName 限制设备名形状，防止审计/列表注入怪字符。
func sanitizeDeviceName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// sanitizePlatform 只允许白名单平台标识。
func sanitizePlatform(platform string) string {
	switch strings.TrimSpace(platform) {
	case "ios-pwa", "android-pwa", "other":
		return strings.TrimSpace(platform)
	default:
		return "other"
	}
}
