package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/messageapp"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/oklog/ulid/v2"
)

func officeArchiveMessageForTest(t *testing.T, e *Engine, sessionID, key string) string {
	t.Helper()
	m, err := e.messages.AppendAssistant(context.Background(), key, "test", sessionID, key, messageapp.AssistantUsage{})
	if err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func officeArchiveFileForTest(t *testing.T, e *Engine, sessionID, name, text string) []byte {
	t.Helper()
	data, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: name, Blocks: []content.Block{{Type: "paragraph", Text: text}}})
	if err != nil {
		t.Fatal(err)
	}
	folder, err := e.tools.SessionFolder(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(folder, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func officeArchiveCardForTest(t *testing.T, e *Engine, sessionID, messageID, name, taskID string) {
	t.Helper()
	e.appendMessageArtifacts(sessionID, messageID, []SessionArtifact{{Kind: "docx", Path: name, CallID: messageID, ToolName: "docx.gen", OfficeTaskID: taskID}})
	got := e.loadSessionArtifactsByMessage(sessionID)[messageID]
	if len(got) != 1 || got[0].OfficeTaskID != taskID {
		t.Fatalf("durable task binding: %+v", got)
	}
}

func officeArchiveVersionsForTest(t *testing.T, e *Engine, taskID string, names ...string) []domain.Version {
	t.Helper()
	vs, err := e.officeStudio.Store.ListOfficeVersions(context.Background(), taskID, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, v := range vs {
		got[v.Name] = true
	}
	if len(vs) != len(names) {
		t.Fatalf("task %s got %v versions, want %v", taskID, got, names)
	}
	for _, n := range names {
		if !got[n] {
			t.Fatalf("missing %s in %v", n, got)
		}
	}
	return vs
}

func TestOfficeArchiveSeparatesTaskIntervalsAndLateBoundTurns(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "archive-a")
	ma := officeArchiveMessageForTest(t, e, a.SessionID, "message-a")
	officeArchiveFileForTest(t, e, a.SessionID, "a.docx", "任务 A 的交付")
	officeArchiveCardForTest(t, e, a.SessionID, ma, "a.docx", "")
	b, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: a.SessionID, Title: "B", StartMessageID: ma}, "archive-b")
	if err != nil {
		t.Fatal(err)
	}
	mb := officeArchiveMessageForTest(t, e, a.SessionID, "message-b")
	officeArchiveFileForTest(t, e, a.SessionID, "b.docx", "任务 B 的交付")
	officeArchiveCardForTest(t, e, a.SessionID, mb, "b.docx", "")
	late := officeArchiveMessageForTest(t, e, a.SessionID, "message-a-late")
	officeArchiveFileForTest(t, e, a.SessionID, "a-late.docx", "较早的 A 任务在 B 创建后完成")
	officeArchiveCardForTest(t, e, a.SessionID, late, "a-late.docx", a.ID)
	if err = e.archiveOfficeTurnNow(ctx, a.SessionID, a.ID); err != nil {
		t.Fatal(err)
	}
	officeArchiveVersionsForTest(t, e, a.ID, "a.docx", "a-late.docx")
	officeArchiveVersionsForTest(t, e, b.ID)
	// A was just updated. An unbound completion still belongs to newly
	// created B, never whichever task has the most recent mutable update.
	if err = e.archiveOfficeTurnNow(ctx, a.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	officeArchiveVersionsForTest(t, e, b.ID, "b.docx")
	if err = e.syncOfficeArtifacts(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = e.syncOfficeArtifacts(ctx, b); err != nil {
		t.Fatal(err)
	}
	officeArchiveVersionsForTest(t, e, a.ID, "a.docx", "a-late.docx")
	officeArchiveVersionsForTest(t, e, b.ID, "b.docx")
	for _, task := range []domain.Task{a, b} {
		receipts, er := store.ListOfficeStepReceipts(ctx, task.ID, "")
		if er != nil {
			t.Fatal(er)
		}
		for _, r := range receipts {
			if r.RunID == "" || r.TaskID != task.ID {
				t.Fatalf("invalid receipt: %+v", r)
			}
		}
	}
}

func TestOfficeArchivePreservesFormalOwnershipAndExplicitReuse(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "formal-a")
	boundary := officeArchiveMessageForTest(t, e, a.SessionID, "formal-boundary")
	b, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: a.SessionID, Title: "B", StartMessageID: boundary}, "formal-b")
	if err != nil {
		t.Fatal(err)
	}
	data := officeArchiveFileForTest(t, e, a.SessionID, "formal.docx", "正式版本仍属于原任务")
	v, err := e.officeStudio.ImportLinked(ctx, a.ID, "", "formal.docx", "formal.docx", data, "", 0, "formal-version")
	if err != nil {
		t.Fatal(err)
	}
	m := officeArchiveMessageForTest(t, e, a.SessionID, "legacy-approval-result")
	officeArchiveCardForTest(t, e, a.SessionID, m, "formal.docx", "")
	if err = e.syncOfficeArtifacts(ctx, b); err != nil {
		t.Fatal(err)
	}
	officeArchiveVersionsForTest(t, e, b.ID)
	if err = e.syncOfficeArtifacts(ctx, a); err != nil {
		t.Fatal(err)
	}
	if got := officeArchiveVersionsForTest(t, e, a.ID, "formal.docx"); got[0].ID != v.ID {
		t.Fatal("formal version replaced")
	}
	// An explicit click can intentionally reuse an artifact from this same
	// chat. It publishes B's own version without taking A's history/head.
	if err = e.syncOfficeArtifacts(ctx, b, "formal.docx"); err != nil {
		t.Fatal(err)
	}
	if got := officeArchiveVersionsForTest(t, e, b.ID, "formal.docx"); got[0].ID == v.ID || got[0].SHA256 != v.SHA256 {
		t.Fatal("explicit reuse lost version isolation")
	}
	officeArchiveVersionsForTest(t, e, a.ID, "formal.docx")
}

