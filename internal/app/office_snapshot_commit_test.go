package app

import (
	"context"
	"errors"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type officeSnapshotFaultStore struct {
	*storage.Store
	failTaskRead, markUpdate, committed bool
	snapshotReads, fallbackReads        int
}

func (s *officeSnapshotFaultStore) ReadOfficeSnapshot(context.Context, string) (domain.Snapshot, error) {
	s.snapshotReads++
	return domain.Snapshot{}, errors.New("injected display snapshot failure")
}
func (s *officeSnapshotFaultStore) GetOfficeTask(ctx context.Context, id string) (domain.Task, error) {
	if s.committed && s.snapshotReads > 0 {
		s.fallbackReads++
		if ctx.Err() != nil {
			return domain.Task{}, errors.New("fallback inherited cancelled request")
		}
		if s.failTaskRead {
			return domain.Task{}, errors.New("injected task read failure")
		}
	}
	return s.Store.GetOfficeTask(ctx, id)
}
func (s *officeSnapshotFaultStore) UpdateOfficeTask(ctx context.Context, task domain.Task, revision int64) (domain.Task, error) {
	out, err := s.Store.UpdateOfficeTask(ctx, task, revision)
	if err == nil && s.markUpdate {
		s.committed = true
	}
	return out, err
}
func (s *officeSnapshotFaultStore) FinishOfficeTask(ctx context.Context, task domain.Task, revision int64, receipt domain.StepReceipt) (domain.Task, error) {
	out, err := s.Store.FinishOfficeTask(ctx, task, revision, receipt)
	if err == nil {
		s.committed = true
	}
	return out, err
}

type officeCommittedDetailForTest struct {
	Task                                             domain.Task
	Artifacts, Steps, Sources                        []any
	Committed, SnapshotIncomplete, TaskSnapshotStale bool
	LoadNotice                                       string
}

func TestOfficeMutationKeepsCommitWhenFirstSnapshotReadFails(t *testing.T) {
	for _, failTaskRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "task_read_available", true: "all_display_reads_fail"}[failTaskRead], func(t *testing.T) {
			e, store := officeEngineFixture(t)
			task := officeCreatedTask(t, e, "committed-update")
			fault := &officeSnapshotFaultStore{Store: store, failTaskRead: failTaskRead, markUpdate: true}
			e.officeStudio.Store = fault
			response := officeCall(t, e, "office.task.update", "", map[string]any{"taskId": task.ID, "title": "确实已更新", "goal": "保存后的目标", "expectedRevision": task.Revision})
			if !response.OK {
				t.Fatalf("committed write reported as failed: %+v", response.Error)
			}
			var detail officeCommittedDetailForTest
			if err := decodeResponsePayload(response.Payload, &detail); err != nil {
				t.Fatal(err)
			}
			actual, err := store.GetOfficeTask(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !detail.Committed || !detail.SnapshotIncomplete || detail.TaskSnapshotStale != failTaskRead || detail.LoadNotice == "" || detail.Task.ID != task.ID || detail.Task.Title != actual.Title || detail.Task.Revision != actual.Revision {
				t.Fatalf("commit/recovery not explicit: %+v actual=%+v", detail, actual)
			}
			if fault.snapshotReads != 1 || fault.fallbackReads != 1 || actual.Revision != task.Revision+1 {
				t.Fatalf("write repeated or wrong read count: %+v", fault)
			}
			// The ordinary read endpoint still reports the underlying read
			// failure; only an actually completed mutation receives a receipt.
			fault.failTaskRead = false
			read := officeCall(t, e, "office.task.get", "", map[string]any{"taskId": task.ID})
			if read.OK {
				t.Fatal("failed ordinary read mislabeled committed")
			}
		})
	}
}

func TestOfficeFailedMutationNeverProducesCommittedSnapshot(t *testing.T) {
	e, store := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "failed-update")
	fault := &officeSnapshotFaultStore{Store: store, markUpdate: true}
	e.officeStudio.Store = fault
	response := officeCall(t, e, "office.task.update", "", map[string]any{"taskId": task.ID, "title": "不应更新", "goal": "失败请求", "expectedRevision": task.Revision + 1})
	if response.OK || response.Error.Code != "OFFICE_VERSION_CONFLICT" || fault.snapshotReads != 0 || fault.committed {
		t.Fatalf("real mutation error hidden: %+v fault=%+v", response, fault)
	}
	actual, err := store.GetOfficeTask(context.Background(), task.ID)
	if err != nil || actual.Revision != task.Revision || actual.Title != task.Title {
		t.Fatalf("failed write changed task: %+v %v", actual, err)
	}
}

func TestOfficePatchCommitFallbackDoesNotInventFreshTaskState(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "committed-patch")
	v, err := e.officeStudio.Generate(ctx, task.ID, "报告.docx", content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "报告", Blocks: []content.Block{{Type: "paragraph", Text: "原文"}}}, "first-version")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := e.officeStudio.Preview(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	var node content.Node
	for _, n := range preview.Nodes {
		if n.Text == "原文" {
			node = n
		}
	}
	if node.ID == "" {
		t.Fatal("missing real document node")
	}
	fault := &officeSnapshotFaultStore{Store: store, failTaskRead: true}
	e.officeStudio.Store = fault
	response := officeCall(t, e, "office.artifact.patch", "committed-patch-version", map[string]any{"taskId": task.ID, "artifactId": v.ArtifactID, "baseVersionId": v.ID, "expectedRevision": 1, "nodeId": node.ID, "nodeDigest": node.Digest, "text": "修改已保存"})
	if !response.OK {
		t.Fatalf("published patch reported failed: %+v", response.Error)
	}
	var detail officeCommittedDetailForTest
	if err = decodeResponsePayload(response.Payload, &detail); err != nil {
		t.Fatal(err)
	}
	actual, err := store.GetOfficeTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Committed || !detail.SnapshotIncomplete || !detail.TaskSnapshotStale || detail.Task.Status != task.Status || detail.Task.Revision != task.Revision || actual.Status != "succeeded" || len(versions) != 2 {
		t.Fatalf("fallback invented state or repeated publish: %+v actual=%+v versions=%d", detail, actual, len(versions))
	}
	if len(detail.Artifacts) != 0 || detail.LoadNotice == "" {
		t.Fatal("missing explicit incomplete snapshot")
	}
}
