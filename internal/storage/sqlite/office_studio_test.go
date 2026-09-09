package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
)

func officeFixture(t *testing.T) (*Store, officestudio.Task) {
	t.Helper()
	s := openRuntimeStore(t)
	task, err := s.CreateOfficeTask(context.Background(), officestudio.Task{SessionID: rtSessionULID, Title: "季度报告", Goal: "生成报告并核对金额"}, "create-task")
	if err != nil {
		t.Fatal(err)
	}
	return s, task
}

func officeVersion(taskID, artifactID string) officestudio.Version {
	return officestudio.Version{TaskID: taskID, ArtifactID: artifactID, Kind: "docx", Name: "报告.docx", ContentRef: "office/blobs/test.docx", SHA256: strings.Repeat("a", 64), Size: 120, MediaType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Spec: json.RawMessage(`{"schemaVersion":1}`), Index: json.RawMessage(`{"nodes":[]}`)}
}

func officePublish(t *testing.T, s *Store, task officestudio.Task, artifact, key string) officestudio.Version {
	t.Helper()
	v, err := s.PublishOfficeVersion(context.Background(), officestudio.PublishRequest{Version: officeVersion(task.ID, artifact), IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOfficeTaskIdempotencyRevisionAndCheckpoint(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	retry, err := s.CreateOfficeTask(ctx, officestudio.Task{SessionID: task.SessionID, Title: task.Title, Goal: task.Goal}, "create-task")
	if err != nil || retry.ID != task.ID {
		t.Fatalf("idempotent creation: %+v %v", retry, err)
	}
	if _, err = s.CreateOfficeTask(ctx, officestudio.Task{SessionID: task.SessionID, Title: "Changed"}, "create-task"); !errors.Is(err, officestudio.ErrConflict) {
		t.Fatalf("payload reused key: %v", err)
	}
	task.Status = "running"
	task.RunID = ulid.Make().String()
	task.Checkpoint = json.RawMessage(`{"completed":["inspect"],"next":"render"}`)
	task, err = s.UpdateOfficeTask(ctx, task, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateOfficeTask(ctx, task, 1); !errors.Is(err, officestudio.ErrConflict) {
		t.Fatalf("stale writer: %v", err)
	}
	task.Status = "cancelled"
	task, err = s.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := s.GetOfficeTask(ctx, task.ID)
	if err != nil || string(recovered.Checkpoint) != string(task.Checkpoint) || recovered.Status != "cancelled" {
		t.Fatalf("checkpoint: %+v %v", recovered, err)
	}
	task.Status = "succeeded"
	if _, err = s.UpdateOfficeTask(ctx, task, task.Revision); !errors.Is(err, officestudio.ErrInvalid) {
		t.Fatalf("cancel cannot become succeeded: %v", err)
	}
	events, err := s.ListOfficeEvents(ctx, task.ID, 100)
	if err != nil || len(events) != 3 {
		t.Fatalf("events: %d %v", len(events), err)
	}
}

func TestOfficeVersionsAtomicCASAcceptanceAndImmutableHistory(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	artifact := ulid.Make().String()
	v1 := officePublish(t, s, task, artifact, "first")
	h, err := s.AcceptOfficeVersion(ctx, task.ID, artifact, v1.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	results := make(chan officestudio.Version, 2)
	for _, key := range []string{"writer-a", "writer-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			v := officeVersion(task.ID, artifact)
			v.BaseVersionID = v1.ID
			v.SHA256 = strings.Repeat("b", 64)
			out, e := s.PublishOfficeVersion(ctx, officestudio.PublishRequest{Version: v, ExpectedHeadRevision: h.Revision, IdempotencyKey: key})
			errs <- e
			results <- out
		}(key)
	}
	wg.Wait()
	close(errs)
	close(results)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, officestudio.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("writers success=%d conflicts=%d", success, conflict)
	}
	versions, err := s.ListOfficeVersions(ctx, task.ID, artifact)
	if err != nil || len(versions) != 2 {
		t.Fatalf("versions=%d %v", len(versions), err)
	}
	heads, err := s.ListOfficeHeads(ctx, task.ID)
	if err != nil || len(heads) != 1 || heads[0].AcceptedVersionID != v1.ID || heads[0].LatestVersionID != versions[0].ID {
		t.Fatalf("separate heads: %+v %v", heads, err)
	}
	for _, query := range []string{`UPDATE artifact_versions SET content_ref='bad' WHERE id=?`, `DELETE FROM artifact_versions WHERE id=?`, `UPDATE office_version_metadata SET spec_json='{}' WHERE version_id=?`, `DELETE FROM office_version_metadata WHERE version_id=?`} {
		if _, err = s.db.Exec(query, v1.ID); err == nil {
			t.Fatalf("immutable history accepted %s", query)
		}
	}
	var derivations int
	if err = s.db.QueryRow(`SELECT count(*) FROM artifact_derivations WHERE derived_from_version=?`, v1.ID).Scan(&derivations); err != nil || derivations != 1 {
		t.Fatalf("shared derivation: %d %v", derivations, err)
	}
}

func TestOfficePublishIdempotencyAndScope(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	r := officestudio.PublishRequest{Version: officeVersion(task.ID, ""), IdempotencyKey: "publish"}
	first, err := s.PublishOfficeVersion(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.PublishOfficeVersion(ctx, r)
	if err != nil || first.ID != retry.ID {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	r.Version.Name = "other.docx"
	if _, err = s.PublishOfficeVersion(ctx, r); !errors.Is(err, officestudio.ErrConflict) {
		t.Fatalf("idempotency payload conflict: %v", err)
	}
	other, err := s.CreateOfficeTask(ctx, officestudio.Task{SessionID: task.SessionID, Title: "second"}, "other")
	if err != nil {
		t.Fatal(err)
	}
	r.Version = officeVersion(other.ID, first.ArtifactID)
	r.IdempotencyKey = "stolen"
	r.ExpectedHeadRevision = 1
	if _, err = s.PublishOfficeVersion(ctx, r); !errors.Is(err, officestudio.ErrScope) {
		t.Fatalf("artifact ownership conflict: %v", err)
	}
	all, err := s.ListOfficeVersions(ctx, other.ID, "")
	if err != nil || len(all) != 0 {
		t.Fatalf("failed publication leaked metadata: %v %v", all, err)
	}
}

func TestOfficeValidationAcceptanceMissingChecksAndLateReports(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	v := officePublish(t, s, task, ulid.Make().String(), "v")
	at := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	report, err := s.AddOfficeValidation(ctx, officestudio.Validation{VersionID: v.ID, SHA256: v.SHA256, Validator: "native-v1", CreatedAt: at, Checks: []officestudio.Check{{ID: "package", Status: "passed", Required: true}, {ID: "render", Status: "unsupported", Required: true}}})
	if err != nil || report.Quality != "partial" {
		t.Fatalf("missing renderer: %+v %v", report, err)
	}
	if _, err = s.AcceptOfficeVersion(ctx, task.ID, v.ArtifactID, v.ID, 1); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOfficeVersion(ctx, v.ID)
	if err != nil || got.Quality != "partial" {
		t.Fatalf("accepted draft falsely verified: %+v %v", got, err)
	}
	second, err := s.AddOfficeValidation(ctx, officestudio.Validation{VersionID: v.ID, SHA256: v.SHA256, Validator: "native-v1", CreatedAt: at.Add(time.Second), Checks: []officestudio.Check{{ID: "package", Status: "passed", Required: true}}})
	if err != nil || second.Quality == "passed" {
		t.Fatalf("required render silently dropped: %+v %v", second, err)
	}
	latest, err := s.AddOfficeValidation(ctx, officestudio.Validation{VersionID: v.ID, SHA256: v.SHA256, Validator: "native-v2", CreatedAt: at.Add(2 * time.Second), Checks: []officestudio.Check{{ID: "package", Status: "passed", Required: true}, {ID: "render", Status: "passed", Required: true}}})
	if err != nil || latest.Quality != "passed" {
		t.Fatalf("full report: %+v %v", latest, err)
	}
	if _, err = s.AddOfficeValidation(ctx, officestudio.Validation{VersionID: v.ID, SHA256: v.SHA256, Validator: "late-v1", CreatedAt: at.Add(-time.Second), Checks: []officestudio.Check{{ID: "package", Status: "failed", Required: true}}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetOfficeVersion(ctx, v.ID)
	if err != nil || got.Quality != "passed" {
		t.Fatalf("late evidence overwrote new projection: %+v %v", got, err)
	}
	if _, err = s.AddOfficeValidation(ctx, officestudio.Validation{VersionID: v.ID, SHA256: strings.Repeat("c", 64), Validator: "bad"}); !errors.Is(err, officestudio.ErrConflict) {
		t.Fatalf("wrong version bytes accepted: %v", err)
	}
	if _, err = s.db.Exec(`UPDATE office_validation_runs SET quality='passed' WHERE id=?`, report.ID); err == nil {
		t.Fatal("rewritable validation proof")
	}
}

func TestOfficeEvidenceStalenessDoesNotOverwriteAcceptedVersion(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	a := officePublish(t, s, task, ulid.Make().String(), "source")
	b := officePublish(t, s, task, ulid.Make().String(), "report")
	c := officePublish(t, s, task, ulid.Make().String(), "deck")
	for _, pair := range [][2]string{{a.ID, b.ID}, {b.ID, c.ID}} {
		if err := s.AddOfficeEvidenceEdge(ctx, officestudio.EvidenceEdge{TaskID: task.ID, SourceVersionID: pair[0], TargetVersionID: pair[1], Metric: json.RawMessage(`{"rawValue":"100.50","unit":"CNY","period":"2026-Q3"}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddOfficeEvidenceEdge(ctx, officestudio.EvidenceEdge{TaskID: task.ID, SourceVersionID: c.ID, TargetVersionID: a.ID}); !errors.Is(err, officestudio.ErrInvalid) {
		t.Fatalf("evidence cycle: %v", err)
	}
	if _, err := s.AcceptOfficeVersion(ctx, task.ID, c.ArtifactID, c.ID, 1); err != nil {
		t.Fatal(err)
	}
	n, err := s.MarkOfficeDependentsStale(ctx, a.ID)
	if err != nil || n != 2 {
		t.Fatalf("stale propagation: %d %v", n, err)
	}
	for _, id := range []string{b.ID, c.ID} {
		v, err := s.GetOfficeVersion(ctx, id)
		if err != nil || v.Quality != "stale" {
			t.Fatalf("dependent: %+v %v", v, err)
		}
	}
	heads, err := s.ListOfficeHeads(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range heads {
		if h.ArtifactID == c.ArtifactID && h.AcceptedVersionID != c.ID {
			t.Fatal("accepted version changed during stale propagation")
		}
	}
	if _, err = s.AddOfficeValidation(ctx, officestudio.Validation{VersionID: c.ID, SHA256: c.SHA256, Validator: "package", Checks: []officestudio.Check{{ID: "package", Status: "passed", Required: true}}}); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetOfficeVersion(ctx, c.ID)
	if err != nil || v.Quality != "stale" {
		t.Fatal("revalidation erased changed source")
	}
}

func TestOfficeStepReceiptsEnforceRunAndInputIdentity(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	task.Status = "running"
	task.RunID = ulid.Make().String()
	var err error
	task, err = s.UpdateOfficeTask(ctx, task, 1)
	if err != nil {
		t.Fatal(err)
	}
	r := officestudio.StepReceipt{TaskID: task.ID, RunID: task.RunID, StepKey: "publish", IdempotencyKey: "op1", InputDigest: strings.Repeat("a", 64), State: "started"}
	if err = s.AppendOfficeStepReceipt(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.State = "succeeded"
	r.Result = json.RawMessage(`{"versionId":"saved"}`)
	for i := 0; i < 2; i++ {
		if err = s.AppendOfficeStepReceipt(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	r.State = "failed"
	if err = s.AppendOfficeStepReceipt(ctx, r); !errors.Is(err, officestudio.ErrConflict) {
		t.Fatalf("terminal outcome overwritten: %v", err)
	}
	r.State = "succeeded"
	r.RunID = ulid.Make().String()
	if err = s.AppendOfficeStepReceipt(ctx, r); !errors.Is(err, officestudio.ErrConflict) {
		t.Fatalf("late run: %v", err)
	}
	all, err := s.ListOfficeStepReceipts(ctx, task.ID, task.RunID)
	if err != nil || len(all) != 2 {
		t.Fatalf("receipts: %d %v", len(all), err)
	}
}

func TestOfficeFinishTaskAndReceiptCommitAtomically(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	task.Status = "running"
	task.RunID = ulid.Make().String()
	var err error
	task, err = s.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	want := task
	want.Status = "succeeded"
	receipt := officestudio.StepReceipt{TaskID: task.ID, RunID: task.RunID, StepKey: "finish", IdempotencyKey: "finish", InputDigest: strings.Repeat("a", 64), State: "succeeded"}
	if _, err = s.db.Exec(`CREATE TEMP TRIGGER office_test_fail_finish BEFORE INSERT ON office_step_receipts BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishOfficeTask(ctx, want, task.Revision, receipt); err == nil {
		t.Fatal("injected failure ignored")
	}
	actual, err := s.GetOfficeTask(ctx, task.ID)
	if err != nil || actual.Status != "running" || actual.Revision != task.Revision {
		t.Fatalf("false terminal survived rolled back receipt: %+v %v", actual, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER office_test_fail_finish`); err != nil {
		t.Fatal(err)
	}
	actual, err = s.FinishOfficeTask(ctx, want, task.Revision, receipt)
	if err != nil || actual.Status != "succeeded" {
		t.Fatalf("finish retry: %+v %v", actual, err)
	}
	replayed, err := s.FinishOfficeTask(ctx, actual, actual.Revision, receipt)
	if err != nil || replayed.Revision != actual.Revision {
		t.Fatalf("terminal retry changed task: %+v %v", replayed, err)
	}
}

func TestOfficeCancelledRunCannotPublishLateNewVersion(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	task.Status = "running"
	var err error
	task, err = s.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v := officePublish(t, s, task, ulid.Make().String(), "before-cancel")
	task.Status = "cancelling"
	task, err = s.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	next := officeVersion(task.ID, v.ArtifactID)
	next.BaseVersionID = v.ID
	if _, err = s.PublishOfficeVersion(ctx, officestudio.PublishRequest{Version: next, ExpectedHeadRevision: 1, IdempotencyKey: "late"}); !errors.Is(err, officestudio.ErrConflict) {
		t.Fatalf("late publication during cancel: %v", err)
	}
	versions, err := s.ListOfficeVersions(ctx, task.ID, v.ArtifactID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("cancel overwrote history: %+v %v", versions, err)
	}
}

func TestOfficeScopeAndRetainedFilesAfterSessionDeletion(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	v := officePublish(t, s, task, ulid.Make().String(), "retained")
	foreign := officestudio.WithScope(ctx, ulid.Make().String())
	if _, err := s.GetOfficeTask(foreign, task.ID); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("task cross-org: %v", err)
	}
	if _, err := s.GetOfficeVersion(foreign, v.ID); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("version cross-org: %v", err)
	}
	all, err := s.ListOfficeTasks(foreign, "", 100)
	if err != nil || len(all) != 0 {
		t.Fatalf("cross-org list: %+v %v", all, err)
	}
	if _, err = s.CreateOfficeTask(foreign, officestudio.Task{SessionID: task.SessionID, Title: task.Title, Goal: task.Goal}, "create-task"); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("idempotency cross-org: %v", err)
	}
	if _, err = s.db.Exec(`DELETE FROM sessions WHERE id=?`, task.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetOfficeVersion(ctx, v.ID); err != nil {
		t.Fatalf("retained file lost after chat delete: %v", err)
	}
	for _, scope := range []string{"", officestudio.Scope(foreign)} {
		err = s.AuthorizeDataResource(ctx, "trace:artifact_version", v.ID, scope)
		if scope == "" && err != nil {
			t.Fatalf("own retained artifact: %v", err)
		}
		if scope != "" && !errors.Is(err, org.ErrCrossOrgAccess) {
			t.Fatalf("foreign retained artifact: %v", err)
		}
	}
}

func TestOfficeOrganizationSnapshotIsolatedFromPersonalAndRebinding(t *testing.T) {
	s, personal := officeFixture(t)
	ctx := context.Background()
	created, err := org.NewService(org.NewGate(s.OrgStorage()), nil).CreateOrg(ctx, "Organization")
	if err != nil {
		t.Fatal(err)
	}
	orgCtx := officestudio.WithScope(ctx, created.OrgID)
	projectID, sessionID := ulid.Make().String(), ulid.Make().String()
	if _, err = s.db.Exec(`INSERT INTO projects(id,name,project_code,org_id,created_at,updated_at) VALUES(?,?,?, ?,?,?)`, projectID, "Organization", "ITM00002", created.OrgID, rfc(rtAt), rfc(rtAt)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,'active',?,?,1)`, sessionID, projectID, "Office", rfc(rtAt), rfc(rtAt)); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateOfficeTask(orgCtx, officestudio.Task{SessionID: sessionID, Title: "organization file"}, "org-task")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		ctx context.Context
		id  string
	}{{ctx, personal.ID}, {orgCtx, task.ID}} {
		rows, err := s.ListOfficeTasks(test.ctx, "", 10)
		if err != nil || len(rows) != 1 || rows[0].ID != test.id {
			t.Fatalf("isolated partition: %+v %v", rows, err)
		}
	}
	if _, err = s.GetOfficeTask(ctx, task.ID); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("personal read org: %v", err)
	}
	if _, err = s.db.Exec(`DELETE FROM sessions WHERE id=?`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetOfficeTask(orgCtx, task.ID); err != nil {
		t.Fatalf("org retained owner: %v", err)
	}
	// A legacy personal-to-org reassignment must not expose old personal tasks
	// in either partition without an explicit ownership migration.
	if _, err = s.db.Exec(`UPDATE projects SET org_id=? WHERE id=?`, created.OrgID, rtProjectULID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []context.Context{ctx, orgCtx} {
		if _, err = s.GetOfficeTask(c, personal.ID); !errors.Is(err, org.ErrCrossOrgAccess) {
			t.Fatalf("scope change bypass: %v", err)
		}
	}
	rows, err := s.ListOfficeTasks(ctx, "", 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("scope change leaked list: %+v %v", rows, err)
	}
}

func TestOfficeUpgradePreservesSharedVersionsAndForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old-release.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err = ensureMigrationJournal(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, m := range manifest {
		if m.name == "0143_office_studio.sql" {
			break
		}
		body, e := migrations.Files.ReadFile(m.name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(body)); e != nil {
			t.Fatalf("old migration %s: %v", m.name, e)
		}
		if m.name == "0002_provider_production.sql" {
			if e = (&Store{}).migrateV1Data(ctx, db); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = db.Exec(`INSERT INTO schema_migrations(version,applied_at,checksum) VALUES(?,?,?)`, m.name, rfc(rtAt), m.checksum); e != nil {
			t.Fatal(e)
		}
	}
	ids := []string{}
	for _, scope := range []string{"project", "stage_run", "dev_task", "release", "m6_root"} {
		id := ulid.Make().String()
		ids = append(ids, id)
		if _, err = db.Exec(`INSERT INTO artifact_versions(id,artifact_id,version_no,kind,scope_type,scope_id,content_ref,sha256,size,media_type,state,created_by,created_at) VALUES(?,?,1,'document',?,'old-scope','original.bin',?,7,'application/octet-stream','active','old-release',?)`, id, "old-"+scope, scope, strings.Repeat("a", 64), rfc(rtAt)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO artifact_derivations(id,artifact_version_id,derived_from_version,relation,created_at) VALUES(?,?,?,'derived_from',?)`, ulid.Make().String(), ids[1], ids[0], rfc(rtAt)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s.Close()
	var n int
	if err = s.db.QueryRow(`SELECT count(*) FROM artifact_versions WHERE content_ref='original.bin' AND size=7`).Scan(&n); err != nil || n != 5 {
		t.Fatalf("old artifacts: %d %v", n, err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("foreign keys: %d %v", n, err)
	}
	var parent string
	if err = s.db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('artifact_derivations')`).Scan(&parent); err != nil || parent != "artifact_versions" {
		t.Fatalf("renamed foreign key: %s %v", parent, err)
	}
	if _, err = s.db.Exec(`DELETE FROM artifact_versions WHERE id=?`, ids[0]); err == nil {
		t.Fatal("upgrade lost immutable trigger")
	}
}
