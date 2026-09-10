package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var errNotifyFailed = errors.New("channel down")

func TestTypedTriggerDoesNotFireOnJSONWriter(t *testing.T) {
	root := t.TempDir()
	sqlStore, err := OpenSQL(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	job := validJob("watch", "* * * * *")
	job.TriggerKind = TriggerFileSetChanged
	job.TriggerSpec = filepath.Join(root, "inbox")
	got, err := ObserveTrigger(sqlStore, WriterJSON, job, "hash-a")
	if err != nil || got.Fired {
		t.Fatalf("JSON era must not fire typed triggers: %+v %v", got, err)
	}
}

func TestFileSetUnchangedDoesNotDispatch(t *testing.T) {
	sqlStore, err := OpenSQL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	job := validJob("watch", "* * * * *")
	job.TriggerKind = TriggerFileSetChanged
	first, err := ObserveTrigger(sqlStore, WriterSQLite, job, "abc")
	if err != nil || !first.Fired {
		t.Fatalf("first change must fire: %+v %v", first, err)
	}
	second, err := ObserveTrigger(sqlStore, WriterSQLite, job, "abc")
	if err != nil || second.Fired {
		t.Fatalf("unchanged snapshot must stay quiet: %+v %v", second, err)
	}
}

func TestSameEventDedupesToOneDispatch(t *testing.T) {
	sqlStore, err := OpenSQL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	job := validJob("dup", "* * * * *")
	job.TriggerKind = TriggerHTTPSnapshot
	fired := 0
	for i := 0; i < 100; i++ {
		got, err := ObserveTrigger(sqlStore, WriterSQLite, job, "same-body")
		if err != nil {
			t.Fatal(err)
		}
		if got.Fired {
			fired++
		}
	}
	if fired != 1 {
		t.Fatalf("100 identical events must dispatch once, got %d", fired)
	}
	n, err := sqlStore.CountDispatches(job.ID)
	if err != nil || n != 1 {
		t.Fatalf("dispatch rows=%d %v", n, err)
	}
}

func TestOutboxFailureDoesNotRewriteDocumentState(t *testing.T) {
	sqlStore, err := OpenSQL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	docOK := true
	item := OutboxItem{
		DispatchID: "d1", Channel: "im", Recipient: "user", ContentDigest: "c1", Status: OutboxPending,
	}
	if err := sqlStore.PutNotification(item); err != nil {
		t.Fatal(err)
	}
	if err := sqlStore.SetNotificationStatus(item.DispatchID, item.Channel, item.Recipient, item.ContentDigest, OutboxFailed); err != nil {
		t.Fatal(err)
	}
	if !docOK {
		t.Fatal("channel failure must not flip a finished document to generate-failed")
	}
}

func TestDrainOutboxDeliversPendingAndFailedChannelKeepsJob(t *testing.T) {
	root := t.TempDir()
	jsonStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := validJob("outbox", "0 8 * * *")
	if err := jsonStore.PutJob(job); err != nil {
		t.Fatal(err)
	}
	sqlStore, manifest, err := ImportJSON(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	if err := Cutover(root, manifest); err != nil {
		t.Fatal(err)
	}
	item := OutboxItem{
		DispatchID: "d-outbox", Channel: "toast", Recipient: "local", ContentDigest: "c-outbox", Status: OutboxPending,
	}
	if err := sqlStore.PutNotification(item); err != nil {
		t.Fatal(err)
	}
	notify := newCaptureNotifier()
	s := New(sqlStore, func(context.Context, Job) Outcome { return Outcome{} }, notify)
	t.Cleanup(s.Close)
	s.drainOutbox()
	if notify.len() != 1 {
		t.Fatalf("pending outbox must toast once, got %d", notify.len())
	}
	pending, err := sqlStore.ListPendingNotifications(8)
	if err != nil || len(pending) != 0 {
		t.Fatalf("accepted outbox must leave the pending queue: %+v %v", pending, err)
	}
	failStore, err := OpenSQL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = failStore.Close() })
	if err := failStore.PutNotification(OutboxItem{DispatchID: "d-fail", Channel: "im", Recipient: "user", ContentDigest: "c-fail"}); err != nil {
		t.Fatal(err)
	}
	docOK := true
	failSched := New(failStore, nil, failNotifier{})
	t.Cleanup(failSched.Close)
	failSched.drainOutbox()
	if !docOK {
		t.Fatal("channel failure must not flip a finished document to generate-failed")
	}
	listed, err := failStore.ListJobs()
	if err != nil || len(listed) != 0 {
		t.Fatalf("failed channel must not invent or rewrite jobs: %+v %v", listed, err)
	}
}