func TestOfficeArchiveReusedPathCannotStealNewTaskBytes(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "replace-a")
	ma := officeArchiveMessageForTest(t, e, a.SessionID, "replace-message-a")
	officeArchiveFileForTest(t, e, a.SessionID, "report.docx", "A 的旧版")
	officeArchiveCardForTest(t, e, a.SessionID, ma, "report.docx", a.ID)
	b, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: a.SessionID, Title: "B", StartMessageID: ma}, "replace-b")
	if err != nil {
		t.Fatal(err)
	}
	mb := officeArchiveMessageForTest(t, e, a.SessionID, "replace-message-b")
	officeArchiveFileForTest(t, e, a.SessionID, "report.docx", "B 已替换路径中的内容")
	officeArchiveCardForTest(t, e, a.SessionID, mb, "report.docx", b.ID)
	if err = e.syncOfficeArtifacts(ctx, a); err != nil {
		t.Fatal(err)
	}
	officeArchiveVersionsForTest(t, e, a.ID)
	if err = e.syncOfficeArtifacts(ctx, b); err != nil {
		t.Fatal(err)
	}
	officeArchiveVersionsForTest(t, e, b.ID, "report.docx")
}

func TestOfficeArchiveSameTaskFileUpdateKeepsArtifactAndAcceptedHead(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "update-archive")
	m := officeArchiveMessageForTest(t, e, a.SessionID, "first-file")
	officeArchiveFileForTest(t, e, a.SessionID, "report.docx", "已接受的原始内容")
	officeArchiveCardForTest(t, e, a.SessionID, m, "report.docx", a.ID)
	if err := e.syncOfficeArtifacts(ctx, a); err != nil {
		t.Fatal(err)
	}
	first := officeArchiveVersionsForTest(t, e, a.ID, "report.docx")[0]
	if _, err := store.AcceptOfficeVersion(ctx, a.ID, first.ArtifactID, first.ID, 1); err != nil {
		t.Fatal(err)
	}
	// Importing a changed path creates a new version of the same artifact,
	// preserving the accepted version and the original immutable bytes.
	next := officeArchiveMessageForTest(t, e, a.SessionID, "changed-file")
	officeArchiveFileForTest(t, e, a.SessionID, "report.docx", "之后补充的新内容")
	officeArchiveCardForTest(t, e, a.SessionID, next, "report.docx", a.ID)
	if err := e.syncOfficeArtifacts(ctx, a); err != nil {
		t.Fatal(err)
	}
	versions := officeArchiveVersionsForTest(t, e, a.ID, "report.docx", "report.docx")
	heads, err := store.ListOfficeHeads(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 || heads[0].AcceptedVersionID != first.ID || heads[0].LatestVersionID == first.ID || versions[0].ArtifactID != first.ArtifactID {
		t.Fatalf("source update broke artifact/accepted head: %+v", heads)
	}
	receipts, err := store.ListOfficeStepReceipts(ctx, a.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	foundReceipt := false
	for _, r := range receipts {
		var result struct{ VersionID, MessageID string }
		if json.Unmarshal(r.Result, &result) == nil && result.VersionID == heads[0].LatestVersionID {
			foundReceipt = true
			if result.MessageID != next {
				t.Fatalf("new bytes attributed to old message: %+v", result)
			}
		}
	}
	if !foundReceipt {
		t.Fatal("missing new version's chat receipt")
	}
	if err := e.syncOfficeArtifacts(ctx, a); err != nil {
		t.Fatal(err)
	}
	officeArchiveVersionsForTest(t, e, a.ID, "report.docx", "report.docx")
}

func TestOfficeArchiveRejectsCrossSessionScopeAndMissingSelectedPath(t *testing.T) {
	e, _ := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "isolated-a")
	b := officeCreatedTask(t, e, "isolated-b")
	if err := e.archiveOfficeTurnNow(ctx, a.SessionID, b.ID); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("bound session escaped: %v", err)
	}
	forged := a
	forged.SessionID = b.SessionID
	if err := e.syncOfficeArtifacts(ctx, forged); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("supplied task session trusted: %v", err)
	}
	if err := e.syncOfficeArtifacts(domain.WithScope(ctx, "other-org"), a); err == nil {
		t.Fatal("organization escaped")
	}
	m := officeArchiveMessageForTest(t, e, a.SessionID, "isolated-message")
	officeArchiveFileForTest(t, e, a.SessionID, "private.docx", "A 会话专有内容")
	officeArchiveCardForTest(t, e, a.SessionID, m, "private.docx", a.ID)
	if err := e.syncOfficeArtifacts(ctx, b, "private.docx"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("selected path looked outside session: %v", err)
	}
	if err := e.archiveOfficeTurnNow(ctx, a.SessionID, ulid.Make().String()); err == nil {
		t.Fatal("invalid explicit binding silently fell back")
	}
	officeArchiveVersionsForTest(t, e, a.ID)
	officeArchiveVersionsForTest(t, e, b.ID)
}

