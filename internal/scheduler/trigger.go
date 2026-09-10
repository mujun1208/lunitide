package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	TriggerSchedule        = "schedule"
	TriggerFileSetChanged  = "file_set_changed"
	TriggerHTTPSnapshot    = "http_snapshot_changed"
	TriggerMetricThreshold = "metric_threshold"
	TriggerAllowedMessage  = "allowed_message"

	OutboxPending  = "pending"
	OutboxAccepted = "accepted"
	OutboxFailed   = "failed"
	OutboxUnknown  = "unknown"
)

type ObserveResult struct {
	Fired      bool
	Digest     string
	DispatchID string
}

type OutboxItem struct {
	ID            string
	DispatchID    string
	OperationID   string
	Channel       string
	Recipient     string
	ContentDigest string
	Status        string
}

func NormalizeTriggerKind(kind string) (string, error) {
	switch strings.TrimSpace(kind) {
	case "", TriggerSchedule:
		return TriggerSchedule, nil
	case TriggerFileSetChanged, TriggerHTTPSnapshot, TriggerMetricThreshold, TriggerAllowedMessage:
		return kind, nil
	default:
		return "", fmt.Errorf("%w: triggerKind", ErrInvalid)
	}
}

func FileSetSnapshot(root string) string {
	h := sha256.New()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		fmt.Fprintf(h, "%s\t%d\t%d\n", filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func ObserveTrigger(store *SQLStore, writer string, job Job, snapshot string) (ObserveResult, error) {
	digest := strings.TrimSpace(snapshot)
	if digest == "" || store == nil {
		return ObserveResult{}, nil
	}
	if writer != WriterSQLite {
		return ObserveResult{Digest: digest}, nil
	}
	kind, err := NormalizeTriggerKind(job.TriggerKind)
	if err != nil || kind == TriggerSchedule {
		return ObserveResult{Digest: digest}, err
	}
	cursorKey := triggerCursorKey(job)
	prev, _ := store.getCursor(job.ID, cursorKey)
	if prev == digest {
		return ObserveResult{Digest: digest}, nil
	}
	id := ulid.Make().String()
	inserted, err := store.putDispatch(id, job.ID, digest+"/"+cursorKey)
	if err != nil {
		return ObserveResult{}, err
	}
	if err := store.putCursor(job.ID, cursorKey, digest); err != nil {
		return ObserveResult{}, err
	}
	return ObserveResult{Fired: inserted, Digest: digest, DispatchID: id}, nil
}

func triggerCursorKey(job Job) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(job.TriggerKind) + "\n" + strings.TrimSpace(job.TriggerSpec)))
	return "snapshot/" + hex.EncodeToString(sum[:8])
}

func RecordMissedWake(store *SQLStore, jobID string) (bool, error) {
	if store == nil || jobID == "" {
		return false, nil
	}
	prev, _ := store.getCursor(jobID, "missed")
	if prev != "" {
		return false, nil
	}
	return true, store.putCursor(jobID, "missed", time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *SQLStore) getCursor(jobID, key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM automation_cursors WHERE job_id=? AND cursor_key=?`, jobID, key).Scan(&value)
	return value, err
}

func (s *SQLStore) putCursor(jobID, key, value string) error {
	_, err := s.db.Exec(`INSERT INTO automation_cursors(job_id,cursor_key,value,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(job_id, cursor_key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		jobID, key, value, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLStore) putDispatch(id, jobID, digest string) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO automation_dispatches(id,job_id,event_digest,created_at) VALUES(?,?,?,?)`,
		id, jobID, digest, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *SQLStore) CountDispatches(jobID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM automation_dispatches WHERE job_id=?`, jobID).Scan(&n)
	return n, err
}

func (s *SQLStore) PutNotification(item OutboxItem) error {
	if item.ID == "" {
		item.ID = ulid.Make().String()
	}
	if item.Status == "" {
		item.Status = OutboxPending
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO notification_outbox(id,dispatch_id,operation_id,channel,recipient,content_digest,status,attempts,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		item.ID, item.DispatchID, item.OperationID, item.Channel, item.Recipient, item.ContentDigest, item.Status, 0, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLStore) ListPendingNotifications(limit int) ([]OutboxItem, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 32
	}
	rows, err := s.db.Query(`SELECT id,dispatch_id,operation_id,channel,recipient,content_digest,status FROM notification_outbox WHERE status=? ORDER BY created_at LIMIT ?`, OutboxPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var item OutboxItem
		if err := rows.Scan(&item.ID, &item.DispatchID, &item.OperationID, &item.Channel, &item.Recipient, &item.ContentDigest, &item.Status); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Scheduler) drainOutbox() {
	if s == nil {
		return
	}
	sqlStore, ok := s.store.(*SQLStore)
	if !ok {
		return
	}
	items, err := sqlStore.ListPendingNotifications(32)
	if err != nil || len(items) == 0 {
		return
	}
	for _, item := range items {
		body := outboxToastBody(s.store, item)
		if err := s.notify.Notify("Lunitide", body); err != nil {
			_ = sqlStore.SetNotificationStatus(item.DispatchID, item.Channel, item.Recipient, item.ContentDigest, OutboxFailed)
			continue
		}
		_ = sqlStore.SetNotificationStatus(item.DispatchID, item.Channel, item.Recipient, item.ContentDigest, OutboxAccepted)
	}
}

func outboxToastBody(store Repository, item OutboxItem) string {
	if strings.HasPrefix(item.ContentDigest, "missed-wake:") {
		id := strings.TrimPrefix(item.ContentDigest, "missed-wake:")
		if store != nil {
			if job, ok, err := store.GetJob(id); err == nil && ok {
				return fmt.Sprintf("自动化任务「%s」在停机期间可能错过了计划唤醒，请查看任务列表。", job.Name)
			}
		}
		return "有自动化任务在停机期间可能错过了计划唤醒，请查看任务列表。"
	}
	return "有未送达的自动化通知，请查看任务列表。"
}

func (s *SQLStore) SetNotificationStatus(dispatchID, channel, recipient, digest, status string) error {
	_, err := s.db.Exec(`UPDATE notification_outbox SET status=?, attempts=attempts+1 WHERE dispatch_id=? AND channel=? AND recipient=? AND content_digest=?`,
		status, dispatchID, channel, recipient, digest)
	return err
}
