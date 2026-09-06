package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/queueapp"
	"github.com/oklog/ulid/v2"
)

func TestQueueDeliveryLegacyOverCapacityBacklogPreservesNextBatch(t *testing.T) {
	f := newMessageFixture(t, "queue-old-backlog")
	ctx := context.Background()
	at := formatTime(time.Now().UTC())
	// Simulate a database written by the earlier non-atomic admission path.
	for i := range 7 {
		if _, err := f.store.db.Exec(`INSERT INTO queued_user_messages(id,session_id,seq,payload,status,mark,request_id,created_at,updated_at) VALUES(?,?,?,?,'queued','turn_boundary',?,?,?)`, ulid.Make().String(), f.sessionID, i+1, fmt.Sprintf("old input %d", i), fmt.Sprintf("old-%d", i), at, at); err != nil {
			t.Fatal(err)
		}
	}
	visible, err := f.store.ListQueued(ctx, f.sessionID)
	if err != nil || len(visible) != 5 {
		t.Fatalf("bounded list: %d %v", len(visible), err)
	}
	first, err := f.store.ClaimQueueDelivery(ctx, f.sessionID, "renderer")
	if err != nil || len(first.Items) != 5 || first.Items[4].Seq != 5 {
		t.Fatalf("first batch: %+v %v", first, err)
	}
	if _, err := f.store.RecoverQueueDelivery(ctx, f.sessionID, first.ID, "dismiss"); err != nil {
		t.Fatal(err)
	}
	second, err := f.store.ClaimQueueDelivery(ctx, f.sessionID, "renderer")
	if err != nil || len(second.Items) != 2 || second.Items[0].Seq != 6 || second.Items[1].Seq != 7 {
		t.Fatalf("dismissing one batch lost the remaining backlog: %+v %v", second, err)
	}
	if n := tableCount(t, f.store, "queued_user_messages"); n != 7 {
		t.Fatalf("backlog was truncated: %d", n)
	}
}

func TestQueueDeliveryAuditRollbackPermanentReplayAndScopeDeletion(t *testing.T) {
	for _, deleteProject := range []bool{false, true} {
		t.Run(map[bool]string{false: "session", true: "project"}[deleteProject], func(t *testing.T) {
			f := newMessageFixture(t, "queue-receipt")
			ctx := context.Background()
			svc := queueapp.New(f.store)
			if _, err := svc.Enqueue(ctx, f.sessionID, "", "retained input", "", "source-key"); err != nil {
				t.Fatal(err)
			}
			d, err := f.store.ClaimQueueDelivery(ctx, f.sessionID, "renderer")
			if err != nil {
				t.Fatal(err)
			}
			part := queueapp.DeliveryParts(d)[0]
			key := "queue:" + part.Key
			request := struct{ SessionID, Text string }{f.sessionID, part.Text}
			first, err := f.app.Append(ctx, key, "queue", request, message.Message{SessionID: f.sessionID, Text: part.Text})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.db.Exec(`UPDATE idempotency_records SET expires_at='2000-01-01T00:00:00Z' WHERE operation='message.append' AND idempotency_key=?`, key); err != nil {
				t.Fatal(err)
			}
			replay, err := f.app.Append(ctx, key, "queue", request, message.Message{SessionID: f.sessionID, Text: part.Text})
			if err != nil || replay.ID != first.ID {
				t.Fatalf("permanent replay: %+v %v", replay, err)
			}
			d, err = f.store.PrepareQueueDelivery(ctx, f.sessionID, d.ID, []string{first.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.db.Exec(`CREATE TRIGGER fail_queue_started BEFORE INSERT ON audit_events WHEN NEW.action='queue.consume' AND json_extract(NEW.metadata_json,'$.phase')='started' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
				t.Fatal(err)
			}
			if err = f.store.StartQueueDelivery(ctx, f.sessionID, d.ID, ulid.Make().String()); err == nil {
				t.Fatal("unaudited start accepted")
			}
			actual, err := f.store.GetQueueDelivery(ctx, f.sessionID, d.ID)
			if err != nil || actual.State != "prepared" || actual.Items[0].Status != "queued" {
				t.Fatalf("start failure did not roll back: %+v %v", actual, err)
			}
			if _, err = f.store.db.Exec(`DROP TRIGGER fail_queue_started`); err != nil {
				t.Fatal(err)
			}
			if deleteProject {
				err = f.store.DeleteProject(ctx, f.projectID)
			} else {
				err = f.store.DeleteSession(ctx, f.sessionID)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"queue_deliveries", "queue_delivery_items", "queued_user_messages"} {
				if n := tableCount(t, f.store, table); n != 0 {
					t.Fatalf("%s leaked %d scope records", table, n)
				}
			}
			var keys int
			if err = f.store.db.QueryRow(`SELECT count(*) FROM idempotency_records WHERE idempotency_key=?`, key).Scan(&keys); err != nil || keys != 0 {
				t.Fatalf("permanent key leaked deleted scope: %d %v", keys, err)
			}
			if _, err = f.store.GetQueueDelivery(ctx, f.sessionID, d.ID); !errors.Is(err, queueapp.ErrNotFound) {
				t.Fatalf("deleted receipt replayable: %v", err)
			}
		})
	}
}