func TestOfficeArchiveUnownedHistoryStopsAtNextTaskCreation(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	a := domain.Task{ID: ulid.Make().String(), CreatedAt: base}
	b := domain.Task{ID: ulid.Make().String(), CreatedAt: base.Add(time.Minute)}
	messageBefore := ulid.MustNew(ulid.Timestamp(base.Add(time.Second)), strings.NewReader(strings.Repeat("a", 10))).String()
	messageAfter := ulid.MustNew(ulid.Timestamp(base.Add(2*time.Minute)), strings.NewReader(strings.Repeat("b", 10))).String()
	if !officeArchiveMessageInTask(messageBefore, a, []domain.Task{b, a}) || officeArchiveMessageInTask(messageAfter, a, []domain.Task{b, a}) {
		t.Fatal("include-history successor did not bound old task")
	}
	if !officeArchiveItemInTask(messageAfter, SessionArtifact{OfficeTaskID: a.ID}, a, []domain.Task{a, b}) {
		t.Fatal("explicit late result lost original task")
	}
	if _, ok := normalizeSessionArtifact(SessionArtifact{Kind: "docx", Path: "a.docx", CallID: "a", ToolName: "docx.gen", OfficeTaskID: "invalid"}); ok {
		t.Fatal("malformed persisted owner became unbound")
	}
}

