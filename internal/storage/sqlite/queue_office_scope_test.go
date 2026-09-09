package sqlite

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/queueapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

func TestQueueOfficeMigrationPreservesLegacyInputsAndReceipts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-office-queue.db")
	db := legacyBeforeMigration(t, path, "0145_")
	legacy := &Store{db: db, path: path, idEntropy: rand.Reader}
	projectID := createSessionProject(t, legacy, "legacy-office-queue-project", "Legacy Queue")
	sess, err := sessionapp.New(legacy, legacy).Create(ctx, "legacy-office-queue-session", "test", struct{ Title string }{"Legacy"}, session.Session{ProjectID: projectID, Title: "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	runID, queuedID, deliveryID := ulid.Make().String(), ulid.Make().String(), ulid.Make().String()
	at := formatTime(time.Now().UTC())
	if _, err = db.Exec(`INSERT INTO queued_user_messages(id,session_id,run_id,seq,payload,status,mark,request_id,created_at,updated_at) VALUES(?,?,?,1,'legacy retained','queued','turn_boundary','legacy-key',?,?)`, queuedID, sess.ID, runID, at, at); err != nil {
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
	d, err := upgraded.PendingQueueDelivery(ctx, sess.ID)
	if err != nil || d.ID != deliveryID || len(d.Items) != 1 || d.Items[0].Payload != "legacy retained" || d.Items[0].RunID != runID || d.Items[0].OfficeTaskID != "" {
		t.Fatalf("upgrade changed legacy receipt: %+v %v", d, err)
	}
	office := queueinput.WithOfficeTask(ctx, ulid.Make().String())
	if d, err := upgraded.PendingQueueDelivery(office, sess.ID); err != nil || d.ID != "" {
		t.Fatalf("Office consumed legacy receipt: %+v %v", d, err)
	}
	if _, err = queueapp.New(upgraded).Enqueue(office, sess.ID, "", "new task input", "", "new-task"); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedOfficeScopeKeepsOrdinaryAndOtherTaskInputsSeparate(t *testing.T) {
	f := newMessageFixture(t, "office-queue-scope")
	svc := queueapp.New(f.store)
	ordinary := context.Background()
	taskA, taskB, runID := ulid.Make().String(), ulid.Make().String(), ulid.Make().String()
	a, b := queueinput.WithOfficeTask(ordinary, taskA), queueinput.WithOfficeTask(ordinary, taskB)
	enqueue := func(ctx context.Context, key string) queueinput.Message {
		t.Helper()
		m, err := svc.Enqueue(ctx, f.sessionID, runID, key, "", key)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	old, ma, mb := enqueue(ordinary, "ordinary"), enqueue(a, "task-a"), enqueue(b, "task-b")
	if ma.RunID != runID || ma.OfficeTaskID != taskA {
		t.Fatalf("run/task identity changed: %+v", ma)
	}
	if _, err := svc.Enqueue(b, f.sessionID, runID, "task-a", "", "task-a"); !errors.Is(err, queueapp.ErrRequestReused) {
		t.Fatalf("cross-scope idempotency replay: %v", err)
	}
	for _, pair := range []struct {
		ctx  context.Context
		want queueinput.Message
	}{{ordinary, old}, {a, ma}, {b, mb}} {
		list, err := svc.List(pair.ctx, f.sessionID)
		if err != nil || len(list) != 1 || list[0].ID != pair.want.ID {
			t.Fatalf("scoped list=%+v %v", list, err)
		}
	}
	if _, err := svc.Withdraw(b, f.sessionID, ma.ID); !errors.Is(err, queueapp.ErrNotFound) {
		t.Fatalf("B withdrew A: %v", err)
	}
	if got, err := f.store.GetQueuedByRequest(b, f.sessionID, "task-a"); err != nil || got.ID != "" {
		t.Fatalf("B read A request: %+v %v", got, err)
	}
	rows, err := svc.Consume(b, f.sessionID)
	if err != nil || len(rows) != 1 || rows[0].ID != mb.ID {
		t.Fatalf("B consumed another scope: %+v %v", rows, err)
	}
	for _, pair := range []struct {
		ctx  context.Context
		want string
	}{{ordinary, old.ID}, {a, ma.ID}} {
		rows, err := svc.List(pair.ctx, f.sessionID)
		if err != nil || len(rows) != 1 || rows[0].ID != pair.want {
			t.Fatalf("B lost other input: %+v %v", rows, err)
		}
	}
	if _, err := svc.Withdraw(a, f.sessionID, ma.ID); err != nil {
		t.Fatal(err)
	}
	rows, err = svc.Consume(ordinary, f.sessionID)
	if err != nil || len(rows) != 1 || rows[0].ID != old.ID {
		t.Fatalf("ordinary input regression: %+v %v", rows, err)
	}
}

func TestOfficeQueueDeliverySurvivesRestartAndRejectsCrossTaskRecovery(t *testing.T) {
	f := newMessageFixture(t, "office-queue-restart")
	ordinary := context.Background()
	taskA, taskB := ulid.Make().String(), ulid.Make().String()
	a, b := queueinput.WithOfficeTask(ordinary, taskA), queueinput.WithOfficeTask(ordinary, taskB)
	svc := queueapp.New(f.store)
	ma, err := svc.Enqueue(a, f.sessionID, "", "original A instruction", "", "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Enqueue(b, f.sessionID, "", "original B instruction", "", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Enqueue(ordinary, f.sessionID, "", "original ordinary instruction", "", "ordinary"); err != nil {
		t.Fatal(err)
	}
	da, err := f.store.ClaimQueueDelivery(a, f.sessionID, "renderer")
	if err != nil || len(da.Items) != 1 || da.Items[0].ID != ma.ID {
		t.Fatalf("A claim %+v %v", da, err)
	}
	part := queueapp.DeliveryParts(da)[0]
	saved, err := f.app.Append(a, "queue:"+part.Key, "queue", struct{ SessionID, Text string }{f.sessionID, part.Text}, message.Message{SessionID: f.sessionID, Text: part.Text})
	if err != nil {
		t.Fatal(err)
	}
	da, err = f.store.PrepareQueueDelivery(a, f.sessionID, da.ID, []string{saved.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []context.Context{ordinary, b} {
		if _, err = f.store.GetQueueDelivery(wrong, f.sessionID, da.ID); !errors.Is(err, queueapp.ErrNotFound) {
			t.Fatalf("cross-scope read: %v", err)
		}
		if _, err = f.store.PrepareQueueDelivery(wrong, f.sessionID, da.ID, []string{saved.ID}); !errors.Is(err, queueapp.ErrNotFound) {
			t.Fatalf("cross-scope prepare: %v", err)
		}
		if err = f.store.StartQueueDelivery(wrong, f.sessionID, da.ID, ulid.Make().String()); !errors.Is(err, queueapp.ErrNotFound) {
			t.Fatalf("cross-scope start: %v", err)
		}
		for _, action := range []string{"resume", "dismiss", "mark-unknown", "handoff"} {
			if _, err = f.store.RecoverQueueDelivery(wrong, f.sessionID, da.ID, action); !errors.Is(err, queueapp.ErrNotFound) {
				t.Fatalf("cross-scope recovery %s: %v", action, err)
			}
		}
	}
	path := f.store.path
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ordinary, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.PendingQueueDelivery(a, f.sessionID)
	if err != nil || restored.ID != da.ID || restored.State != "prepared" || len(restored.MessageIDs) != 1 || restored.MessageIDs[0] != saved.ID {
		t.Fatalf("restart lost A receipt: %+v %v", restored, err)
	}
	if d, err := reopened.PendingQueueDelivery(b, f.sessionID); err != nil || d.ID != "" {
		t.Fatalf("B sees A receipt: %+v %v", d, err)
	}
	db, err := reopened.ClaimQueueDelivery(b, f.sessionID, "renderer")
	if err != nil || db.ID == da.ID || len(db.Items) != 1 || db.Items[0].OfficeTaskID != taskB {
		t.Fatalf("B mixed with A receipt: %+v %v", db, err)
	}
	legacy, err := reopened.ClaimQueueDelivery(ordinary, f.sessionID, "renderer")
	if err != nil || len(legacy.Items) != 1 || legacy.Items[0].OfficeTaskID != "" {
		t.Fatalf("ordinary mixed receipt: %+v %v", legacy, err)
	}
	stream := ulid.Make().String()
	if err = reopened.StartQueueDelivery(a, f.sessionID, da.ID, stream); err != nil {
		t.Fatal(err)
	}
	if err = reopened.FinishQueueDeliveries(b, f.sessionID, stream, true); err != nil {
		t.Fatal(err)
	}
	still, err := reopened.GetQueueDelivery(a, f.sessionID, da.ID)
	if err != nil || still.State != "started" {
		t.Fatalf("B settled A: %+v %v", still, err)
	}
	if err = reopened.FinishQueueDeliveries(a, f.sessionID, stream, false); err != nil {
		t.Fatal(err)
	}
	resumed, err := reopened.RecoverQueueDelivery(a, f.sessionID, da.ID, "resume")
	if err != nil || resumed.ID != da.ID || resumed.Items[0].Payload != "original A instruction" {
		t.Fatalf("same-scope recovery failed: %+v %v", resumed, err)
	}
}

func TestOfficeQueueScopeRejectsMixedReceiptAndMalformedTaskID(t *testing.T) {
	f := newMessageFixture(t, "office-queue-mixed")
	ordinary := context.Background()
	a := queueinput.WithOfficeTask(ordinary, ulid.Make().String())
	b := queueinput.WithOfficeTask(ordinary, ulid.Make().String())
	svc := queueapp.New(f.store)
	if _, err := svc.Enqueue(queueinput.WithOfficeTask(ordinary, "not-a-task"), f.sessionID, "", "invalid", "", "invalid"); err == nil {
		t.Fatal("malformed Office task stored")
	}
	if _, err := svc.Enqueue(a, f.sessionID, "", "a", "", "a"); err != nil {
		t.Fatal(err)
	}
	mb, err := svc.Enqueue(b, f.sessionID, "", "b", "", "b")
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.store.ClaimQueueDelivery(a, f.sessionID, "renderer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.db.Exec(`INSERT INTO queue_delivery_items(delivery_id,queued_id) VALUES(?,?)`, d.ID, mb.ID); err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []context.Context{ordinary, a, b} {
		if _, err = f.store.GetQueueDelivery(ctx, f.sessionID, d.ID); !errors.Is(err, queueapp.ErrNotFound) {
			t.Fatalf("mixed receipt exposed: %v", err)
		}
		if _, err = f.store.RecoverQueueDelivery(ctx, f.sessionID, d.ID, "dismiss"); !errors.Is(err, queueapp.ErrNotFound) {
			t.Fatalf("mixed receipt modified: %v", err)
		}
	}
}