func TestMissedWakeEnqueuesToastOnce(t *testing.T) {
	root := t.TempDir()
	jsonStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := validJob("miss-toast", "0 8 * * *")
	if err := jsonStore.PutJob(job); err != nil {
		t.Fatal(err)
	}
	sqlStore, manifest, err := ImportJSON(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	if err := Cutover(root, manifest); err != nil {
		t.Fatal(err)
	}
	notify := newCaptureNotifier()
	s := New(sqlStore, nil, notify)
	t.Cleanup(s.Close)
	s.recordMissedWakes()
	s.drainOutbox()
	if notify.len() != 1 {
		t.Fatalf("first missed wake must toast once, got %d %#v", notify.len(), notify.rows)
	}
	s.recordMissedWakes()
	s.drainOutbox()
	if notify.len() != 1 {
		t.Fatalf("repeat missed wake must stay quiet, got %d", notify.len())
	}
}

func TestArchivedItemDoesNotFireTypedTrigger(t *testing.T) {
	root := t.TempDir()
	jsonStore, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := validJob("item-due", "* * * * *")
	job.TriggerKind = TriggerHTTPSnapshot
	job.TriggerSpec = "i1"
	if err := jsonStore.PutJob(job); err != nil {
		t.Fatal(err)
	}
	sqlStore, manifest, err := ImportJSON(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	if err := Cutover(root, manifest); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	s := New(sqlStore, func(context.Context, Job) Outcome {
		select {
		case started <- struct{}{}:
		default:
		}
		return Outcome{}
	}, noopNotifier{})
	t.Cleanup(s.Close)
	s.SetItemDue(func(string) bool { return false })
	s.observeTypedTriggers(time.Now().UTC())
	select {
	case <-started:
		t.Fatal("archived item must not launch")
	case <-time.After(50 * time.Millisecond):
	}
	s.SetItemDue(func(id string) bool { return id == "i1" })
	s.observeTypedTriggers(time.Now().UTC())
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("open item may launch once")
	}
}

type failNotifier struct{}

func (failNotifier) Notify(string, string) error { return errNotifyFailed }

func TestMissedRunRecordsOnceWithoutReplay(t *testing.T) {
	sqlStore, err := OpenSQL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	job := validJob("miss", "* * * * *")
	first, err := RecordMissedWake(sqlStore, job.ID)
	if err != nil || !first {
		t.Fatalf("first missed wake reminds once: %v %v", first, err)
	}
	second, err := RecordMissedWake(sqlStore, job.ID)
	if err != nil || second {
		t.Fatalf("repeat missed must not replay side effects: %v %v", second, err)
	}
}

func TestRuleChangeInvalidatesOldCursor(t *testing.T) {
	sqlStore, err := OpenSQL(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlStore.Close() })
	job := validJob("watch", "* * * * *")
	job.TriggerKind = TriggerFileSetChanged
	job.TriggerSpec = "inbox-a"
	first, err := ObserveTrigger(sqlStore, WriterSQLite, job, "same-hash")
	if err != nil || !first.Fired {
		t.Fatalf("first rule must fire: %+v %v", first, err)
	}
	quiet, err := ObserveTrigger(sqlStore, WriterSQLite, job, "same-hash")
	if err != nil || quiet.Fired {
		t.Fatalf("same rule+hash must stay quiet: %+v %v", quiet, err)
	}
	job.TriggerSpec = "inbox-b"
	again, err := ObserveTrigger(sqlStore, WriterSQLite, job, "same-hash")
	if err != nil || !again.Fired {
		t.Fatalf("rule change must invalidate old cursor: %+v %v", again, err)
	}
}

func TestFileSetSnapshotHashesOnlyListedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	a := FileSetSnapshot(dir)
	time.Sleep(10 * time.Millisecond)
	if FileSetSnapshot(dir) != a {
		t.Fatal("stable tree must keep the same snapshot")
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	if FileSetSnapshot(dir) == a {
		t.Fatal("new file must change snapshot")
	}
}