func TestOfficeArchiveSnapshotKeepsOldVersionsBeyondListCutoffs(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "large-archive")
	data := officeArchiveFileForTest(t, e, a.SessionID, "old.docx", "最早的正式归档仍应去重")
	first, err := e.officeStudio.ImportLinked(ctx, a.ID, "", "old.docx", "old.docx", data, "", 0, "old-version")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 1000; i++ {
		_, err = store.PublishOfficeVersion(ctx, domain.PublishRequest{Version: domain.Version{TaskID: a.ID, ArtifactID: first.ArtifactID, Kind: "docx", Name: "old.docx", ContentRef: "fixture", SHA256: strings.Repeat("b", 64), Size: 1, MediaType: "application/octet-stream"}, ExpectedHeadRevision: int64(i), IdempotencyKey: fmt.Sprintf("filler-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	m := officeArchiveMessageForTest(t, e, a.SessionID, "old-version-result")
	officeArchiveCardForTest(t, e, a.SessionID, m, "old.docx", a.ID)
	if err = e.syncOfficeArtifacts(ctx, a); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.ReadOfficeArchiveSnapshot(ctx, a.ID)
	if err != nil || len(snapshot.Versions) != 1001 {
		t.Fatalf("old version duplicated/lost: %d %v", len(snapshot.Versions), err)
	}
	if snapshot.Versions[0].ID != first.ID || snapshot.Versions[0].SourcePath != "old.docx" {
		t.Fatal("complete source metadata missing")
	}
	for i := 0; i < 501; i++ {
		if _, err = store.CreateOfficeTask(ctx, domain.Task{SessionID: a.SessionID, Title: "其他任务", StartMessageID: m}, fmt.Sprintf("sibling-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err = store.ReadOfficeArchiveSnapshot(ctx, a.ID)
	if err != nil || len(snapshot.Tasks) != 502 {
		t.Fatalf("task boundary list truncated: %d %v", len(snapshot.Tasks), err)
	}
	if _, err = store.ReadOfficeArchiveSnapshot(domain.WithScope(ctx, "other-org"), a.ID); err == nil {
		t.Fatal("archive snapshot bypassed scope")
	}
}

func TestOfficeArchiveConcurrentSyncIsBusyNotVersionConflict(t *testing.T) {
	e, _ := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "busy-sync")
	m := officeArchiveMessageForTest(t, e, a.SessionID, "busy-message")
	officeArchiveFileForTest(t, e, a.SessionID, "busy.docx", "需要导入才会碰到写入锁")
	officeArchiveCardForTest(t, e, a.SessionID, m, "busy.docx", a.ID)
	started, release, ended := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		ended <- e.officeStudio.Execute(ctx, a.ID, "running", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("hold not started")
	}
	err := e.syncOfficeArtifacts(ctx, a)
	if !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("overlap must be busy, not version conflict: %v", err)
	}
	if errors.Is(err, domain.ErrConflict) {
		t.Fatal("busy overlap leaked as version conflict")
	}
	if err = e.archiveOfficeTurnNow(ctx, a.SessionID, a.ID); err != nil {
		t.Fatalf("chat archive must ignore busy: %v", err)
	}
	close(release)
	if err = <-ended; err != nil {
		t.Fatal(err)
	}
}

func TestRetryOfficeBusyStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retryOfficeBusy(ctx, func() error { return domain.ErrBusy })
	if !errors.Is(err, context.Canceled) && !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("busy retry ignored cancellation: %v", err)
	}
}

func TestIgnoreOfficeBusyKeepsRealConflicts(t *testing.T) {
	if ignoreOfficeBusy(domain.ErrBusy) != nil {
		t.Fatal("busy must be ignored by deferred archive")
	}
	if !errors.Is(ignoreOfficeBusy(fmt.Errorf("同步文件 x.pptx：%w", domain.ErrConflict)), domain.ErrConflict) {
		t.Fatal("real conflict must still fail")
	}
}
