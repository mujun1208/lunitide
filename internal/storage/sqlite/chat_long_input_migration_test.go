package sqlite

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/queueapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

func TestChatLongInputMigrationPreservesHistoryFTSAndQueueReceipts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-long-chat.db")
	db := legacyBeforeMigration(t, path, "0146_")
	legacy := &Store{db: db, path: path, idEntropy: rand.Reader}
	projectID := createSessionProject(t, legacy, "long-upgrade-project", "Legacy Long Input")
	sess, err := sessionapp.New(legacy, legacy).Create(ctx, "long-upgrade-session", "test", struct{ Title string }{"Legacy"}, session.Session{ProjectID: projectID, Title: "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	f := messageFixture{legacy, newMessageApp(t, legacy), projectID, sess.ID}
	old := f.append(t, "old-message-replay", "Original searchable transcript. 保留历史")
	queuedID, deliveryID, officeID := ulid.Make().String(), ulid.Make().String(), ulid.Make().String()
	at := formatTime(time.Now().UTC())
	payload := strings.Repeat("旧", 8000)
	if _, err = db.Exec(`INSERT INTO queued_user_messages(id,session_id,seq,payload,status,mark,request_id,created_at,updated_at,office_task_id) VALUES(?,?,1,?,'queued','turn_boundary','legacy-long',?,?,?)`, queuedID, sess.ID, payload, at, at, officeID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO queue_deliveries(id,session_id,consumer,state,created_at,updated_at) VALUES(?,?,'renderer','claimed',?,?)`, deliveryID, sess.ID, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO queue_delivery_items(delivery_id,queued_id) VALUES(?,?)`, deliveryID, queuedID); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	u := messageFixture{upgraded, newMessageApp(t, upgraded), projectID, sess.ID}
	if replay := u.append(t, "old-message-replay", old.Text); replay.ID != old.ID || replay.Text != old.Text {
		t.Fatal("old idempotent message changed")
	}
	var indexed int
	if err = upgraded.db.QueryRow(`SELECT COUNT(*) FROM message_fts WHERE message_id=? AND text=?`, old.ID, old.Text).Scan(&indexed); err != nil || indexed != 1 {
		t.Fatalf("FTS changed: count=%d err=%v", indexed, err)
	}
	scope := queueinput.WithOfficeTask(ctx, officeID)
	d, err := upgraded.PendingQueueDelivery(scope, sess.ID)
	if err != nil || d.ID != deliveryID || len(d.Items) != 1 || d.Items[0].Payload != payload || d.Items[0].OfficeTaskID != officeID {
		t.Fatalf("queue receipt changed: err=%v", err)
	}
	parts := queueapp.DeliveryParts(d)
	if len(parts) != 4 || parts[3].Key != queuedID+":3" {
		t.Fatal("old queue split changed")
	}
	rows, err := upgraded.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("foreign key was broken by migration")
	}
	rows.Close()
	long := strings.Repeat("😀", message.MaxRunes)
	created := u.append(t, "max-after-upgrade", long)
	if created.Text != long {
		t.Fatal("upgrade clipped new long message")
	}
	if err = upgraded.db.QueryRow(`SELECT COUNT(*) FROM message_fts WHERE message_id=? AND text=?`, created.ID, long).Scan(&indexed); err != nil || indexed != 1 {
		t.Fatalf("new FTS trigger missing: %d %v", indexed, err)
	}
}

func TestChatLongInputStorageReplayPaginationAndQueue(t *testing.T) {
	f := newMessageFixture(t, "long-input-storage")
	ctx := context.Background()
	// JSON escaped control characters are the largest response for a legal
	// input: the receipt and default list budget must retain all 32768 runes.
	text := strings.Repeat("\u0001", message.MaxRunes)
	first := f.append(t, "max-escaped", text)
	if first.Text != text {
		t.Fatal("input changed")
	}
	if replay := f.append(t, "max-escaped", text); replay.ID != first.ID || replay.Text != text {
		t.Fatal("large receipt did not replay")
	}
	page, err := f.app.List(ctx, messageapp.PageRequest{SessionID: f.sessionID, RequestID: ulid.Make().String()})
	if err != nil || len(page.Items) != 1 || page.Items[0].Text != text {
		t.Fatalf("long message not reloadable: %v", err)
	}
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) <= 65536 || len(encoded) > messageapp.MaxByteBudget {
		t.Fatalf("unexpected encoded page length %d %v", len(encoded), err)
	}
	if _, err = f.app.Append(ctx, "too-long", "test", struct{ Text string }{text + "a"}, message.Message{SessionID: f.sessionID, Text: text + "a"}); err == nil {
		t.Fatal("oversize accepted")
	}
	svc := queueapp.New(f.store)
	for i := 0; i < queueinput.MaxQueuedPerSession; i++ {
		qtext := strings.Repeat("🙂", message.MaxRunes)
		q, err := svc.Enqueue(ctx, f.sessionID, "", qtext, "", string(rune('a'+i)))
		if err != nil || q.Payload != qtext {
			t.Fatalf("long queue input rejected: %v", err)
		}
	}
	d, err := f.store.ClaimQueueDelivery(ctx, f.sessionID, "renderer")
	if err != nil || len(d.Items) != 5 || len(queueapp.DeliveryParts(d)) != 5 {
		t.Fatalf("long queue lost items: %v", err)
	}
	if _, err = svc.Enqueue(ctx, f.sessionID, "", strings.Repeat("a", message.MaxRunes+1), "", "overflow"); err != queueapp.ErrPayloadInvalid {
		t.Fatalf("oversize queue input admitted: %v", err)
	}
}
