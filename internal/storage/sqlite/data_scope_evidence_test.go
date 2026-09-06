package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/org"
	"github.com/oklog/ulid/v2"
)

func TestDataScopeEvidenceRejectsLegacyCyclesAndMixedParents(t *testing.T) {
	ctx := context.Background()
	wf, store, personalID := newM7WorkflowService(t)
	v, err := wf.CreateVersion(ctx, personalID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wf.Publish(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	sr, err := wf.StartStage(ctx, personalID, "INITIATION_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range [][2]string{{"trace:workflow_version", v.ID}, {"trace:workflow_instance", sr.Instance.ID}, {"trace:stage_run", sr.Run.ID}} {
		if err = store.AuthorizeDataResource(ctx, ref[0], ref[1], ""); err != nil {
			t.Fatalf("own %v: %v", ref, err)
		}
	}
	a, err := org.NewService(org.NewGate(store.OrgStorage()), nil).CreateOrg(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	p := ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = store.db.ExecContext(ctx, `INSERT INTO projects(id,name,project_code,org_id,created_at,updated_at) VALUES(?,?,?,?,?,?)`, p, "A", "ITM00002", a.OrgID, now, now); err != nil {
		t.Fatal(err)
	}
	// Historical association can declare an own instance while its actual
	// workflow definition belongs to personal space. Both parents must agree.
	if _, err = store.db.ExecContext(ctx, `UPDATE workflow_instances SET project_id=? WHERE id=?`, p, sr.Instance.ID); err != nil {
		t.Fatal(err)
	}
	for _, ref := range [][2]string{{"trace:workflow_instance", sr.Instance.ID}, {"trace:stage_run", sr.Run.ID}} {
		if err = store.AuthorizeDataResource(ctx, ref[0], ref[1], a.OrgID); !errors.Is(err, org.ErrCrossOrgAccess) {
			t.Fatalf("mixed parent %v: %v", ref, err)
		}
	}
	left, right := ulid.Make().String(), ulid.Make().String()
	for _, pair := range [][2]string{{left, right}, {right, left}} {
		if _, err = store.db.ExecContext(ctx, `INSERT INTO reviews(id,subject_type,subject_id,subject_version,verdict,reviewer_id,reason,created_at) VALUES(?,'review',?,1,'approve','reviewer','legacy cycle',?)`, pair[0], pair[1], now); err != nil {
			t.Fatal(err)
		}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = store.AuthorizeDataResource(checkCtx, "trace:review", left, ""); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("cyclic ownership accepted: %v", err)
	}
	good := ulid.Make().String()
	if _, err = store.db.ExecContext(ctx, `INSERT INTO reviews(id,subject_type,subject_id,subject_version,verdict,reviewer_id,reason,created_at) VALUES(?,'project',?,1,'approve','reviewer','own',?)`, good, personalID, now); err != nil {
		t.Fatal(err)
	}
	if err = store.AuthorizeDataResource(ctx, "trace:review", good, ""); err != nil {
		t.Fatal(err)
	}
	artifact := ulid.Make().String()
	if _, err = store.db.ExecContext(ctx, `INSERT INTO artifact_versions(id,artifact_id,version_no,kind,scope_type,scope_id,content_ref,sha256,size,media_type,state,created_by,created_at) VALUES(?,'proof',1,'document','project',?,'proof.txt',?,1,'text/plain','active','test',?)`, artifact, personalID, strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	if err = store.AuthorizeDataResource(ctx, "trace:artifact_version", artifact, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.AuthorizeDataResource(ctx, "trace:artifact_version", artifact, a.OrgID); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("artifact parent bypassed: %v", err)
	}
}
