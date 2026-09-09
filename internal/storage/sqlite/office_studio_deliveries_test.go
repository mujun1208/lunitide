package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

func TestOfficeDeliveryEvidenceAndManifestAreImmutable(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	v := officeVersion(task.ID, ulid.Make().String())
	v.Index = json.RawMessage(`{"nodes":[{"id":"cell:A1","kind":"cell:number","text":"42","digest":"` + strings.Repeat("b", 64) + `"}]}`)
	v, err := s.PublishOfficeVersion(ctx, officestudio.PublishRequest{Version: v, IdempotencyKey: "source"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateOfficeMetric(ctx, officestudio.Metric{TaskID: task.ID, Name: "指标", SourceVersionID: v.ID, SourceSHA256: v.SHA256, SourceNodeID: "cell:A1", SourceNodeDigest: strings.Repeat("b", 64), RawValue: "42", ValueType: "number", DisplayValue: "42", Aggregation: "identity", RoundingPolicy: "none"}, "metric")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateOfficeBundle(ctx, officestudio.Bundle{TaskID: task.ID, Title: "交付", Files: []officestudio.BundleFile{{VersionID: v.ID, ArtifactID: v.ArtifactID, Name: "报告.docx", Kind: v.Kind, SHA256: v.SHA256, Size: v.Size}}}, "bundle")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE office_metrics SET metric_json='{}' WHERE id=?`, `DELETE FROM office_metrics WHERE id=?`} {
		if _, err = s.db.Exec(q, m.ID); err == nil {
			t.Fatal("metric evidence mutated")
		}
	}
	for _, q := range []string{`UPDATE office_bundles SET manifest_json='{}' WHERE id=?`, `DELETE FROM office_bundles WHERE id=?`} {
		if _, err = s.db.Exec(q, b.ID); err == nil {
			t.Fatal("fixed manifest mutated")
		}
	}
	if _, err = s.GetOfficeMetric(officestudio.WithScope(ctx, ulid.Make().String()), m.ID); err == nil {
		t.Fatal("foreign metric read")
	}
	if _, err = s.GetOfficeBundle(officestudio.WithScope(ctx, ulid.Make().String()), b.ID); err == nil {
		t.Fatal("foreign bundle read")
	}
}

func TestOfficePublicationAndStalePropagationRollbackTogether(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	source := officePublish(t, s, task, ulid.Make().String(), "source")
	target := officePublish(t, s, task, ulid.Make().String(), "target")
	if err := s.AddOfficeEvidenceEdge(ctx, officestudio.EvidenceEdge{TaskID: task.ID, SourceVersionID: source.ID, TargetVersionID: target.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TEMP TRIGGER office_test_stale_failure BEFORE UPDATE OF quality ON office_version_metadata WHEN NEW.quality='stale' BEGIN SELECT RAISE(ABORT,'injected stale projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	next := officeVersion(task.ID, source.ArtifactID)
	next.BaseVersionID = source.ID
	r := officestudio.PublishRequest{Version: next, ExpectedHeadRevision: 1, IdempotencyKey: "next"}
	if _, err := s.PublishOfficeVersion(ctx, r); err == nil {
		t.Fatal("injected failure ignored")
	}
	versions, err := s.ListOfficeVersions(ctx, task.ID, source.ArtifactID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("partial publication survived: %#v %v", versions, err)
	}
	old, err := s.GetOfficeVersion(ctx, target.ID)
	if err != nil || old.Quality == "stale" {
		t.Fatalf("partial stale survived: %#v %v", old, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER office_test_stale_failure`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishOfficeVersion(ctx, r); err != nil {
		t.Fatal(err)
	}
	old, err = s.GetOfficeVersion(ctx, target.ID)
	if err != nil || old.Quality != "stale" {
		t.Fatalf("retry failed stale propagation: %#v %v", old, err)
	}
}
